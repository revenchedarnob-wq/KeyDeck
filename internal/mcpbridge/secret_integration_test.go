package mcpbridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"keydeck.local/feasibilitylab/internal/secretbroker"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type captureAdapter struct {
	calls int
	args  map[string]any
	text  string
	err   error
}

func (a *captureAdapter) Invoke(_ context.Context, _ string, args map[string]any) (CallToolResult, error) {
	a.calls++
	a.args = cloneMap(args)
	if a.err != nil {
		return CallToolResult{}, a.err
	}
	return CallToolResult{Content: []Content{{Type: "text", Text: a.text}}}, nil
}

func openSecretBridge(t *testing.T, profile PermissionProfile, adapter Adapter, broker *secretbroker.Broker) (*Bridge, *tooljournal.Journal, *timeline.Store) {
	t.Helper()
	root := t.TempDir()
	journal, err := tooljournal.Open(filepath.Join(root, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	timelineStore, err := timeline.Open(filepath.Join(root, "timeline.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	schemas := proofSchema()
	permissions := &PermissionPolicy{Profile: profile, ToolProfiles: map[string]PermissionProfile{"secure.fetch": ProfileSafeEdit}}
	return &Bridge{Journal: journal, Timeline: timelineStore, TaskID: "task", SessionID: "session", Permissions: permissions, Schemas: &schemas, Secrets: broker, Adapter: adapter}, journal, timelineStore
}

func testBroker(t *testing.T, allowedScope string) *secretbroker.Broker {
	t.Helper()
	broker, err := secretbroker.New([]secretbroker.Entry{{Scope: "provider.read", Name: "primary", Value: "proof21-secret-value-123456"}}, secretbroker.Policy{ToolScopes: map[string]map[string]bool{"secure.fetch": {allowedScope: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

func TestPermissionDenialPrecedesSecretPlanningAndAdapter(t *testing.T) {
	adapter := &captureAdapter{text: "ok"}
	broker := testBroker(t, "provider.read")
	bridge, _, _ := openSecretBridge(t, ProfileReadOnly, adapter, broker)
	_, err := bridge.Execute(context.Background(), Operation{OperationID: "deny-perm", Tool: "secure.fetch", Arguments: map[string]any{"resource": "alpha", "credential": secretbroker.Value("provider.read", "primary")}, Policy: tooljournal.ReplayForbidden})
	if !errors.Is(err, ErrToolNotAllowed) {
		t.Fatalf("expected permission denial, got %v", err)
	}
	plans, resolutions := broker.Counts()
	if adapter.calls != 0 || plans != 0 || resolutions != 0 {
		t.Fatalf("denial touched downstream boundary: calls=%d plans=%d resolutions=%d", adapter.calls, plans, resolutions)
	}
}

func TestSchemaDenialPrecedesSecretPlanningAndAdapter(t *testing.T) {
	adapter := &captureAdapter{text: "ok"}
	broker := testBroker(t, "provider.read")
	bridge, _, _ := openSecretBridge(t, ProfileSafeEdit, adapter, broker)
	_, err := bridge.Execute(context.Background(), Operation{OperationID: "deny-schema", Tool: "secure.fetch", Arguments: map[string]any{"resource": true, "credential": secretbroker.Value("provider.read", "primary")}, Policy: tooljournal.ReplayForbidden})
	if !errors.Is(err, ErrArgumentSchemaDenied) {
		t.Fatalf("expected schema denial, got %v", err)
	}
	plans, resolutions := broker.Counts()
	if adapter.calls != 0 || plans != 0 || resolutions != 0 {
		t.Fatalf("schema denial touched downstream boundary: calls=%d plans=%d resolutions=%d", adapter.calls, plans, resolutions)
	}
}

func TestScopeDenialPrecedesJournalAndAdapter(t *testing.T) {
	adapter := &captureAdapter{text: "ok"}
	broker := testBroker(t, "provider.other")
	bridge, journal, _ := openSecretBridge(t, ProfileSafeEdit, adapter, broker)
	_, err := bridge.Execute(context.Background(), Operation{OperationID: "deny-scope", Tool: "secure.fetch", Arguments: map[string]any{"resource": "alpha", "credential": secretbroker.Value("provider.read", "primary")}, Policy: tooljournal.ReplayForbidden})
	if !errors.Is(err, secretbroker.ErrScopeDenied) {
		t.Fatalf("expected scope denial, got %v", err)
	}
	_, resolutions := broker.Counts()
	if adapter.calls != 0 || resolutions != 0 || len(journal.Snapshot()) != 0 {
		t.Fatalf("scope denial crossed execution boundary: calls=%d resolutions=%d journal=%d", adapter.calls, resolutions, len(journal.Snapshot()))
	}
}

func TestCompletedSecretOperationReusesWithoutSecondResolution(t *testing.T) {
	adapter := &captureAdapter{text: "ok"}
	broker := testBroker(t, "provider.read")
	bridge, _, _ := openSecretBridge(t, ProfileSafeEdit, adapter, broker)
	op := Operation{OperationID: "complete-secret", Tool: "secure.fetch", Arguments: map[string]any{"resource": "alpha", "credential": secretbroker.Value("provider.read", "primary")}, Policy: tooljournal.ReplayForbidden}
	first, err := bridge.Execute(context.Background(), op)
	if err != nil || first.Text != "ok" {
		t.Fatalf("first execution result=%+v err=%v", first, err)
	}
	_, resolutionsBefore := broker.Counts()
	second, err := bridge.Execute(context.Background(), op)
	if err != nil || !second.Reused || second.Text != "ok" {
		t.Fatalf("replay result=%+v err=%v", second, err)
	}
	_, resolutionsAfter := broker.Counts()
	if adapter.calls != 1 || resolutionsBefore != 1 || resolutionsAfter != 1 {
		t.Fatalf("completed replay touched secret/adapter: calls=%d before=%d after=%d", adapter.calls, resolutionsBefore, resolutionsAfter)
	}
	if adapter.args["credential"] != "proof21-secret-value-123456" {
		t.Fatalf("adapter did not receive resolved credential: %#v", adapter.args)
	}
}

func TestSecretIsRedactedFromResultAndTransportError(t *testing.T) {
	secret := "proof21-secret-value-123456"
	broker := testBroker(t, "provider.read")
	adapter := &captureAdapter{text: "tool echoed " + secret}
	bridge, journal, timelineStore := openSecretBridge(t, ProfileSafeEdit, adapter, broker)
	op := Operation{OperationID: "redact-result", Tool: "secure.fetch", Arguments: map[string]any{"resource": "alpha", "credential": secretbroker.Value("provider.read", "primary")}, Policy: tooljournal.ReplayForbidden}
	result, err := bridge.Execute(context.Background(), op)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, secret) || !strings.Contains(result.Text, "[REDACTED_SECRET]") {
		t.Fatalf("result not redacted: %q", result.Text)
	}
	if strings.Contains(journal.Snapshot()[op.OperationID].Result, secret) {
		t.Fatalf("journal leaked secret")
	}
	for _, event := range timelineStore.Snapshot() {
		if strings.Contains(event.Summary, secret) {
			t.Fatalf("timeline leaked secret: %+v", event)
		}
	}
}
