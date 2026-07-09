package proofreceipt

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

func testState() tasks.State {
	now := time.Now().UTC()
	return tasks.State{
		TaskID: "task-1", SessionID: "session-1", Status: tasks.StatusCompleted,
		Contract:  tasks.Contract{Goal: "prove receipts", Checks: []tasks.AcceptanceCheck{{ID: "check-1", Description: "proof", Status: tasks.CheckPassed, Evidence: "test passed"}}},
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestBuildReceiptIsDeterministicAndHumanReadable(t *testing.T) {
	now := time.Now().UTC()
	events := []timeline.Event{{Sequence: 1, At: now, EventID: "evt-1", TaskID: "task-1", SessionID: "session-1", Domain: timeline.DomainTask, Kind: "task_created"}}
	first, err := Build(testState(), events, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(testState(), events, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReceiptID != second.ReceiptID || first.InputDigest != second.InputDigest {
		t.Fatalf("receipt identity changed: first=%+v second=%+v", first, second)
	}
	md := first.Markdown()
	if !strings.Contains(md, "Verified progress") || !strings.Contains(md, "test passed") {
		t.Fatalf("markdown missing evidence: %s", md)
	}
}

func TestBuildRejectsSecretLikeEvidence(t *testing.T) {
	state := testState()
	state.Contract.Checks[0].Evidence = "api_key=TEST_VALUE_123456789"
	_, err := Build(state, nil, nil)
	if !errors.Is(err, ErrSecretDetected) {
		t.Fatalf("expected secret rejection, got %v", err)
	}
}

func TestReceiptStoreSaveOnceSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	receipt, err := Build(testState(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, appended, err := store.SaveOnce(receipt); err != nil || !appended {
		t.Fatalf("first save: appended=%v err=%v", appended, err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, appended, err := reopened.SaveOnce(receipt); err != nil || appended || len(reopened.Snapshot()) != 1 {
		t.Fatalf("restart save: appended=%v err=%v count=%d", appended, err, len(reopened.Snapshot()))
	}
}

func TestBuildRedactedIncludesSafeTimelineSummary(t *testing.T) {
	secret := "proof21-sensitive-value-123456"
	now := time.Now().UTC()
	events := []timeline.Event{{Sequence: 1, At: now, EventID: "evt-secret", TaskID: "task-1", SessionID: "session-1", Domain: timeline.DomainTool, Kind: "tool_error", SourceRef: "secure.fetch", Summary: "upstream rejected " + secret}}
	receipt, err := BuildRedacted(testState(), events, nil, []string{secret})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	combined := string(raw) + receipt.Markdown()
	if strings.Contains(combined, secret) {
		t.Fatalf("redacted receipt leaked sensitive value: %s", combined)
	}
	if !strings.Contains(combined, "[REDACTED_SECRET]") {
		t.Fatalf("redacted receipt should preserve redaction marker: %s", combined)
	}
}
