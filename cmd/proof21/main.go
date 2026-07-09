package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/secretbroker"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

const (
	taskID      = "proof21-task"
	sessionID   = "proof21-session"
	secretValue = "kd-proof21-runtime-secret-7f9c2a1e"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                  string     `json:"proof"`
	Status                 string     `json:"status"`
	Passed                 bool       `json:"passed"`
	Scenarios              []scenario `json:"scenarios"`
	SecretReferenceKey     string     `json:"secret_reference_key"`
	PreflightOrder         []string   `json:"preflight_order"`
	RawSecretPersistence   string     `json:"raw_secret_persistence"`
	AdapterBoundary        string     `json:"adapter_boundary"`
	FinalServerStateSHA256 string     `json:"final_server_state_sha256"`
	NextGate               string     `json:"next_gate"`
}

type serverState struct {
	RPCCounts        map[string]int `json:"rpc_counts"`
	ToolCalls        map[string]int `json:"tool_calls"`
	CredentialHashes []string       `json:"credential_hashes"`
	Resources        []string       `json:"resources"`
}

type countingAdapter struct {
	Inner mcpbridge.Adapter
	Calls int
}

func (a *countingAdapter) Invoke(ctx context.Context, tool string, args map[string]any) (mcpbridge.CallToolResult, error) {
	a.Calls++
	return a.Inner.Invoke(ctx, tool, args)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := report{
		Proof:                "0.21-mcp-secret-broker-schema-authorization",
		Status:               "failed",
		SecretReferenceKey:   secretbroker.ReferenceKey,
		PreflightOrder:       []string{"permission", "argument_schema", "secret_scope_plan", "tool_journal", "secret_resolution", "adapter"},
		RawSecretPersistence: "forbidden",
		AdapterBoundary:      "secret values exist only in memory after journal execute decision and are passed only to mcpbridge.Adapter",
	}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	root := filepath.Join(os.TempDir(), "keydeck-proof21")
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	statePath := filepath.Join(root, "server-state.json")
	journalPath := filepath.Join(root, "tool-journal.jsonl")
	timelinePath := filepath.Join(root, "timeline.jsonl")
	taskPath := filepath.Join(root, "task-events.jsonl")
	receiptPath := filepath.Join(root, "receipts.jsonl")

	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		return err
	}
	timelineStore, err := timeline.Open(timelinePath)
	if err != nil {
		return err
	}
	taskStore, err := tasks.Open(taskPath)
	if err != nil {
		return err
	}
	manager := &tasks.Manager{Store: taskStore, Journal: journal}
	contract := tasks.Contract{
		Goal:           "Prove scoped secret references and schema-aware MCP authorization without persisting raw secrets or weakening Tool Journal safety.",
		ForbiddenScope: []string{"no external network", "no raw secret persistence", "no broad SaaS integration", "no replay-policy weakening"},
		Checks: []tasks.AcceptanceCheck{
			{ID: "secret_reference", Description: "Scoped secret reference resolves only after journal execute decision at the adapter boundary"},
			{ID: "permission_order", Description: "Permission denial happens before secret planning or adapter invocation"},
			{ID: "schema_order", Description: "Schema denial happens before secret planning or adapter invocation"},
			{ID: "scope_order", Description: "Secret-scope denial happens before journal begin, resolution or adapter invocation"},
			{ID: "replay", Description: "Completed secret-backed operation is reused without resolving the secret again"},
			{ID: "redaction", Description: "Sensitive adapter results and tool errors are redacted before persistence or return"},
			{ID: "persistence", Description: "Raw secret is absent from journal, timeline, task and server-state artifacts"},
			{ID: "receipt", Description: "Proof Receipt includes redacted timeline summary and is stored exactly once"},
		},
	}
	if _, err := manager.Create(taskID, sessionID, contract); err != nil {
		return err
	}

	broker, err := secretbroker.New([]secretbroker.Entry{
		{Scope: "provider.read", Name: "primary", Value: secretValue},
		{Scope: "provider.admin", Name: "admin", Value: "kd-proof21-admin-secret-not-used"},
	}, secretbroker.Policy{ToolScopes: map[string]map[string]bool{
		"secure.fetch": {"provider.read": true},
		"secure.fail":  {"provider.read": true},
	}})
	if err != nil {
		return err
	}

	cfg, err := serverConfig(statePath, sha256Hex([]byte(secretValue)))
	if err != nil {
		return err
	}
	baseAdapter := mcpbridge.NewCommandAdapter(mcpbridge.NewClient(cfg))
	counted := &countingAdapter{Inner: baseAdapter}
	permissions := map[string]mcpbridge.PermissionProfile{
		"secure.fetch": mcpbridge.ProfileSafeEdit,
		"secure.fail":  mcpbridge.ProfileSafeEdit,
	}
	schemas := mcpbridge.SchemaPolicy{Tools: map[string]mcpbridge.ArgumentSchema{
		"secure.fetch": secureSchema(),
		"secure.fail":  secureSchema(),
	}}

	safeBridge := newBridge(journal, timelineStore, counted, broker, &schemas, mcpbridge.ProfileSafeEdit, permissions)
	fetchOp := mcpbridge.Operation{OperationID: "secret-fetch", Tool: "secure.fetch", Arguments: map[string]any{
		"resource": "alpha", "credential": secretbroker.Value("provider.read", "primary"),
	}, Policy: tooljournal.ReplayForbidden}
	fetchResult, fetchErr := safeBridge.Execute(context.Background(), fetchOp)
	plansAfterFetch, resolutionsAfterFetch := broker.Counts()
	stateAfterFetch, stateErr := loadState(statePath)
	secretReferencePass := fetchErr == nil && fetchResult.Text == "secure:ok:alpha" && counted.Calls == 1 && plansAfterFetch == 1 && resolutionsAfterFetch == 1 && stateErr == nil && len(stateAfterFetch.CredentialHashes) == 1 && stateAfterFetch.CredentialHashes[0] == sha256Hex([]byte(secretValue))
	add("scoped_secret_reference_resolves_only_at_adapter_boundary", secretReferencePass, map[string]any{"adapter_calls": counted.Calls, "plans": plansAfterFetch, "resolutions": resolutionsAfterFetch, "server_verified_hash": len(stateAfterFetch.CredentialHashes) == 1})
	mark(manager, "secret_reference", secretReferencePass, "real MCP server accepted resolved credential hash; only scoped reference was journaled")

	plansBeforePermission, resolutionsBeforePermission := broker.Counts()
	callsBeforePermission := counted.Calls
	readBridge := newBridge(journal, timelineStore, counted, broker, &schemas, mcpbridge.ProfileReadOnly, permissions)
	_, permissionErr := readBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "permission-denied", Tool: "secure.fetch", Arguments: map[string]any{
		"resource": "beta", "credential": secretbroker.Value("provider.read", "primary"),
	}, Policy: tooljournal.ReplayForbidden})
	plansAfterPermission, resolutionsAfterPermission := broker.Counts()
	permissionPass := errors.Is(permissionErr, mcpbridge.ErrToolNotAllowed) && counted.Calls == callsBeforePermission && plansAfterPermission == plansBeforePermission && resolutionsAfterPermission == resolutionsBeforePermission
	add("permission_denial_precedes_secret_planning_and_adapter", permissionPass, map[string]any{"error": errString(permissionErr), "adapter_calls_delta": counted.Calls - callsBeforePermission, "plan_delta": plansAfterPermission - plansBeforePermission, "resolution_delta": resolutionsAfterPermission - resolutionsBeforePermission})
	mark(manager, "permission_order", permissionPass, "permission denial caused zero secret plans, zero resolutions and zero adapter calls")

	plansBeforeSchema, resolutionsBeforeSchema := broker.Counts()
	callsBeforeSchema := counted.Calls
	_, schemaErr := safeBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "schema-denied", Tool: "secure.fetch", Arguments: map[string]any{
		"resource": true, "credential": secretValue,
	}, Policy: tooljournal.ReplayForbidden})
	plansAfterSchema, resolutionsAfterSchema := broker.Counts()
	schemaPass := errors.Is(schemaErr, mcpbridge.ErrArgumentSchemaDenied) && counted.Calls == callsBeforeSchema && plansAfterSchema == plansBeforeSchema && resolutionsAfterSchema == resolutionsBeforeSchema
	add("schema_denial_precedes_secret_planning_and_adapter", schemaPass, map[string]any{"error": errString(schemaErr), "adapter_calls_delta": counted.Calls - callsBeforeSchema, "plan_delta": plansAfterSchema - plansBeforeSchema, "resolution_delta": resolutionsAfterSchema - resolutionsBeforeSchema})
	mark(manager, "schema_order", schemaPass, "raw credential and wrong resource type rejected before Secret Broker planning")

	callsBeforeScope := counted.Calls
	_, resolutionsBeforeScope := broker.Counts()
	_, scopeErr := safeBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "scope-denied", Tool: "secure.fetch", Arguments: map[string]any{
		"resource": "gamma", "credential": secretbroker.Value("provider.admin", "admin"),
	}, Policy: tooljournal.ReplayForbidden})
	_, resolutionsAfterScope := broker.Counts()
	_, journalHasDeniedScope := journal.Snapshot()["scope-denied"]
	scopePass := errors.Is(scopeErr, secretbroker.ErrScopeDenied) && counted.Calls == callsBeforeScope && resolutionsAfterScope == resolutionsBeforeScope && !journalHasDeniedScope
	add("secret_scope_denial_precedes_journal_resolution_and_adapter", scopePass, map[string]any{"error": errString(scopeErr), "adapter_calls_delta": counted.Calls - callsBeforeScope, "resolution_delta": resolutionsAfterScope - resolutionsBeforeScope, "journal_record_created": journalHasDeniedScope})
	mark(manager, "scope_order", scopePass, "denied scope created no Tool Journal record and resolved no secret")

	journal, err = tooljournal.Open(journalPath)
	if err != nil {
		return err
	}
	timelineStore, err = timeline.Open(timelinePath)
	if err != nil {
		return err
	}
	safeBridge = newBridge(journal, timelineStore, counted, broker, &schemas, mcpbridge.ProfileSafeEdit, permissions)
	_, resolutionsBeforeReplay := broker.Counts()
	callsBeforeReplay := counted.Calls
	replayResult, replayErr := safeBridge.Execute(context.Background(), fetchOp)
	_, resolutionsAfterReplay := broker.Counts()
	replayPass := replayErr == nil && replayResult.Reused && replayResult.Text == "secure:ok:alpha" && counted.Calls == callsBeforeReplay && resolutionsAfterReplay == resolutionsBeforeReplay
	add("completed_secret_operation_reused_without_secret_resolution", replayPass, map[string]any{"reused": replayResult.Reused, "adapter_calls_delta": counted.Calls - callsBeforeReplay, "resolution_delta": resolutionsAfterReplay - resolutionsBeforeReplay})
	mark(manager, "replay", replayPass, "completed result reused after restart with zero additional secret resolutions")

	failOp := mcpbridge.Operation{OperationID: "secret-fail", Tool: "secure.fail", Arguments: map[string]any{
		"resource": "delta", "credential": secretbroker.Value("provider.read", "primary"),
	}, Policy: tooljournal.ReplayIdempotent}
	_, failErr := safeBridge.Execute(context.Background(), failOp)
	failRecord := journal.Snapshot()[failOp.OperationID]
	redactionText := errString(failErr) + "\n" + failRecord.Error
	for _, event := range timelineStore.Snapshot() {
		redactionText += "\n" + event.Summary
	}
	redactionPass := failErr != nil && !strings.Contains(redactionText, secretValue) && strings.Contains(redactionText, "[REDACTED_SECRET]")
	add("sensitive_tool_error_redacted_before_return_journal_and_timeline", redactionPass, map[string]any{"returned_error": errString(failErr), "journal_error": failRecord.Error, "contains_redaction_marker": strings.Contains(redactionText, "[REDACTED_SECRET]")})
	mark(manager, "redaction", redactionPass, "tool error containing runtime credential was replaced with [REDACTED_SECRET]")

	stateRaw, stateReadErr := os.ReadFile(statePath)
	serverStateHash := sha256Hex(stateRaw)
	out.FinalServerStateSHA256 = serverStateHash
	persistedBeforeReceipt := []string{journalPath, timelinePath, taskPath, statePath}
	persistencePass, persistenceLeaks := scanPathsForValue(persistedBeforeReceipt, secretValue)
	persistencePass = persistencePass && stateReadErr == nil && !strings.Contains(string(stateRaw), secretValue)
	add("raw_secret_absent_from_persisted_runtime_surfaces", persistencePass, map[string]any{"checked_files": persistedBeforeReceipt, "leaks": persistenceLeaks, "server_state_sha256": serverStateHash})
	mark(manager, "persistence", persistencePass, "journal, timeline, task store and server state contain no raw credential")

	events := timelineStore.Snapshot()
	synthetic := timeline.Event{Sequence: uint64(len(events) + 1), At: time.Now().UTC(), EventID: "proof21:redaction-input", TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainTool, Kind: "proof21_redaction_source", SourceRef: "secure.fail", Summary: "diagnostic contained " + secretValue}
	receiptEvents := append(append([]timeline.Event(nil), events...), synthetic)
	provisional, provisionalErr := proofreceipt.BuildRedacted(taskStore.State(), receiptEvents, []proofreceipt.Artifact{{Name: "proof21-server-state", Path: "proof21-server-state.json", SHA256: serverStateHash, Size: int64(len(stateRaw))}}, []string{secretValue})
	provisionalRaw, _ := json.Marshal(provisional)
	provisionalText := string(provisionalRaw) + provisional.Markdown()
	provisionalPass := provisionalErr == nil && !strings.Contains(provisionalText, secretValue) && strings.Contains(provisionalText, "[REDACTED_SECRET]")
	mark(manager, "receipt", provisionalPass, "BuildRedacted preserved a redaction marker without the runtime credential")

	finalReceipt, receiptErr := proofreceipt.BuildRedacted(taskStore.State(), receiptEvents, []proofreceipt.Artifact{{Name: "proof21-server-state", Path: "proof21-server-state.json", SHA256: serverStateHash, Size: int64(len(stateRaw))}}, []string{secretValue})
	receipts, openReceiptErr := proofreceipt.Open(receiptPath)
	var created, createdAgain bool
	var saveErr, saveAgainErr error
	if openReceiptErr == nil && receiptErr == nil {
		_, created, saveErr = receipts.SaveOnce(finalReceipt)
		_, createdAgain, saveAgainErr = receipts.SaveOnce(finalReceipt)
	}
	finalReceiptRaw, _ := os.ReadFile(receiptPath)
	finalReceiptText := string(finalReceiptRaw) + finalReceipt.Markdown()
	receiptPass := provisionalPass && receiptErr == nil && openReceiptErr == nil && saveErr == nil && saveAgainErr == nil && created && !createdAgain && taskStore.State().Progress().Complete && !strings.Contains(finalReceiptText, secretValue) && strings.Contains(finalReceiptText, "[REDACTED_SECRET]")
	add("redacted_proof_receipt_stored_exactly_once", receiptPass, map[string]any{"receipt_id": finalReceipt.ReceiptID, "created_once": created && !createdAgain, "complete_progress": taskStore.State().Progress().Complete, "contains_redaction_marker": strings.Contains(finalReceiptText, "[REDACTED_SECRET]"), "error": errString(errors.Join(provisionalErr, receiptErr, openReceiptErr, saveErr, saveAgainErr))})

	out.Passed = true
	for _, item := range out.Scenarios {
		out.Passed = out.Passed && item.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	out.NextGate = "Proof 0.22 — connect one immutable third-party local MCP server through the hardened bridge and Secret Broker, preserving KeyDeck-owned permissions and replay safety."
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !out.Passed {
		return errors.New("Proof 0.21 failed")
	}
	return nil
}

func secureSchema() mcpbridge.ArgumentSchema {
	return mcpbridge.ArgumentSchema{Fields: map[string]mcpbridge.FieldPolicy{
		"resource":   {Type: mcpbridge.ValueString, Required: true, MaxStringBytes: 64},
		"credential": {Required: true, SecretReference: true, Sensitive: true},
	}}
}

func newBridge(j *tooljournal.Journal, t *timeline.Store, a mcpbridge.Adapter, broker *secretbroker.Broker, schemas *mcpbridge.SchemaPolicy, profile mcpbridge.PermissionProfile, tools map[string]mcpbridge.PermissionProfile) *mcpbridge.Bridge {
	return &mcpbridge.Bridge{Journal: j, Timeline: t, TaskID: taskID, SessionID: sessionID, Adapter: a, Permissions: &mcpbridge.PermissionPolicy{Profile: profile, ToolProfiles: tools}, Schemas: schemas, Secrets: broker}
}

func serverConfig(statePath, expectedSecretHash string) (mcpbridge.CommandConfig, error) {
	args := []string{"--state", statePath, "--expected-secret-sha256", expectedSecretHash}
	if explicit := os.Getenv("KEYDECK_PROOF21_SERVER"); explicit != "" {
		return mcpbridge.CommandConfig{Path: explicit, Args: args, MaxFrameBytes: 1 << 20}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return mcpbridge.CommandConfig{}, err
	}
	name := "KeyDeck-Proof-0.21-MCP-Server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	sibling := filepath.Join(filepath.Dir(exe), name)
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return mcpbridge.CommandConfig{Path: sibling, Args: args, MaxFrameBytes: 1 << 20}, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return mcpbridge.CommandConfig{}, err
	}
	localName := "proof21-local-mcp-server"
	if runtime.GOOS == "windows" {
		localName += ".exe"
	}
	localServer := filepath.Join(filepath.Dir(statePath), localName)
	if _, statErr := os.Stat(localServer); os.IsNotExist(statErr) {
		build := exec.Command("go", "build", "-trimpath", "-o", localServer, "./cmd/proof21server")
		build.Dir = cwd
		if output, buildErr := build.CombinedOutput(); buildErr != nil {
			return mcpbridge.CommandConfig{}, fmt.Errorf("build proof21 local MCP server: %w: %s", buildErr, output)
		}
	}
	return mcpbridge.CommandConfig{Path: localServer, Args: args, MaxFrameBytes: 1 << 20}, nil
}

func loadState(path string) (serverState, error) {
	var state serverState
	raw, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(raw, &state)
	return state, err
}

func scanPathsForValue(paths []string, value string) (bool, []string) {
	leaks := make([]string, 0)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			leaks = append(leaks, path+":read-error")
			continue
		}
		if strings.Contains(string(raw), value) {
			leaks = append(leaks, path)
		}
	}
	return len(leaks) == 0, leaks
}

func mark(m *tasks.Manager, id string, passed bool, evidence string) {
	status := tasks.CheckFailed
	if passed {
		status = tasks.CheckPassed
	}
	_, _ = m.UpdateCheck(id, status, evidence)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
