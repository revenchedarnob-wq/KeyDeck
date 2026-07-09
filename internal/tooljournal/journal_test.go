package tooljournal

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCompletedNonReplayableOperationReturnsPreviousResultAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := j.Begin("op-1", "delete_files", []byte(`{"paths":["a.tmp"]}`), ReplayForbidden)
	if err != nil || decision.Kind != DecisionExecute {
		t.Fatalf("unexpected first decision: %#v err=%v", decision, err)
	}
	if err := j.Complete("op-1", "deleted 1 file"); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	decision, err = reopened.Begin("op-1", "delete_files", []byte(`{"paths":["a.tmp"]}`), ReplayForbidden)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != DecisionReturnPrevious || decision.Result != "deleted 1 file" {
		t.Fatalf("completed action would be replayed instead of reused: %#v", decision)
	}
}

func TestInterruptedNonReplayableOperationRequiresResolutionAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Begin("op-2", "charge_card", []byte(`{"amount":100}`), ReplayForbidden); err != nil {
		t.Fatal(err)
	}
	// Simulate crash: no Complete/Fail record is written.
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reopened.Begin("op-2", "charge_card", []byte(`{"amount":100}`), ReplayForbidden)
	if !errors.Is(err, ErrAmbiguousOperation) {
		t.Fatalf("expected ambiguous operation stop, got %v", err)
	}
}

func TestInterruptedIdempotentOperationMayRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Begin("op-3", "read_file", []byte(`{"path":"README.md"}`), ReplayIdempotent); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reopened.Begin("op-3", "read_file", []byte(`{"path":"README.md"}`), ReplayIdempotent)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != DecisionExecute {
		t.Fatalf("idempotent interrupted action should be retryable: %#v", decision)
	}
}

func TestOperationIDCollisionIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-journal.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Begin("op-4", "write_file", []byte(`{"path":"a"}`), ReplayForbidden); err != nil {
		t.Fatal(err)
	}
	_, err = j.Begin("op-4", "write_file", []byte(`{"path":"b"}`), ReplayForbidden)
	if !errors.Is(err, ErrOperationCollision) {
		t.Fatalf("expected collision, got %v", err)
	}
}
