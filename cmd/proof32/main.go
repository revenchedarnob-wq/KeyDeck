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
	Proof               string     `json:"proof"`
	Status              string     `json:"status"`
	Passed              bool       `json:"passed"`
	Scenarios           []scenario `json:"scenarios"`
	AssessmentID        string     `json:"assessment_id"`
	ResolutionID        string     `json:"resolution_id"`
	SelectedCandidateID string     `json:"selected_candidate_id"`
	RecoveryCommitID    string     `json:"recovery_commit_id"`
	NextGate            string     `json:"next_gate"`
}

type routeState struct {
	req        routing.Requirements
	candidates []routing.Candidate
}
type fixture struct {
	root, taskPath, journalPath, sessionPath, timelinePath, canonicalEnginePath string
	taskStore                                                                   *tasks.Store
	taskManager                                                                 *tasks.Manager
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
	routeState                                                                  map[string]routeState
}

func newFixture(base, name string) (*fixture, error) {
	root := filepath.Join(base, name)
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	f := &fixture{root: root, taskPath: filepath.Join(root, "task.jsonl"), journalPath: filepath.Join(root, "journal.jsonl"), sessionPath: filepath.Join(root, "session.json"), timelinePath: filepath.Join(root, "timeline.jsonl"), canonicalEnginePath: filepath.Join(root, "canonical-engine.jsonl"), routeState: map[string]routeState{}}
	ts, err := tasks.Open(f.taskPath)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(f.journalPath)
	if err != nil {
		return nil, err
	}
	tm := &tasks.Manager{Store: ts, Journal: journal}
	_, err = tm.Create("proof32-task-"+name, "proof32-session-"+name, tasks.Contract{Goal: "resolve disagreement and commit exactly one candidate", RequiredOutcomes: []string{"explicit evidence resolution", "selected-only recovery"}, Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "proof32"}}})
	if err != nil {
		return nil, err
	}
	state := ts.State()
	if err = session.Save(f.sessionPath, session.New(state.SessionID, root, state.Contract.Goal, "keydeck")); err != nil {
		return nil, err
	}
	brain := projectbrain.Revision{ProjectID: "project-" + name, SessionID: state.SessionID, ProjectFingerprint: "fp-" + name, RevisionSHA256: strings.Repeat("b", 64), Context: projectbrain.ContextInspection{PacketID: "packet-" + name, PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "fp-" + name, InspectionSHA256: strings.Repeat("c", 64)}}
	passport := session.Passport{SessionID: state.SessionID, ProjectRoot: root, Goal: state.Contract.Goal, FromEngine: "api", ToEngine: "engine-a", HandoffReason: "proof32"}
	pkg, err := handoff.Assemble(handoff.Input{Task: state, ContextPacketID: brain.Context.PacketID, ContextPacketSHA256: brain.Context.PacketSHA256, MCPServerID: "mcp-proof32", MCPSchemaSHA256: strings.Repeat("d", 64), ProjectSourceFingerprint: brain.ProjectFingerprint, Brain: brain, Passport: passport, EngineID: "engine-a", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}})
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
	f.taskStore, f.taskManager, f.handoffStore, f.routeStore, f.runtimeStore, f.candidateEngineStore, f.canonicalEngineStore, f.collectionStore, f.recoveryCoordinator, f.pkg = ts, tm, hs, routes, runtimeStore, candidateEngines, rc.EngineStore(), collection, rc, pkg
	f.current = handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: pkg.ContextPacketID, ProjectBrainRevisionSHA256: pkg.ProjectBrainRevisionSHA256}
	validator := &candidatecollection.KeyDeckValidator{Tasks: ts, Handoffs: hs, HandoffCurrent: func() handoff.CurrentState { return f.current }, Routes: routes, RouteCurrent: func(_ context.Context, p routing.Plan) (routing.Requirements, []routing.Candidate, error) {
		x, ok := f.routeState[p.RouteID]
		if !ok {
			return routing.Requirements{}, nil, errors.New("route state missing")
		}
		return x.req, x.candidates, nil
	}, Runtime: runtimeStore, CandidateEngineLedger: candidateEngines}
	port := &candidatecollection.KeyDeckRecoveryPort{Coordinator: rc, EngineLedger: rc.EngineStore()}
	coord, err := candidatecollection.NewCoordinator(collection, validator, candidatecollection.EvidenceReconciler{}, port)
	if err != nil {
		return nil, err
	}
	f.coordinator = coord
	return f, nil
}

func (f *fixture) prepare(engine, provider, text string) (candidatecollection.CollectRequest, error) {
	state := f.taskStore.State()
	rr := routing.Requirements{TaskID: state.TaskID, SessionID: state.SessionID, HandoffPackageID: f.pkg.PackageID, HandoffPackageSHA256: f.pkg.PackageSHA256, RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
	cs := []routing.Candidate{{EngineID: engine, ProviderID: provider, Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 100, EvidenceRefs: []string{"route-proof-" + engine}}}
	plan, err := routing.Select(rr, cs)
	if err != nil {
		return candidatecollection.CollectRequest{}, err
	}
	if _, _, err = f.routeStore.SaveOnce(plan); err != nil {
		return candidatecollection.CollectRequest{}, err
	}
	f.routeState[plan.RouteID] = routeState{rr, cs}
	n := len(f.routeState)
	execID := fmt.Sprintf("exec-%s-%d", engine, n)
	resultID := fmt.Sprintf("result-%s-%d", engine, n)
	if _, _, err = f.runtimeStore.BeginOnce(engineruntime.Execution{ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, EngineID: engine, Operation: engineruntime.OperationStart, Disposition: engineruntime.DispositionRunning}); err != nil {
		return candidatecollection.CollectRequest{}, err
	}
	if _, _, err = f.runtimeStore.SetDispositionOnce(execID, engineruntime.DispositionCompleted, "", resultID, "candidate complete"); err != nil {
		return candidatecollection.CollectRequest{}, err
	}
	if _, _, err = f.candidateEngineStore.StartOnce(recovery.Execution{ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, Engine: engine, StartedAt: time.Unix(0, 0).UTC()}); err != nil {
		return candidatecollection.CollectRequest{}, err
	}
	res := recovery.Result{ResultID: resultID, ExecutionID: execID, TaskID: state.TaskID, SessionID: state.SessionID, Engine: engine, Output: session.EngineResult{Text: text}, CompletedAt: time.Unix(0, 0).UTC()}
	if _, _, err = f.candidateEngineStore.CompleteResultOnce(res); err != nil {
		return candidatecollection.CollectRequest{}, err
	}
	req, err := candidatecollection.BuildCollectRequest(state, f.pkg, plan, f.runtimeStore.Execution(execID), f.candidateEngineStore.Result(resultID))
	if err != nil {
		return req, err
	}
	req.RuntimeEvidenceIDs = append(req.RuntimeEvidenceIDs, "verification:proof32-"+engine)
	return req, nil
}
func (f *fixture) collect(engine, provider, text string) (candidatecollection.CollectOutcome, error) {
	req, err := f.prepare(engine, provider, text)
	if err != nil {
		return candidatecollection.CollectOutcome{}, err
	}
	return f.coordinator.Collect(context.Background(), req)
}
func (f *fixture) restartCoordinator() error {
	collection, err := candidatecollection.OpenStore(filepath.Join(f.root, "candidates.jsonl"))
	if err != nil {
		return err
	}
	f.collectionStore = collection
	validator := &candidatecollection.KeyDeckValidator{Tasks: f.taskStore, Handoffs: f.handoffStore, HandoffCurrent: func() handoff.CurrentState { return f.current }, Routes: f.routeStore, RouteCurrent: func(_ context.Context, p routing.Plan) (routing.Requirements, []routing.Candidate, error) {
		x, ok := f.routeState[p.RouteID]
		if !ok {
			return routing.Requirements{}, nil, errors.New("route state missing")
		}
		return x.req, x.candidates, nil
	}, Runtime: f.runtimeStore, CandidateEngineLedger: f.candidateEngineStore}
	port := &candidatecollection.KeyDeckRecoveryPort{Coordinator: f.recoveryCoordinator, EngineLedger: f.canonicalEngineStore}
	coord, err := candidatecollection.NewCoordinator(collection, validator, candidatecollection.EvidenceReconciler{}, port)
	if err != nil {
		return err
	}
	f.coordinator = coord
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	base := filepath.Join(os.TempDir(), "keydeck-proof32-reconstructed")
	_ = os.RemoveAll(base)
	defer os.RemoveAll(base)
	out := report{Proof: "0.32-explicit-review-resolution-and-selected-only-canonical-recovery-reconstructed", Status: "failed", NextGate: "Proof 0.33 — Integrated Route-Bound Reconciliation and Canonical Recovery (reconstructed)"}
	add := func(n string, p bool, d any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: n, Passed: p, Detail: d})
	}
	f, _ := newFixture(base, "main")
	ca, _ := f.collect("engine-a", "provider-a", "selected-answer")
	cb, _ := f.collect("engine-b", "provider-b", "rejected-answer")
	assessment, err := f.coordinator.Assess(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	add("conflicting_candidates_remain_unresolved_and_cannot_commit", err == nil && assessment.State == candidatecollection.AssessmentDisagreement && assessment.SelectedCandidateID == "", assessment.AssessmentID)
	before, _ := session.Load(f.sessionPath)
	_, commitBeforeErr := f.coordinator.CommitResolved(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	after, _ := session.Load(f.sessionPath)
	add("unresolved_candidates_cannot_mutate_canonical_state", errors.Is(commitBeforeErr, candidatecollection.ErrResolutionRequired) && len(before.Transcript) == len(after.Transcript) && len(f.canonicalEngineStore.Results()) == 0, fmt.Sprint(commitBeforeErr))
	review, err := f.coordinator.ReviewRequest(ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	add("disagreement_produces_ui_neutral_explicit_review_contract", err == nil && review.AssessmentID == assessment.AssessmentID && len(review.CandidateIDs) == 2 && len(review.RequiredEvidenceKinds) > 0, review.RequiredEvidenceKinds)
	_, missingErr := f.coordinator.ResolveReview(candidatecollection.ReviewerResolutionRequest{AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA, SelectedCandidateID: ca.Candidate.CandidateID, ReviewerRef: "reviewer-proof32", Rationale: "evidence selects candidate"})
	add("review_resolution_without_decisive_evidence_is_rejected", errors.Is(missingErr, candidatecollection.ErrMissingDecisiveEvidence), fmt.Sprint(missingErr))
	_, outsideErr := f.coordinator.ResolveReview(candidatecollection.ReviewerResolutionRequest{AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA, SelectedCandidateID: "candidate-not-in-set", ReviewerRef: "reviewer-proof32", DecisiveEvidenceIDs: []string{"test:outside"}, Rationale: "invalid"})
	add("review_cannot_select_candidate_outside_assessed_set", errors.Is(outsideErr, candidatecollection.ErrCandidateNotInAssessment), fmt.Sprint(outsideErr))
	resolution, err := f.coordinator.ResolveReview(candidatecollection.ReviewerResolutionRequest{AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA, SelectedCandidateID: ca.Candidate.CandidateID, ReviewerRef: "reviewer-proof32", DecisiveEvidenceIDs: []string{"test:reproduced-selected-answer", "artifact:verified-output"}, Rationale: "independent reproduction matches candidate A"})
	add("explicit_evidence_backed_resolution_selects_exact_candidate", err == nil && resolution.SelectedCandidateID == ca.Candidate.CandidateID && len(resolution.DecisiveEvidenceIDs) == 2, resolution.ResolutionID)
	if err = f.restartCoordinator(); err != nil {
		return err
	}
	reviewed, ok := f.collectionStore.ResolutionForAssessment(assessment.AssessmentID)
	add("reviewer_resolution_survives_restart_with_same_identity", ok && reviewed.ResolutionID == resolution.ResolutionID && reviewed.ResolutionSHA == resolution.ResolutionSHA, reviewed.ResolutionID)
	commit, err := f.coordinator.CommitResolved(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	if err != nil {
		return err
	}
	canonical, _ := session.Load(f.sessionPath)
	selectedResult := f.canonicalEngineStore.Result(ca.Candidate.ResultID)
	unselectedResult := f.canonicalEngineStore.Result(cb.Candidate.ResultID)
	selectedOnly := selectedResult.CanonicalCommitted && unselectedResult.ResultID == "" && len(canonical.Transcript) == 1 && canonical.Transcript[0].Text == "selected-answer" && canonical.Transcript[0].Engine == ca.Candidate.EngineID
	add("only_explicitly_resolved_candidate_enters_recovery_coordinator", selectedOnly, map[string]any{"selected": selectedResult.ResultID, "unselected": unselectedResult.ResultID})
	again, err := f.coordinator.CommitResolved(context.Background(), ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	canonicalAgain, _ := session.Load(f.sessionPath)
	add("repeated_commit_reuses_same_canonical_result_without_duplication", err == nil && again.CommitID == commit.CommitID && len(canonicalAgain.Transcript) == 1, again.CommitID)
	add("direct_raw_engine_result_recovery_path_fails_closed", errors.Is(f.coordinator.RejectRawRecovery(), candidatecollection.ErrDirectRawRecoveryForbidden), fmt.Sprint(f.coordinator.RejectRawRecovery()))
	evidence, err := f.coordinator.ReceiptEvidence(ca.Candidate.TaskID, ca.Candidate.HandoffPackageID)
	evidenceBound := err == nil && evidence.AssessmentID == assessment.AssessmentID && evidence.ResolutionID == resolution.ResolutionID && evidence.RecoveryPreparationID != "" && evidence.RecoveryOperationID != "" && evidence.RecoveryCommitID == commit.CommitID && evidence.CanonicalCommitRef == commit.CanonicalRef
	add("receipt_evidence_binds_collection_assessment_resolution_preparation_operation_and_canonical_commit", evidenceBound, evidence.RecoveryOperationID)

	late, _ := newFixture(base, "late")
	l1, _ := late.collect("engine-a", "provider-a", "a")
	_, _ = late.collect("engine-b", "provider-b", "b")
	oldAssessment, _ := late.coordinator.Assess(context.Background(), l1.Candidate.TaskID, l1.Candidate.HandoffPackageID)
	_, _ = late.collect("engine-c", "provider-c", "c")
	_, lateReviewErr := late.coordinator.ReviewRequest(l1.Candidate.TaskID, l1.Candidate.HandoffPackageID)
	_, lateCommitErr := late.coordinator.CommitResolved(context.Background(), l1.Candidate.TaskID, l1.Candidate.HandoffPackageID)
	add("late_candidate_invalidates_stale_assessment_before_review_or_commit", errors.Is(lateReviewErr, candidatecollection.ErrStaleAssessment) && errors.Is(lateCommitErr, candidatecollection.ErrStaleAssessment), map[string]any{"assessment": oldAssessment.AssessmentID, "review_error": fmt.Sprint(lateReviewErr)})

	out.AssessmentID = assessment.AssessmentID
	out.ResolutionID = resolution.ResolutionID
	out.SelectedCandidateID = ca.Candidate.CandidateID
	out.RecoveryCommitID = commit.CommitID
	out.Passed = len(out.Scenarios) == 12
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
		return errors.New("proof 0.32 reconstructed acceptance gate failed")
	}
	return nil
}
