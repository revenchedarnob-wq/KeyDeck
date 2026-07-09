package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpmanager"
	"keydeck.local/feasibilitylab/internal/mcpregistry"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/secretbroker"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

const (
	packageName       = "@modelcontextprotocol/server-filesystem"
	packageVersion    = "2026.7.4"
	packageIntegrity  = "sha512-JwEaH4dRRzwcNMwX8WJVCJyXfFxXjFKdgwHxjQhFLhi02kszgyyj611LV9puBLDO1IiDQSCjfKFSPaemegnvwg=="
	packageSHA256     = "7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54"
	packageLockSHA256 = "e367ec6701c275457847b8692b55edb5aa2fecde8b01cd5a2966935f35f59e29"
	taskID            = "proof25-task"
	sessionID         = "proof25-session"
	secretSentinel    = "PROOF25_SECRET_MUST_NEVER_PERSIST_123456"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}
type report struct {
	Proof          string     `json:"proof"`
	Status         string     `json:"status"`
	Passed         bool       `json:"passed"`
	Scenarios      []scenario `json:"scenarios"`
	ServerID       string     `json:"server_id"`
	IdentitySHA256 string     `json:"identity_sha256"`
	RuntimeSHA256  string     `json:"runtime_sha256"`
	SchemaSHA256   string     `json:"schema_sha256"`
	ReceiptID      string     `json:"receipt_id"`
	NextGate       string     `json:"next_gate"`
}
type paths struct{ Node, ServerJS, Tarball, PackageLock string }

type staticHealth struct {
	status mcpmanager.HealthStatus
	detail string
	tools  int
}

func (h staticHealth) Check(_ context.Context, b mcpmanager.LocalBinding) (mcpmanager.HealthObservation, error) {
	return mcpmanager.HealthObservation{ServerID: b.ServerID, BindingSHA256: b.BindingSHA256, Status: h.status, DetailCode: h.detail, ToolCount: h.tools}, nil
}

type countFactory struct {
	inner  mcpmanager.AdapterFactory
	mu     sync.Mutex
	builds int
}

func (f *countFactory) Build(ctx context.Context, p mcpmanager.ExecutionPlan) (mcpbridge.Adapter, error) {
	f.mu.Lock()
	f.builds++
	f.mu.Unlock()
	return f.inner.Build(ctx, p)
}
func (f *countFactory) Count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.builds }

type captureAdapter struct {
	identity mcpbridge.ServerIdentity
	mu       *sync.Mutex
	calls    *int
	text     string
	err      error
}

func (a *captureAdapter) BoundServerIdentity() *mcpbridge.ServerIdentity { x := a.identity; return &x }
func (a *captureAdapter) Invoke(_ context.Context, _ string, _ map[string]any) (mcpbridge.CallToolResult, error) {
	a.mu.Lock()
	*a.calls++
	a.mu.Unlock()
	if a.err != nil {
		return mcpbridge.CallToolResult{}, a.err
	}
	return mcpbridge.CallToolResult{Content: []mcpbridge.Content{{Type: "text", Text: a.text}}}, nil
}

type captureFactory struct {
	identity mcpbridge.ServerIdentity
	mu       sync.Mutex
	calls    int
	builds   int
	text     string
	err      error
}

func (f *captureFactory) Build(context.Context, mcpmanager.ExecutionPlan) (mcpbridge.Adapter, error) {
	f.mu.Lock()
	f.builds++
	f.mu.Unlock()
	return &captureAdapter{identity: f.identity, mu: &f.mu, calls: &f.calls, text: f.text, err: f.err}, nil
}
func (f *captureFactory) Counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds, f.calls
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	p, err := resolvePaths()
	if err != nil {
		return err
	}
	if h, _, err := fileSHA256(p.Tarball); err != nil || h != packageSHA256 {
		return fmt.Errorf("pinned tarball mismatch: %s %v", h, err)
	}
	if h, _, err := fileSHA256(p.PackageLock); err != nil || h != packageLockSHA256 {
		return fmt.Errorf("pinned lock mismatch: %s %v", h, err)
	}
	identity := mcpbridge.ServerIdentity{Name: "Official MCP Filesystem reference server", Version: packageVersion, Registry: "npm", Package: packageName, PackageIntegrity: packageIntegrity, PackageSHA256: packageSHA256, EntryPoint: "dist/index.js"}
	identityHash, _ := identity.Hash()
	runtime := mcpregistry.RuntimeContract{Transport: mcpregistry.TransportStdio, Runtime: "node", Entrypoint: "dist/index.js", ProtocolVersion: mcpbridge.ProtocolVersion, MaxFrameBytes: 4 << 20, ArgumentSlots: []string{"allowed_root"}}
	reg, err := mcpregistry.NewRegistration(identity, runtime)
	if err != nil {
		return err
	}
	out := report{Proof: "0.25-manager-gated-mcp-execution-routing-reconstructed", Status: "failed", ServerID: reg.ServerID, IdentitySHA256: identityHash, RuntimeSHA256: reg.RuntimeSHA256, NextGate: "Proof 0.26 — Production Context Scout/Compiler"}
	add := func(name string, pass bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: pass, Detail: detail})
	}

	root := filepath.Join(os.TempDir(), "keydeck-proof25-state")
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)
	allowedRoot := filepath.Join(root, "allowed-root")
	if err := os.MkdirAll(allowedRoot, 0700); err != nil {
		return err
	}
	fixturePath := filepath.Join(allowedRoot, "hello.txt")
	if err := os.WriteFile(fixturePath, []byte("Proof 0.25 routed execution fixture\n"), 0600); err != nil {
		return err
	}
	registryPath := filepath.Join(root, "registry.jsonl")
	registry, err := mcpregistry.Open(registryPath)
	if err != nil {
		return err
	}
	if _, _, err = registry.Register(reg); err != nil {
		return err
	}
	client := mcpbridge.NewClient(mcpbridge.CommandConfig{Path: p.Node, Args: []string{p.ServerJS, allowedRoot}, MaxFrameBytes: 4 << 20})
	snap, _, err := registry.Discover(context.Background(), reg.ServerID, &mcpregistry.ClientDiscoverer{Client: client, Identity: &identity, Contract: runtime})
	if err != nil {
		return err
	}
	out.SchemaSHA256 = snap.SchemaSHA256

	readyManagerPath := filepath.Join(root, "manager-ready.jsonl")
	ready, err := mcpmanager.Open(readyManagerPath, registry)
	if err != nil {
		return err
	}
	binding, err := mcpmanager.NewLocalBinding(reg, p.Node, p.ServerJS, map[string]string{"allowed_root": allowedRoot})
	if err != nil {
		return err
	}
	if _, err = ready.Bind(binding); err != nil {
		return err
	}
	if _, err = ready.SetEnabled(reg.ServerID, true, "proof execution"); err != nil {
		return err
	}
	if _, err = ready.ApproveTools(reg.ServerID, snap.SchemaSHA256, []string{"read_text_file"}); err != nil {
		return err
	}
	if _, err = ready.CheckHealth(context.Background(), reg.ServerID, staticHealth{mcpmanager.HealthHealthy, "real_ready", 14}); err != nil {
		return err
	}

	schemaRead := &mcpbridge.SchemaPolicy{Tools: map[string]mcpbridge.ArgumentSchema{"read_text_file": {Fields: map[string]mcpbridge.FieldPolicy{"path": {Type: mcpbridge.ValueString, Required: true, MaxStringBytes: 4096}}, AllowUnknown: false}}}
	journalPath := filepath.Join(root, "journal.jsonl")
	timelinePath := filepath.Join(root, "timeline.jsonl")
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		return err
	}
	tl, err := timeline.Open(timelinePath)
	if err != nil {
		return err
	}

	// 1. Manager readiness states block before adapter construction.
	blockedKinds := []struct {
		name  string
		setup func(*mcpmanager.Manager) error
		want  error
	}{
		{"unbound", func(*mcpmanager.Manager) error { return nil }, mcpmanager.ErrExecutionUnbound},
		{"disabled", func(m *mcpmanager.Manager) error { _, e := m.Bind(binding); return e }, mcpmanager.ErrExecutionDisabled},
		{"unavailable", func(m *mcpmanager.Manager) error {
			if _, e := m.Bind(binding); e != nil {
				return e
			}
			if _, e := m.SetEnabled(reg.ServerID, true, "x"); e != nil {
				return e
			}
			if _, e := m.ApproveTools(reg.ServerID, snap.SchemaSHA256, []string{"read_text_file"}); e != nil {
				return e
			}
			_, e := m.CheckHealth(context.Background(), reg.ServerID, staticHealth{mcpmanager.HealthUnavailable, "missing", 0})
			return e
		}, mcpmanager.ErrExecutionUnavailable},
		{"unhealthy", func(m *mcpmanager.Manager) error {
			if _, e := m.Bind(binding); e != nil {
				return e
			}
			if _, e := m.SetEnabled(reg.ServerID, true, "x"); e != nil {
				return e
			}
			if _, e := m.ApproveTools(reg.ServerID, snap.SchemaSHA256, []string{"read_text_file"}); e != nil {
				return e
			}
			_, e := m.CheckHealth(context.Background(), reg.ServerID, staticHealth{mcpmanager.HealthUnhealthy, "failed", 0})
			return e
		}, mcpmanager.ErrExecutionUnhealthy},
	}
	blockedPass := true
	blockedDetail := map[string]string{}
	for i, b := range blockedKinds {
		m, _ := mcpmanager.Open(filepath.Join(root, fmt.Sprintf("manager-block-%d.jsonl", i)), registry)
		if err := b.setup(m); err != nil {
			return err
		}
		cf := &countFactory{inner: mcpmanager.CommandAdapterFactory{}}
		r := router(m, cf, journal, tl, schemaRead, nil, mcpbridge.ProfileReadOnly)
		_, e := r.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "blocked-" + b.name, Tool: "read_text_file", Arguments: map[string]any{"path": fixturePath}, Policy: tooljournal.ReplayIdempotent})
		blockedDetail[b.name] = fmt.Sprint(e)
		if !errors.Is(e, b.want) || cf.Count() != 0 {
			blockedPass = false
		}
	}
	add("manager_states_block_before_adapter_construction", blockedPass, blockedDetail)

	// 2. Unapproved tool and insufficient profile block before adapter construction.
	m2, _ := mcpmanager.Open(filepath.Join(root, "manager-policy.jsonl"), registry)
	_, _ = m2.Bind(binding)
	_, _ = m2.SetEnabled(reg.ServerID, true, "x")
	_, _ = m2.CheckHealth(context.Background(), reg.ServerID, staticHealth{mcpmanager.HealthHealthy, "ok", 14})
	cf2 := &countFactory{inner: mcpmanager.CommandAdapterFactory{}}
	r2 := router(m2, cf2, journal, tl, schemaRead, nil, mcpbridge.ProfileReadOnly)
	_, eUnapproved := r2.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "unapproved", Tool: "read_text_file", Arguments: map[string]any{"path": fixturePath}, Policy: tooljournal.ReplayIdempotent})
	_, _ = m2.ApproveTools(reg.ServerID, snap.SchemaSHA256, []string{"write_file"})
	r2.ActiveProfile = mcpbridge.ProfileReadOnly
	_, eProfile := r2.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "profile", Tool: "write_file", Arguments: map[string]any{}, Policy: tooljournal.ReplayForbidden})
	add("unapproved_and_insufficient_profile_block_before_adapter_construction", errors.Is(eUnapproved, mcpmanager.ErrExecutionUnapproved) && errors.Is(eProfile, mcpmanager.ErrExecutionProfileDenied) && cf2.Count() == 0, map[string]any{"unapproved": fmt.Sprint(eUnapproved), "profile": fmt.Sprint(eProfile)})

	// 3. Approved route executes real third-party tool.
	realFactory := &countFactory{inner: mcpmanager.CommandAdapterFactory{}}
	realRouter := router(ready, realFactory, journal, tl, schemaRead, nil, mcpbridge.ProfileReadOnly)
	realResult, realErr := realRouter.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "real-read", Tool: "read_text_file", Arguments: map[string]any{"path": fixturePath}, Policy: tooljournal.ReplayIdempotent})
	add("approved_route_executes_real_pinned_third_party_tool", realErr == nil && strings.Contains(realResult.Text, "Proof 0.25 routed execution fixture") && realFactory.Count() == 1, map[string]any{"error": fmt.Sprint(realErr), "result": realResult.Text})

	// Shared secret schema/factory for 4-8.
	secureSchema := &mcpbridge.SchemaPolicy{Tools: map[string]mcpbridge.ArgumentSchema{"read_text_file": {Fields: map[string]mcpbridge.FieldPolicy{"resource": {Type: mcpbridge.ValueString, Required: true}, "credential": {Required: true, SecretReference: true, Sensitive: true}}, AllowUnknown: false}}}
	allowedBroker, _ := secretbroker.New([]secretbroker.Entry{{Scope: "provider.read", Name: "primary", Value: secretSentinel}}, secretbroker.Policy{ToolScopes: map[string]map[string]bool{"read_text_file": {"provider.read": true}}})
	deniedBroker, _ := secretbroker.New([]secretbroker.Entry{{Scope: "provider.read", Name: "primary", Value: secretSentinel}}, secretbroker.Policy{ToolScopes: map[string]map[string]bool{"read_text_file": {"provider.other": true}}})
	secureArgs := map[string]any{"resource": "alpha", "credential": secretbroker.Value("provider.read", "primary")}

	// 4. Schema denial precedes secret planning/journal/adapter invocation.
	fac4 := &captureFactory{identity: identity, text: "ok"}
	broker4, _ := secretbroker.New([]secretbroker.Entry{{Scope: "provider.read", Name: "primary", Value: secretSentinel}}, secretbroker.Policy{ToolScopes: map[string]map[string]bool{"read_text_file": {"provider.read": true}}})
	j4, _ := tooljournal.Open(filepath.Join(root, "j4.jsonl"))
	t4, _ := timeline.Open(filepath.Join(root, "t4.jsonl"))
	r4 := router(ready, fac4, j4, t4, secureSchema, broker4, mcpbridge.ProfileReadOnly)
	_, e4 := r4.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "schema-deny", Tool: "read_text_file", Arguments: map[string]any{"resource": true, "credential": secretbroker.Value("provider.read", "primary")}, Policy: tooljournal.ReplayForbidden})
	p4, res4 := broker4.Counts()
	_, calls4 := fac4.Counts()
	add("schema_denial_precedes_secret_plan_journal_and_adapter", errors.Is(e4, mcpbridge.ErrArgumentSchemaDenied) && p4 == 0 && res4 == 0 && len(j4.Snapshot()) == 0 && calls4 == 0, map[string]any{"error": fmt.Sprint(e4), "plans": p4, "resolutions": res4, "calls": calls4})

	// 5. Scope denial permits one value-free plan, then blocks before journal/resolution/adapter.
	fac5 := &captureFactory{identity: identity, text: "ok"}
	j5, _ := tooljournal.Open(filepath.Join(root, "j5.jsonl"))
	t5, _ := timeline.Open(filepath.Join(root, "t5.jsonl"))
	r5 := router(ready, fac5, j5, t5, secureSchema, deniedBroker, mcpbridge.ProfileReadOnly)
	_, e5 := r5.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "scope-deny", Tool: "read_text_file", Arguments: secureArgs, Policy: tooljournal.ReplayForbidden})
	p5, res5 := deniedBroker.Counts()
	_, calls5 := fac5.Counts()
	add("secret_scope_denial_stops_after_value_free_plan_before_journal_resolution_and_adapter", errors.Is(e5, secretbroker.ErrScopeDenied) && p5 == 1 && res5 == 0 && len(j5.Snapshot()) == 0 && calls5 == 0, map[string]any{"error": fmt.Sprint(e5), "plans": p5, "resolutions": res5, "calls": calls5})

	// 6. Approved execution journals before resolve and adapter; result is redacted.
	fac6 := &captureFactory{identity: identity, text: "tool result did not echo secret"}
	j6, _ := tooljournal.Open(filepath.Join(root, "j6.jsonl"))
	t6, _ := timeline.Open(filepath.Join(root, "t6.jsonl"))
	r6 := router(ready, fac6, j6, t6, secureSchema, allowedBroker, mcpbridge.ProfileReadOnly)
	result6, e6 := r6.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "secure-ok", Tool: "read_text_file", Arguments: secureArgs, Policy: tooljournal.ReplayForbidden})
	p6, res6 := allowedBroker.Counts()
	_, calls6 := fac6.Counts()
	rec6 := j6.Snapshot()["secure-ok"]
	add("approved_execution_preserves_schema_secret_broker_tool_journal_adapter_order", e6 == nil && p6 == 1 && res6 == 1 && calls6 == 1 && rec6.State == tooljournal.StateCompleted && !strings.Contains(result6.Text, secretSentinel), map[string]any{"plans": p6, "resolutions": res6, "calls": calls6, "journal_state": rec6.State})

	// 7. Completed operation reuses after restart without second resolution or adapter invoke.
	fac7 := &captureFactory{identity: identity, text: "stable"}
	broker7, _ := secretbroker.New([]secretbroker.Entry{{Scope: "provider.read", Name: "primary", Value: secretSentinel}}, secretbroker.Policy{ToolScopes: map[string]map[string]bool{"read_text_file": {"provider.read": true}}})
	j7Path := filepath.Join(root, "j7.jsonl")
	t7Path := filepath.Join(root, "t7.jsonl")
	j7, _ := tooljournal.Open(j7Path)
	t7, _ := timeline.Open(t7Path)
	r7 := router(ready, fac7, j7, t7, secureSchema, broker7, mcpbridge.ProfileReadOnly)
	op7 := mcpbridge.Operation{OperationID: "complete-reuse", Tool: "read_text_file", Arguments: secureArgs, Policy: tooljournal.ReplayForbidden}
	_, e71 := r7.Execute(context.Background(), reg.ServerID, op7)
	_, res71 := broker7.Counts()
	j7b, _ := tooljournal.Open(j7Path)
	t7b, _ := timeline.Open(t7Path)
	r7b := router(ready, fac7, j7b, t7b, secureSchema, broker7, mcpbridge.ProfileReadOnly)
	rr7, e72 := r7b.Execute(context.Background(), reg.ServerID, op7)
	_, res72 := broker7.Counts()
	_, calls7 := fac7.Counts()
	add("completed_operation_reuses_after_restart_without_secret_or_adapter_replay", e71 == nil && e72 == nil && rr7.Reused && res71 == 1 && res72 == 1 && calls7 == 1, map[string]any{"reused": rr7.Reused, "resolutions_before": res71, "resolutions_after": res72, "adapter_calls": calls7})

	// 8. Ambiguous non-repeatable operation remains blocked after restart.
	j8Path := filepath.Join(root, "j8.jsonl")
	t8Path := filepath.Join(root, "t8.jsonl")
	j8, _ := tooljournal.Open(j8Path)
	raw8, _ := json.Marshal(map[string]any{"path": fixturePath})
	_, _ = j8.Begin("ambiguous-op", "mcp:read_text_file", raw8, tooljournal.ReplayForbidden)
	j8b, _ := tooljournal.Open(j8Path)
	t8, _ := timeline.Open(t8Path)
	fac8 := &captureFactory{identity: identity, text: "must-not-run"}
	r8 := router(ready, fac8, j8b, t8, schemaRead, nil, mcpbridge.ProfileReadOnly)
	_, e8 := r8.Execute(context.Background(), reg.ServerID, mcpbridge.Operation{OperationID: "ambiguous-op", Tool: "read_text_file", Arguments: map[string]any{"path": fixturePath}, Policy: tooljournal.ReplayForbidden})
	_, calls8 := fac8.Counts()
	add("ambiguous_non_repeatable_operation_remains_blocked_after_restart", errors.Is(e8, tooljournal.ErrAmbiguousOperation) && calls8 == 0, map[string]any{"error": fmt.Sprint(e8), "adapter_calls": calls8})

	// 9. Restarted manager preserves exact server/schema route evidence.
	restarted, err := mcpmanager.Open(readyManagerPath, registry)
	if err != nil {
		return err
	}
	view9, err := restarted.View(reg.ServerID)
	pass9 := err == nil && view9.Ready && view9.ServerID == reg.ServerID && view9.Discovery != nil && view9.Discovery.SchemaSHA256 == snap.SchemaSHA256 && view9.ApprovalCurrent
	add("restart_route_preserves_canonical_server_and_current_schema", pass9, map[string]any{"server_id": view9.ServerID, "schema": func() string {
		if view9.Discovery == nil {
			return ""
		}
		return view9.Discovery.SchemaSHA256
	}(), "ready": view9.Ready})

	// 10. Receipt provenance binds manager/registry/journal/timeline/package and excludes secret.
	taskStore, _ := tasks.Open(filepath.Join(root, "task.jsonl"))
	taskManager := &tasks.Manager{Store: taskStore, Journal: journal}
	checkNames := []string{"manager states", "permission profiles", "real third-party execution", "schema ordering", "secret scope ordering", "approved execution ordering", "completed restart reuse", "ambiguous restart block", "route provenance restart", "receipt provenance"}
	checks := make([]tasks.AcceptanceCheck, 10)
	for i, name := range checkNames {
		checks[i] = tasks.AcceptanceCheck{ID: fmt.Sprintf("c%02d", i+1), Description: name}
	}
	contract := tasks.Contract{Goal: "Prove manager-gated MCP execution routing on the exact recovered Proof 0.24 lineage.", ForbiddenScope: []string{"no automatic grants", "no secret persistence", "no bypass of Tool Journal"}, Checks: checks}
	if _, err = taskManager.Create(taskID, sessionID, contract); err != nil {
		return err
	}
	for i, s := range out.Scenarios {
		status := tasks.CheckFailed
		if s.Passed {
			status = tasks.CheckPassed
		}
		if _, err = taskManager.UpdateCheck(fmt.Sprintf("c%02d", i+1), status, s.Name); err != nil {
			return err
		}
	}
	artifacts := []proofreceipt.Artifact{}
	for _, item := range []struct{ name, path string }{{"portable MCP registry", registryPath}, {"local MCP manager", readyManagerPath}, {"Tool Journal", journalPath}, {"execution timeline", timelinePath}, {"pinned package tarball", p.Tarball}, {"pinned package lock", p.PackageLock}} {
		h, size, e := fileSHA256(item.path)
		if e != nil {
			return e
		}
		artifacts = append(artifacts, proofreceipt.Artifact{Name: item.name, Path: item.path, SHA256: h, Size: size})
	}
	receipt, err := proofreceipt.BuildRedacted(taskStore.State(), tl.Snapshot(), artifacts, []string{secretSentinel})
	if err != nil {
		return err
	}
	out.ReceiptID = receipt.ReceiptID
	receiptRaw, _ := json.Marshal(receipt)
	persisted := []string{registryPath, readyManagerPath, journalPath, timelinePath}
	leak := strings.Contains(string(receiptRaw), secretSentinel)
	for _, path := range persisted {
		b, _ := os.ReadFile(path)
		leak = leak || strings.Contains(string(b), secretSentinel)
	}
	pass10 := !leak && len(receipt.Artifacts) == 6 && strings.Contains(string(receiptRaw), reg.ServerID)
	add("proof_receipt_binds_manager_registry_journal_timeline_and_package_without_raw_secret", pass10, map[string]any{"receipt_id": receipt.ReceiptID, "artifact_count": len(receipt.Artifacts), "secret_present": leak})
	// update final check after scenario 10 itself
	if _, err = taskManager.UpdateCheck("c10", map[bool]tasks.CheckStatus{true: tasks.CheckPassed, false: tasks.CheckFailed}[pass10], "receipt provenance"); err != nil {
		return err
	}

	out.Passed = len(out.Scenarios) == 10
	for _, s := range out.Scenarios {
		out.Passed = out.Passed && s.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	if !out.Passed {
		return errors.New("Proof 0.25 acceptance contract failed")
	}
	return nil
}

func router(m *mcpmanager.Manager, f mcpmanager.AdapterFactory, j *tooljournal.Journal, t *timeline.Store, s *mcpbridge.SchemaPolicy, b *secretbroker.Broker, p mcpbridge.PermissionProfile) *mcpmanager.ExecutionRouter {
	return &mcpmanager.ExecutionRouter{Manager: m, Factory: f, Journal: j, Timeline: t, TaskID: taskID, SessionID: sessionID, ActiveProfile: p, Schemas: s, Secrets: b}
}
func resolvePaths() (paths, error) {
	p := paths{Node: os.Getenv("KEYDECK_PROOF25_NODE"), ServerJS: os.Getenv("KEYDECK_PROOF25_SERVER_JS"), Tarball: os.Getenv("KEYDECK_PROOF25_TARBALL"), PackageLock: os.Getenv("KEYDECK_PROOF25_PACKAGE_LOCK")}
	for name, value := range map[string]string{"node": p.Node, "server_js": p.ServerJS, "tarball": p.Tarball, "package_lock": p.PackageLock} {
		if strings.TrimSpace(value) == "" {
			return paths{}, fmt.Errorf("missing Proof 0.25 path %s", name)
		}
	}
	return p, nil
}
func fileSHA256(path string) (string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), st.Size(), nil
}
