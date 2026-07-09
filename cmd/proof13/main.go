package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"keydeck.local/feasibilitylab/internal/engineruntime"
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
	Proof     string     `json:"proof"`
	Passed    bool       `json:"passed"`
	Claims    []string   `json:"claims"`
	Scenarios []scenario `json:"scenarios"`
}

type fakeAdapter struct {
	id            string
	capabilities  []engineruntime.Capability
	health        engineruntime.Health
	startOutcome  engineruntime.Outcome
	resumeOutcome engineruntime.Outcome
	startErr      error
	starts        int
	continues     int
	resumes       int
	cancels       int
}

func (f *fakeAdapter) ID() string { return f.id }
func (f *fakeAdapter) Capabilities(context.Context) ([]engineruntime.Capability, error) {
	return append([]engineruntime.Capability(nil), f.capabilities...), nil
}
func (f *fakeAdapter) Health(context.Context) (engineruntime.Health, error) { return f.health, nil }
func (f *fakeAdapter) Start(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	f.starts++
	return f.startOutcome, f.startErr
}
func (f *fakeAdapter) Continue(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	f.continues++
	return f.startOutcome, f.startErr
}
func (f *fakeAdapter) Resume(_ context.Context, req engineruntime.Request) (engineruntime.Outcome, error) {
	f.resumes++
	if req.Binding == nil || req.Binding.ExternalHandle == "" {
		return engineruntime.Outcome{}, errors.New("resume binding missing")
	}
	return f.resumeOutcome, nil
}
func (f *fakeAdapter) Cancel(context.Context, engineruntime.Binding) error {
	f.cancels++
	return nil
}

type paths struct {
	runtime  string
	recovery recovery.Paths
}

func main() {
	dir, err := os.MkdirTemp("", "keydeck-proof13-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	scenarios, err := runProof(dir)
	if err != nil {
		scenarios = append(scenarios, scenario{Name: "proof_harness", Passed: false, Detail: err.Error()})
	}
	passed := err == nil
	for _, s := range scenarios {
		passed = passed && s.Passed
	}
	result := report{
		Proof:  "0.13-engine-neutral-runtime-contract",
		Passed: passed,
		Claims: []string{
			"replaceable engines obey one durable start, continue, resume, cancel, capabilities and health boundary",
			"capability and health failures block before engine work begins",
			"durable runtime bindings, failures, cancellation and recovery dispositions survive restart without unsafe replay",
			"completed engine output enters the existing recovery ledger before canonical state and commits exactly once through the Recovery Coordinator",
			"durable external work resumes through a stored binding while interrupted non-resumable work becomes input_required",
			"engines remain workers and never own canonical KeyDeck session state",
		},
		Scenarios: scenarios,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(b))
	if !passed {
		os.Exit(1)
	}
}

func runProof(dir string) ([]scenario, error) {
	p := makePaths(dir)
	manager, err := initializeTaskAndSession(p, dir)
	if err != nil {
		return nil, err
	}
	scenarios := []scenario{}

	runtime, _, _, err := openRuntime(p)
	if err != nil {
		return scenarios, err
	}

	capAdapter := &fakeAdapter{id: "cap-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, health: engineruntime.Health{State: engineruntime.HealthHealthy}}
	capResult, err := runtime.Invoke(context.Background(), capAdapter, engineruntime.OperationStart, req("exec-cap", capAdapter.id, engineruntime.CapabilityResume))
	if err != nil {
		return scenarios, err
	}
	capPassed := capResult.Execution.Disposition == engineruntime.DispositionFailed && !capResult.AdapterInvoked && capAdapter.starts == 0
	scenarios = append(scenarios, scenario{Name: "capability_gate_blocks_before_execution", Passed: capPassed, Detail: map[string]any{"disposition": capResult.Execution.Disposition, "adapter_calls": capAdapter.starts}})
	if !capPassed {
		return scenarios, errors.New("capability gate failed")
	}
	if _, err := manager.UpdateCheck("capability-gate", tasks.CheckPassed, "missing required capability blocked before adapter invocation"); err != nil {
		return scenarios, err
	}

	healthAdapter := &fakeAdapter{id: "health-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, health: engineruntime.Health{State: engineruntime.HealthUnhealthy, Detail: "offline"}}
	healthResult, err := runtime.Invoke(context.Background(), healthAdapter, engineruntime.OperationStart, req("exec-health", healthAdapter.id, engineruntime.CapabilityText))
	if err != nil {
		return scenarios, err
	}
	healthPassed := healthResult.Execution.Disposition == engineruntime.DispositionFailed && !healthResult.AdapterInvoked && healthAdapter.starts == 0
	scenarios = append(scenarios, scenario{Name: "health_gate_blocks_before_execution", Passed: healthPassed, Detail: map[string]any{"disposition": healthResult.Execution.Disposition, "adapter_calls": healthAdapter.starts}})
	if !healthPassed {
		return scenarios, errors.New("health gate failed")
	}
	if _, err := manager.UpdateCheck("health-gate", tasks.CheckPassed, "unhealthy engine blocked before adapter invocation"); err != nil {
		return scenarios, err
	}

	runtime, _, _, err = openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	continueAdapter := &fakeAdapter{id: "continue-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, health: engineruntime.Health{State: engineruntime.HealthHealthy}, startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "continued result"}}}
	continued, err := runtime.Invoke(context.Background(), continueAdapter, engineruntime.OperationContinue, req("exec-continue", continueAdapter.id, engineruntime.CapabilityText))
	if err != nil {
		return scenarios, err
	}
	continuePassed := continued.Execution.Disposition == engineruntime.DispositionCompleted && continueAdapter.continues == 1 && continueAdapter.starts == 0
	scenarios = append(scenarios, scenario{Name: "continue_operation_dispatches_through_common_contract", Passed: continuePassed, Detail: map[string]any{"continue_calls": continueAdapter.continues, "start_calls": continueAdapter.starts}})
	if !continuePassed {
		return scenarios, errors.New("continue operation did not use common runtime contract")
	}
	if _, err := manager.UpdateCheck("continue-dispatch", tasks.CheckPassed, "continue operation dispatched through the common runtime contract"); err != nil {
		return scenarios, err
	}

	failAdapter := &fakeAdapter{id: "fail-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, health: engineruntime.Health{State: engineruntime.HealthHealthy}, startErr: errors.New("synthetic engine failure")}
	failResult, err := runtime.Invoke(context.Background(), failAdapter, engineruntime.OperationStart, req("exec-fail", failAdapter.id, engineruntime.CapabilityText))
	if err != nil {
		return scenarios, err
	}
	reopened, _, _, err := openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	failReplayAdapter := &fakeAdapter{id: failAdapter.id, capabilities: failAdapter.capabilities, health: failAdapter.health}
	failReplay, err := reopened.Invoke(context.Background(), failReplayAdapter, engineruntime.OperationStart, req("exec-fail", failReplayAdapter.id, engineruntime.CapabilityText))
	if err != nil {
		return scenarios, err
	}
	failPassed := failResult.Execution.Disposition == engineruntime.DispositionFailed && failReplay.Execution.Disposition == engineruntime.DispositionFailed && failReplayAdapter.starts == 0
	scenarios = append(scenarios, scenario{Name: "failed_runtime_survives_restart_without_replay", Passed: failPassed, Detail: map[string]any{"first_calls": failAdapter.starts, "restart_calls": failReplayAdapter.starts}})
	if !failPassed {
		return scenarios, errors.New("failed runtime replayed")
	}
	if _, err := manager.UpdateCheck("failure-durable", tasks.CheckPassed, "failed execution survived restart and was not replayed"); err != nil {
		return scenarios, err
	}

	runtime, _, _, err = openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	resumeAdapter := &fakeAdapter{
		id:            "resume-engine",
		capabilities:  []engineruntime.Capability{engineruntime.CapabilityText, engineruntime.CapabilityPersistentSession, engineruntime.CapabilityResume, engineruntime.CapabilityCancel},
		health:        engineruntime.Health{State: engineruntime.HealthHealthy},
		startOutcome:  engineruntime.Outcome{Disposition: engineruntime.DispositionResumeRequired, ExternalHandle: "thread-proof13", Resumable: true, Detail: "durable thread persisted"},
		resumeOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, ExternalHandle: "thread-proof13", Resumable: true, Result: session.EngineResult{Text: "resumed completion"}},
	}
	started, err := runtime.Invoke(context.Background(), resumeAdapter, engineruntime.OperationStart, req("exec-resume", resumeAdapter.id, engineruntime.CapabilityResume))
	if err != nil {
		return scenarios, err
	}
	reopened, reopenedStore, _, err := openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	binding, bindingOK := reopenedStore.BindingForExecution("exec-resume")
	resumed, err := reopened.Invoke(context.Background(), resumeAdapter, engineruntime.OperationResume, req("exec-resume", resumeAdapter.id, engineruntime.CapabilityResume))
	if err != nil {
		return scenarios, err
	}
	resumePassed := started.Execution.Disposition == engineruntime.DispositionResumeRequired && bindingOK && binding.ExternalHandle == "thread-proof13" && resumed.Execution.Disposition == engineruntime.DispositionCompleted && resumeAdapter.resumes == 1
	scenarios = append(scenarios, scenario{Name: "durable_binding_survives_restart_and_resumes", Passed: resumePassed, Detail: map[string]any{"binding": binding.ExternalHandle, "resume_calls": resumeAdapter.resumes, "final_disposition": resumed.Execution.Disposition}})
	if !resumePassed {
		return scenarios, errors.New("durable resume failed")
	}
	if _, err := manager.UpdateCheck("binding-resume", tasks.CheckPassed, "durable external binding survived restart and resumed through common runtime"); err != nil {
		return scenarios, err
	}

	runtime, _, _, err = openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	cancelAdapter := &fakeAdapter{id: "cancel-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText, engineruntime.CapabilityResume, engineruntime.CapabilityCancel}, health: engineruntime.Health{State: engineruntime.HealthHealthy}, startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionResumeRequired, ExternalHandle: "job-proof13", Resumable: true}}
	if _, err := runtime.Invoke(context.Background(), cancelAdapter, engineruntime.OperationStart, req("exec-cancel", cancelAdapter.id, engineruntime.CapabilityResume)); err != nil {
		return scenarios, err
	}
	cancelled, err := runtime.Cancel(context.Background(), cancelAdapter, "exec-cancel")
	if err != nil {
		return scenarios, err
	}
	reopened, _, _, err = openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	afterCancel, err := reopened.Invoke(context.Background(), cancelAdapter, engineruntime.OperationResume, req("exec-cancel", cancelAdapter.id, engineruntime.CapabilityResume))
	if err != nil {
		return scenarios, err
	}
	cancelPassed := cancelled.Execution.Disposition == engineruntime.DispositionCancelled && afterCancel.Execution.Disposition == engineruntime.DispositionCancelled && cancelAdapter.cancels == 1 && cancelAdapter.resumes == 0
	scenarios = append(scenarios, scenario{Name: "cancellation_survives_restart", Passed: cancelPassed, Detail: map[string]any{"cancel_calls": cancelAdapter.cancels, "resume_calls_after_cancel": cancelAdapter.resumes}})
	if !cancelPassed {
		return scenarios, errors.New("cancellation durability failed")
	}
	if _, err := manager.UpdateCheck("cancel-durable", tasks.CheckPassed, "cancelled runtime stayed cancelled after restart"); err != nil {
		return scenarios, err
	}

	_, runtimeStore, _, err := openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	if _, _, err := runtimeStore.BeginOnce(engineruntime.Execution{ExecutionID: "exec-interrupted", TaskID: "task-proof13", SessionID: "session-proof13", EngineID: "interrupted-engine", Operation: engineruntime.OperationStart}); err != nil {
		return scenarios, err
	}
	reopened, _, _, err = openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	interruptedAdapter := &fakeAdapter{id: "interrupted-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, health: engineruntime.Health{State: engineruntime.HealthHealthy}}
	interrupted, err := reopened.Invoke(context.Background(), interruptedAdapter, engineruntime.OperationStart, req("exec-interrupted", interruptedAdapter.id, engineruntime.CapabilityText))
	if err != nil {
		return scenarios, err
	}
	inputPassed := interrupted.Execution.Disposition == engineruntime.DispositionInputRequired && interruptedAdapter.starts == 0
	scenarios = append(scenarios, scenario{Name: "interrupted_nonresumable_becomes_input_required", Passed: inputPassed, Detail: map[string]any{"adapter_calls": interruptedAdapter.starts, "disposition": interrupted.Execution.Disposition}})
	if !inputPassed {
		return scenarios, errors.New("interrupted runtime replayed")
	}
	if _, err := manager.UpdateCheck("input-required", tasks.CheckPassed, "interrupted non-resumable runtime became input_required without replay"); err != nil {
		return scenarios, err
	}

	runtime, _, _, err = openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	completeAdapter := &fakeAdapter{id: "complete-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, health: engineruntime.Health{State: engineruntime.HealthHealthy}, startOutcome: engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "canonical result"}}}
	completed, err := runtime.Invoke(context.Background(), completeAdapter, engineruntime.OperationStart, req("exec-complete", completeAdapter.id, engineruntime.CapabilityText))
	if err != nil {
		return scenarios, err
	}
	beforeRecovery, err := session.Load(p.recovery.SessionState)
	if err != nil {
		return scenarios, err
	}
	ownerPassed := completed.Execution.Disposition == engineruntime.DispositionCompleted && assistantCount(beforeRecovery) == 0
	scenarios = append(scenarios, scenario{Name: "engine_result_does_not_own_canonical_state", Passed: ownerPassed, Detail: map[string]any{"assistant_messages_before_recovery": assistantCount(beforeRecovery)}})
	if !ownerPassed {
		return scenarios, errors.New("engine mutated canonical state directly")
	}
	if _, err := manager.UpdateCheck("canonical-owner", tasks.CheckPassed, "engine result stayed in durable recovery ledger until KeyDeck coordinator committed it"); err != nil {
		return scenarios, err
	}

	_, crashStore, crashEngines, err := openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	if _, _, err := crashStore.BeginOnce(engineruntime.Execution{ExecutionID: "exec-crash-result", TaskID: "task-proof13", SessionID: "session-proof13", EngineID: "crash-engine", Operation: engineruntime.OperationStart}); err != nil {
		return scenarios, err
	}
	if _, _, err := crashEngines.StartOnce(recovery.Execution{ExecutionID: "exec-crash-result", TaskID: "task-proof13", SessionID: "session-proof13", Engine: "crash-engine"}); err != nil {
		return scenarios, err
	}
	if _, _, err := crashEngines.CompleteResultOnce(recovery.Result{ResultID: "runtime-result-exec-crash-result", ExecutionID: "exec-crash-result", TaskID: "task-proof13", SessionID: "session-proof13", Engine: "crash-engine", Output: session.EngineResult{Text: "crash-window result"}}); err != nil {
		return scenarios, err
	}
	reopened, _, _, err = openRuntime(p)
	if err != nil {
		return scenarios, err
	}
	crashAdapter := &fakeAdapter{id: "crash-engine", capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, health: engineruntime.Health{State: engineruntime.HealthHealthy}}
	reconciled, err := reopened.Invoke(context.Background(), crashAdapter, engineruntime.OperationStart, req("exec-crash-result", crashAdapter.id, engineruntime.CapabilityText))
	if err != nil {
		return scenarios, err
	}
	crashPassed := reconciled.Execution.Disposition == engineruntime.DispositionCompleted && crashAdapter.starts == 0
	scenarios = append(scenarios, scenario{Name: "persisted_result_reconciles_after_runtime_crash_without_replay", Passed: crashPassed, Detail: map[string]any{"adapter_calls": crashAdapter.starts, "disposition": reconciled.Execution.Disposition}})
	if !crashPassed {
		return scenarios, errors.New("persisted result crash recovery failed")
	}
	if _, err := manager.UpdateCheck("crash-reconcile", tasks.CheckPassed, "persisted engine result reconciled runtime state after restart without adapter replay"); err != nil {
		return scenarios, err
	}

	coord, err := recovery.Open(p.recovery)
	if err != nil {
		return scenarios, err
	}
	if _, err := coord.Recover(); err != nil {
		return scenarios, err
	}
	afterFirst, err := session.Load(p.recovery.SessionState)
	if err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(p.recovery)
	if err != nil {
		return scenarios, err
	}
	if _, err := coord.Recover(); err != nil {
		return scenarios, err
	}
	afterSecond, err := session.Load(p.recovery.SessionState)
	if err != nil {
		return scenarios, err
	}
	exactlyOncePassed := assistantCount(afterFirst) == 4 && assistantCount(afterSecond) == 4 && markerCount(afterSecond) == 4
	scenarios = append(scenarios, scenario{Name: "completed_results_commit_exactly_once_through_recovery_coordinator", Passed: exactlyOncePassed, Detail: map[string]any{"assistant_messages": assistantCount(afterSecond), "commit_markers": markerCount(afterSecond)}})
	if !exactlyOncePassed {
		return scenarios, errors.New("exactly-once canonical commit failed")
	}
	if _, err := manager.UpdateCheck("exactly-once", tasks.CheckPassed, "four completed runtime results committed once each across repeated recovery"); err != nil {
		return scenarios, err
	}

	coord, err = recovery.Open(p.recovery)
	if err != nil {
		return scenarios, err
	}
	receiptReport, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(p.recovery)
	if err != nil {
		return scenarios, err
	}
	repeatReport, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	receiptPassed := receiptReport.ReceiptID != "" && repeatReport.ReceiptID == receiptReport.ReceiptID
	scenarios = append(scenarios, scenario{Name: "integrated_runtime_proof_receipt_exactly_once", Passed: receiptPassed, Detail: map[string]any{"receipt_id": receiptReport.ReceiptID}})
	if !receiptPassed {
		return scenarios, errors.New("proof receipt was not stable")
	}

	return scenarios, nil
}

func makePaths(dir string) paths {
	return paths{
		runtime: filepath.Join(dir, "runtime-ledger.jsonl"),
		recovery: recovery.Paths{
			TaskEvents:     filepath.Join(dir, "task-events.jsonl"),
			SessionState:   filepath.Join(dir, "session.json"),
			ToolJournal:    filepath.Join(dir, "tool-journal.jsonl"),
			Timeline:       filepath.Join(dir, "timeline.jsonl"),
			ProofReceipts:  filepath.Join(dir, "proof-receipts.jsonl"),
			EngineLedger:   filepath.Join(dir, "engine-ledger.jsonl"),
			ArtifactLedger: filepath.Join(dir, "artifact-ledger.jsonl"),
		},
	}
}

func initializeTaskAndSession(p paths, projectRoot string) (*tasks.Manager, error) {
	taskStore, err := tasks.Open(p.recovery.TaskEvents)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(p.recovery.ToolJournal)
	if err != nil {
		return nil, err
	}
	manager := &tasks.Manager{Store: taskStore, Journal: journal}
	_, err = manager.Create("task-proof13", "session-proof13", tasks.Contract{
		Goal:             "prove one durable engine-neutral runtime contract for replaceable KeyDeck workers",
		RequiredOutcomes: []string{"common lifecycle", "durable bindings", "safe restart dispositions", "exactly-once canonical result commit"},
		ForbiddenScope:   []string{"engine-owned canonical state", "unsafe replay", "health or capability bypass"},
		Checks: []tasks.AcceptanceCheck{
			{ID: "capability-gate", Description: "Required capabilities are checked before engine execution"},
			{ID: "health-gate", Description: "Unhealthy engines are blocked before work begins"},
			{ID: "continue-dispatch", Description: "Continue operation dispatches through the common runtime contract"},
			{ID: "failure-durable", Description: "Engine failure survives restart without replay"},
			{ID: "binding-resume", Description: "Durable external binding survives restart and resumes"},
			{ID: "cancel-durable", Description: "Cancellation survives restart"},
			{ID: "input-required", Description: "Interrupted non-resumable work becomes input_required"},
			{ID: "canonical-owner", Description: "Engine output does not mutate canonical state directly"},
			{ID: "crash-reconcile", Description: "Persisted result reconciles runtime state without replay"},
			{ID: "exactly-once", Description: "Completed runtime results commit to canonical state exactly once"},
		},
	})
	if err != nil {
		return nil, err
	}
	state := session.New("session-proof13", projectRoot, "Proof 0.13 engine-neutral runtime", "keydeck")
	if err := session.Save(p.recovery.SessionState, state); err != nil {
		return nil, err
	}
	return manager, nil
}

func openRuntime(p paths) (*engineruntime.Runtime, *engineruntime.Store, *recovery.EngineStore, error) {
	store, err := engineruntime.Open(p.runtime)
	if err != nil {
		return nil, nil, nil, err
	}
	engines, err := recovery.OpenEngineStore(p.recovery.EngineLedger)
	if err != nil {
		return nil, nil, nil, err
	}
	activity, err := timeline.Open(p.recovery.Timeline)
	if err != nil {
		return nil, nil, nil, err
	}
	runtime, err := engineruntime.New(store, engines, activity)
	return runtime, store, engines, err
}

func req(executionID, engineID string, required ...engineruntime.Capability) engineruntime.Request {
	return engineruntime.Request{
		ExecutionID:          executionID,
		TaskID:               "task-proof13",
		SessionID:            "session-proof13",
		EngineID:             engineID,
		Prompt:               "continue proof",
		Passport:             session.Passport{SessionID: "session-proof13", ToEngine: engineID},
		RequiredCapabilities: required,
	}
}

func assistantCount(state session.State) int {
	count := 0
	for _, message := range state.Transcript {
		if message.Role == session.RoleAssistant {
			count++
		}
	}
	return count
}

func markerCount(state session.State) int {
	count := 0
	for _, action := range state.CompletedActions {
		if len(action.Source) >= len("keydeck-engine-result:") && action.Source[:len("keydeck-engine-result:")] == "keydeck-engine-result:" {
			count++
		}
	}
	return count
}
