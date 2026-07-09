package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/proofreceipt"
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

func main() {
	dir, err := os.MkdirTemp("", "keydeck-proof11-")
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
		Proof:  "0.11-universal-activity-timeline-proof-receipts",
		Passed: passed,
		Claims: []string{
			"task, engine, tool, artifact and proof events share one durable append-only timeline identity",
			"timeline event IDs survive restart without duplication and preserve canonical ordering",
			"human-readable Proof Receipts are generated from acceptance evidence, timeline references and artifact hashes",
			"secret-like evidence is rejected before receipt creation",
			"receipt inputs and proof receipt identity survive restart exactly once",
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
	taskEventsPath := filepath.Join(dir, "task-events.jsonl")
	journalPath := filepath.Join(dir, "tool-journal.jsonl")
	timelinePath := filepath.Join(dir, "activity-timeline.jsonl")
	receiptStorePath := filepath.Join(dir, "proof-receipts.jsonl")
	artifactPath := filepath.Join(dir, "proof-artifact.txt")
	receiptMarkdownPath := filepath.Join(dir, "Proof-0.11-Receipt.md")

	taskStore, err := tasks.Open(taskEventsPath)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		return nil, err
	}
	manager := &tasks.Manager{Store: taskStore, Journal: journal}
	contract := tasks.Contract{
		Goal:             "prove one durable KeyDeck activity timeline and evidence-based Proof Receipt",
		RequiredOutcomes: []string{"canonical cross-domain ordering", "human-readable evidence receipt", "restart exact-once behavior"},
		ForbiddenScope:   []string{"duplicate timeline events", "model-guessed proof", "secrets in receipts"},
		Checks: []tasks.AcceptanceCheck{
			{ID: "timeline-identity", Description: "All required activity domains share one ordered durable timeline"},
			{ID: "proof-receipt", Description: "Receipt is generated from task evidence, timeline references and artifact hashes"},
			{ID: "secret-safety", Description: "Secret-like material is rejected before receipt creation"},
			{ID: "restart-exactly-once", Description: "Restart replay preserves timeline and receipt inputs exactly once"},
		},
	}
	state, err := manager.Create("task-proof11", "session-proof11", contract)
	if err != nil {
		return nil, err
	}

	activity, err := timeline.Open(timelinePath)
	if err != nil {
		return nil, err
	}

	artifactBody := []byte("KeyDeck Proof 0.11 durable artifact evidence\n")
	if err := os.WriteFile(artifactPath, artifactBody, 0o600); err != nil {
		return nil, err
	}
	artifactHash := sha256.Sum256(artifactBody)
	artifact := proofreceipt.Artifact{
		Name: "proof-artifact.txt", Path: filepath.Base(artifactPath),
		SHA256: hex.EncodeToString(artifactHash[:]), Size: int64(len(artifactBody)),
	}

	baseInputs := []timeline.Input{
		{EventID: "evt-task-created", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTask, Kind: "task_created", SourceRef: "task-events.jsonl#1", Summary: "Task Contract created"},
		{EventID: "evt-engine-started", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainEngine, Kind: "engine_started", SourceRef: "engine:fixture", Summary: "Proof worker started"},
		{EventID: "evt-tool-started", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTool, Kind: "tool_started", SourceRef: "operation:write-proof-artifact", Summary: "Artifact write started"},
		{EventID: "evt-tool-completed", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTool, Kind: "tool_completed", SourceRef: "operation:write-proof-artifact", Summary: "Artifact write completed"},
		{EventID: "evt-artifact-produced", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainArtifact, Kind: "artifact_produced", SourceRef: artifact.Path, Summary: "Proof artifact produced", DataHash: artifact.SHA256},
	}
	for _, input := range baseInputs {
		if _, appended, err := activity.AppendOnce(input); err != nil || !appended {
			return nil, fmt.Errorf("append initial event %s: appended=%v err=%w", input.EventID, appended, err)
		}
	}

	scenarios := []scenario{}

	state, err = manager.UpdateCheck("timeline-identity", tasks.CheckPassed, "five event types entered one durable sequence before restart")
	if err != nil {
		return scenarios, err
	}
	checkEvent := timeline.Input{EventID: "evt-check-timeline", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTask, Kind: "acceptance_check_passed", SourceRef: "check:timeline-identity", Summary: "Timeline identity check passed"}
	if _, _, err := activity.AppendOnce(checkEvent); err != nil {
		return scenarios, err
	}
	baseInputs = append(baseInputs, checkEvent)

	secretState := state
	secretState.Contract.Checks = append([]tasks.AcceptanceCheck(nil), state.Contract.Checks...)
	for i := range secretState.Contract.Checks {
		if secretState.Contract.Checks[i].ID == "secret-safety" {
			secretState.Contract.Checks[i].Evidence = "api_key=TEST_VALUE_123456789"
		}
	}
	_, secretErr := proofreceipt.Build(secretState, activity.Snapshot(), []proofreceipt.Artifact{artifact})
	secretPassed := errors.Is(secretErr, proofreceipt.ErrSecretDetected)
	scenarios = append(scenarios, scenario{Name: "secret_like_evidence_rejected", Passed: secretPassed, Detail: map[string]any{"error": errorString(secretErr)}})
	if !secretPassed {
		return scenarios, errors.New("secret rejection scenario failed")
	}
	state, err = manager.UpdateCheck("secret-safety", tasks.CheckPassed, "secret-like API key evidence was rejected before receipt creation")
	if err != nil {
		return scenarios, err
	}
	secretCheckEvent := timeline.Input{EventID: "evt-check-secret", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTask, Kind: "acceptance_check_passed", SourceRef: "check:secret-safety", Summary: "Secret safety check passed"}
	if _, _, err := activity.AppendOnce(secretCheckEvent); err != nil {
		return scenarios, err
	}
	baseInputs = append(baseInputs, secretCheckEvent)

	state, err = manager.UpdateCheck("proof-receipt", tasks.CheckPassed, "receipt builder uses acceptance evidence, timeline references and SHA-256 artifact evidence")
	if err != nil {
		return scenarios, err
	}
	receiptCheckEvent := timeline.Input{EventID: "evt-check-receipt", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTask, Kind: "acceptance_check_passed", SourceRef: "check:proof-receipt", Summary: "Proof Receipt evidence check passed"}
	if _, _, err := activity.AppendOnce(receiptCheckEvent); err != nil {
		return scenarios, err
	}
	baseInputs = append(baseInputs, receiptCheckEvent)

	reopenedTimeline, err := timeline.Open(timelinePath)
	if err != nil {
		return scenarios, err
	}
	duplicateAppends := 0
	for _, input := range baseInputs {
		_, appended, err := reopenedTimeline.AppendOnce(input)
		if err != nil {
			return scenarios, err
		}
		if appended {
			duplicateAppends++
		}
	}
	orderedBeforeFinal := canonicalOrder(reopenedTimeline.Snapshot())
	restartTimelinePassed := duplicateAppends == 0 && orderedBeforeFinal
	scenarios = append(scenarios, scenario{Name: "timeline_restart_dedup_and_order", Passed: restartTimelinePassed, Detail: map[string]any{"event_count": len(reopenedTimeline.Snapshot()), "duplicate_appends": duplicateAppends, "canonical_order": orderedBeforeFinal}})
	if !restartTimelinePassed {
		return scenarios, errors.New("timeline restart deduplication failed")
	}

	reopenedTaskStore, err := tasks.Open(taskEventsPath)
	if err != nil {
		return scenarios, err
	}
	reopenedJournal, err := tooljournal.Open(journalPath)
	if err != nil {
		return scenarios, err
	}
	restartedManager := &tasks.Manager{Store: reopenedTaskStore, Journal: reopenedJournal}
	state, err = restartedManager.UpdateCheck("restart-exactly-once", tasks.CheckPassed, "replaying all persisted event IDs after restart appended zero duplicates")
	if err != nil {
		return scenarios, err
	}
	restartCheckEvent := timeline.Input{EventID: "evt-check-restart", TaskID: state.TaskID, SessionID: state.SessionID, Domain: timeline.DomainTask, Kind: "acceptance_check_passed", SourceRef: "check:restart-exactly-once", Summary: "Restart exact-once check passed"}
	if _, appended, err := reopenedTimeline.AppendOnce(restartCheckEvent); err != nil || !appended {
		return scenarios, fmt.Errorf("append restart check: appended=%v err=%w", appended, err)
	}

	receipt, err := proofreceipt.Build(state, reopenedTimeline.Snapshot(), []proofreceipt.Artifact{artifact})
	if err != nil {
		return scenarios, err
	}
	markdown := receipt.Markdown()
	readablePassed := strings.Contains(markdown, "Verified progress: 100.00% (4/4 checks)") &&
		strings.Contains(markdown, artifact.SHA256) &&
		strings.Contains(markdown, "Timeline references") &&
		!strings.Contains(markdown, "TEST_VALUE_123456789")
	scenarios = append(scenarios, scenario{Name: "human_readable_evidence_receipt", Passed: readablePassed, Detail: map[string]any{"receipt_id": receipt.ReceiptID, "input_digest": receipt.InputDigest, "timeline_refs": len(receipt.TimelineRefs), "artifact_sha256": artifact.SHA256}})
	if !readablePassed {
		return scenarios, errors.New("human-readable receipt validation failed")
	}
	if err := os.WriteFile(receiptMarkdownPath, []byte(markdown), 0o600); err != nil {
		return scenarios, err
	}

	receipts, err := proofreceipt.Open(receiptStorePath)
	if err != nil {
		return scenarios, err
	}
	saved, appended, err := receipts.SaveOnce(receipt)
	if err != nil || !appended {
		return scenarios, fmt.Errorf("initial receipt save: appended=%v err=%w", appended, err)
	}
	proofEvent := timeline.Input{
		EventID: "evt-proof-" + saved.ReceiptID, TaskID: saved.TaskID, SessionID: saved.SessionID,
		Domain: timeline.DomainProof, Kind: "proof_receipt_generated", SourceRef: saved.ReceiptID,
		Summary: "Proof Receipt generated", DataHash: saved.InputDigest,
	}
	if _, appended, err := reopenedTimeline.AppendOnce(proofEvent); err != nil || !appended {
		return scenarios, fmt.Errorf("initial proof event append: appended=%v err=%w", appended, err)
	}

	finalTimeline, err := timeline.Open(timelinePath)
	if err != nil {
		return scenarios, err
	}
	finalTaskStore, err := tasks.Open(taskEventsPath)
	if err != nil {
		return scenarios, err
	}
	finalReceipts, err := proofreceipt.Open(receiptStorePath)
	if err != nil {
		return scenarios, err
	}
	rebuilt, err := proofreceipt.Build(finalTaskStore.State(), finalTimeline.Snapshot(), []proofreceipt.Artifact{artifact})
	if err != nil {
		return scenarios, err
	}
	_, receiptAppendedAgain, err := finalReceipts.SaveOnce(rebuilt)
	if err != nil {
		return scenarios, err
	}
	_, proofEventAppendedAgain, err := finalTimeline.AppendOnce(proofEvent)
	if err != nil {
		return scenarios, err
	}
	domains := domainSet(finalTimeline.Snapshot())
	exactOncePassed := !receiptAppendedAgain && !proofEventAppendedAgain && len(finalReceipts.Snapshot()) == 1 && rebuilt.ReceiptID == receipt.ReceiptID && rebuilt.InputDigest == receipt.InputDigest
	scenarios = append(scenarios, scenario{Name: "receipt_restart_exactly_once", Passed: exactOncePassed, Detail: map[string]any{"receipt_count": len(finalReceipts.Snapshot()), "receipt_appended_again": receiptAppendedAgain, "proof_event_appended_again": proofEventAppendedAgain, "receipt_id": rebuilt.ReceiptID}})

	allDomainsPassed := canonicalOrder(finalTimeline.Snapshot()) && containsAllDomains(domains)
	scenarios = append(scenarios, scenario{Name: "single_cross_domain_canonical_timeline", Passed: allDomainsPassed, Detail: map[string]any{"event_count": len(finalTimeline.Snapshot()), "domains": domains, "canonical_order": canonicalOrder(finalTimeline.Snapshot())}})

	return scenarios, nil
}

func canonicalOrder(events []timeline.Event) bool {
	for i, event := range events {
		if event.Sequence != uint64(i+1) || event.EventID == "" {
			return false
		}
	}
	return true
}

func domainSet(events []timeline.Event) []string {
	set := map[string]bool{}
	for _, event := range events {
		set[string(event.Domain)] = true
	}
	out := make([]string, 0, len(set))
	for domain := range set {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out
}

func containsAllDomains(domains []string) bool {
	required := []string{"artifact", "engine", "proof", "task", "tool"}
	if len(domains) != len(required) {
		return false
	}
	for i := range required {
		if domains[i] != required[i] {
			return false
		}
	}
	return true
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
