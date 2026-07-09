package tasks

import (
	"errors"
	"math"
	"path/filepath"
	"testing"

	"keydeck.local/feasibilitylab/internal/tooljournal"
)

func newManager(t *testing.T) (*Manager, string, string) {
	t.Helper()
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "task-events.jsonl")
	journalPath := filepath.Join(dir, "tool-journal.jsonl")
	store, err := Open(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{Store: store, Journal: journal}
	_, err = m.Create("task-1", "session-1", Contract{Goal: "ship safely", Checks: []AcceptanceCheck{
		{ID: "c1", Description: "first"}, {ID: "c2", Description: "second"}, {ID: "c3", Description: "third"}, {ID: "c4", Description: "fourth"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return m, eventsPath, journalPath
}

func TestProgressComesOnlyFromAcceptanceChecks(t *testing.T) {
	m, _, _ := newManager(t)
	if _, err := m.UpdateCheck("c1", CheckPassed, "proof-1"); err != nil {
		t.Fatal(err)
	}
	state, err := m.UpdateCheck("c2", CheckPassed, "proof-2")
	if err != nil {
		t.Fatal(err)
	}
	progress := state.Progress()
	if progress.PassedChecks != 2 || progress.TotalChecks != 4 || math.Abs(progress.Percent-50) > 0.001 || progress.Complete {
		t.Fatalf("unexpected progress: %#v", progress)
	}
	if state.Status == StatusCompleted {
		t.Fatal("task completed before all acceptance checks passed")
	}
}

func TestCompletedNonReplayableToolIsReconciledAfterCrash(t *testing.T) {
	m, eventsPath, journalPath := newManager(t)
	decision, _, err := m.BeginStep("delete", "op-delete", "delete_files", []byte(`{"path":"x.tmp"}`), tooljournal.ReplayForbidden)
	if err != nil || decision.Kind != tooljournal.DecisionExecute {
		t.Fatalf("begin: %#v %v", decision, err)
	}
	if err := m.Journal.Complete("op-delete", "deleted x.tmp"); err != nil {
		t.Fatal(err)
	}
	// Crash window: journal committed, task step completion event not committed.
	store, err := Open(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Manager{Store: store, Journal: journal}
	decision, state, err := restarted.BeginStep("delete", "op-delete", "delete_files", []byte(`{"path":"x.tmp"}`), tooljournal.ReplayForbidden)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != tooljournal.DecisionReturnPrevious {
		t.Fatalf("expected reuse, got %#v", decision)
	}
	if state.Steps["delete"].Status != "completed" || state.Steps["delete"].Result != "deleted x.tmp" {
		t.Fatalf("not reconciled: %#v", state.Steps["delete"])
	}
}

func TestInterruptedNonReplayableToolBlocksTaskAfterRestart(t *testing.T) {
	m, eventsPath, journalPath := newManager(t)
	if _, _, err := m.BeginStep("charge", "op-charge", "charge_card", []byte(`{"amount":100}`), tooljournal.ReplayForbidden); err != nil {
		t.Fatal(err)
	}
	store, err := Open(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Manager{Store: store, Journal: journal}
	_, state, err := restarted.BeginStep("charge", "op-charge", "charge_card", []byte(`{"amount":100}`), tooljournal.ReplayForbidden)
	if !errors.Is(err, tooljournal.ErrAmbiguousOperation) {
		t.Fatalf("expected ambiguity, got %v", err)
	}
	if state.Status != StatusInputRequired || state.Steps["charge"].Status != "ambiguous" {
		t.Fatalf("unsafe recovery state: %#v", state)
	}
}

func TestInterruptedIdempotentToolMayRetryAfterRestart(t *testing.T) {
	m, eventsPath, journalPath := newManager(t)
	if _, _, err := m.BeginStep("read", "op-read", "read_file", []byte(`{"path":"README.md"}`), tooljournal.ReplayIdempotent); err != nil {
		t.Fatal(err)
	}
	store, err := Open(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Manager{Store: store, Journal: journal}
	decision, state, err := restarted.BeginStep("read", "op-read", "read_file", []byte(`{"path":"README.md"}`), tooljournal.ReplayIdempotent)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != tooljournal.DecisionExecute || state.Status != StatusWorking {
		t.Fatalf("idempotent retry blocked: %#v %#v", decision, state)
	}
}

func TestAllChecksPassedCompletesTaskDurably(t *testing.T) {
	m, eventsPath, _ := newManager(t)
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		if _, err := m.UpdateCheck(id, CheckPassed, "evidence-"+id); err != nil {
			t.Fatal(err)
		}
	}
	state := m.Store.State()
	if state.Status != StatusCompleted || !state.Progress().Complete {
		t.Fatalf("task not completed: %#v", state)
	}
	reopened, err := Open(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.State().Status != StatusCompleted || !reopened.State().Progress().Complete {
		t.Fatal("completion did not survive restart")
	}
}
