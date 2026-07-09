package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/handoff"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/recovery"
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
	Proof             string     `json:"proof"`
	Status            string     `json:"status"`
	Passed            bool       `json:"passed"`
	Scenarios         []scenario `json:"scenarios"`
	PackageID         string     `json:"package_id"`
	ExecutionID       string     `json:"execution_id"`
	CanonicalResultID string     `json:"canonical_result_id"`
	ReceiptID         string     `json:"receipt_id"`
	NextGate          string     `json:"next_gate"`
}

type adapter struct {
	id                       string
	mu                       sync.Mutex
	starts, resumes, cancels int
	startOutcome             engineruntime.Outcome
	resumeOutcome            engineruntime.Outcome
}

func (a *adapter) ID() string { return a.id }
func (a *adapter) Capabilities(context.Context) ([]engineruntime.Capability, error) {
	return []engineruntime.Capability{engineruntime.CapabilityText, engineruntime.CapabilityResume, engineruntime.CapabilityCancel, engineruntime.CapabilityPersistentSession}, nil
}
func (a *adapter) Health(context.Context) (engineruntime.Health, error) {
	return engineruntime.Health{State: engineruntime.HealthHealthy}, nil
}
func (a *adapter) Start(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.starts++
	return a.startOutcome, nil
}
func (a *adapter) Continue(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	return engineruntime.Outcome{}, errors.New("unused")
}
func (a *adapter) Resume(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.resumes++
	return a.resumeOutcome, nil
}
func (a *adapter) Cancel(context.Context, engineruntime.Binding) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cancels++
	return nil
}
func (a *adapter) counts() (int, int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.starts, a.resumes, a.cancels
}

type fixture struct {
	root         string
	taskStore    *tasks.Store
	taskManager  *tasks.Manager
	sessionPath  string
	timelinePath string
	runtimePath  string
	enginePath   string
	handoffPath  string
	paths        recovery.Paths
	runtime      *engineruntime.Runtime
	engineStore  *recovery.EngineStore
	handoffStore *handoff.Store
	executor     *handoff.Executor
	pkg          handoff.Package
	current      handoff.CurrentState
}

func newFixture(base, name, engineID string) (*fixture, error) {
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
	journalPath := filepath.Join(root, "journal.jsonl")
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		return nil, err
	}
	tm := &tasks.Manager{Store: ts, Journal: journal}
	_, err = tm.Create("proof29-task-"+name, "proof29-session-"+name, tasks.Contract{Goal: "execute exact handoff safely", RequiredOutcomes: []string{"survive restart", "commit once"}, ForbiddenScope: []string{"no replay"}, Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "proof scenario"}}})
	if err != nil {
		return nil, err
	}
	state := ts.State()
	sessionPath := filepath.Join(root, "session.json")
	ss := session.New(state.SessionID, root, state.Contract.Goal, "api")
	if err := session.Save(sessionPath, ss); err != nil {
		return nil, err
	}
	tlPath := filepath.Join(root, "timeline.jsonl")
	tl, err := timeline.Open(tlPath)
	if err != nil {
		return nil, err
	}
	enginePath := filepath.Join(root, "engine.jsonl")
	eng, err := recovery.OpenEngineStore(enginePath)
	if err != nil {
		return nil, err
	}
	runtimePath := filepath.Join(root, "runtime.jsonl")
	rs, err := engineruntime.Open(runtimePath)
	if err != nil {
		return nil, err
	}
	rt, err := engineruntime.New(rs, eng, tl)
	if err != nil {
		return nil, err
	}
	handoffPath := filepath.Join(root, "handoff.jsonl")
	hs, err := handoff.OpenStore(handoffPath)
	if err != nil {
		return nil, err
	}
	brain := projectbrain.Revision{ProjectID: "project-" + name, SessionID: state.SessionID, ProjectFingerprint: "fp-" + name, RevisionSHA256: strings.Repeat("b", 64), Context: projectbrain.ContextInspection{PacketID: "packet-" + name, PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "fp-" + name, InspectionSHA256: strings.Repeat("c", 64)}}
	passport := session.Passport{SessionID: state.SessionID, ProjectRoot: root, Goal: state.Contract.Goal, FromEngine: "api", ToEngine: engineID, HandoffReason: "proof29", Checkpoint: "checkpoint"}
	pkg, err := handoff.Assemble(handoff.Input{Task: state, ContextPacketID: brain.Context.PacketID, ContextPacketSHA256: brain.Context.PacketSHA256, MCPServerID: "mcp-proof29", MCPSchemaSHA256: strings.Repeat("d", 64), ProjectSourceFingerprint: brain.ProjectFingerprint, Brain: brain, Passport: passport, EngineID: engineID, RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText, engineruntime.CapabilityResume}})
	if err != nil {
		return nil, err
	}
	current := handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: pkg.ContextPacketID, ProjectBrainRevisionSHA256: pkg.ProjectBrainRevisionSHA256}
	paths := recovery.Paths{TaskEvents: taskPath, SessionState: sessionPath, ToolJournal: journalPath, Timeline: tlPath, ProofReceipts: filepath.Join(root, "receipts.jsonl"), EngineLedger: enginePath, ArtifactLedger: filepath.Join(root, "artifacts.jsonl")}
	f := &fixture{root: root, taskStore: ts, taskManager: tm, sessionPath: sessionPath, timelinePath: tlPath, runtimePath: runtimePath, enginePath: enginePath, handoffPath: handoffPath, paths: paths, runtime: rt, engineStore: eng, handoffStore: hs, pkg: pkg, current: current}
	f.executor = &handoff.Executor{Store: hs, Runtime: rt, Current: func() handoff.CurrentState { return f.current }}
	return f, nil
}
func (f *fixture) restart() error {
	hs, err := handoff.OpenStore(f.handoffPath)
	if err != nil {
		return err
	}
	eng, err := recovery.OpenEngineStore(f.enginePath)
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
	rt, err := engineruntime.New(rs, eng, tl)
	if err != nil {
		return err
	}
	f.handoffStore = hs
	f.engineStore = eng
	f.runtime = rt
	f.executor = &handoff.Executor{Store: hs, Runtime: rt, Current: func() handoff.CurrentState { return f.current }}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	base := filepath.Join(os.TempDir(), "keydeck-proof29-reconstructed")
	_ = os.RemoveAll(base)
	defer os.RemoveAll(base)
	out := report{Proof: "0.29-handoff-persistence-replay-safe-execution-restart-reconciliation-reconstructed", Status: "failed", NextGate: "Proof 0.30 — Deterministic Route Selection and Route-Bound Continuation"}
	add := func(n string, p bool, d any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: n, Passed: p, Detail: d})
	}
	f1, err := newFixture(base, "persist", "engine-persist")
	if err != nil {
		return err
	}
	saved, created, err := f1.handoffStore.SaveOnce(f1.pkg)
	if err != nil {
		return err
	}
	if err = f1.restart(); err != nil {
		return err
	}
	loaded, ok := f1.handoffStore.Package(f1.pkg.PackageID)
	add("append_only_handoff_store_replays_exact_package_after_restart", created && ok && loaded.PackageSHA256 == saved.PackageSHA256, map[string]any{"package": loaded.PackageID})
	f2, _ := newFixture(base, "stale", "engine-stale")
	ad2 := &adapter{id: "engine-stale", startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "should not run"}}}
	f2.current.TaskSequence++
	_, staleErr := f2.executor.Execute(context.Background(), ad2, f2.pkg)
	s2, _, _ := ad2.counts()
	add("current_state_is_validated_before_execute_or_resume", errors.Is(staleErr, handoff.ErrStaleTask) && s2 == 0 && f2.handoffStore.Cancelled(f2.pkg.PackageID) == false, fmt.Sprint(staleErr))
	f3, _ := newFixture(base, "binding", "engine-binding")
	_, _, _ = f3.handoffStore.SaveOnce(f3.pkg)
	b, created, err := f3.handoffStore.BindExecutionOnce(f3.pkg.PackageID, f3.pkg.EngineRequest.ExecutionID)
	_, _, conflict := f3.handoffStore.BindExecutionOnce(f3.pkg.PackageID, "other-exec")
	add("one_execution_id_is_bound_to_one_exact_package", err == nil && created && b.PackageSHA256 == f3.pkg.PackageSHA256 && errors.Is(conflict, handoff.ErrStoreConflict), b.ExecutionID)
	f4, _ := newFixture(base, "prestart", "engine-prestart")
	_, _, _ = f4.handoffStore.SaveOnce(f4.pkg)
	_, _, _ = f4.handoffStore.BindExecutionOnce(f4.pkg.PackageID, f4.pkg.EngineRequest.ExecutionID)
	_ = f4.restart()
	ad4 := &adapter{id: "engine-prestart", startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "started once"}}}
	r4, err := f4.executor.Execute(context.Background(), ad4, f4.pkg)
	st4, _, _ := ad4.counts()
	add("crash_after_package_persistence_before_engine_start_restarts_with_one_safe_start", err == nil && r4.Execution.Disposition == engineruntime.DispositionCompleted && st4 == 1, map[string]any{"starts": st4})
	f5, _ := newFixture(base, "resume", "engine-resume")
	ad5 := &adapter{id: "engine-resume", startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionResumeRequired, ExternalHandle: "thread-proof29", Resumable: true}, resumeOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, ExternalHandle: "thread-proof29", Resumable: true, Result: session.EngineResult{Text: "resumed completion"}}}
	first5, err := f5.executor.Execute(context.Background(), ad5, f5.pkg)
	if err != nil {
		return err
	}
	_ = f5.restart()
	reconciled5, err := f5.executor.Execute(context.Background(), ad5, f5.pkg)
	if err != nil {
		return err
	}
	st5, re5, _ := ad5.counts()
	final5, err := f5.executor.Execute(context.Background(), ad5, f5.pkg)
	if err != nil {
		return err
	}
	st5b, re5b, _ := ad5.counts()
	add("durable_resumable_binding_becomes_resume_required_after_restart_without_start_replay", first5.Execution.Disposition == engineruntime.DispositionResumeRequired && reconciled5.Execution.Disposition == engineruntime.DispositionCompleted && final5.Execution.Disposition == engineruntime.DispositionCompleted && st5 == 1 && st5b == 1 && re5 == 1 && re5b == 1, map[string]any{"starts": st5b, "resumes": re5b})
	f6, _ := newFixture(base, "complete", "engine-complete")
	ad6 := &adapter{id: "engine-complete", startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "canonical output", CompletedActions: []string{"finished"}}}}
	r6, err := f6.executor.Execute(context.Background(), ad6, f6.pkg)
	if err != nil {
		return err
	}
	_ = f6.restart()
	r6b, err := f6.executor.Execute(context.Background(), ad6, f6.pkg)
	if err != nil {
		return err
	}
	st6, _, _ := ad6.counts()
	add("completed_runtime_result_reuses_after_restart_without_duplicate_engine_start", r6.Execution.ResultID != "" && r6b.Execution.ResultID == r6.Execution.ResultID && st6 == 1, map[string]any{"result": r6.Execution.ResultID, "starts": st6})
	out.PackageID = f6.pkg.PackageID
	out.ExecutionID = f6.pkg.EngineRequest.ExecutionID
	out.CanonicalResultID = r6.Execution.ResultID
	coord, err := recovery.Open(f6.paths)
	if err != nil {
		return err
	}
	beforeSession, _ := session.Load(f6.sessionPath)
	rep1, err := coord.Recover()
	if err != nil {
		return err
	}
	after1, _ := session.Load(f6.sessionPath)
	rep2, err := coord.Recover()
	if err != nil {
		return err
	}
	after2, _ := session.Load(f6.sessionPath)
	result := coord.EngineStore().Result(r6.Execution.ResultID)
	pass7 := len(beforeSession.Transcript) == 0 && len(after1.Transcript) == 1 && len(after2.Transcript) == 1 && result.CanonicalCommitted
	add("result_persisted_before_canonical_commit_reconciles_exactly_once_through_recovery_coordinator", pass7, map[string]any{"first_decisions": len(rep1.Decisions), "second_decisions": len(rep2.Decisions), "messages": len(after2.Transcript)})
	tampered := f6.pkg
	tampered.EngineRequest.Prompt += "forged"
	_, tamperErr := f6.executor.Execute(context.Background(), ad6, tampered)
	st6b, _, _ := ad6.counts()
	stale := f6.pkg
	f6.current.ContextPacketID = "new-packet"
	_, staleCtxErr := f6.executor.Execute(context.Background(), ad6, stale)
	add("stale_or_tampered_packages_are_rejected_before_engine_execution", errors.Is(tamperErr, handoff.ErrInvalidPackage) && errors.Is(staleCtxErr, handoff.ErrStaleContext) && st6b == 1, map[string]any{"tamper": fmt.Sprint(tamperErr), "stale": fmt.Sprint(staleCtxErr)})
	f9, _ := newFixture(base, "cancel", "engine-cancel")
	ad9 := &adapter{id: "engine-cancel", startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionResumeRequired, ExternalHandle: "thread-cancel", Resumable: true}}
	_, err = f9.executor.Cancel(context.Background(), ad9, f9.pkg)
	if err != nil {
		return err
	}
	_ = f9.restart()
	_, cancelErr := f9.executor.Execute(context.Background(), ad9, f9.pkg)
	st9, re9, _ := ad9.counts()
	add("cancellation_persists_and_blocks_later_execute_or_resume", errors.Is(cancelErr, handoff.ErrPackageCancelled) && st9 == 0 && re9 == 0, map[string]any{"error": fmt.Sprint(cancelErr)})
	artH, err := f6.handoffStore.ReceiptArtifact()
	if err != nil {
		return err
	}
	arts := []proofreceipt.Artifact{artH, fileArtifact("runtime execution ledger", f6.runtimePath), fileArtifact("engine result ledger", f6.enginePath), fileArtifact("canonical session state", f6.sessionPath)}
	tl, _ := timeline.Open(f6.timelinePath)
	receipt, err := proofreceipt.Build(f6.taskStore.State(), tl.Snapshot(), arts)
	if err != nil {
		return err
	}
	bound := map[string]bool{}
	for _, a := range receipt.Artifacts {
		bound[a.Name] = true
	}
	pass10 := bound["handoff package store"] && bound["runtime execution ledger"] && bound["engine result ledger"] && bound["canonical session state"] && result.CanonicalCommitted
	add("proof_receipt_binds_package_store_runtime_result_and_canonical_commit_evidence", pass10, map[string]any{"receipt": receipt.ReceiptID, "bound": bound})
	out.ReceiptID = receipt.ReceiptID
	out.Passed = len(out.Scenarios) == 10
	for _, s := range out.Scenarios {
		out.Passed = out.Passed && s.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !out.Passed {
		return errors.New("Proof 0.29 acceptance gate failed")
	}
	return nil
}
func fileArtifact(name, path string) proofreceipt.Artifact {
	raw, _ := os.ReadFile(path)
	s := sha256.Sum256(raw)
	return proofreceipt.Artifact{Name: name, Path: path, SHA256: hex.EncodeToString(s[:]), Size: int64(len(raw))}
}
