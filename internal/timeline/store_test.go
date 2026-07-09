package timeline

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAppendOnceSurvivesRestartWithoutDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{EventID: "evt-1", TaskID: "task-1", SessionID: "session-1", Domain: DomainTask, Kind: "task_created"}
	first, appended, err := store.AppendOnce(input)
	if err != nil || !appended || first.Sequence != 1 {
		t.Fatalf("first append: event=%+v appended=%v err=%v", first, appended, err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again, appended, err := reopened.AppendOnce(input)
	if err != nil || appended || again.Sequence != 1 || len(reopened.Snapshot()) != 1 {
		t.Fatalf("restart append: event=%+v appended=%v err=%v count=%d", again, appended, err, len(reopened.Snapshot()))
	}
}

func TestAppendOnceRejectsConflictingEventID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	base := Input{EventID: "evt-1", TaskID: "task-1", SessionID: "session-1", Domain: DomainTask, Kind: "task_created"}
	if _, _, err := store.AppendOnce(base); err != nil {
		t.Fatal(err)
	}
	base.Summary = "different"
	if _, _, err := store.AppendOnce(base); !errors.Is(err, ErrEventIDConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}
