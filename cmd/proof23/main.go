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

	"keydeck.local/feasibilitylab/internal/mcpbridge"
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
	taskID            = "proof23-task"
	sessionID         = "proof23-session"
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
	RegistrySHA256 string     `json:"registry_sha256"`
	ToolCount      int        `json:"tool_count"`
	ReceiptID      string     `json:"receipt_id"`
	NextGate       string     `json:"next_gate"`
}

type paths struct {
	Node        string
	ServerJS    string
	Tarball     string
	PackageLock string
}

type countingDiscoverer struct {
	inner mcpregistry.Discoverer
	calls int
}

func (d *countingDiscoverer) BoundServerIdentity() *mcpbridge.ServerIdentity {
	return d.inner.BoundServerIdentity()
}
func (d *countingDiscoverer) BoundRuntimeContract() mcpregistry.RuntimeContract {
	return d.inner.BoundRuntimeContract()
}
func (d *countingDiscoverer) DiscoverTools(ctx context.Context) ([]mcpbridge.Tool, error) {
	d.calls++
	return d.inner.DiscoverTools(ctx)
}

type staticDiscoverer struct {
	identity mcpbridge.ServerIdentity
	contract mcpregistry.RuntimeContract
	tools    []mcpbridge.Tool
	calls    int
}

func (d *staticDiscoverer) BoundServerIdentity() *mcpbridge.ServerIdentity {
	copy := d.identity
	return &copy
}
func (d *staticDiscoverer) BoundRuntimeContract() mcpregistry.RuntimeContract { return d.contract }
func (d *staticDiscoverer) DiscoverTools(context.Context) ([]mcpbridge.Tool, error) {
	d.calls++
	return d.tools, nil
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
	out := report{Proof: "0.23-production-mcp-registration-discovery-contracts", Status: "failed", ServerID: registration.ServerID, IdentitySHA256: identityHash, RuntimeSHA256: registration.RuntimeSHA256}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	root, err := os.MkdirTemp("", "keydeck-proof23-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	fixtureRoot := filepath.Join(root, "allowed-root")
	if err := os.MkdirAll(fixtureRoot, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(fixtureRoot, "hello.txt"), []byte("Proof 0.23 registry fixture\n"), 0o600); err != nil {
		return err
	}
	registryPath := filepath.Join(root, "mcp-registry.jsonl")
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
	manager := &tasks.Manager{Store: taskStore, Journal: journal}
	contract := tasks.Contract{Goal: "Prove restart-safe production MCP registration/discovery contracts without auto-granting tools or weakening KeyDeck safety policy.", ForbiddenScope: []string{"no network during proof execution", "no automatic tool grants", "no mutable latest package identity", "no raw secret persistence"}, Checks: []tasks.AcceptanceCheck{
		{ID: "registration", Description: "Immutable registration is created once and duplicate registration is deduplicated"},
		{ID: "canonical_id", Description: "Canonical server ID derives only from immutable identity while runtime drift conflicts"},
		{ID: "discovery", Description: "Real third-party tool discovery is persisted with exact identity/runtime/schema digests"},
		{ID: "cache", Description: "Restart reuses persisted discovery without invoking the server"},
		{ID: "runtime_drift", Description: "Runtime configuration drift is rejected before discovery invocation"},
		{ID: "capability_drift", Description: "Capability/schema drift is rejected without replacing trusted cache"},
		{ID: "permissions", Description: "Discovered tools receive suggestions but no automatic grants"},
		{ID: "provenance", Description: "Restart-safe registry artifact and Proof Receipt bind server ID and schema digest"},
	}}
	if _, err := manager.Create(taskID, sessionID, contract); err != nil {
		return err
	}

	// Pin exact package evidence before binding the discoverer.
	tarHash, tarSize, err := fileSHA256(p.Tarball)
	if err != nil {
		return err
	}
	lockHash, lockSize, err := fileSHA256(p.PackageLock)
	if err != nil {
		return err
	}
	if tarHash != packageSHA256 || lockHash != packageLockSHA256 {
		return errors.New("Proof 0.23 package evidence mismatch")
	}

	registry, err := mcpregistry.Open(registryPath)
	if err != nil {
		return err
	}
	_, created, regErr := registry.Register(registration)
	_, createdAgain, regAgainErr := registry.Register(registration)
	registrationPass := regErr == nil && regAgainErr == nil && created && !createdAgain
	add("immutable_registration_created_once_and_deduplicated", registrationPass, map[string]any{"created_once": created && !createdAgain, "server_id": registration.ServerID})
	mark(manager, "registration", registrationPass, "immutable registration appended once; exact duplicate returned existing record")
	_, _, _ = timelineStore.AppendOnce(timeline.Input{EventID: "proof23:registered", TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainTool, Kind: "mcp_server_registered", SourceRef: identity.CanonicalRef(), Summary: "immutable MCP server registration persisted", DataHash: registration.IdentitySHA256})

	alternateRuntime := runtimeContract
	alternateRuntime.MaxFrameBytes = 8 << 20
	alternateReg, err := mcpregistry.NewRegistration(identity, alternateRuntime)
	if err != nil {
		return err
	}
	_, _, conflictErr := registry.Register(alternateReg)
	canonicalPass := alternateReg.ServerID == registration.ServerID && alternateReg.RuntimeSHA256 != registration.RuntimeSHA256 && errors.Is(conflictErr, mcpregistry.ErrRegistrationConflict)
	add("canonical_server_id_survives_runtime_change_but_conflicting_runtime_is_rejected", canonicalPass, map[string]any{"same_server_id": alternateReg.ServerID == registration.ServerID, "runtime_hash_changed": alternateReg.RuntimeSHA256 != registration.RuntimeSHA256, "error": errString(conflictErr)})
	mark(manager, "canonical_id", canonicalPass, "server ID remained identity-derived while conflicting runtime contract was rejected")

	client := mcpbridge.NewClient(mcpbridge.CommandConfig{Path: p.Node, Args: []string{p.ServerJS, fixtureRoot}, MaxFrameBytes: 4 << 20})
	real := &mcpregistry.ClientDiscoverer{Client: client, Identity: &identity, Contract: runtimeContract}
	counted := &countingDiscoverer{inner: real}
	snapshot, cached, discoverErr := registry.Discover(context.Background(), registration.ServerID, counted)
	toolNames := make([]string, 0, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	discoveryPass := discoverErr == nil && !cached && counted.calls == 1 && len(snapshot.Tools) == 14 && contains(toolNames, "read_text_file") && contains(toolNames, "write_file") && snapshot.IdentitySHA256 == registration.IdentitySHA256 && snapshot.RuntimeSHA256 == registration.RuntimeSHA256 && snapshot.SchemaSHA256 != ""
	out.SchemaSHA256 = snapshot.SchemaSHA256
	out.ToolCount = len(snapshot.Tools)
	add("real_third_party_discovery_persisted_with_exact_identity_runtime_and_schema_digest", discoveryPass, map[string]any{"calls": counted.calls, "cached": cached, "tool_count": len(snapshot.Tools), "schema_sha256": snapshot.SchemaSHA256, "error": errString(discoverErr)})
	mark(manager, "discovery", discoveryPass, "real initialize/tools-list snapshot persisted with exact identity/runtime/schema digests")
	_, _, _ = timelineStore.AppendOnce(timeline.Input{EventID: "proof23:discovered", TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainTool, Kind: "mcp_tools_discovered", SourceRef: registration.ServerID, Summary: "tool capability snapshot persisted", DataHash: snapshot.SchemaSHA256})

	registry, err = mcpregistry.Open(registryPath)
	if err != nil {
		return err
	}
	countedAfterRestart := &countingDiscoverer{inner: real}
	cachedSnapshot, wasCached, cacheErr := registry.Discover(context.Background(), registration.ServerID, countedAfterRestart)
	cachePass := cacheErr == nil && wasCached && countedAfterRestart.calls == 0 && cachedSnapshot.SchemaSHA256 == snapshot.SchemaSHA256
	add("restart_reuses_persisted_discovery_without_server_invocation", cachePass, map[string]any{"cached": wasCached, "calls": countedAfterRestart.calls, "schema_same": cachedSnapshot.SchemaSHA256 == snapshot.SchemaSHA256, "error": errString(cacheErr)})
	mark(manager, "cache", cachePass, "registry restart loaded exact discovery cache and made zero server calls")

	driftReal := &mcpregistry.ClientDiscoverer{Client: client, Identity: &identity, Contract: alternateRuntime}
	driftCounted := &countingDiscoverer{inner: driftReal}
	_, _, runtimeDriftErr := registry.Discover(context.Background(), registration.ServerID, driftCounted)
	runtimeDriftPass := errors.Is(runtimeDriftErr, mcpregistry.ErrRuntimeDrift) && driftCounted.calls == 0
	add("runtime_configuration_drift_rejected_before_discovery_invocation", runtimeDriftPass, map[string]any{"calls": driftCounted.calls, "error": errString(runtimeDriftErr)})
	mark(manager, "runtime_drift", runtimeDriftPass, "runtime contract mismatch blocked before MCP process invocation")

	mutatedTools := make([]mcpbridge.Tool, 0, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		mutatedTools = append(mutatedTools, mcpbridge.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	if len(mutatedTools) > 0 {
		mutatedTools[0].Description += " drift"
	}
	mutated := &staticDiscoverer{identity: identity, contract: runtimeContract, tools: mutatedTools}
	_, capabilityDriftErr := registry.Revalidate(context.Background(), registration.ServerID, mutated)
	trustedAfterDrift, _ := registry.CachedDiscovery(registration.ServerID)
	capabilityPass := errors.Is(capabilityDriftErr, mcpregistry.ErrCapabilityDrift) && mutated.calls == 1 && trustedAfterDrift.SchemaSHA256 == snapshot.SchemaSHA256
	add("capability_schema_drift_rejected_without_replacing_trusted_cache", capabilityPass, map[string]any{"calls": mutated.calls, "cache_unchanged": trustedAfterDrift.SchemaSHA256 == snapshot.SchemaSHA256, "error": errString(capabilityDriftErr)})
	mark(manager, "capability_drift", capabilityPass, "revalidation drift was rejected and trusted discovery cache remained unchanged")

	proposal := mcpregistry.ProposePermissions(snapshot)
	noAutoGrant := true
	for _, tool := range proposal.Tools {
		if tool.DefaultGranted {
			noAutoGrant = false
		}
	}
	policy, policyErr := proposal.BuildPolicy(mcpbridge.ProfileSafeEdit, map[string]bool{"read_text_file": true})
	permissionsPass := policyErr == nil && noAutoGrant && policy.Allows("read_text_file") && !policy.Allows("write_file") && !policy.Allows("edit_file") && len(policy.ToolProfiles) == 1
	add("permission_proposal_grants_nothing_until_explicit_approval", permissionsPass, map[string]any{"proposal_tools": len(proposal.Tools), "all_default_denied": noAutoGrant, "approved_read": policyErr == nil && policy.Allows("read_text_file"), "write_denied": policyErr == nil && !policy.Allows("write_file"), "approved_tool_count": len(policy.ToolProfiles), "error": errString(policyErr)})
	mark(manager, "permissions", permissionsPass, "discovery generated suggestions only; explicit approval granted one read tool and nothing else")

	registryRaw, err := os.ReadFile(registryPath)
	if err != nil {
		return err
	}
	out.RegistrySHA256 = sha256Hex(registryRaw)
	discoveryRaw, err := json.Marshal(snapshot.Tools)
	if err != nil {
		return err
	}
	if sha256Hex(discoveryRaw) != snapshot.SchemaSHA256 {
		return errors.New("discovery schema artifact hash mismatch")
	}
	discoveryPath := filepath.Join(root, "mcp-discovery-schema.json")
	if err := os.WriteFile(discoveryPath, discoveryRaw, 0o600); err != nil {
		return err
	}
	artifacts := []proofreceipt.Artifact{
		{Name: "mcp-registry-events", Path: "mcp-registry.jsonl", SHA256: out.RegistrySHA256, Size: int64(len(registryRaw))},
		{Name: "mcp-discovery-schema", Path: "mcp-discovery-schema.json", SHA256: snapshot.SchemaSHA256, Size: int64(len(discoveryRaw))},
		{Name: "third-party-package", Path: "evidence/modelcontextprotocol-server-filesystem-2026.7.4.tgz", SHA256: tarHash, Size: tarSize},
		{Name: "third-party-package-lock", Path: "runtime/package-lock.json", SHA256: lockHash, Size: lockSize},
	}
	provisional, provisionalErr := proofreceipt.Build(taskStore.State(), timelineStore.Snapshot(), artifacts)
	provisionalText := string(mustJSON(provisional)) + provisional.Markdown()
	provenancePrePass := provisionalErr == nil && strings.Contains(provisionalText, registration.ServerID) && strings.Contains(provisionalText, snapshot.SchemaSHA256) && strings.Contains(provisionalText, out.RegistrySHA256)
	mark(manager, "provenance", provenancePrePass, "registry artifact and timeline bind canonical server ID and schema digest")
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
	add("restart_safe_registry_and_proof_receipt_bind_server_id_and_schema_digest", provenancePass, map[string]any{"registry_sha256": out.RegistrySHA256, "receipt_id": finalReceipt.ReceiptID, "created_once": saved && !savedAgain, "progress_complete": taskStore.State().Progress().Complete, "error": errString(errors.Join(provisionalErr, receiptErr, openReceiptErr, saveErr, saveAgainErr))})

	out.Passed = true
	for _, s := range out.Scenarios {
		out.Passed = out.Passed && s.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	out.NextGate = "Proof 0.24 — bind registered/discovered MCP servers to durable local runtime bindings and a user-visible server manager, then connect pinned codebase-memory-mcp when immutable executable retrieval is available."
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !out.Passed {
		return errors.New("Proof 0.23 failed")
	}
	return nil
}

func resolvePaths() (paths, error) {
	p := paths{Node: envOr("KEYDECK_PROOF23_NODE", "node"), ServerJS: os.Getenv("KEYDECK_PROOF23_SERVER_JS"), Tarball: os.Getenv("KEYDECK_PROOF23_PACKAGE_TARBALL"), PackageLock: os.Getenv("KEYDECK_PROOF23_PACKAGE_LOCK")}
	if p.ServerJS == "" || p.Tarball == "" || p.PackageLock == "" {
		return paths{}, errors.New("Proof 0.23 requires exact server JS, package tarball and package-lock paths")
	}
	for name, path := range map[string]string{"server_js": p.ServerJS, "tarball": p.Tarball, "package_lock": p.PackageLock} {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return paths{}, fmt.Errorf("Proof 0.23 %s unavailable at %q", name, path)
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
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
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
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
