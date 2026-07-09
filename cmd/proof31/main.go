package main

import (
	"context"
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
	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/routing"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}
type report struct {
	Proof              string     `json:"proof"`
	Status             string     `json:"status"`
	Passed             bool       `json:"passed"`
	Scenarios          []scenario `json:"scenarios"`
	CandidateID        string     `json:"candidate_id"`
	AssessmentID       string     `json:"assessment_id"`
	CandidateSetSHA256 string     `json:"candidate_set_sha256"`
	NextGate           string     `json:"next_gate"`
}

type noRecovery struct{}

func (noRecovery) CommitResolvedCandidate(context.Context, candidatecollection.ResolvedRecoveryInput) (candidatecollection.RecoveryCommit, error) {
	return candidatecollection.RecoveryCommit{}, errors.New("unused")
}

type fixture struct {
	root                 string
	taskStore            *tasks.Store
	taskManager          *tasks.Manager
	handoffStore         *handoff.Store
	routeStore           *routing.Store
	runtimeStore         *engineruntime.Store
	candidateEngineStore *recovery.EngineStore
	collectionStore      *candidatecollection.Store
	coordinator          *candidatecollection.Coordinator
	pkg                  handoff.Package
	current              handoff.CurrentState
	routeState           map[string]struct {
		req        routing.Requirements
		candidates []routing.Candidate
	}
}

func newFixture(base, name string) (*fixture, error) {
	root := filepath.Join(base, name)
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	taskPath := filepath.Join(root, "task.jsonl")
	ts, err := tasks.Open(taskPath)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(filepath.Join(root, "journal.jsonl"))
	if err != nil {
		return nil, err
	}
	tm := &tasks.Manager{Store: ts, Journal: journal}
	_, err = tm.Create("proof31-task-"+name, "proof31-session-"+name, tasks.Contract{Goal: "collect and reconcile engine candidates safely", RequiredOutcomes: []string{"no stale candidate", "no majority truth"}, Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "candidate proof"}}})
	if err != nil {
		return nil, err
	}
	state := ts.State()
	brain := projectbrain.Revision{ProjectID: "project-" + name, SessionID: state.SessionID, ProjectFingerprint: "fp-" + name, RevisionSHA256: strings.Repeat("b", 64), Context: projectbrain.ContextInspection{PacketID: "packet-" + name, PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "fp-" + name, InspectionSHA256: strings.Repeat("c", 64)}}
	passport := session.Passport{SessionID: state.SessionID, ProjectRoot: root, Goal: state.Contract.Goal, FromEngine: "api", ToEngine: "engine-a", HandoffReason: "proof31"}
	pkg, err := handoff.Assemble(handoff.Input{Task: state, ContextPacketID: brain.Context.PacketID, ContextPacketSHA256: brain.Context.PacketSHA256, MCPServerID: "mcp-proof31", MCPSchemaSHA256: strings.Repeat("d", 64), ProjectSourceFingerprint: brain.ProjectFingerprint, Brain: brain, Passport: passport, EngineID: "engine-a", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}})
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
	cs, err := candidatecollection.OpenStore(filepath.Join(root, "candidates.jsonl"))
	if err != nil {
		return nil, err
	}
	f := &fixture{root: root, taskStore: ts, taskManager: tm, handoffStore: hs, routeStore: routes, runtimeStore: runtimeStore, candidateEngineStore: candidateEngines, collectionStore: cs, pkg: pkg, routeState: map[string]struct {
		req        routing.Requirements
		candidates []routing.Candidate
	}{}}
	f.current = handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: pkg.ContextPacketID, ProjectBrainRevisionSHA256: pkg.ProjectBrainRevisionSHA256}
	validator := &candidatecollection.KeyDeckValidator{Tasks: ts, Handoffs: hs, HandoffCurrent: func() handoff.CurrentState { return f.current }, Routes: routes, RouteCurrent: func(_ context.Context, p routing.Plan) (routing.Requirements, []routing.Candidate, error) {
		x, ok := f.routeState[p.RouteID]
		if !ok {
			return routing.Requirements{}, nil, errors.New("route state missing")
		}
		return x.req, x.candidates, nil
	}, Runtime: runtimeStore, CandidateEngineLedger: candidateEngines}
	coord, err := candidatecollection.NewCoordinator(cs, validator, candidatecollection.EvidenceReconciler{}, noRecovery{})
	if err != nil {
		return nil, err
	}
	f.coordinator = coord
	return f, nil
}

func (f *fixture) prepare(engine, provider, text string, evidence bool, score int) (candidatecollection.CollectRequest, routing.Plan, error) {
	state := f.taskStore.State()
	routeReq := routing.Requirements{TaskID: state.TaskID, SessionID: state.SessionID, HandoffPackageID: f.pkg.PackageID, HandoffPackageSHA256: f.pkg.PackageSHA256, RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
	routeCandidates := []routing.Candidate{{EngineID: engine, ProviderID: provider, Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: score, EvidenceRefs: []string{"route-proof-" + engine}}}
	plan, err := routing.Select(routeReq, routeCandidates)
	if err != nil {
		return candidatecollection.CollectRequest{}, routing.Plan{}, err
	}
	if _, _, err = f.routeStore.SaveOnce(plan); err != nil {
		return candidatecollection.CollectRequest{}, routing.Plan{}, err
	}
	f.routeState[plan.RouteID] = struct {
		req        routing.Requirements
		candidates []routing.Candidate
	}{routeReq, routeCandidates}
	execID := "exec-" + engine + "-" + fmt.Sprint(len(f.routeState))
	resultID := "result-" + engine + "-" + fmt.Sprint(len(f.routeState))
	execution := engineruntime.Execution{ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, EngineID: engine, Operation: engineruntime.OperationStart, Disposition: engineruntime.DispositionRunning}
	if _, _, err = f.runtimeStore.BeginOnce(execution); err != nil {
		return candidatecollection.CollectRequest{}, routing.Plan{}, err
	}
	if _, _, err = f.runtimeStore.SetDispositionOnce(execID, engineruntime.DispositionCompleted, "", resultID, "candidate completed"); err != nil {
		return candidatecollection.CollectRequest{}, routing.Plan{}, err
	}
	if _, _, err = f.candidateEngineStore.StartOnce(recovery.Execution{ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, Engine: engine, StartedAt: time.Unix(0, 0).UTC()}); err != nil {
		return candidatecollection.CollectRequest{}, routing.Plan{}, err
	}
	result := recovery.Result{ResultID: resultID, ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, Engine: engine, Output: session.EngineResult{Text: text}, CompletedAt: time.Unix(0, 0).UTC()}
	if _, _, err = f.candidateEngineStore.CompleteResultOnce(result); err != nil {
		return candidatecollection.CollectRequest{}, routing.Plan{}, err
	}
	currentExec := f.runtimeStore.Execution(execID)
	currentResult := f.candidateEngineStore.Result(resultID)
	req, err := candidatecollection.BuildCollectRequest(state, f.pkg, plan, currentExec, currentResult)
	if err != nil {
		return candidatecollection.CollectRequest{}, routing.Plan{}, err
	}
	if evidence {
		req.RuntimeEvidenceIDs = append(req.RuntimeEvidenceIDs, "verification:proof31-"+engine)
	}
	return req, plan, nil
}

func (f *fixture) collect(engine, provider, text string, evidence bool, score int) (candidatecollection.CollectOutcome, candidatecollection.CollectRequest, error) {
	req, _, err := f.prepare(engine, provider, text, evidence, score)
	if err != nil {
		return candidatecollection.CollectOutcome{}, req, err
	}
	out, err := f.coordinator.Collect(context.Background(), req)
	return out, req, err
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	base := filepath.Join(os.TempDir(), "keydeck-proof31-reconstructed")
	_ = os.RemoveAll(base)
	defer os.RemoveAll(base)
	out := report{Proof: "0.31-evidence-bound-candidate-collection-and-reconciliation-assessment-reconstructed", Status: "failed", NextGate: "Proof 0.32 — Explicit Review Resolution and Selected-Only Canonical Recovery (reconstructed)"}
	add := func(n string, p bool, d any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: n, Passed: p, Detail: d})
	}

	f1, _ := newFixture(base, "current")
	first, req1, err := f1.collect("engine-a", "provider-a", "answer-a", true, 100)
	add("exact_current_task_handoff_route_runtime_and_engine_binding_is_validated_before_candidate_persistence", err == nil && !first.Reused && first.Candidate.RouteDecisionID == req1.RouteDecisionID, first.Candidate.CandidateID)
	reused, err := f1.coordinator.Collect(context.Background(), req1)
	add("duplicate_execution_result_identity_reuses_one_candidate_without_engine_rerun", err == nil && reused.Reused && reused.Candidate.CandidateID == first.Candidate.CandidateID, len(f1.collectionStore.CandidatesForScope(req1.TaskID, req1.HandoffPackageID)))
	conflictReq := req1
	conflictReq.RuntimeEvidenceIDs = append(conflictReq.RuntimeEvidenceIDs, "extra-independent-evidence")
	_, conflictErr := f1.coordinator.Collect(context.Background(), conflictReq)
	add("conflicting_duplicate_execution_result_binding_is_rejected", errors.Is(conflictErr, candidatecollection.ErrIdentityConflict), fmt.Sprint(conflictErr))

	f2, _ := newFixture(base, "stale-task")
	staleReq, _, _ := f2.prepare("engine-a", "provider-a", "answer", true, 100)
	_, _ = f2.taskStore.Append(tasks.EventStatusChanged, map[string]tasks.Status{"status": tasks.StatusWorking})
	_, staleErr := f2.coordinator.Collect(context.Background(), staleReq)
	add("stale_task_sequence_is_rejected_before_candidate_persistence", errors.Is(staleErr, candidatecollection.ErrStaleCurrentState) && len(f2.collectionStore.CandidatesForScope(staleReq.TaskID, staleReq.HandoffPackageID)) == 0, fmt.Sprint(staleErr))

	f3, _ := newFixture(base, "stale-package")
	packageReq, _, _ := f3.prepare("engine-a", "provider-a", "answer", true, 100)
	packageReq.HandoffPackageSHA = strings.Repeat("f", 64)
	_, packageErr := f3.coordinator.Collect(context.Background(), packageReq)
	add("stale_or_tampered_handoff_package_is_rejected_before_persistence", errors.Is(packageErr, candidatecollection.ErrStaleCurrentState), fmt.Sprint(packageErr))

	f4, _ := newFixture(base, "stale-route")
	routeReq, plan4, _ := f4.prepare("engine-a", "provider-a", "answer", true, 100)
	x := f4.routeState[plan4.RouteID]
	x.candidates[0].EvidenceScore = 1
	x.candidates = append(x.candidates, routing.Candidate{EngineID: "engine-z", ProviderID: "provider-z", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999})
	f4.routeState[plan4.RouteID] = x
	_, routeErr := f4.coordinator.Collect(context.Background(), routeReq)
	add("stale_route_decision_is_rejected_before_candidate_persistence", errors.Is(routeErr, candidatecollection.ErrStaleCurrentState), fmt.Sprint(routeErr))

	f5, _ := newFixture(base, "runtime-mismatch")
	runtimeReq, _, _ := f5.prepare("engine-a", "provider-a", "answer", true, 100)
	runtimeReq.ResultSHA = strings.Repeat("0", 64)
	_, runtimeErr := f5.coordinator.Collect(context.Background(), runtimeReq)
	add("runtime_result_identity_mismatch_is_rejected_before_persistence", errors.Is(runtimeErr, candidatecollection.ErrStaleCurrentState), fmt.Sprint(runtimeErr))

	f6, _ := newFixture(base, "secret")
	secretReq, _, _ := f6.prepare("engine-a", "provider-a", "api_key=SUPERSECRET123456789", true, 100)
	_, secretErr := f6.coordinator.Collect(context.Background(), secretReq)
	add("secret_like_candidate_evidence_is_blocked_before_durable_collection", errors.Is(secretErr, candidatecollection.ErrStaleCurrentState) && len(f6.collectionStore.CandidatesForScope(secretReq.TaskID, secretReq.HandoffPackageID)) == 0, fmt.Sprint(secretErr))

	f7, _ := newFixture(base, "restart")
	c7, req7, _ := f7.collect("engine-a", "provider-a", "restart-answer", true, 100)
	reopened, err := candidatecollection.OpenStore(filepath.Join(f7.root, "candidates.jsonl"))
	candidates7 := reopened.CandidatesForScope(req7.TaskID, req7.HandoffPackageID)
	add("candidate_collection_replays_after_restart_without_reexecuting_producer", err == nil && len(candidates7) == 1 && candidates7[0].CandidateID == c7.Candidate.CandidateID, candidates7[0].CandidateID)

	f8, _ := newFixture(base, "single")
	s8, _, _ := f8.collect("engine-a", "provider-a", "verified", true, 100)
	a8, err := f8.coordinator.Assess(context.Background(), s8.Candidate.TaskID, s8.Candidate.HandoffPackageID)
	add("one_verified_candidate_becomes_single_verified", err == nil && a8.State == candidatecollection.AssessmentSingleVerified && a8.SelectedCandidateID == s8.Candidate.CandidateID, a8.AssessmentID)

	f9, _ := newFixture(base, "agreement")
	c9a, _, _ := f9.collect("engine-a", "provider-a", "same-answer", true, 100)
	_, _, _ = f9.collect("engine-b", "provider-b", "same-answer", true, 90)
	a9, err := f9.coordinator.Assess(context.Background(), c9a.Candidate.TaskID, c9a.Candidate.HandoffPackageID)
	add("exact_cross_engine_output_agreement_is_recorded_without_judge_model_preference", err == nil && a9.State == candidatecollection.AssessmentAgreement && a9.SelectedCandidateID != "", a9.AssessmentID)

	f10, _ := newFixture(base, "disagreement")
	c10, _, _ := f10.collect("engine-a", "provider-a", "answer-a", true, 100)
	_, _, _ = f10.collect("engine-b", "provider-b", "answer-b", true, 90)
	a10, err := f10.coordinator.Assess(context.Background(), c10.Candidate.TaskID, c10.Candidate.HandoffPackageID)
	add("conflicting_verified_candidates_become_unresolved_disagreement", err == nil && a10.State == candidatecollection.AssessmentDisagreement && a10.SelectedCandidateID == "", a10.AssessmentID)

	f11, _ := newFixture(base, "majority")
	c11, _, _ := f11.collect("engine-a", "provider-a", "majority-answer", true, 100)
	_, _, _ = f11.collect("engine-b", "provider-b", "majority-answer", true, 90)
	_, _, _ = f11.collect("engine-c", "provider-c", "conflicting-evidence", true, 80)
	a11, err := f11.coordinator.Assess(context.Background(), c11.Candidate.TaskID, c11.Candidate.HandoffPackageID)
	add("two_matching_candidates_do_not_establish_truth_over_one_conflicting_candidate", err == nil && a11.State == candidatecollection.AssessmentDisagreement && a11.SelectedCandidateID == "", map[string]any{"state": a11.State, "candidates": len(a11.CandidateIDs)})

	f12, _ := newFixture(base, "needs-review")
	c12, _, _ := f12.collect("engine-a", "provider-a", "unverified", false, 100)
	a12, err := f12.coordinator.Assess(context.Background(), c12.Candidate.TaskID, c12.Candidate.HandoffPackageID)
	add("candidate_without_verified_runtime_evidence_requires_review", err == nil && a12.State == candidatecollection.AssessmentNeedsReview && a12.SelectedCandidateID == "", a12.AssessmentID)

	out.CandidateID = first.Candidate.CandidateID
	out.AssessmentID = a11.AssessmentID
	out.CandidateSetSHA256 = a11.CandidateSetSHA
	out.Passed = len(out.Scenarios) == 14
	for _, s := range out.Scenarios {
		if !s.Passed {
			out.Passed = false
		}
	}
	if out.Passed {
		out.Status = "passed"
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))
	if !out.Passed {
		return errors.New("proof 0.31 reconstructed acceptance gate failed")
	}
	return nil
}
