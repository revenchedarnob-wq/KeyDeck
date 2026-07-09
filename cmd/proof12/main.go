package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

const crashExitCode = 42

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

type proofPaths struct {
	Recovery recovery.Paths
	Artifact string
}

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--child-phase" {
		if err := runChildPhase(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(crashExitCode)
	}

	dir, err := os.MkdirTemp("", "keydeck-proof12-")
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
		Proof:  "0.12-integrated-recovery-coordinator-exactly-once-canonical-commit",
		Passed: passed,
		Claims: []string{
			"completed destructive tool work is reconciled without replay and remains exactly once across repeated restarts",
			"ambiguous non-repeatable work remains blocked across repeated restarts while idempotent work remains safely retryable",
			"completed engine results survive both pre-commit and post-canonical-save crash windows and commit to canonical state exactly once",
			"durable external threads become resume_required while interrupted non-resumable engines become input_required",
			"task, session, engine ledger, Tool Journal, Activity Timeline, artifacts and Proof Receipt reconcile through one coordinator",
			"recovery decisions and final proof identity are durable timeline evidence",
		},
		Scenarios: scenarios,
	}
	b, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		panic(marshalErr)
	}
	fmt.Println(string(b))
	if !passed {
		os.Exit(1)
	}
}

func runProof(dir string) ([]scenario, error) {
	paths := makePaths(dir)
	manager, taskState, err := initialize(paths, dir)
	if err != nil {
		return nil, err
	}
	scenarios := []scenario{}

	if err := runCrashPhase("tool-crash", dir); err != nil {
		return scenarios, err
	}
	coord, err := recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	firstToolRecovery, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	manager, err = openManager(paths)
	if err != nil {
		return scenarios, err
	}
	afterFirstTool := manager.Store.State()
	timelineBeforeRepeat := countLines(paths.Recovery.Timeline)
	taskEventsBeforeRepeat := countLines(paths.Recovery.TaskEvents)
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	secondToolRecovery, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	manager, err = openManager(paths)
	if err != nil {
		return scenarios, err
	}
	afterSecondTool := manager.Store.State()
	destructivePassed := afterFirstTool.Steps["destructive"].Status == "completed" &&
		afterSecondTool.Steps["destructive"].Status == "completed" &&
		countDisposition(firstToolRecovery, recovery.DispositionReusedCompletedToolResult) == 1 &&
		countLines(paths.Recovery.TaskEvents) == taskEventsBeforeRepeat && countLines(paths.Recovery.Timeline) == timelineBeforeRepeat
	scenarios = append(scenarios, scenario{Name: "completed_destructive_work_exactly_once", Passed: destructivePassed, Detail: map[string]any{
		"step_status":                 afterSecondTool.Steps["destructive"].Status,
		"repeat_task_event_delta":     countLines(paths.Recovery.TaskEvents) - taskEventsBeforeRepeat,
		"repeat_timeline_event_delta": countLines(paths.Recovery.Timeline) - timelineBeforeRepeat,
	}})
	if !destructivePassed {
		return scenarios, errors.New("destructive exactly-once recovery failed")
	}

	ambiguousPassed := afterSecondTool.Steps["ambiguous"].Status == "ambiguous" &&
		afterSecondTool.Status == tasks.StatusInputRequired &&
		countDisposition(firstToolRecovery, recovery.DispositionBlockedAmbiguous) == 1 &&
		countDisposition(secondToolRecovery, recovery.DispositionBlockedAmbiguous) == 1
	scenarios = append(scenarios, scenario{Name: "ambiguous_work_remains_blocked", Passed: ambiguousPassed, Detail: map[string]any{
		"step_status": afterSecondTool.Steps["ambiguous"].Status, "task_status": afterSecondTool.Status, "needs_input": afterSecondTool.NeedsInput,
	}})
	if !ambiguousPassed {
		return scenarios, errors.New("ambiguous work did not remain blocked")
	}

	idempotentPassed := afterSecondTool.Steps["idempotent"].Status == "started" &&
		countDisposition(firstToolRecovery, recovery.DispositionRetryIdempotent) == 1 &&
		countDisposition(secondToolRecovery, recovery.DispositionRetryIdempotent) == 1
	scenarios = append(scenarios, scenario{Name: "idempotent_work_retryable", Passed: idempotentPassed, Detail: map[string]any{"step_status": afterSecondTool.Steps["idempotent"].Status}})
	if !idempotentPassed {
		return scenarios, errors.New("idempotent work was not classified retryable")
	}

	manager, err = openManager(paths)
	if err != nil {
		return scenarios, err
	}
	if _, err := manager.UpdateCheck("destructive-once", tasks.CheckPassed, "completed destructive tool result reused without replay across repeated restart recovery"); err != nil {
		return scenarios, err
	}
	if _, err := manager.UpdateCheck("ambiguous-blocked", tasks.CheckPassed, "ambiguous non-repeatable work remained blocked across repeated restarts"); err != nil {
		return scenarios, err
	}
	if _, err := manager.UpdateCheck("idempotent-retry", tasks.CheckPassed, "interrupted idempotent work remained eligible for safe retry"); err != nil {
		return scenarios, err
	}
	appendCheckEvents(paths, taskState, []string{"destructive-once", "ambiguous-blocked", "idempotent-retry"})

	if err := runCrashPhase("result-before-commit", dir); err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	beforeCommitReport, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	sessionAfterFirstCommit, err := session.Load(paths.Recovery.SessionState)
	if err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	if _, err := coord.Recover(); err != nil {
		return scenarios, err
	}
	sessionAfterFirstRepeat, err := session.Load(paths.Recovery.SessionState)
	if err != nil {
		return scenarios, err
	}
	preCommitWindowPassed := countResultMarkers(sessionAfterFirstCommit, "result-before") == 1 &&
		countResultMarkers(sessionAfterFirstRepeat, "result-before") == 1 &&
		countAssistantText(sessionAfterFirstRepeat, "result survived before canonical commit") == 1 &&
		countDisposition(beforeCommitReport, recovery.DispositionCanonicalCommitApplied) == 1
	if !preCommitWindowPassed {
		return append(scenarios, scenario{Name: "engine_result_precommit_crash_window", Passed: false}), errors.New("pre-commit crash window failed")
	}

	if err := runCrashPhase("result-after-session-save", dir); err != nil {
		return scenarios, err
	}
	preRecoverySession, err := session.Load(paths.Recovery.SessionState)
	if err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	postSaveReport, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	if _, err := coord.Recover(); err != nil {
		return scenarios, err
	}
	postRecoverySession, err := session.Load(paths.Recovery.SessionState)
	if err != nil {
		return scenarios, err
	}
	postSaveWindowPassed := countResultMarkers(preRecoverySession, "result-after-save") == 1 &&
		countResultMarkers(postRecoverySession, "result-after-save") == 1 &&
		countAssistantText(postRecoverySession, "result survived after canonical save") == 1 &&
		countDisposition(postSaveReport, recovery.DispositionCanonicalCommitReconciled) == 1
	engineResultPassed := preCommitWindowPassed && postSaveWindowPassed
	scenarios = append(scenarios, scenario{Name: "engine_result_two_crash_windows_exactly_once", Passed: engineResultPassed, Detail: map[string]any{
		"pre_commit_marker_count": countResultMarkers(postRecoverySession, "result-before"),
		"post_save_marker_count":  countResultMarkers(postRecoverySession, "result-after-save"),
		"assistant_message_count": len(postRecoverySession.Transcript),
	}})
	if !engineResultPassed {
		return scenarios, errors.New("engine result exactly-once recovery failed")
	}
	manager, err = openManager(paths)
	if err != nil {
		return scenarios, err
	}
	if _, err := manager.UpdateCheck("result-once", tasks.CheckPassed, "two engine-result crash windows converged to one canonical commit per result"); err != nil {
		return scenarios, err
	}
	appendCheckEvents(paths, taskState, []string{"result-once"})

	if err := runCrashPhase("engine-interruptions", dir); err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	interruptReport, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	manager, err = openManager(paths)
	if err != nil {
		return scenarios, err
	}
	stateAfterInterrupt := manager.Store.State()
	interruptTimelineLines := countLines(paths.Recovery.Timeline)
	interruptTaskLines := countLines(paths.Recovery.TaskEvents)
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	interruptRepeat, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	resumePassed := countDisposition(interruptReport, recovery.DispositionResumeRequired) == 1 &&
		countDisposition(interruptRepeat, recovery.DispositionResumeRequired) == 1 && countLines(paths.Recovery.Timeline) == interruptTimelineLines
	inputPassed := countDisposition(interruptReport, recovery.DispositionInputRequired) == 1 &&
		countDisposition(interruptRepeat, recovery.DispositionInputRequired) == 1 &&
		stateAfterInterrupt.Status == tasks.StatusInputRequired &&
		strings.Contains(stateAfterInterrupt.NeedsInput, "exec-nonresumable") &&
		countLines(paths.Recovery.TaskEvents) == interruptTaskLines
	scenarios = append(scenarios,
		scenario{Name: "durable_external_thread_resume_required", Passed: resumePassed, Detail: map[string]any{"repeat_timeline_delta": countLines(paths.Recovery.Timeline) - interruptTimelineLines}},
		scenario{Name: "nonresumable_engine_input_required", Passed: inputPassed, Detail: map[string]any{"task_status": stateAfterInterrupt.Status, "needs_input": stateAfterInterrupt.NeedsInput, "repeat_task_event_delta": countLines(paths.Recovery.TaskEvents) - interruptTaskLines}},
	)
	if !resumePassed || !inputPassed {
		return scenarios, errors.New("engine interruption disposition recovery failed")
	}
	manager, err = openManager(paths)
	if err != nil {
		return scenarios, err
	}
	if _, err := manager.UpdateCheck("resume-required", tasks.CheckPassed, "durable external thread classified resume_required with durable timeline evidence"); err != nil {
		return scenarios, err
	}
	if _, err := manager.UpdateCheck("input-required", tasks.CheckPassed, "non-resumable interrupted engine classified input_required across repeated restarts"); err != nil {
		return scenarios, err
	}
	appendCheckEvents(paths, taskState, []string{"resume-required", "input-required"})

	manager, err = openManager(paths)
	if err != nil {
		return scenarios, err
	}
	completedState := manager.Store.State()
	if completedState.Status != tasks.StatusCompleted || !completedState.Progress().Complete {
		return scenarios, fmt.Errorf("task did not reach evidence-based completion: status=%s progress=%+v", completedState.Status, completedState.Progress())
	}
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	finalReport, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	receipts, err := proofreceipt.Open(paths.Recovery.ProofReceipts)
	if err != nil {
		return scenarios, err
	}
	activity, err := timeline.Open(paths.Recovery.Timeline)
	if err != nil {
		return scenarios, err
	}
	coord, err = recovery.Open(paths.Recovery)
	if err != nil {
		return scenarios, err
	}
	repeatFinalReport, err := coord.Recover()
	if err != nil {
		return scenarios, err
	}
	finalReceipts, err := proofreceipt.Open(paths.Recovery.ProofReceipts)
	if err != nil {
		return scenarios, err
	}
	finalActivity, err := timeline.Open(paths.Recovery.Timeline)
	if err != nil {
		return scenarios, err
	}
	integratedPassed := finalReport.ReceiptID != "" && repeatFinalReport.ReceiptID == finalReport.ReceiptID &&
		len(receipts.Snapshot()) == 1 && len(finalReceipts.Snapshot()) == 1 &&
		countProofEvents(activity.Snapshot()) == 1 && countProofEvents(finalActivity.Snapshot()) == 1 &&
		containsRecoveryDomains(finalActivity.Snapshot())
	scenarios = append(scenarios, scenario{Name: "integrated_recovery_unit_and_proof_receipt_exactly_once", Passed: integratedPassed, Detail: map[string]any{
		"receipt_id": finalReport.ReceiptID, "receipt_count": len(finalReceipts.Snapshot()), "proof_event_count": countProofEvents(finalActivity.Snapshot()), "timeline_events": len(finalActivity.Snapshot()),
	}})
	if !integratedPassed {
		return scenarios, errors.New("integrated receipt recovery failed")
	}

	return scenarios, nil
}

func initialize(paths proofPaths, projectRoot string) (*tasks.Manager, tasks.State, error) {
	taskStore, err := tasks.Open(paths.Recovery.TaskEvents)
	if err != nil {
		return nil, tasks.State{}, err
	}
	journal, err := tooljournal.Open(paths.Recovery.ToolJournal)
	if err != nil {
		return nil, tasks.State{}, err
	}
	manager := &tasks.Manager{Store: taskStore, Journal: journal}
	contract := tasks.Contract{
		Goal:             "prove integrated crash recovery and exactly-once canonical commit",
		RequiredOutcomes: []string{"one recovery coordinator", "no destructive replay", "exactly-once engine result commit", "durable recovery dispositions"},
		ForbiddenScope:   []string{"blind replay", "duplicate canonical result", "lost recovery evidence", "model-guessed completion"},
		Checks: []tasks.AcceptanceCheck{
			{ID: "destructive-once", Description: "Completed destructive work is reconciled without replay"},
			{ID: "ambiguous-blocked", Description: "Ambiguous non-repeatable work remains blocked across restarts"},
			{ID: "idempotent-retry", Description: "Interrupted idempotent work remains safely retryable"},
			{ID: "result-once", Description: "Completed engine results commit to canonical state exactly once across crash windows"},
			{ID: "resume-required", Description: "Durable external threads become resume_required"},
			{ID: "input-required", Description: "Interrupted non-resumable engines become input_required"},
		},
	}
	state, err := manager.Create("task-proof12", "session-proof12", contract)
	if err != nil {
		return nil, tasks.State{}, err
	}
	sessionState := session.New(state.SessionID, projectRoot, contract.Goal, "api")
	if err := session.Save(paths.Recovery.SessionState, sessionState); err != nil {
		return nil, tasks.State{}, err
	}
	activity, err := timeline.Open(paths.Recovery.Timeline)
	if err != nil {
		return nil, tasks.State{}, err
	}
	if _, _, err := activity.AppendOnce(timeline.Input{EventID: "proof12-task-created", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTask, Kind: "task_created", SourceRef: "task-events.jsonl#1", Summary: "Proof 0.12 Task Contract created"}); err != nil {
		return nil, tasks.State{}, err
	}
	artifactBody := []byte("KeyDeck Proof 0.12 integrated recovery artifact\n")
	if err := os.WriteFile(paths.Artifact, artifactBody, 0o600); err != nil {
		return nil, tasks.State{}, err
	}
	sum := sha256.Sum256(artifactBody)
	artifacts, err := recovery.OpenArtifactStore(paths.Recovery.ArtifactLedger)
	if err != nil {
		return nil, tasks.State{}, err
	}
	record := recovery.ArtifactRecord{ArtifactID: "artifact-proof12", TaskID: state.TaskID, SessionID: state.SessionID, Name: filepath.Base(paths.Artifact), Path: filepath.Base(paths.Artifact), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(artifactBody))}
	if _, _, err := artifacts.SaveOnce(record); err != nil {
		return nil, tasks.State{}, err
	}
	if _, _, err := activity.AppendOnce(timeline.Input{EventID: "proof12-artifact", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainArtifact, Kind: "artifact_registered", SourceRef: record.ArtifactID, Summary: "Recovery proof artifact registered", DataHash: record.SHA256}); err != nil {
		return nil, tasks.State{}, err
	}
	return manager, state, nil
}

func openManager(paths proofPaths) (*tasks.Manager, error) {
	taskStore, err := tasks.Open(paths.Recovery.TaskEvents)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(paths.Recovery.ToolJournal)
	if err != nil {
		return nil, err
	}
	return &tasks.Manager{Store: taskStore, Journal: journal}, nil
}

func runChildPhase(phase, dir string) error {
	paths := makePaths(dir)
	switch phase {
	case "tool-crash":
		taskStore, err := tasks.Open(paths.Recovery.TaskEvents)
		if err != nil {
			return err
		}
		journal, err := tooljournal.Open(paths.Recovery.ToolJournal)
		if err != nil {
			return err
		}
		manager := &tasks.Manager{Store: taskStore, Journal: journal}
		if _, _, err := manager.BeginStep("destructive", "op-destructive", "delete_deployment", []byte(`{"deployment":"demo"}`), tooljournal.ReplayForbidden); err != nil {
			return err
		}
		if err := journal.Complete("op-destructive", "deployment removed"); err != nil {
			return err
		}
		if _, _, err := manager.BeginStep("ambiguous", "op-ambiguous", "charge_card", []byte(`{"invoice":"demo"}`), tooljournal.ReplayForbidden); err != nil {
			return err
		}
		if _, _, err := manager.BeginStep("idempotent", "op-idempotent", "write_file", []byte(`{"path":"demo.txt"}`), tooljournal.ReplayIdempotent); err != nil {
			return err
		}
		return nil
	case "result-before-commit":
		coord, err := recovery.Open(paths.Recovery)
		if err != nil {
			return err
		}
		execution := recovery.Execution{ExecutionID: "exec-result-before", TaskID: "task-proof12", SessionID: "session-proof12", Engine: "codex", Resumable: true, ExternalThreadID: "thread-result-before"}
		if _, _, err := coord.EngineStore().StartOnce(execution); err != nil {
			return err
		}
		_, _, err = coord.EngineStore().CompleteResultOnce(recovery.Result{ResultID: "result-before", ExecutionID: execution.ExecutionID, TaskID: execution.TaskID, SessionID: execution.SessionID, Engine: execution.Engine, ExternalThreadID: execution.ExternalThreadID, Output: session.EngineResult{Text: "result survived before canonical commit", Decisions: []string{"commit from durable result ledger"}, CompletedActions: []string{"phase one complete"}, RelevantFiles: []string{"recovery.go"}, Checkpoint: "checkpoint-before"}})
		return err
	case "result-after-session-save":
		coord, err := recovery.Open(paths.Recovery)
		if err != nil {
			return err
		}
		execution := recovery.Execution{ExecutionID: "exec-result-after-save", TaskID: "task-proof12", SessionID: "session-proof12", Engine: "codex", Resumable: true, ExternalThreadID: "thread-result-after-save"}
		if _, _, err := coord.EngineStore().StartOnce(execution); err != nil {
			return err
		}
		result, _, err := coord.EngineStore().CompleteResultOnce(recovery.Result{ResultID: "result-after-save", ExecutionID: execution.ExecutionID, TaskID: execution.TaskID, SessionID: execution.SessionID, Engine: execution.Engine, ExternalThreadID: execution.ExternalThreadID, Output: session.EngineResult{Text: "result survived after canonical save", Decisions: []string{"detect canonical marker after restart"}, CompletedActions: []string{"phase two complete"}, RelevantFiles: []string{"session.json"}, Checkpoint: "checkpoint-after-save"}})
		if err != nil {
			return err
		}
		state, err := session.Load(paths.Recovery.SessionState)
		if err != nil {
			return err
		}
		updated, applied := recovery.ApplyResultToSession(state, result)
		if !applied {
			return errors.New("expected canonical result application before simulated crash")
		}
		return session.Save(paths.Recovery.SessionState, updated)
	case "engine-interruptions":
		coord, err := recovery.Open(paths.Recovery)
		if err != nil {
			return err
		}
		if _, _, err := coord.EngineStore().StartOnce(recovery.Execution{ExecutionID: "exec-resumable", TaskID: "task-proof12", SessionID: "session-proof12", Engine: "codex", Resumable: true, ExternalThreadID: "thread-resumable"}); err != nil {
			return err
		}
		_, _, err = coord.EngineStore().StartOnce(recovery.Execution{ExecutionID: "exec-nonresumable", TaskID: "task-proof12", SessionID: "session-proof12", Engine: "ephemeral-worker", Resumable: false})
		return err
	default:
		return fmt.Errorf("unknown child phase %q", phase)
	}
}

func runCrashPhase(phase, dir string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "--child-phase", phase, dir)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return fmt.Errorf("phase %s unexpectedly exited successfully", phase)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != crashExitCode {
		return fmt.Errorf("phase %s failed unexpectedly: %v output=%s", phase, err, string(output))
	}
	return nil
}

func makePaths(dir string) proofPaths {
	return proofPaths{Recovery: recovery.Paths{
		TaskEvents: filepath.Join(dir, "task-events.jsonl"), SessionState: filepath.Join(dir, "session.json"),
		ToolJournal: filepath.Join(dir, "tool-journal.jsonl"), Timeline: filepath.Join(dir, "activity-timeline.jsonl"),
		ProofReceipts: filepath.Join(dir, "proof-receipts.jsonl"), EngineLedger: filepath.Join(dir, "engine-ledger.jsonl"),
		ArtifactLedger: filepath.Join(dir, "artifact-ledger.jsonl"),
	}, Artifact: filepath.Join(dir, "recovery-artifact.txt")}
}

func appendCheckEvents(paths proofPaths, state tasks.State, checkIDs []string) {
	activity, err := timeline.Open(paths.Recovery.Timeline)
	if err != nil {
		panic(err)
	}
	for _, checkID := range checkIDs {
		_, _, err := activity.AppendOnce(timeline.Input{EventID: "proof12-check-" + checkID, TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTask, Kind: "acceptance_check_passed", SourceRef: "check:" + checkID, Summary: "Acceptance check passed"})
		if err != nil {
			panic(err)
		}
	}
}

func countDisposition(report recovery.Report, disposition recovery.Disposition) int {
	count := 0
	for _, decision := range report.Decisions {
		if decision.Disposition == disposition {
			count++
		}
	}
	return count
}

func countResultMarkers(state session.State, resultID string) int {
	marker := recovery.ResultCommitMarker(resultID)
	count := 0
	for _, action := range state.CompletedActions {
		if action.Source == marker {
			count++
		}
	}
	return count
}

func countAssistantText(state session.State, text string) int {
	count := 0
	for _, message := range state.Transcript {
		if message.Role == session.RoleAssistant && message.Text == text {
			count++
		}
	}
	return count
}

func countProofEvents(events []timeline.Event) int {
	count := 0
	for _, event := range events {
		if event.Domain == timeline.DomainProof && event.Kind == "proof_receipt_generated" {
			count++
		}
	}
	return count
}

func containsRecoveryDomains(events []timeline.Event) bool {
	set := map[timeline.Domain]bool{}
	for _, event := range events {
		set[event.Domain] = true
	}
	for _, domain := range []timeline.Domain{timeline.DomainTask, timeline.DomainEngine, timeline.DomainTool, timeline.DomainArtifact, timeline.DomainProof} {
		if !set[domain] {
			return false
		}
	}
	return canonicalOrder(events)
}

func canonicalOrder(events []timeline.Event) bool {
	sorted := append([]timeline.Event(nil), events...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Sequence < sorted[j].Sequence })
	for i, event := range sorted {
		if event.Sequence != uint64(i+1) || event.EventID == "" {
			return false
		}
	}
	return true
}

func countLines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	text := strings.TrimSpace(string(b))
	if text == "" {
		return 0
	}
	return len(strings.Split(text, "\n"))
}
