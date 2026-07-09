package recovery

import (
	"path/filepath"
	"testing"

	"keydeck.local/feasibilitylab/internal/session"
)

func TestApplyResultToSessionExactlyOnce(t *testing.T) {
	state := session.New("session-1", t.TempDir(), "test recovery", "api")
	result := Result{
		ResultID: "result-1", ExecutionID: "execution-1", TaskID: "task-1", SessionID: state.SessionID,
		Engine: "codex", ExternalThreadID: "thread-1",
		Output: session.EngineResult{Text: "done", Decisions: []string{"keep state canonical"}, CompletedActions: []string{"edited file"}, RelevantFiles: []string{"main.go"}},
	}
	first, applied := ApplyResultToSession(state, result)
	if !applied {
		t.Fatal("expected first canonical application")
	}
	second, applied := ApplyResultToSession(first, result)
	if applied {
		t.Fatal("result applied twice")
	}
	if len(second.Transcript) != 1 || len(second.Decisions) != 1 {
		t.Fatalf("unexpected duplicate canonical state: transcript=%d decisions=%d", len(second.Transcript), len(second.Decisions))
	}
	if !HasCanonicalResult(second, result.ResultID) {
		t.Fatal("missing canonical commit marker")
	}
}

func TestEngineStoreSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine-ledger.jsonl")
	store, err := OpenEngineStore(path)
	if err != nil {
		t.Fatal(err)
	}
	execution := Execution{ExecutionID: "exec-1", TaskID: "task-1", SessionID: "session-1", Engine: "codex", Resumable: true, ExternalThreadID: "thread-1"}
	if _, appended, err := store.StartOnce(execution); err != nil || !appended {
		t.Fatalf("start: appended=%v err=%v", appended, err)
	}
	result := Result{ResultID: "result-1", ExecutionID: execution.ExecutionID, TaskID: execution.TaskID, SessionID: execution.SessionID, Engine: execution.Engine, Output: session.EngineResult{Text: "done"}}
	if _, appended, err := store.CompleteResultOnce(result); err != nil || !appended {
		t.Fatalf("complete: appended=%v err=%v", appended, err)
	}

	reopened, err := OpenEngineStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Result(result.ResultID).CanonicalCommitted {
		t.Fatal("result unexpectedly committed")
	}
	if _, appended, err := reopened.MarkCommittedOnce(result.ResultID); err != nil || !appended {
		t.Fatalf("commit: appended=%v err=%v", appended, err)
	}
	final, err := OpenEngineStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Result(result.ResultID).CanonicalCommitted {
		t.Fatal("commit did not survive reopen")
	}
}
