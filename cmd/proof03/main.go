package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"keydeck.local/feasibilitylab/internal/tooljournal"
)

func main() {
	dir, err := os.MkdirTemp("", "keydeck-proof03-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "tool-journal.jsonl")

	j, err := tooljournal.Open(path)
	if err != nil {
		panic(err)
	}
	first, err := j.Begin("op-delete-1", "delete_files", []byte(`{"paths":["generated.tmp"]}`), tooljournal.ReplayForbidden)
	if err != nil {
		panic(err)
	}
	if err := j.Complete("op-delete-1", "deleted generated.tmp"); err != nil {
		panic(err)
	}

	restarted, err := tooljournal.Open(path)
	if err != nil {
		panic(err)
	}
	second, secondErr := restarted.Begin("op-delete-1", "delete_files", []byte(`{"paths":["generated.tmp"]}`), tooljournal.ReplayForbidden)

	ambiguousStart, err := restarted.Begin("op-charge-1", "charge_card", []byte(`{"amount":100}`), tooljournal.ReplayForbidden)
	if err != nil {
		panic(err)
	}
	restartedAgain, err := tooljournal.Open(path)
	if err != nil {
		panic(err)
	}
	_, ambiguousErr := restartedAgain.Begin("op-charge-1", "charge_card", []byte(`{"amount":100}`), tooljournal.ReplayForbidden)

	report := map[string]any{
		"proof":                              "0.3-tool-journal-replay-safety",
		"passed":                             first.Kind == tooljournal.DecisionExecute && secondErr == nil && second.Kind == tooljournal.DecisionReturnPrevious && ambiguousStart.Kind == tooljournal.DecisionExecute && errors.Is(ambiguousErr, tooljournal.ErrAmbiguousOperation),
		"completed_operation_first_decision": first,
		"completed_operation_after_restart":  second,
		"ambiguous_operation_first_decision": ambiguousStart,
		"ambiguous_operation_after_restart":  errorString(ambiguousErr),
		"journal_state":                      restartedAgain.Snapshot(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if !report["passed"].(bool) {
		os.Exit(1)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
