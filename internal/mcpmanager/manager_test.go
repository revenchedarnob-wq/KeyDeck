package mcpmanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpregistry"
)

type testDiscoverer struct {
	identity mcpbridge.ServerIdentity
	contract mcpregistry.RuntimeContract
	tools    []mcpbridge.Tool
}

func (d *testDiscoverer) BoundServerIdentity() *mcpbridge.ServerIdentity    { x := d.identity; return &x }
func (d *testDiscoverer) BoundRuntimeContract() mcpregistry.RuntimeContract { return d.contract }
func (d *testDiscoverer) DiscoverTools(context.Context) ([]mcpbridge.Tool, error) {
	return d.tools, nil
}

type staticHealth struct{ observation HealthObservation }

func (h staticHealth) Check(context.Context, LocalBinding) (HealthObservation, error) {
	return h.observation, nil
}

func setupRegistry(t *testing.T) (*mcpregistry.Registry, mcpregistry.Registration, mcpregistry.DiscoverySnapshot) {
	t.Helper()
	identity := mcpbridge.ServerIdentity{
		Name: "filesystem", Version: "1.0.0", Registry: "npm", Package: "@example/filesystem",
		PackageIntegrity: "sha512-test", PackageSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EntryPoint: "dist/index.js",
	}
	contract := mcpregistry.RuntimeContract{
		Transport: mcpregistry.TransportStdio, Runtime: "node", Entrypoint: "dist/index.js",
		ProtocolVersion: mcpbridge.ProtocolVersion, MaxFrameBytes: 1 << 20, ArgumentSlots: []string{"allowed_root"},
	}
	reg, err := mcpregistry.NewRegistration(identity, contract)
	if err != nil {
		t.Fatal(err)
	}
	r, err := mcpregistry.Open(filepath.Join(t.TempDir(), "registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Register(reg); err != nil {
		t.Fatal(err)
	}
	d := &testDiscoverer{identity: identity, contract: contract, tools: []mcpbridge.Tool{
		{Name: "read_text_file", InputSchema: map[string]any{"type": "object"}},
		{Name: "write_file", InputSchema: map[string]any{"type": "object"}},
	}}
	snapshot, _, err := r.Discover(context.Background(), reg.ServerID, d)
	if err != nil {
		t.Fatal(err)
	}
	return r, reg, snapshot
}

func TestBindingRequiresExplicitRebindAndKeepsPortableIdentity(t *testing.T) {
	r, reg, _ := setupRegistry(t)
	path := filepath.Join(t.TempDir(), "manager.jsonl")
	m, err := Open(path, r)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := NewLocalBinding(reg, filepath.Join(t.TempDir(), "node"), filepath.Join(t.TempDir(), "server.js"), map[string]string{"allowed_root": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if created, err := m.Bind(b1); err != nil || !created {
		t.Fatalf("bind created=%v err=%v", created, err)
	}
	b2, err := NewLocalBinding(reg, filepath.Join(t.TempDir(), "node"), filepath.Join(t.TempDir(), "server.js"), map[string]string{"allowed_root": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Bind(b2); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("expected explicit rebind requirement, got %v", err)
	}
	if changed, err := m.Rebind(b2, "runtime repaired"); err != nil || !changed {
		t.Fatalf("rebind changed=%v err=%v", changed, err)
	}
	loaded, ok := r.Registration(reg.ServerID)
	if !ok || loaded.IdentitySHA256 != reg.IdentitySHA256 {
		t.Fatal("portable registration changed during local rebind")
	}
	if m.EventCount() != 2 {
		t.Fatalf("expected bound+rebound events, got %d", m.EventCount())
	}
}

func TestEnableApprovalHealthAndRestartSafeView(t *testing.T) {
	r, reg, snapshot := setupRegistry(t)
	path := filepath.Join(t.TempDir(), "manager.jsonl")
	m, err := Open(path, r)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewLocalBinding(reg, filepath.Join(t.TempDir(), "node"), filepath.Join(t.TempDir(), "server.js"), map[string]string{"allowed_root": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Bind(binding); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetEnabled(reg.ServerID, true, "user enabled server"); err != nil {
		t.Fatal(err)
	}
	approval, err := m.ApproveTools(reg.ServerID, snapshot.SchemaSHA256, []string{"read_text_file"})
	if err != nil {
		t.Fatal(err)
	}
	obs := HealthObservation{ServerID: reg.ServerID, BindingSHA256: binding.BindingSHA256, Status: HealthHealthy, DetailCode: "mcp_initialize_ok", ToolCount: 2}
	if _, err := m.CheckHealth(context.Background(), reg.ServerID, staticHealth{observation: obs}); err != nil {
		t.Fatal(err)
	}
	view, err := m.View(reg.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Ready || !view.Enabled || !view.ApprovalCurrent || view.Health.Status != HealthHealthy {
		t.Fatalf("unexpected view: %+v", view)
	}
	policy, err := m.EffectivePolicy(reg.ServerID, mcpbridge.ProfileSafeEdit)
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Allows("read_text_file") || policy.Allows("write_file") {
		t.Fatal("approval policy escaped explicit tool list")
	}

	reopened, err := Open(path, r)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := reopened.View(reg.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Ready || restarted.Approval == nil || restarted.Approval.ApprovalSHA256 != approval.ApprovalSHA256 {
		t.Fatalf("manager state did not survive restart: %+v", restarted)
	}
}

func TestMissingRuntimeHealthDoesNotDeletePortableState(t *testing.T) {
	r, reg, snapshot := setupRegistry(t)
	m, err := Open(filepath.Join(t.TempDir(), "manager.jsonl"), r)
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing-node")
	binding, err := NewLocalBinding(reg, missing, filepath.Join(t.TempDir(), "missing-server.js"), map[string]string{"allowed_root": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Bind(binding); err != nil {
		t.Fatal(err)
	}
	obs := HealthObservation{ServerID: reg.ServerID, BindingSHA256: binding.BindingSHA256, Status: HealthUnavailable, DetailCode: "runtime_missing"}
	if _, err := m.CheckHealth(context.Background(), reg.ServerID, staticHealth{observation: obs}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Registration(reg.ServerID); !ok {
		t.Fatal("portable registration was deleted")
	}
	cached, ok := r.CachedDiscovery(reg.ServerID)
	if !ok || cached.SchemaSHA256 != snapshot.SchemaSHA256 {
		t.Fatal("portable discovery was deleted or changed")
	}
	view, err := m.View(reg.ServerID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Health.Status != HealthUnavailable || view.Ready {
		t.Fatalf("unexpected unavailable view: %+v", view)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("test runtime unexpectedly exists: %v", err)
	}
}

func TestApprovalsAreSchemaBoundAndUnknownToolsRejected(t *testing.T) {
	r, reg, snapshot := setupRegistry(t)
	m, err := Open(filepath.Join(t.TempDir(), "manager.jsonl"), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.ApproveTools(reg.ServerID, "deadbeef", []string{"read_text_file"}); !errors.Is(err, ErrApprovalSchema) {
		t.Fatalf("expected schema denial, got %v", err)
	}
	if _, err := m.ApproveTools(reg.ServerID, snapshot.SchemaSHA256, []string{"unknown_tool"}); !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("expected unknown tool denial, got %v", err)
	}
	policy, err := m.EffectivePolicy(reg.ServerID, mcpbridge.ProfileFullControl)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Allows("read_text_file") || len(policy.ToolProfiles) != 0 {
		t.Fatal("manager auto-granted permissions")
	}
}
