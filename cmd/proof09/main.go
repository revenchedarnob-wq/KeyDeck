package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type scenarioReport struct {
	Name   string      `json:"name"`
	Passed bool        `json:"passed"`
	Detail interface{} `json:"detail"`
}

func main() {
	dir, err := os.MkdirTemp("", "keydeck-proof09-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	reports := []scenarioReport{
		completedToolCrashScenario(filepath.Join(dir, "completed")),
		ambiguousToolCrashScenario(filepath.Join(dir, "ambiguous")),
		idempotentRetryScenario(filepath.Join(dir, "idempotent")),
		progressProofScenario(filepath.Join(dir, "progress")),
	}
	passed := true
	for _, r := range reports {
		if !r.Passed {
			passed = false
		}
	}
	report := map[string]any{
		"proof":     "0.9-durable-task-contract-progress-proof",
		"passed":    passed,
		"scenarios": reports,
		"claims": []string{
			"completed non-repeatable tool actions are reconciled after crash without replay",
			"ambiguous non-repeatable actions block automatic recovery",
			"idempotent interrupted actions may retry",
			"progress percentage is derived only from acceptance checks",
			"task completion survives restart",
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if !passed {
		os.Exit(1)
	}
}

func setup(dir string) (*tasks.Manager, string, string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		panic(err)
	}
	eventsPath := filepath.Join(dir, "task-events.jsonl")
	journalPath := filepath.Join(dir, "tool-journal.jsonl")
	store, err := tasks.Open(eventsPath)
	if err != nil {
		panic(err)
	}
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		panic(err)
	}
	m := &tasks.Manager{Store: store, Journal: journal}
	_, err = m.Create("task-proof09", "session-proof09", tasks.Contract{
		Goal:             "finish a durable KeyDeck task safely",
		RequiredOutcomes: []string{"safe crash recovery", "evidence-based progress"},
		ForbiddenScope:   []string{"blind replay", "model-guessed completion percentage"},
		Checks: []tasks.AcceptanceCheck{
			{ID: "safe-recovery", Description: "Recovery does not replay completed or ambiguous destructive work"},
			{ID: "idempotent-retry", Description: "Interrupted idempotent work can retry"},
			{ID: "progress-proof", Description: "Progress comes from acceptance evidence"},
			{ID: "restart-proof", Description: "Completion survives restart"},
		},
	})
	if err != nil {
		panic(err)
	}
	return m, eventsPath, journalPath
}

func completedToolCrashScenario(dir string) scenarioReport {
	m, eventsPath, journalPath := setup(dir)
	_, _, err := m.BeginStep("delete", "op-delete", "delete_files", []byte(`{"path":"generated.tmp"}`), tooljournal.ReplayForbidden)
	if err != nil {
		return scenarioReport{Name: "completed_tool_crash_window", Detail: err.Error()}
	}
	if err := m.Journal.Complete("op-delete", "deleted generated.tmp"); err != nil {
		return scenarioReport{Name: "completed_tool_crash_window", Detail: err.Error()}
	}
	store, _ := tasks.Open(eventsPath)
	journal, _ := tooljournal.Open(journalPath)
	restarted := &tasks.Manager{Store: store, Journal: journal}
	decision, state, err := restarted.BeginStep("delete", "op-delete", "delete_files", []byte(`{"path":"generated.tmp"}`), tooljournal.ReplayForbidden)
	passed := err == nil && decision.Kind == tooljournal.DecisionReturnPrevious && state.Steps["delete"].Status == "completed"
	return scenarioReport{Name: "completed_tool_crash_window", Passed: passed, Detail: map[string]any{"decision": decision, "state": state}}
}

func ambiguousToolCrashScenario(dir string) scenarioReport {
	m, eventsPath, journalPath := setup(dir)
	_, _, err := m.BeginStep("charge", "op-charge", "charge_card", []byte(`{"amount":100}`), tooljournal.ReplayForbidden)
	if err != nil {
		return scenarioReport{Name: "ambiguous_nonrepeatable_crash", Detail: err.Error()}
	}
	store, _ := tasks.Open(eventsPath)
	journal, _ := tooljournal.Open(journalPath)
	restarted := &tasks.Manager{Store: store, Journal: journal}
	_, state, err := restarted.BeginStep("charge", "op-charge", "charge_card", []byte(`{"amount":100}`), tooljournal.ReplayForbidden)
	passed := errors.Is(err, tooljournal.ErrAmbiguousOperation) && state.Status == tasks.StatusInputRequired && state.Steps["charge"].Status == "ambiguous"
	return scenarioReport{Name: "ambiguous_nonrepeatable_crash", Passed: passed, Detail: map[string]any{"error": fmt.Sprint(err), "state": state}}
}

func idempotentRetryScenario(dir string) scenarioReport {
	m, eventsPath, journalPath := setup(dir)
	_, _, err := m.BeginStep("read", "op-read", "read_file", []byte(`{"path":"README.md"}`), tooljournal.ReplayIdempotent)
	if err != nil {
		return scenarioReport{Name: "idempotent_retry_after_crash", Detail: err.Error()}
	}
	store, _ := tasks.Open(eventsPath)
	journal, _ := tooljournal.Open(journalPath)
	restarted := &tasks.Manager{Store: store, Journal: journal}
	decision, state, err := restarted.BeginStep("read", "op-read", "read_file", []byte(`{"path":"README.md"}`), tooljournal.ReplayIdempotent)
	passed := err == nil && decision.Kind == tooljournal.DecisionExecute && state.Status == tasks.StatusWorking
	return scenarioReport{Name: "idempotent_retry_after_crash", Passed: passed, Detail: map[string]any{"decision": decision, "state": state}}
}

func progressProofScenario(dir string) scenarioReport {
	m, eventsPath, _ := setup(dir)
	state, _ := m.UpdateCheck("safe-recovery", tasks.CheckPassed, "completed and ambiguous recovery scenarios passed")
	state, _ = m.UpdateCheck("idempotent-retry", tasks.CheckPassed, "idempotent retry scenario passed")
	half := state.Progress()
	state, _ = m.UpdateCheck("progress-proof", tasks.CheckPassed, "2 of 4 checks measured as 50 percent before this check")
	state, _ = m.UpdateCheck("restart-proof", tasks.CheckPassed, "event log replayed after restart")
	reopened, err := tasks.Open(eventsPath)
	if err != nil {
		return scenarioReport{Name: "acceptance_based_progress", Detail: err.Error()}
	}
	final := reopened.State()
	progress := final.Progress()
	passed := half.PassedChecks == 2 && half.TotalChecks == 4 && half.Percent == 50 && !half.Complete && final.Status == tasks.StatusCompleted && progress.Complete && progress.Percent == 100
	return scenarioReport{Name: "acceptance_based_progress", Passed: passed, Detail: map[string]any{"mid_progress": half, "final_progress": progress, "final_state": final}}
}
