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
	"strings"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpmanager"
	"keydeck.local/feasibilitylab/internal/mcpregistry"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
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
	taskID            = "proof24-task"
	sessionID         = "proof24-session"
	secretSentinel    = "PROOF24_SECRET_MUST_NEVER_PERSIST"
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
	ServerID             string     `json:"server_id"`
	IdentitySHA256       string     `json:"identity_sha256"`
	RuntimeSHA256        string     `json:"runtime_sha256"`
	SchemaSHA256         string     `json:"schema_sha256"`
	RegistrySHA256       string     `json:"registry_sha256"`
	ManagerSHA256        string     `json:"manager_sha256"`
	HealthyBindingSHA256 string     `json:"healthy_binding_sha256"`
	BrokenBindingSHA256  string     `json:"broken_binding_sha256"`
	ManagerEventCount    int        `json:"manager_event_count"`
	ReceiptID            string     `json:"receipt_id"`
	NextGate             string     `json:"next_gate"`
}

type paths struct {
	Node        string
	ServerJS    string
	Tarball     string
	PackageLock string
}

type realHealthChecker struct {
	maxFrameBytes int
}

func (h realHealthChecker) Check(ctx context.Context, binding mcpmanager.LocalBinding) (mcpmanager.HealthObservation, error) {
	base := mcpmanager.HealthObservation{ServerID: binding.ServerID, BindingSHA256: binding.BindingSHA256}
	if info, err := os.Stat(binding.RuntimePath); err != nil || info.IsDir() {
		base.Status = mcpmanager.HealthUnavailable
		base.DetailCode = "runtime_missing"
		return base, nil
	}
	if info, err := os.Stat(binding.EntrypointPath); err != nil || info.IsDir() {
		base.Status = mcpmanager.HealthUnavailable
		base.DetailCode = "entrypoint_missing"
		return base, nil
	}
	allowedRoot := binding.Arguments["allowed_root"]
	client := mcpbridge.NewClient(mcpbridge.CommandConfig{Path: binding.RuntimePath, Args: []string{binding.EntrypointPath, allowedRoot}, MaxFrameBytes: h.maxFrameBytes})
	session, err := client.Connect(ctx)
	if err != nil {
		base.Status = mcpmanager.HealthUnhealthy
		base.DetailCode = "launch_failed"
		return base, nil
	}
	defer session.Close()
	if err := session.Initialize(); err != nil {
		base.Status = mcpmanager.HealthUnhealthy
		base.DetailCode = "initialize_failed"
		return base, nil
	}
	tools, err := session.ListTools()
	if err != nil {
		base.Status = mcpmanager.HealthUnhealthy
		base.DetailCode = "tools_list_failed"
		return base, nil
	}
	base.Status = mcpmanager.HealthHealthy
	base.DetailCode = "initialize_and_tools_list_ok"
	base.ToolCount = len(tools)
	return base, nil
}

type registryDiscoverer struct {
	client   *mcpbridge.Client
	identity mcpbridge.ServerIdentity
	contract mcpregistry.RuntimeContract
}

func (d *registryDiscoverer) BoundServerIdentity() *mcpbridge.ServerIdentity {
	x := d.identity
	return &x
}
func (d *registryDiscoverer) BoundRuntimeContract() mcpregistry.RuntimeContract { return d.contract }
func (d *registryDiscoverer) DiscoverTools(ctx context.Context) ([]mcpbridge.Tool, error) {
	session, err := d.client.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if err := session.Initialize(); err != nil {
		return nil, err
	}
	return session.ListTools()
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
	identity := mcpbridge.ServerIdentity{Name: "Official MCP Filesystem reference server", Version: packageVersion, Registry: "npm", Package: packageName, PackageIntegrity: packageIntegrity, PackageSHA256: packageSHA256, EntryPoint: "dist/index.js"}
	identityHash, err := identity.Hash()
	if err != nil {
		return err
	}
	runtimeContract := mcpregistry.RuntimeContract{Transport: mcpregistry.TransportStdio, Runtime: "node", Entrypoint: "dist/index.js", ProtocolVersion: mcpbridge.ProtocolVersion, MaxFrameBytes: 4 << 20, ArgumentSlots: []string{"allowed_root"}}
	registration, err := mcpregistry.NewRegistration(identity, runtimeContract)
	if err != nil {
		return err
	}
	out := report{Proof: "0.24-durable-local-runtime-bindings-mcp-server-manager", Status: "failed", ServerID: registration.ServerID, IdentitySHA256: identityHash, RuntimeSHA256: registration.RuntimeSHA256}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	root := filepath.Join(os.TempDir(), "keydeck-proof24-state")
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	defer os.RemoveAll(root)
	fixtureRoot := filepath.Join(root, "allowed-root")
	if err := os.MkdirAll(fixtureRoot, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(fixtureRoot, "hello.txt"), []byte("Proof 0.24 manager fixture\n"), 0o600); err != nil {
		return err
	}
	registryPath := filepath.Join(root, "mcp-registry.jsonl")
	managerPath := filepath.Join(root, "mcp-manager.jsonl")
	journalPath := filepath.Join(root, "journal.jsonl")
	timelinePath := filepath.Join(root, "timeline.jsonl")
	taskPath := filepath.Join(root, "task.jsonl")
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
	taskManager := &tasks.Manager{Store: taskStore, Journal: journal}
	contract := tasks.Contract{Goal: "Prove durable machine-local MCP runtime bindings and one restart-safe server manager without mutating portable server identity or auto-granting tools.", ForbiddenScope: []string{"no network during proof execution", "no automatic tool grants", "no silent runtime rebinding", "no secret persistence"}, Checks: []tasks.AcceptanceCheck{
		{ID: "portable", Description: "Portable registration/discovery survive with no local binding"},
		{ID: "binding", Description: "Machine-local binding preserves canonical server identity and requires explicit rebind"},
		{ID: "approval", Description: "Permission approvals are explicit and schema-bound"},
		{ID: "health", Description: "Real pinned third-party runtime passes local health check"},
		{ID: "toggle", Description: "Enable/disable state survives restart"},
		{ID: "view", Description: "One manager view combines registration, discovery, binding, health and grants"},
		{ID: "unavailable", Description: "Missing runtime becomes unavailable without deleting portable state"},
		{ID: "repair", Description: "Repair/rebind is explicit and restores health without changing canonical identity"},
		{ID: "restart", Description: "Final manager state survives restart with no raw secret persistence"},
		{ID: "provenance", Description: "Proof Receipt binds exact portable registry and local manager artifacts"},
	}}
	if _, err := taskManager.Create(taskID, sessionID, contract); err != nil {
		return err
	}

	tarHash, tarSize, err := fileSHA256(p.Tarball)
	if err != nil {
		return err
	}
	lockHash, lockSize, err := fileSHA256(p.PackageLock)
	if err != nil {
		return err
	}
	if tarHash != packageSHA256 || lockHash != packageLockSHA256 {
		return errors.New("Proof 0.24 package evidence mismatch")
	}

	registry, err := mcpregistry.Open(registryPath)
	if err != nil {
		return err
	}
	if _, _, err := registry.Register(registration); err != nil {
		return err
	}
	client := mcpbridge.NewClient(mcpbridge.CommandConfig{Path: p.Node, Args: []string{p.ServerJS, fixtureRoot}, MaxFrameBytes: 4 << 20})
	discoverer := &registryDiscoverer{client: client, identity: identity, contract: runtimeContract}
	snapshot, cached, err := registry.Discover(context.Background(), registration.ServerID, discoverer)
	if err != nil || cached {
		return fmt.Errorf("Proof 0.24 initial discovery failed cached=%v err=%v", cached, err)
	}
	out.SchemaSHA256 = snapshot.SchemaSHA256
	serverManager, err := mcpmanager.Open(managerPath, registry)
	if err != nil {
		return err
	}
	initialView, err := serverManager.View(registration.ServerID)
	if err != nil {
		return err
	}
	portablePass := initialView.Binding == nil && initialView.Discovery != nil && initialView.Discovery.SchemaSHA256 == snapshot.SchemaSHA256 && initialView.ServerID == registration.ServerID && !initialView.Enabled && !initialView.Ready
	add("portable_registration_and_discovery_survive_without_local_binding", portablePass, map[string]any{"server_id": initialView.ServerID, "binding_present": initialView.Binding != nil, "discovery_present": initialView.Discovery != nil})
	mark(taskManager, "portable", portablePass, "portable registration and trusted discovery remained available before any machine-local binding")

	healthyBinding, err := mcpmanager.NewLocalBinding(registration, p.Node, p.ServerJS, map[string]string{"allowed_root": fixtureRoot})
	if err != nil {
		return err
	}
	out.HealthyBindingSHA256 = healthyBinding.BindingSHA256
	bound, bindErr := serverManager.Bind(healthyBinding)
	brokenBinding, err := mcpmanager.NewLocalBinding(registration, filepath.Join(root, "missing", "node"), p.ServerJS, map[string]string{"allowed_root": fixtureRoot})
	if err != nil {
		return err
	}
	out.BrokenBindingSHA256 = brokenBinding.BindingSHA256
	_, silentMutationErr := serverManager.Bind(brokenBinding)
	loadedReg, stillRegistered := registry.Registration(registration.ServerID)
	bindingPass := bindErr == nil && bound && errors.Is(silentMutationErr, mcpmanager.ErrBindingConflict) && stillRegistered && loadedReg.IdentitySHA256 == registration.IdentitySHA256
	add("machine_local_binding_preserves_canonical_server_id_and_blocks_silent_mutation", bindingPass, map[string]any{"bound": bound, "canonical_server_id": registration.ServerID, "silent_mutation_error": errString(silentMutationErr)})
	mark(taskManager, "binding", bindingPass, "local paths were stored separately and conflicting replacement required explicit rebind")
	_, _, _ = timelineStore.AppendOnce(timeline.Input{EventID: "proof24:bound", TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainTool, Kind: "mcp_local_runtime_bound", SourceRef: registration.ServerID, Summary: "machine-local runtime binding persisted separately from portable identity", DataHash: healthyBinding.BindingSHA256})

	_, wrongSchemaErr := serverManager.ApproveTools(registration.ServerID, "deadbeef", []string{"read_text_file"})
	_, unknownToolErr := serverManager.ApproveTools(registration.ServerID, snapshot.SchemaSHA256, []string{"unknown_tool"})
	approval, approvalErr := serverManager.ApproveTools(registration.ServerID, snapshot.SchemaSHA256, []string{"read_text_file"})
	policy, policyErr := serverManager.EffectivePolicy(registration.ServerID, mcpbridge.ProfileSafeEdit)
	approvalPass := errors.Is(wrongSchemaErr, mcpmanager.ErrApprovalSchema) && errors.Is(unknownToolErr, mcpmanager.ErrUnknownTool) && approvalErr == nil && policyErr == nil && policy.Allows("read_text_file") && !policy.Allows("write_file") && len(policy.ToolProfiles) == 1
	add("permission_approvals_are_explicit_schema_bound_and_default_deny", approvalPass, map[string]any{"approval_sha256": approval.ApprovalSHA256, "read_allowed": policyErr == nil && policy.Allows("read_text_file"), "write_allowed": policyErr == nil && policy.Allows("write_file"), "wrong_schema_error": errString(wrongSchemaErr), "unknown_tool_error": errString(unknownToolErr)})
	mark(taskManager, "approval", approvalPass, "only one explicitly approved read tool entered the schema-bound effective policy")

	if _, err := serverManager.SetEnabled(registration.ServerID, true, "explicit proof enable"); err != nil {
		return err
	}
	health, healthErr := serverManager.CheckHealth(context.Background(), registration.ServerID, realHealthChecker{maxFrameBytes: 4 << 20})
	healthPass := healthErr == nil && health.Status == mcpmanager.HealthHealthy && health.ToolCount == 14 && health.BindingSHA256 == healthyBinding.BindingSHA256
	add("real_pinned_third_party_runtime_health_check_succeeds", healthPass, map[string]any{"status": health.Status, "detail_code": health.DetailCode, "tool_count": health.ToolCount, "error": errString(healthErr)})
	mark(taskManager, "health", healthPass, "real local runtime initialized and listed all 14 pinned third-party tools")

	readyView, err := serverManager.View(registration.ServerID)
	if err != nil {
		return err
	}
	viewPass := readyView.Ready && readyView.Enabled && readyView.Binding != nil && readyView.Discovery != nil && readyView.Health.Status == mcpmanager.HealthHealthy && readyView.ApprovalCurrent && readyView.Approval != nil && len(readyView.Approval.ApprovedTools) == 1
	add("single_manager_view_combines_portable_and_machine_local_state", viewPass, map[string]any{"ready": readyView.Ready, "enabled": readyView.Enabled, "health": readyView.Health.Status, "approved_tools": mcpmanager.ApprovedToolNames(*readyView.Approval)})
	mark(taskManager, "view", viewPass, "one manager view combined registration, discovery, binding, health, enable state and grants")

	if _, err := serverManager.SetEnabled(registration.ServerID, false, "explicit proof disable"); err != nil {
		return err
	}
	serverManager, err = mcpmanager.Open(managerPath, registry)
	if err != nil {
		return err
	}
	disabledView, err := serverManager.View(registration.ServerID)
	if err != nil {
		return err
	}
	if _, err := serverManager.SetEnabled(registration.ServerID, true, "explicit proof re-enable"); err != nil {
		return err
	}
	togglePass := !disabledView.Enabled && !disabledView.Ready
	add("enable_disable_state_is_durable_across_restart", togglePass, map[string]any{"disabled_after_restart": !disabledView.Enabled, "ready_after_restart": disabledView.Ready})
	mark(taskManager, "toggle", togglePass, "disabled state survived manager restart before explicit re-enable")

	reboundBroken, rebindBrokenErr := serverManager.Rebind(brokenBinding, "simulate missing machine-local runtime")
	unavailableHealth, unavailableErr := serverManager.CheckHealth(context.Background(), registration.ServerID, realHealthChecker{maxFrameBytes: 4 << 20})
	unavailableView, viewErr := serverManager.View(registration.ServerID)
	portableReg, portableRegOK := registry.Registration(registration.ServerID)
	portableDiscovery, portableDiscoveryOK := registry.CachedDiscovery(registration.ServerID)
	unavailablePass := reboundBroken && rebindBrokenErr == nil && unavailableErr == nil && viewErr == nil && unavailableHealth.Status == mcpmanager.HealthUnavailable && unavailableHealth.DetailCode == "runtime_missing" && !unavailableView.Ready && portableRegOK && portableDiscoveryOK && portableReg.IdentitySHA256 == registration.IdentitySHA256 && portableDiscovery.SchemaSHA256 == snapshot.SchemaSHA256
	add("missing_local_runtime_becomes_unavailable_without_deleting_portable_state", unavailablePass, map[string]any{"status": unavailableHealth.Status, "detail_code": unavailableHealth.DetailCode, "registration_preserved": portableRegOK, "discovery_preserved": portableDiscoveryOK})
	mark(taskManager, "unavailable", unavailablePass, "missing runtime marked unavailable while portable registration and discovery remained intact")

	repaired, repairErr := serverManager.Rebind(healthyBinding, "repair machine-local runtime binding")
	recoveredHealth, recoveredErr := serverManager.CheckHealth(context.Background(), registration.ServerID, realHealthChecker{maxFrameBytes: 4 << 20})
	recoveredView, recoveredViewErr := serverManager.View(registration.ServerID)
	repairPass := repaired && repairErr == nil && recoveredErr == nil && recoveredViewErr == nil && recoveredHealth.Status == mcpmanager.HealthHealthy && recoveredHealth.ToolCount == 14 && recoveredView.Ready && recoveredView.ServerID == registration.ServerID && recoveredView.Registration.IdentitySHA256 == registration.IdentitySHA256
	add("explicit_repair_rebind_restores_health_without_changing_canonical_identity", repairPass, map[string]any{"repaired": repaired, "status": recoveredHealth.Status, "ready": recoveredView.Ready, "server_id": recoveredView.ServerID})
	mark(taskManager, "repair", repairPass, "explicit rebound event restored healthy local runtime while canonical identity stayed unchanged")
	_, _, _ = timelineStore.AppendOnce(timeline.Input{EventID: "proof24:repaired", TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainTool, Kind: "mcp_local_runtime_rebound", SourceRef: registration.ServerID, Summary: "explicit local runtime repair restored healthy binding", DataHash: healthyBinding.BindingSHA256})

	serverManager, err = mcpmanager.Open(managerPath, registry)
	if err != nil {
		return err
	}
	finalView, err := serverManager.View(registration.ServerID)
	if err != nil {
		return err
	}
	managerRaw, err := os.ReadFile(managerPath)
	if err != nil {
		return err
	}
	out.ManagerSHA256 = sha256Hex(managerRaw)
	out.ManagerEventCount = serverManager.EventCount()
	registryRaw, err := os.ReadFile(registryPath)
	if err != nil {
		return err
	}
	out.RegistrySHA256 = sha256Hex(registryRaw)
	persisted := append(append([]byte(nil), managerRaw...), registryRaw...)
	persisted = append(persisted, mustJSON(finalView)...)
	restartPass := finalView.Ready && finalView.Binding != nil && finalView.Binding.BindingSHA256 == healthyBinding.BindingSHA256 && finalView.ApprovalCurrent && out.ManagerEventCount == 10 && !strings.Contains(string(persisted), secretSentinel)
	add("restart_safe_manager_state_contains_no_raw_secret_persistence", restartPass, map[string]any{"ready": finalView.Ready, "event_count": out.ManagerEventCount, "secret_absent": !strings.Contains(string(persisted), secretSentinel)})
	mark(taskManager, "restart", restartPass, "final binding, health, enable state and approval survived restart with no secret sentinel in durable state")

	managerArtifactPath := filepath.Join(root, "mcp-manager-state.jsonl")
	if err := os.WriteFile(managerArtifactPath, managerRaw, 0o600); err != nil {
		return err
	}
	artifacts := []proofreceipt.Artifact{
		{Name: "mcp-portable-registry", Path: "mcp-registry.jsonl", SHA256: out.RegistrySHA256, Size: int64(len(registryRaw))},
		{Name: "mcp-local-manager", Path: "mcp-manager-state.jsonl", SHA256: out.ManagerSHA256, Size: int64(len(managerRaw))},
		{Name: "third-party-package", Path: "evidence/modelcontextprotocol-server-filesystem-2026.7.4.tgz", SHA256: tarHash, Size: tarSize},
		{Name: "third-party-package-lock", Path: "runtime/package-lock.json", SHA256: lockHash, Size: lockSize},
	}
	_, _, _ = timelineStore.AppendOnce(timeline.Input{EventID: "proof24:manager-state", TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainProof, Kind: "mcp_server_manager_state_sealed", SourceRef: registration.ServerID, Summary: "portable registry and machine-local manager artifacts sealed separately", DataHash: out.ManagerSHA256})
	provisional, provisionalErr := proofreceipt.Build(taskStore.State(), timelineStore.Snapshot(), artifacts)
	provisionalText := string(mustJSON(provisional)) + provisional.Markdown()
	provenancePrePass := provisionalErr == nil && strings.Contains(provisionalText, registration.ServerID) && strings.Contains(provisionalText, out.ManagerSHA256) && strings.Contains(provisionalText, out.RegistrySHA256)
	mark(taskManager, "provenance", provenancePrePass, "Proof Receipt binds canonical server ID plus separate portable-registry and local-manager artifacts")
	finalReceipt, receiptErr := proofreceipt.Build(taskStore.State(), timelineStore.Snapshot(), artifacts)
	receipts, openReceiptErr := proofreceipt.Open(receiptPath)
	var saved, savedAgain bool
	var saveErr, saveAgainErr error
	if receiptErr == nil && openReceiptErr == nil {
		_, saved, saveErr = receipts.SaveOnce(finalReceipt)
		_, savedAgain, saveAgainErr = receipts.SaveOnce(finalReceipt)
	}
	provenancePass := provenancePrePass && receiptErr == nil && openReceiptErr == nil && saveErr == nil && saveAgainErr == nil && saved && !savedAgain && taskStore.State().Progress().Complete
	out.ReceiptID = finalReceipt.ReceiptID
	add("proof_receipt_binds_portable_registry_and_local_manager_artifacts", provenancePass, map[string]any{"registry_sha256": out.RegistrySHA256, "manager_sha256": out.ManagerSHA256, "receipt_id": finalReceipt.ReceiptID, "created_once": saved && !savedAgain, "progress_complete": taskStore.State().Progress().Complete, "error": errString(errors.Join(provisionalErr, receiptErr, openReceiptErr, saveErr, saveAgainErr))})

	out.Passed = true
	for _, s := range out.Scenarios {
		out.Passed = out.Passed && s.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	out.NextGate = "Proof 0.25 — connect the durable MCP server manager to execution routing so only enabled, healthy, explicitly approved tools can reach the existing Bridge/Tool Journal, then resume codebase-memory-mcp v0.8.1 retrieval when immutable executable access is available."
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !out.Passed {
		return errors.New("Proof 0.24 failed")
	}
	return nil
}

func resolvePaths() (paths, error) {
	node := os.Getenv("KEYDECK_PROOF24_NODE")
	if node == "" {
		resolved, err := exec.LookPath("node")
		if err != nil {
			return paths{}, errors.New("Proof 0.24 requires local Node.js")
		}
		node, err = filepath.Abs(resolved)
		if err != nil {
			return paths{}, err
		}
	}
	p := paths{Node: node, ServerJS: os.Getenv("KEYDECK_PROOF24_SERVER_JS"), Tarball: os.Getenv("KEYDECK_PROOF24_PACKAGE_TARBALL"), PackageLock: os.Getenv("KEYDECK_PROOF24_PACKAGE_LOCK")}
	for name, path := range map[string]string{"node": p.Node, "server_js": p.ServerJS, "tarball": p.Tarball, "package_lock": p.PackageLock} {
		if strings.TrimSpace(path) == "" {
			return paths{}, fmt.Errorf("Proof 0.24 %s path is required", name)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return paths{}, err
		}
		switch name {
		case "node":
			p.Node = abs
		case "server_js":
			p.ServerJS = abs
		case "tarball":
			p.Tarball = abs
		case "package_lock":
			p.PackageLock = abs
		}
		if info, err := os.Stat(abs); err != nil || info.IsDir() {
			return paths{}, fmt.Errorf("Proof 0.24 %s unavailable at %q", name, abs)
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
func sha256Hex(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func mark(m *tasks.Manager, id string, passed bool, evidence string) {
	status := tasks.CheckFailed
	if passed {
		status = tasks.CheckPassed
	}
	_, _ = m.UpdateCheck(id, status, evidence)
}
func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
