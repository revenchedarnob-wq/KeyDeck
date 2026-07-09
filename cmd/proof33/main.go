package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/candidatecollection"
	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/handoff"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/routing"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}
type report struct {
	Proof               string     `json:"proof"`
	Status              string     `json:"status"`
	Passed              bool       `json:"passed"`
	Scenarios           []scenario `json:"scenarios"`
	AssessmentID        string     `json:"assessment_id"`
	ResolutionID        string     `json:"resolution_id"`
	SelectedCandidateID string     `json:"selected_candidate_id"`
	ReceiptID           string     `json:"receipt_id"`
	NextGate            string     `json:"next_gate"`
}

// ---------- Handoff/runtime integration ----------

type runtimeAdapter struct {
	id     string
	starts int
}

func (a *runtimeAdapter) ID() string { return a.id }
func (a *runtimeAdapter) Capabilities(context.Context) ([]engineruntime.Capability, error) {
	return []engineruntime.Capability{engineruntime.CapabilityText}, nil
}
func (a *runtimeAdapter) Health(context.Context) (engineruntime.Health, error) {
	return engineruntime.Health{State: engineruntime.HealthHealthy}, nil
}
func (a *runtimeAdapter) Start(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	a.starts++
	return engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "durable-route-result"}}, nil
}
func (a *runtimeAdapter) Continue(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	return engineruntime.Outcome{}, errors.New("unused")
}
func (a *runtimeAdapter) Resume(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	return engineruntime.Outcome{}, errors.New("unused")
}
func (a *runtimeAdapter) Cancel(context.Context, engineruntime.Binding) error { return nil }

type handoffFixture struct {
	root, handoffPath, runtimePath, enginePath, timelinePath string
	pkg                                                      handoff.Package
	plan                                                     routing.Plan
	routeReq                                                 routing.Requirements
	routeCandidates                                          []routing.Candidate
	current                                                  handoff.CurrentState
	routeExecutor                                            *routing.Executor
}

func newHandoffFixture(root string) (*handoffFixture, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	state := tasks.State{TaskID: "proof33-handoff-task", SessionID: "proof33-handoff-session", Status: tasks.StatusWorking, Contract: tasks.Contract{Goal: "durable route-bound handoff", Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "done"}}}, LastSequence: 1}
	brain := projectbrain.Revision{ProjectID: "proof33-handoff-project", SessionID: state.SessionID, ProjectFingerprint: "proof33-fp", RevisionSHA256: strings.Repeat("b", 64), Context: projectbrain.ContextInspection{PacketID: "proof33-packet", PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "proof33-fp", InspectionSHA256: strings.Repeat("c", 64)}}
	passport := session.Passport{SessionID: state.SessionID, ProjectRoot: root, Goal: state.Contract.Goal, FromEngine: "api", ToEngine: "engine-a", HandoffReason: "proof33"}
	pkg, err := handoff.Assemble(handoff.Input{Task: state, ContextPacketID: brain.Context.PacketID, ContextPacketSHA256: brain.Context.PacketSHA256, MCPServerID: "mcp-proof33", MCPSchemaSHA256: strings.Repeat("d", 64), ProjectSourceFingerprint: brain.ProjectFingerprint, Brain: brain, Passport: passport, EngineID: "engine-a", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}})
	if err != nil {
		return nil, err
	}
	rr := routing.Requirements{TaskID: state.TaskID, SessionID: state.SessionID, HandoffPackageID: pkg.PackageID, HandoffPackageSHA256: pkg.PackageSHA256, RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
	cs := []routing.Candidate{{EngineID: "engine-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 100, EvidenceRefs: []string{"verified-history"}}}
	plan, err := routing.Select(rr, cs)
	if err != nil {
		return nil, err
	}
	f := &handoffFixture{root: root, handoffPath: filepath.Join(root, "handoff.jsonl"), runtimePath: filepath.Join(root, "runtime.jsonl"), enginePath: filepath.Join(root, "engine.jsonl"), timelinePath: filepath.Join(root, "timeline.jsonl"), pkg: pkg, plan: plan, routeReq: rr, routeCandidates: cs, current: handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: pkg.ContextPacketID, ProjectBrainRevisionSHA256: pkg.ProjectBrainRevisionSHA256}}
	if err = f.open(); err != nil {
		return nil, err
	}
	return f, nil
}
func (f *handoffFixture) open() error {
	hs, err := handoff.OpenStore(f.handoffPath)
	if err != nil {
		return err
	}
	es, err := recovery.OpenEngineStore(f.enginePath)
	if err != nil {
		return err
	}
	rs, err := engineruntime.Open(f.runtimePath)
	if err != nil {
		return err
	}
	tl, err := timeline.Open(f.timelinePath)
	if err != nil {
		return err
	}
	rt, err := engineruntime.New(rs, es, tl)
	if err != nil {
		return err
	}
	hx := &handoff.Executor{Store: hs, Runtime: rt, Current: func() handoff.CurrentState { return f.current }}
	f.routeExecutor = &routing.Executor{Handoff: hx, Current: func() (routing.Requirements, []routing.Candidate) { return f.routeReq, f.routeCandidates }}
	return nil
}

// ---------- Visible-response continuation integration ----------

type partialEngine struct {
	name  string
	calls int
}

func (e *partialEngine) Name() string { return e.name }
func (e *partialEngine) Run(_ context.Context, _ session.Passport, prompt string) (session.EngineResult, error) {
	e.calls++
	return session.EngineResult{Text: "confirmed-part"}, &session.PartialResultError{Cause: errors.New("key exhausted"), Partial: session.EngineResult{Text: "confirmed-part"}, Continuation: session.ContinuationState{OriginalPrompt: prompt, ConfirmedOutput: "confirmed-part", SourceEngine: e.name, Reason: "key exhausted"}}
}

type finishEngine struct {
	name  string
	calls int
	text  string
}

func (e *finishEngine) Name() string { return e.name }
func (e *finishEngine) Run(_ context.Context, _ session.Passport, _ string) (session.EngineResult, error) {
	e.calls++
	return session.EngineResult{Text: e.text}, nil
}

// ---------- Candidate/reconciliation integration ----------

type routeState struct {
	req        routing.Requirements
	candidates []routing.Candidate
}
type candidateFixture struct {
	root, taskPath, journalPath, sessionPath, timelinePath, canonicalEnginePath string
	taskStore                                                                   *tasks.Store
	handoffStore                                                                *handoff.Store
	routeStore                                                                  *routing.Store
	runtimeStore                                                                *engineruntime.Store
	candidateEngineStore                                                        *recovery.EngineStore
	canonicalEngineStore                                                        *recovery.EngineStore
	collectionStore                                                             *candidatecollection.Store
	coordinator                                                                 *candidatecollection.Coordinator
	recoveryCoordinator                                                         *recovery.Coordinator
	pkg                                                                         handoff.Package
	current                                                                     handoff.CurrentState
	routes                                                                      map[string]routeState
}

func newCandidateFixture(base, name string) (*candidateFixture, error) {
	root := filepath.Join(base, name)
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	f := &candidateFixture{root: root, taskPath: filepath.Join(root, "task.jsonl"), journalPath: filepath.Join(root, "journal.jsonl"), sessionPath: filepath.Join(root, "session.json"), timelinePath: filepath.Join(root, "timeline.jsonl"), canonicalEnginePath: filepath.Join(root, "canonical-engine.jsonl"), routes: map[string]routeState{}}
	ts, err := tasks.Open(f.taskPath)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(f.journalPath)
	if err != nil {
		return nil, err
	}
	tm := &tasks.Manager{Store: ts, Journal: journal}
	_, err = tm.Create("proof33-task-"+name, "proof33-session-"+name, tasks.Contract{Goal: "integrate route-bound reconciliation", RequiredOutcomes: []string{"no majority truth", "exactly one canonical result"}, Checks: []tasks.AcceptanceCheck{{ID: "reconciled", Description: "candidate reconciliation is evidenced"}}})
	if err != nil {
		return nil, err
	}
	state := ts.State()
	if err = session.Save(f.sessionPath, session.New(state.SessionID, root, state.Contract.Goal, "keydeck")); err != nil {
		return nil, err
	}
	brain := projectbrain.Revision{ProjectID: "project-" + name, SessionID: state.SessionID, ProjectFingerprint: "fp-" + name, RevisionSHA256: strings.Repeat("b", 64), Context: projectbrain.ContextInspection{PacketID: "packet-" + name, PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "fp-" + name, InspectionSHA256: strings.Repeat("c", 64)}}
	passport := session.Passport{SessionID: state.SessionID, ProjectRoot: root, Goal: state.Contract.Goal, FromEngine: "api", ToEngine: "engine-a", HandoffReason: "proof33-candidates"}
	pkg, err := handoff.Assemble(handoff.Input{Task: state, ContextPacketID: brain.Context.PacketID, ContextPacketSHA256: brain.Context.PacketSHA256, MCPServerID: "mcp-proof33", MCPSchemaSHA256: strings.Repeat("d", 64), ProjectSourceFingerprint: brain.ProjectFingerprint, Brain: brain, Passport: passport, EngineID: "engine-a", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}})
	if err != nil {
		return nil, err
	}
	hs, err := handoff.OpenStore(filepath.Join(root, "handoff.jsonl"))
	if err != nil {
		return nil, err
	}
	if _, _, err = hs.SaveOnce(pkg); err != nil {
		return nil, err
	}
	routes, err := routing.OpenStore(filepath.Join(root, "routes.jsonl"))
	if err != nil {
		return nil, err
	}
	runtimeStore, err := engineruntime.Open(filepath.Join(root, "runtime.jsonl"))
	if err != nil {
		return nil, err
	}
	candidateEngines, err := recovery.OpenEngineStore(filepath.Join(root, "candidate-engine.jsonl"))
	if err != nil {
		return nil, err
	}
	collection, err := candidatecollection.OpenStore(filepath.Join(root, "candidates.jsonl"))
	if err != nil {
		return nil, err
	}
	paths := recovery.Paths{TaskEvents: f.taskPath, SessionState: f.sessionPath, ToolJournal: f.journalPath, Timeline: f.timelinePath, ProofReceipts: filepath.Join(root, "receipts.jsonl"), EngineLedger: f.canonicalEnginePath, ArtifactLedger: filepath.Join(root, "artifacts.jsonl")}
	rc, err := recovery.Open(paths)
	if err != nil {
		return nil, err
	}
	f.taskStore, f.handoffStore, f.routeStore, f.runtimeStore, f.candidateEngineStore, f.collectionStore, f.recoveryCoordinator, f.canonicalEngineStore, f.pkg = ts, hs, routes, runtimeStore, candidateEngines, collection, rc, rc.EngineStore(), pkg
	f.current = handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: pkg.ContextPacketID, ProjectBrainRevisionSHA256: pkg.ProjectBrainRevisionSHA256}
	if err = f.rebuildCoordinator(); err != nil {
		return nil, err
	}
	return f, nil
}
func (f *candidateFixture) rebuildCoordinator() error {
	validator := &candidatecollection.KeyDeckValidator{Tasks: f.taskStore, Handoffs: f.handoffStore, HandoffCurrent: func() handoff.CurrentState { return f.current }, Routes: f.routeStore, RouteCurrent: func(_ context.Context, p routing.Plan) (routing.Requirements, []routing.Candidate, error) {
		x, ok := f.routes[p.RouteID]
		if !ok {
			return routing.Requirements{}, nil, errors.New("route state missing")
		}
		return x.req, x.candidates, nil
	}, Runtime: f.runtimeStore, CandidateEngineLedger: f.candidateEngineStore}
	port := &candidatecollection.KeyDeckRecoveryPort{Coordinator: f.recoveryCoordinator, EngineLedger: f.canonicalEngineStore}
	coord, err := candidatecollection.NewCoordinator(f.collectionStore, validator, candidatecollection.EvidenceReconciler{}, port)
	if err != nil {
		return err
	}
	f.coordinator = coord
	return nil
}
func (f *candidateFixture) collect(engine, provider, text string, verified bool) (candidatecollection.CollectOutcome, candidatecollection.CollectRequest, routing.Plan, error) {
	state := f.taskStore.State()
	rr := routing.Requirements{TaskID: state.TaskID, SessionID: state.SessionID, HandoffPackageID: f.pkg.PackageID, HandoffPackageSHA256: f.pkg.PackageSHA256, RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
	cs := []routing.Candidate{{EngineID: engine, ProviderID: provider, Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 100, EvidenceRefs: []string{"route-proof-" + engine}}}
	plan, err := routing.Select(rr, cs)
	if err != nil {
		return candidatecollection.CollectOutcome{}, candidatecollection.CollectRequest{}, plan, err
	}
	if _, _, err = f.routeStore.SaveOnce(plan); err != nil {
		return candidatecollection.CollectOutcome{}, candidatecollection.CollectRequest{}, plan, err
	}
	f.routes[plan.RouteID] = routeState{rr, cs}
	n := len(f.routes)
	execID := fmt.Sprintf("exec-%s-%d", engine, n)
	resultID := fmt.Sprintf("result-%s-%d", engine, n)
	if _, _, err = f.runtimeStore.BeginOnce(engineruntime.Execution{ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, EngineID: engine, Operation: engineruntime.OperationStart, Disposition: engineruntime.DispositionRunning}); err != nil {
		return candidatecollection.CollectOutcome{}, candidatecollection.CollectRequest{}, plan, err
	}
	if _, _, err = f.runtimeStore.SetDispositionOnce(execID, engineruntime.DispositionCompleted, "", resultID, "candidate complete"); err != nil {
		return candidatecollection.CollectOutcome{}, candidatecollection.CollectRequest{}, plan, err
	}
	if _, _, err = f.candidateEngineStore.StartOnce(recovery.Execution{ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, Engine: engine, StartedAt: time.Unix(0, 0).UTC()}); err != nil {
		return candidatecollection.CollectOutcome{}, candidatecollection.CollectRequest{}, plan, err
	}
	res := recovery.Result{ResultID: resultID, ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, Engine: engine, Output: session.EngineResult{Text: text}, CompletedAt: time.Unix(0, 0).UTC()}
	if _, _, err = f.candidateEngineStore.CompleteResultOnce(res); err != nil {
		return candidatecollection.CollectOutcome{}, candidatecollection.CollectRequest{}, plan, err
	}
	req, err := candidatecollection.BuildCollectRequest(state, f.pkg, plan, f.runtimeStore.Execution(execID), f.candidateEngineStore.Result(resultID))
	if err != nil {
		return candidatecollection.CollectOutcome{}, req, plan, err
	}
	if verified {
		req.RuntimeEvidenceIDs = append(req.RuntimeEvidenceIDs, "verification:proof33-"+engine)
	}
	out, err := f.coordinator.Collect(context.Background(), req)
	return out, req, plan, err
}
func (f *candidateFixture) restartCollection() error {
	store, err := candidatecollection.OpenStore(filepath.Join(f.root, "candidates.jsonl"))
	if err != nil {
		return err
	}
	f.collectionStore = store
	return f.rebuildCoordinator()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	base := filepath.Join(os.TempDir(), "keydeck-proof33-reconstructed")
	_ = os.RemoveAll(base)
	defer os.RemoveAll(base)
	out := report{Proof: "0.33-integrated-route-bound-reconciliation-and-canonical-recovery-reconstructed", Status: "failed", NextGate: "Proof 0.34 — Production Candidate Collection Coordinator integration"}
	add := func(n string, p bool, d any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: n, Passed: p, Detail: d})
	}

	// 1-2: exact handoff safety + durable route-bound execution.
	hf, err := newHandoffFixture(filepath.Join(base, "handoff"))
	if err != nil {
		return err
	}
	tampered := hf.pkg
	tampered.ContextPacketID = "tampered"
	adTampered := &runtimeAdapter{id: "engine-a"}
	_, tamperErr := hf.routeExecutor.Execute(context.Background(), adTampered, tampered, hf.plan)
	add("restored_handoff_package_safety_rejects_tamper_before_engine_execution", tamperErr != nil && adTampered.starts == 0, fmt.Sprint(tamperErr))
	ad1 := &runtimeAdapter{id: "engine-a"}
	firstRun, err := hf.routeExecutor.Execute(context.Background(), ad1, hf.pkg, hf.plan)
	if err != nil {
		return err
	}
	if err = hf.open(); err != nil {
		return err
	}
	ad2 := &runtimeAdapter{id: "engine-a"}
	secondRun, err := hf.routeExecutor.Execute(context.Background(), ad2, hf.pkg, hf.plan)
	add("durable_route_bound_handoff_execution_reuses_completed_result_after_restart", err == nil && firstRun.Execution.Disposition == engineruntime.DispositionCompleted && secondRun.Execution.ResultID == firstRun.Execution.ResultID && ad1.starts == 1 && ad2.starts == 0, map[string]any{"result": secondRun.Execution.ResultID, "restart_starts": ad2.starts})

	// 3: deterministic routing.
	rr := routing.Requirements{TaskID: "proof33-route-task", SessionID: "proof33-route-session", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
	routeCandidates := []routing.Candidate{{EngineID: "engine-b", ProviderID: "provider-b", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 80}, {EngineID: "engine-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 90}}
	rp1, _ := routing.Select(rr, routeCandidates)
	rp2, _ := routing.Select(rr, []routing.Candidate{routeCandidates[1], routeCandidates[0]})
	add("deterministic_routing_selects_only_qualified_evidence_ranked_engine", rp1.RouteSHA256 == rp2.RouteSHA256 && rp1.SelectedEngineID == "engine-a", rp1.RouteID)

	// 4-5: route-bound visible continuation and provider-busy denial.
	sourcePlan, _ := routing.Select(rr, []routing.Candidate{{EngineID: "api-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	targetPlan, _ := routing.Select(rr, []routing.Candidate{{EngineID: "agent-b", ProviderID: "provider-b", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	sourceEngine := &partialEngine{name: "api-a"}
	targetEngine := &finishEngine{name: "agent-b", text: "continued-part"}
	orch := session.Orchestrator{State: session.New(rr.SessionID, base, "continue response", "api-a")}
	_, _, beginErr := orch.BeginInterruptible(context.Background(), sourceEngine, "original task", "start")
	contPlan, contErr := routing.PlanContinuation(orch.State.InFlightResponse.ResponseID, routing.FailureKeyExhausted, sourcePlan, targetPlan)
	_, continued, continueErr := orch.ContinueInFlight(context.Background(), targetEngine, "route:"+contPlan.ContinuationID)
	add("safe_route_bound_mid_answer_continuation_preserves_one_visible_response", beginErr != nil && contErr == nil && continueErr == nil && continued.Text == "confirmed-part\n\ncontinued-part" && len(orch.State.Transcript) == 2 && targetEngine.calls == 1, contPlan.ContinuationID)
	busyOrch := session.Orchestrator{State: session.New("proof33-busy-session", base, "busy response", "api-a")}
	busySource := &partialEngine{name: "api-a"}
	busyTarget := &finishEngine{name: "api-c", text: "must-not-run"}
	_, _, _ = busyOrch.BeginInterruptible(context.Background(), busySource, "busy task", "start")
	busyReq := routing.Requirements{TaskID: "proof33-busy-task", SessionID: "proof33-busy-session", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
	busyFrom, _ := routing.Select(busyReq, []routing.Candidate{{EngineID: "api-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}}})
	busyTo, _ := routing.Select(busyReq, []routing.Candidate{{EngineID: "api-c", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}}})
	_, busyErr := routing.PlanContinuation(busyOrch.State.InFlightResponse.ResponseID, routing.FailureProviderBusy, busyFrom, busyTo)
	add("provider_wide_busy_continuation_is_denied_before_target_engine_execution", errors.Is(busyErr, routing.ErrProviderBusySameProvider) && busyTarget.calls == 0 && busyOrch.State.InFlightResponse != nil, fmt.Sprint(busyErr))

	// 6: all saved reconciliation states.
	fs, _ := newCandidateFixture(base, "states-single")
	single, _, _, _ := fs.collect("engine-a", "provider-a", "single", true)
	singleA, _ := fs.coordinator.Assess(context.Background(), single.Candidate.TaskID, single.Candidate.HandoffPackageID)
	fa, _ := newCandidateFixture(base, "states-agreement")
	agree1, _, _, _ := fa.collect("engine-a", "provider-a", "same", true)
	_, _, _, _ = fa.collect("engine-b", "provider-b", "same", true)
	agreeA, _ := fa.coordinator.Assess(context.Background(), agree1.Candidate.TaskID, agree1.Candidate.HandoffPackageID)
	fd, _ := newCandidateFixture(base, "states-disagree")
	dis1, _, _, _ := fd.collect("engine-a", "provider-a", "a", true)
	_, _, _, _ = fd.collect("engine-b", "provider-b", "b", true)
	disA, _ := fd.coordinator.Assess(context.Background(), dis1.Candidate.TaskID, dis1.Candidate.HandoffPackageID)
	fn, _ := newCandidateFixture(base, "states-review")
	need, _, _, _ := fn.collect("engine-a", "provider-a", "needs-review", false)
	needA, _ := fn.coordinator.Assess(context.Background(), need.Candidate.TaskID, need.Candidate.HandoffPackageID)
	_, _ = fd.coordinator.ResolveReview(candidatecollection.ReviewerResolutionRequest{AssessmentID: disA.AssessmentID, AssessmentSHA: disA.AssessmentSHA, SelectedCandidateID: dis1.Candidate.CandidateID, ReviewerRef: "proof33-reviewer", DecisiveEvidenceIDs: []string{"test:decisive"}, Rationale: "reproduced candidate"})
	resolvedState, resolvedCandidate, _ := fd.coordinator.EffectiveState(dis1.Candidate.TaskID, dis1.Candidate.HandoffPackageID)
	add("reconciliation_supports_single_verified_agreement_disagreement_needs_review_and_resolved_states", singleA.State == candidatecollection.AssessmentSingleVerified && agreeA.State == candidatecollection.AssessmentAgreement && disA.State == candidatecollection.AssessmentDisagreement && needA.State == candidatecollection.AssessmentNeedsReview && resolvedState == candidatecollection.AssessmentResolved && resolvedCandidate == dis1.Candidate.CandidateID, map[string]any{"single": singleA.State, "agreement": agreeA.State, "disagreement": disA.State, "needs_review": needA.State, "resolved": resolvedState})

	// 7: no majority truth.
	fm, _ := newCandidateFixture(base, "majority")
	m1, _, _, _ := fm.collect("engine-a", "provider-a", "same", true)
	_, _, _, _ = fm.collect("engine-b", "provider-b", "same", true)
	_, _, _, _ = fm.collect("engine-c", "provider-c", "conflict", true)
	majorityA, _ := fm.coordinator.Assess(context.Background(), m1.Candidate.TaskID, m1.Candidate.HandoffPackageID)
	add("two_matching_engines_cannot_majority_override_one_conflicting_candidate", majorityA.State == candidatecollection.AssessmentDisagreement && majorityA.SelectedCandidateID == "", majorityA.AssessmentID)

	// 8: stale task/package blocked before collection.
	fstale, _ := newCandidateFixture(base, "stale")
	_, staleReq, _, _ := fstale.collect("engine-a", "provider-a", "valid", true)
	staleReq.TaskSequence++
	_, staleTaskErr := fstale.coordinator.Collect(context.Background(), staleReq)
	staleReq.TaskSequence--
	staleReq.HandoffPackageSHA = strings.Repeat("f", 64)
	_, stalePkgErr := fstale.coordinator.Collect(context.Background(), staleReq)
	add("stale_task_or_handoff_package_candidate_is_rejected_before_persistence", errors.Is(staleTaskErr, candidatecollection.ErrStaleCurrentState) && errors.Is(stalePkgErr, candidatecollection.ErrStaleCurrentState), map[string]any{"task": fmt.Sprint(staleTaskErr), "package": fmt.Sprint(stalePkgErr)})

	// 9: duplicate reuse after restart.
	fdup, _ := newCandidateFixture(base, "duplicate")
	dup, dupReq, _, _ := fdup.collect("engine-a", "provider-a", "dup", true)
	if err = fdup.restartCollection(); err != nil {
		return err
	}
	reused, reuseErr := fdup.coordinator.Collect(context.Background(), dupReq)
	add("duplicate_candidate_is_reused_after_restart_without_rerunning_producer", reuseErr == nil && reused.Reused && reused.Candidate.CandidateID == dup.Candidate.CandidateID && len(fdup.collectionStore.CandidatesForScope(dupReq.TaskID, dupReq.HandoffPackageID)) == 1, reused.Candidate.CandidateID)

	// 10: secret-like evidence blocked.
	fsecret, _ := newCandidateFixture(base, "secret")
	_, secretReq, _, _ := fsecret.collect("engine-a", "provider-a", "safe", true)
	var payload session.EngineResult
	_ = json.Unmarshal([]byte(secretReq.ResultPayload), &payload)
	payload.Text = "api_key=SUPERSECRET123456789"
	rawSecret, _ := json.Marshal(payload)
	secretReq.ResultPayload = string(rawSecret)
	sum := sha256Hex(secretReq.ResultPayload)
	secretReq.ResultPayloadSHA = sum
	_, secretErr := fsecret.coordinator.Collect(context.Background(), secretReq)
	add("secret_like_candidate_evidence_is_blocked_before_persistence", errors.Is(secretErr, candidatecollection.ErrStaleCurrentState), fmt.Sprint(secretErr))

	// 11-12: exact decisive resolution, selected-only recovery, exact-once canonical result, receipt provenance.
	fmain, _ := newCandidateFixture(base, "main")
	ca, _, planA, _ := fmain.collect("engine-a", "provider-a", "selected-answer", true)
	cb, _, _, _ := fmain.collect("engine-b", "provider-b", "rejected-answer", true)
	assessment, _ := fmain.coordinator.Assess(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	before, _ := session.Load(fmain.sessionPath)
	_, unresolvedErr := fmain.coordinator.CommitResolved(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	afterUnresolved, _ := session.Load(fmain.sessionPath)
	resolution, _ := fmain.coordinator.ResolveReview(candidatecollection.ReviewerResolutionRequest{AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA, SelectedCandidateID: ca.Candidate.CandidateID, ReviewerRef: "proof33-reviewer", DecisiveEvidenceIDs: []string{"test:reproduced-selected-answer", "artifact:verified-output"}, Rationale: "independent evidence selects candidate A"})
	if err = fmain.restartCollection(); err != nil {
		return err
	}
	effectiveState, _, _ := fmain.coordinator.EffectiveState(ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	commit, err := fmain.coordinator.CommitResolved(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	if err != nil {
		return err
	}
	again, err := fmain.coordinator.CommitResolved(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	if err != nil {
		return err
	}
	canonical, _ := session.Load(fmain.sessionPath)
	selected := fmain.canonicalEngineStore.Result(ca.Candidate.ResultID)
	unselected := fmain.canonicalEngineStore.Result(cb.Candidate.ResultID)
	add("unresolved_candidates_cannot_mutate_canonical_state_and_resolved_result_commits_exactly_once", errors.Is(unresolvedErr, candidatecollection.ErrResolutionRequired) && len(before.Transcript) == len(afterUnresolved.Transcript) && effectiveState == candidatecollection.AssessmentResolved && selected.CanonicalCommitted && unselected.ResultID == "" && again.CommitID == commit.CommitID && len(canonical.Transcript) == 1 && canonical.Transcript[0].Text == "selected-answer", map[string]any{"commit": commit.CommitID, "canonical_messages": len(canonical.Transcript)})

	evidence, err := fmain.coordinator.ReceiptEvidence(ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	if err != nil {
		return err
	}
	tl, err := timeline.Open(fmain.timelinePath)
	if err != nil {
		return err
	}
	_, _, _ = routing.RecordPlan(tl, planA)
	_, _, _ = tl.AppendOnce(timeline.Input{EventID: "proof33-assessment-" + assessment.AssessmentID, TaskID: ca.Candidate.TaskID, SessionID: ca.Candidate.SessionID, Domain: timeline.DomainProof, Kind: "reconciliation_disagreement", SourceRef: assessment.AssessmentID, Summary: "candidate disagreement requires evidence", DataHash: assessment.AssessmentSHA})
	_, _, _ = tl.AppendOnce(timeline.Input{EventID: "proof33-resolution-" + resolution.ResolutionID, TaskID: ca.Candidate.TaskID, SessionID: ca.Candidate.SessionID, Domain: timeline.DomainProof, Kind: "reconciliation_resolved", SourceRef: resolution.ResolutionID, Summary: "decisive evidence selected exact candidate", DataHash: resolution.ResolutionSHA})
	_, _, _ = tl.AppendOnce(timeline.Input{EventID: "proof33-commit-" + commit.CommitID, TaskID: ca.Candidate.TaskID, SessionID: ca.Candidate.SessionID, Domain: timeline.DomainEngine, Kind: "resolved_candidate_canonical_commit", SourceRef: commit.CanonicalRef, Summary: "selected candidate entered Recovery Coordinator exactly once", DataHash: commit.CommitSHA})
	handoffArtifact, _ := fmain.handoffStore.ReceiptArtifact()
	routeArtifact, _ := fmain.routeStore.ReceiptArtifact()
	candidateArtifact, _ := fmain.collectionStore.ReceiptArtifact()
	receipt, err := proofreceipt.Build(fmain.taskStore.State(), tl.ByTask(ca.Candidate.TaskID), []proofreceipt.Artifact{handoffArtifact, routeArtifact, candidateArtifact})
	if err != nil {
		return err
	}
	provenance := evidence.ReconciliationState == candidatecollection.AssessmentResolved && evidence.AssessmentID == assessment.AssessmentID && evidence.ResolutionID == resolution.ResolutionID && evidence.RecoveryCommitID == commit.CommitID && len(receipt.TimelineRefs) >= 3 && len(receipt.Artifacts) == 3
	add("proof_receipt_provenance_binds_disagreement_resolution_decisive_evidence_and_reconciliation_state", provenance, map[string]any{"receipt": receipt.ReceiptID, "state": evidence.ReconciliationState, "resolution": evidence.ResolutionID})

	out.AssessmentID = assessment.AssessmentID
	out.ResolutionID = resolution.ResolutionID
	out.SelectedCandidateID = ca.Candidate.CandidateID
	out.ReceiptID = receipt.ReceiptID
	out.Passed = len(out.Scenarios) == 12
	for _, s := range out.Scenarios {
		if !s.Passed {
			out.Passed = false
		}
	}
	if out.Passed {
		out.Status = "passed"
	}
	result, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(result))
	if !out.Passed {
		return errors.New("proof 0.33 reconstructed acceptance gate failed")
	}
	return nil
}

func sha256Hex(value string) string {
	// Local helper used only to create an intentionally tampered secret payload.
	h := fmt.Sprintf("%x", sha256Sum([]byte(value)))
	return h
}
func sha256Sum(b []byte) [32]byte {
	// indirection keeps the proof's tamper setup visually separate from product code.
	return sha256.Sum256(b)
}
