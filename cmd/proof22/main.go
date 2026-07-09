package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/secretbroker"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

const (
	taskID                 = "proof22-task"
	sessionID              = "proof22-session"
	packageName            = "@modelcontextprotocol/server-filesystem"
	packageVersion         = "2026.7.4"
	packageIntegrity       = "sha512-JwEaH4dRRzwcNMwX8WJVCJyXfFxXjFKdgwHxjQhFLhi02kszgyyj611LV9puBLDO1IiDQSCjfKFSPaemegnvwg=="
	packageSHA256          = "7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54"
	packageLockSHA256      = "e367ec6701c275457847b8692b55edb5aa2fecde8b01cd5a2966935f35f59e29"
	fixtureKeyword         = "ORANGE-CONTINUITY-22"
	fixtureText            = "KeyDeck Proof 0.22 third-party MCP fixture.\nThe canonical keyword is ORANGE-CONTINUITY-22.\n"
	ambiguousContent       = "written exactly once through real third-party MCP before synthetic response loss\n"
	secretSentinel         = "PROOF22_SECRET_MUST_NEVER_PERSIST"
	canonicalPackageSource = "npm:@modelcontextprotocol/server-filesystem@2026.7.4"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                string     `json:"proof"`
	Status               string     `json:"status"`
	Passed               bool       `json:"passed"`
	Scenarios            []scenario `json:"scenarios"`
	ServerIdentity       any        `json:"server_identity"`
	ServerIdentitySHA256 string     `json:"server_identity_sha256"`
	PackageSHA256        string     `json:"package_sha256"`
	PackageLockSHA256    string     `json:"package_lock_sha256"`
	FixtureSHA256        string     `json:"fixture_sha256"`
	AmbiguousFileSHA256  string     `json:"ambiguous_file_sha256"`
	ReceiptID            string     `json:"receipt_id"`
	NextGate             string     `json:"next_gate"`
}

type proofPaths struct {
	Node        string
	ServerJS    string
	Tarball     string
	PackageLock string
	PackageJSON string
}

type packageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type packageLock struct {
	Packages map[string]struct {
		Version   string `json:"version"`
		Integrity string `json:"integrity"`
	} `json:"packages"`
}

type countingAdapter struct {
	Inner mcpbridge.Adapter
	Calls int
}

func (a *countingAdapter) BoundServerIdentity() *mcpbridge.ServerIdentity {
	provider, ok := a.Inner.(mcpbridge.ServerIdentityProvider)
	if !ok {
		return nil
	}
	return provider.BoundServerIdentity()
}

func (a *countingAdapter) Invoke(ctx context.Context, tool string, args map[string]any) (mcpbridge.CallToolResult, error) {
	a.Calls++
	return a.Inner.Invoke(ctx, tool, args)
}

type loseResponseAdapter struct {
	Inner mcpbridge.Adapter
	Calls int
}

func (a *loseResponseAdapter) BoundServerIdentity() *mcpbridge.ServerIdentity {
	provider, ok := a.Inner.(mcpbridge.ServerIdentityProvider)
	if !ok {
		return nil
	}
	return provider.BoundServerIdentity()
}

func (a *loseResponseAdapter) Invoke(ctx context.Context, tool string, args map[string]any) (mcpbridge.CallToolResult, error) {
	a.Calls++
	result, err := a.Inner.Invoke(ctx, tool, args)
	if err != nil {
		return result, err
	}
	return mcpbridge.CallToolResult{}, io.ErrUnexpectedEOF
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	paths, err := resolvePaths()
	if err != nil {
		return err
	}
	identity := mcpbridge.ServerIdentity{
		Name:             "Official MCP Filesystem reference server",
		Version:          packageVersion,
		Registry:         "npm",
		Package:          packageName,
		PackageIntegrity: packageIntegrity,
		PackageSHA256:    packageSHA256,
		EntryPoint:       "dist/index.js",
	}
	identityHash, err := identity.Hash()
	if err != nil {
		return err
	}

	out := report{
		Proof:                "0.22-immutable-third-party-local-mcp-server",
		Status:               "failed",
		ServerIdentity:       identity,
		ServerIdentitySHA256: identityHash,
		PackageSHA256:        packageSHA256,
		PackageLockSHA256:    packageLockSHA256,
	}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	root, err := os.MkdirTemp("", "keydeck-proof22-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	fixtureRoot := filepath.Join(root, "allowed-root")
	if err := os.MkdirAll(fixtureRoot, 0o700); err != nil {
		return err
	}
	fixturePath := filepath.Join(fixtureRoot, "hello.txt")
	ambiguousPath := filepath.Join(fixtureRoot, "ambiguous-once.txt")
	if err := os.WriteFile(fixturePath, []byte(fixtureText), 0o600); err != nil {
		return err
	}
	out.FixtureSHA256 = sha256Hex([]byte(fixtureText))

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
		Goal: "Prove an exact immutable third-party local MCP package executes through KeyDeck-owned permission, schema, Tool Journal, Secret Broker, timeline, and Proof Receipt safety layers.",
		ForbiddenScope: []string{
			"no network access during proof execution",
			"no broad SaaS integration",
			"no raw secret persistence",
			"no weakening of replay safety",
			"do not treat the reference server as KeyDeck's security boundary",
		},
		Checks: []tasks.AcceptanceCheck{
			{ID: "identity", Description: "Exact third-party package, integrity, tarball hash, lock hash and installed version match the immutable evidence"},
			{ID: "real_call", Description: "Real third-party MCP initialize/list/call reads the fixture through mcpbridge.Adapter"},
			{ID: "permission", Description: "Permission denial prevents third-party process invocation"},
			{ID: "schema", Description: "Schema denial prevents third-party process invocation"},
			{ID: "replay", Description: "Completed third-party read is reused after restart without server replay"},
			{ID: "ambiguity", Description: "Real non-repeatable write followed by response loss remains blocked after restart"},
			{ID: "secrets", Description: "Secret Broker is configured but raw secret is never planned, resolved, or persisted"},
			{ID: "receipt", Description: "Timeline and Proof Receipt bind exact third-party package identity and immutable artifacts"},
		},
	}
	if _, err := manager.Create(taskID, sessionID, contract); err != nil {
		return err
	}

	identityPass, identityDetail, artifacts, err := verifyImmutablePackage(paths, identity)
	if err != nil {
		identityDetail = map[string]any{"error": err.Error()}
	}
	add("immutable_third_party_package_identity_matches_pinned_tarball", identityPass, identityDetail)
	mark(manager, "identity", identityPass, "exact npm version, integrity, tarball SHA-256, package-lock SHA-256 and package metadata verified")

	broker, err := secretbroker.New([]secretbroker.Entry{{Scope: "proof22.unused", Name: "sentinel", Value: secretSentinel}}, secretbroker.Policy{ToolScopes: map[string]map[string]bool{}})
	if err != nil {
		return err
	}
	permissions := map[string]mcpbridge.PermissionProfile{
		"read_text_file": mcpbridge.ProfileReadOnly,
		"write_file":     mcpbridge.ProfileSafeEdit,
	}
	schemas := mcpbridge.SchemaPolicy{Tools: map[string]mcpbridge.ArgumentSchema{
		"read_text_file": {
			Fields: map[string]mcpbridge.FieldPolicy{
				"path": {Type: mcpbridge.ValueString, Required: true, MaxStringBytes: 4096},
				"head": {Type: mcpbridge.ValueNumber},
				"tail": {Type: mcpbridge.ValueNumber},
			},
		},
		"write_file": {
			Fields: map[string]mcpbridge.FieldPolicy{
				"path":    {Type: mcpbridge.ValueString, Required: true, MaxStringBytes: 4096},
				"content": {Type: mcpbridge.ValueString, Required: true, MaxStringBytes: 1 << 20},
			},
		},
	}}
	client := mcpbridge.NewClient(mcpbridge.CommandConfig{Path: paths.Node, Args: []string{paths.ServerJS, fixtureRoot}, MaxFrameBytes: 4 << 20})
	baseAdapter := mcpbridge.NewIdentifiedCommandAdapter(client, identity)
	counted := &countingAdapter{Inner: baseAdapter}
	readBridge := newBridge(journal, timelineStore, counted, broker, &schemas, mcpbridge.ProfileReadOnly, permissions, &identity)

	readOp := mcpbridge.Operation{OperationID: "third-party-read", Tool: "read_text_file", Arguments: map[string]any{"path": fixturePath}, Policy: tooljournal.ReplayForbidden}
	readResult, readErr := readBridge.Execute(context.Background(), readOp)
	realCallPass := readErr == nil && strings.Contains(readResult.Text, fixtureKeyword) && counted.Calls == 1
	add("real_third_party_initialize_list_and_read_call_succeeds", realCallPass, map[string]any{"adapter_calls": counted.Calls, "contains_fixture_keyword": strings.Contains(readResult.Text, fixtureKeyword), "error": errString(readErr)})
	mark(manager, "real_call", realCallPass, "real third-party initialize, tools/list and read_text_file call returned the canonical fixture keyword")

	callsBeforePermission := counted.Calls
	_, permissionErr := readBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "third-party-permission-denied", Tool: "write_file", Arguments: map[string]any{"path": filepath.Join(fixtureRoot, "permission-denied.txt"), "content": "must not execute"}, Policy: tooljournal.ReplayForbidden})
	permissionPass := errors.Is(permissionErr, mcpbridge.ErrToolNotAllowed) && counted.Calls == callsBeforePermission
	add("permission_denial_prevents_third_party_process_invocation", permissionPass, map[string]any{"adapter_calls_delta": counted.Calls - callsBeforePermission, "error": errString(permissionErr)})
	mark(manager, "permission", permissionPass, "read-only permission profile denied write_file before adapter invocation")

	callsBeforeSchema := counted.Calls
	_, schemaErr := readBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "third-party-schema-denied", Tool: "read_text_file", Arguments: map[string]any{"path": true, "unexpected": "blocked"}, Policy: tooljournal.ReplayForbidden})
	schemaPass := errors.Is(schemaErr, mcpbridge.ErrArgumentSchemaDenied) && counted.Calls == callsBeforeSchema
	add("schema_denial_prevents_third_party_process_invocation", schemaPass, map[string]any{"adapter_calls_delta": counted.Calls - callsBeforeSchema, "error": errString(schemaErr)})
	mark(manager, "schema", schemaPass, "wrong path type and unknown field were rejected before adapter invocation")

	journal, err = tooljournal.Open(journalPath)
	if err != nil {
		return err
	}
	timelineStore, err = timeline.Open(timelinePath)
	if err != nil {
		return err
	}
	readBridge = newBridge(journal, timelineStore, counted, broker, &schemas, mcpbridge.ProfileReadOnly, permissions, &identity)
	callsBeforeReplay := counted.Calls
	replayResult, replayErr := readBridge.Execute(context.Background(), readOp)
	replayPass := replayErr == nil && replayResult.Reused && strings.Contains(replayResult.Text, fixtureKeyword) && counted.Calls == callsBeforeReplay
	add("completed_third_party_read_reused_after_restart_without_server_replay", replayPass, map[string]any{"reused": replayResult.Reused, "adapter_calls_delta": counted.Calls - callsBeforeReplay, "error": errString(replayErr)})
	mark(manager, "replay", replayPass, "completed real third-party read reused after journal/timeline restart with zero adapter calls")

	lost := &loseResponseAdapter{Inner: baseAdapter}
	writeBridge := newBridge(journal, timelineStore, lost, broker, &schemas, mcpbridge.ProfileSafeEdit, permissions, &identity)
	writeOp := mcpbridge.Operation{OperationID: "third-party-ambiguous-write", Tool: "write_file", Arguments: map[string]any{"path": ambiguousPath, "content": ambiguousContent}, Policy: tooljournal.ReplayForbidden}
	_, firstWriteErr := writeBridge.Execute(context.Background(), writeOp)
	written, readWrittenErr := os.ReadFile(ambiguousPath)
	callsAfterFirstWrite := lost.Calls
	journal, err = tooljournal.Open(journalPath)
	if err != nil {
		return err
	}
	timelineStore, err = timeline.Open(timelinePath)
	if err != nil {
		return err
	}
	writeBridge = newBridge(journal, timelineStore, lost, broker, &schemas, mcpbridge.ProfileSafeEdit, permissions, &identity)
	_, secondWriteErr := writeBridge.Execute(context.Background(), writeOp)
	ambiguousPass := errors.Is(firstWriteErr, io.ErrUnexpectedEOF) && readWrittenErr == nil && string(written) == ambiguousContent && errors.Is(secondWriteErr, tooljournal.ErrAmbiguousOperation) && lost.Calls == callsAfterFirstWrite && callsAfterFirstWrite == 1
	out.AmbiguousFileSHA256 = sha256Hex(written)
	add("ambiguous_nonrepeatable_third_party_write_remains_blocked_after_restart", ambiguousPass, map[string]any{"first_error": errString(firstWriteErr), "second_error": errString(secondWriteErr), "adapter_calls": lost.Calls, "file_exists_once": string(written) == ambiguousContent, "file_sha256": out.AmbiguousFileSHA256})
	mark(manager, "ambiguity", ambiguousPass, "real write_file effect committed once; synthetic response loss left operation ambiguous and restart blocked replay")

	plans, resolutions := broker.Counts()
	secretPass, leaks := scanPathsForValue([]string{journalPath, timelinePath, taskPath, fixturePath, ambiguousPath}, secretSentinel)
	secretPass = secretPass && plans == 0 && resolutions == 0
	add("secret_broker_configured_without_raw_secret_persistence", secretPass, map[string]any{"plans": plans, "resolutions": resolutions, "leaks": leaks})
	mark(manager, "secrets", secretPass, "configured Secret Broker had zero plans/resolutions and sentinel was absent from durable runtime artifacts")

	events := timelineStore.Snapshot()
	identityBound := len(events) > 0
	toolEventCount := 0
	for _, event := range events {
		if event.Domain != timeline.DomainTool {
			continue
		}
		toolEventCount++
		if !strings.HasPrefix(event.SourceRef, canonicalPackageSource+"#") {
			identityBound = false
		}
	}
	if toolEventCount == 0 {
		identityBound = false
	}
	provisionalReceipt, provisionalErr := proofreceipt.Build(taskStore.State(), events, artifacts)
	provisionalText := string(mustJSON(provisionalReceipt)) + provisionalReceipt.Markdown()
	artifactBound := strings.Contains(provisionalText, packageSHA256) && strings.Contains(provisionalText, packageLockSHA256)
	provisionalPass := provisionalErr == nil && identityBound && artifactBound && strings.Contains(provisionalText, canonicalPackageSource)
	mark(manager, "receipt", provisionalPass, "timeline SourceRef and receipt artifacts bind exact npm package identity, tarball SHA-256 and lock SHA-256")

	finalReceipt, receiptErr := proofreceipt.Build(taskStore.State(), events, artifacts)
	receipts, openReceiptErr := proofreceipt.Open(receiptPath)
	var created, createdAgain bool
	var saveErr, saveAgainErr error
	if openReceiptErr == nil && receiptErr == nil {
		_, created, saveErr = receipts.SaveOnce(finalReceipt)
		_, createdAgain, saveAgainErr = receipts.SaveOnce(finalReceipt)
	}
	receiptRaw, _ := os.ReadFile(receiptPath)
	receiptText := string(receiptRaw) + finalReceipt.Markdown()
	finalSecretPass := !strings.Contains(receiptText, secretSentinel)
	receiptPass := provisionalPass && receiptErr == nil && openReceiptErr == nil && saveErr == nil && saveAgainErr == nil && created && !createdAgain && taskStore.State().Progress().Complete && finalSecretPass
	out.ReceiptID = finalReceipt.ReceiptID
	add("timeline_and_receipt_bind_exact_third_party_package_identity", receiptPass, map[string]any{"tool_events": toolEventCount, "canonical_source_ref": canonicalPackageSource, "created_once": created && !createdAgain, "progress_complete": taskStore.State().Progress().Complete, "raw_secret_absent": finalSecretPass, "error": errString(errors.Join(provisionalErr, receiptErr, openReceiptErr, saveErr, saveAgainErr))})

	out.Passed = true
	for _, item := range out.Scenarios {
		out.Passed = out.Passed && item.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	out.NextGate = "Proof 0.23 — productionize third-party MCP discovery/configuration and connect the pinned codebase-memory-mcp context server when immutable executable retrieval is available without weakening identity verification."
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !out.Passed {
		return errors.New("Proof 0.22 failed")
	}
	return nil
}

func verifyImmutablePackage(paths proofPaths, identity mcpbridge.ServerIdentity) (bool, any, []proofreceipt.Artifact, error) {
	if err := identity.Validate(); err != nil {
		return false, nil, nil, err
	}
	tarballHash, tarballSize, err := fileSHA256(paths.Tarball)
	if err != nil {
		return false, nil, nil, err
	}
	lockHash, lockSize, err := fileSHA256(paths.PackageLock)
	if err != nil {
		return false, nil, nil, err
	}
	packageJSONHash, packageJSONSize, err := fileSHA256(paths.PackageJSON)
	if err != nil {
		return false, nil, nil, err
	}
	serverHash, serverSize, err := fileSHA256(paths.ServerJS)
	if err != nil {
		return false, nil, nil, err
	}

	var pkg packageJSON
	rawPackage, err := os.ReadFile(paths.PackageJSON)
	if err != nil {
		return false, nil, nil, err
	}
	if err := json.Unmarshal(rawPackage, &pkg); err != nil {
		return false, nil, nil, err
	}
	var lock packageLock
	rawLock, err := os.ReadFile(paths.PackageLock)
	if err != nil {
		return false, nil, nil, err
	}
	if err := json.Unmarshal(rawLock, &lock); err != nil {
		return false, nil, nil, err
	}
	entry, ok := lock.Packages["node_modules/"+packageName]
	pass := tarballHash == packageSHA256 && lockHash == packageLockSHA256 && pkg.Name == packageName && pkg.Version == packageVersion && ok && entry.Version == packageVersion && entry.Integrity == packageIntegrity
	detail := map[string]any{
		"package": pkg.Name, "version": pkg.Version, "installed_version": entry.Version,
		"integrity_matches": entry.Integrity == packageIntegrity, "tarball_sha256": tarballHash,
		"package_lock_sha256": lockHash, "package_json_sha256": packageJSONHash,
		"server_entrypoint_sha256": serverHash,
	}
	artifacts := []proofreceipt.Artifact{
		{Name: "third-party-npm-package", Path: "evidence/modelcontextprotocol-server-filesystem-2026.7.4.tgz", SHA256: tarballHash, Size: tarballSize},
		{Name: "third-party-package-lock", Path: "runtime/package-lock.json", SHA256: lockHash, Size: lockSize},
		{Name: "third-party-package-json", Path: "runtime/node_modules/@modelcontextprotocol/server-filesystem/package.json", SHA256: packageJSONHash, Size: packageJSONSize},
		{Name: "third-party-server-entrypoint", Path: "runtime/node_modules/@modelcontextprotocol/server-filesystem/dist/index.js", SHA256: serverHash, Size: serverSize},
	}
	return pass, detail, artifacts, nil
}

func newBridge(j *tooljournal.Journal, t *timeline.Store, a mcpbridge.Adapter, broker *secretbroker.Broker, schemas *mcpbridge.SchemaPolicy, profile mcpbridge.PermissionProfile, tools map[string]mcpbridge.PermissionProfile, identity *mcpbridge.ServerIdentity) *mcpbridge.Bridge {
	return &mcpbridge.Bridge{Journal: j, Timeline: t, TaskID: taskID, SessionID: sessionID, Adapter: a, Permissions: &mcpbridge.PermissionPolicy{Profile: profile, ToolProfiles: tools}, Schemas: schemas, Secrets: broker, ServerIdentity: identity}
}

func resolvePaths() (proofPaths, error) {
	p := proofPaths{
		Node:        envOr("KEYDECK_PROOF22_NODE", "node"),
		ServerJS:    os.Getenv("KEYDECK_PROOF22_SERVER_JS"),
		Tarball:     os.Getenv("KEYDECK_PROOF22_PACKAGE_TARBALL"),
		PackageLock: os.Getenv("KEYDECK_PROOF22_PACKAGE_LOCK"),
		PackageJSON: os.Getenv("KEYDECK_PROOF22_PACKAGE_JSON"),
	}
	if p.ServerJS == "" || p.Tarball == "" || p.PackageLock == "" || p.PackageJSON == "" {
		exe, err := os.Executable()
		if err == nil {
			base := filepath.Dir(exe)
			if p.ServerJS == "" {
				p.ServerJS = filepath.Join(base, "runtime", "node_modules", "@modelcontextprotocol", "server-filesystem", "dist", "index.js")
			}
			if p.Tarball == "" {
				p.Tarball = filepath.Join(base, "evidence", "modelcontextprotocol-server-filesystem-2026.7.4.tgz")
			}
			if p.PackageLock == "" {
				p.PackageLock = filepath.Join(base, "runtime", "package-lock.json")
			}
			if p.PackageJSON == "" {
				p.PackageJSON = filepath.Join(base, "runtime", "node_modules", "@modelcontextprotocol", "server-filesystem", "package.json")
			}
		}
	}
	for name, path := range map[string]string{"server JS": p.ServerJS, "package tarball": p.Tarball, "package lock": p.PackageLock, "package JSON": p.PackageJSON} {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return proofPaths{}, fmt.Errorf("Proof 0.22 %s is unavailable at %q", name, path)
		}
	}
	return p, nil
}

func fileSHA256(path string) (string, int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return sha256Hex(raw), int64(len(raw)), nil
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func scanPathsForValue(paths []string, value string) (bool, []string) {
	var leaks []string
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

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
