package mcpregistry

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
)

type fakeDiscoverer struct {
	identity mcpbridge.ServerIdentity
	contract RuntimeContract
	tools    []mcpbridge.Tool
	calls    int
}

func (d *fakeDiscoverer) BoundServerIdentity() *mcpbridge.ServerIdentity {
	copy := d.identity
	return &copy
}
func (d *fakeDiscoverer) BoundRuntimeContract() RuntimeContract { return d.contract }
func (d *fakeDiscoverer) DiscoverTools(context.Context) ([]mcpbridge.Tool, error) {
	d.calls++
	return d.tools, nil
}

func testIdentity() mcpbridge.ServerIdentity {
	return mcpbridge.ServerIdentity{Name: "filesystem", Version: "1.0.0", Registry: "npm", Package: "@example/filesystem", PackageIntegrity: "sha512-test", PackageSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EntryPoint: "dist/index.js"}
}

func testRuntime() RuntimeContract {
	return RuntimeContract{Transport: TransportStdio, Runtime: "node", Entrypoint: "dist/index.js", ProtocolVersion: mcpbridge.ProtocolVersion, MaxFrameBytes: 1 << 20, ArgumentSlots: []string{"allowed_root"}}
}

func TestRegistrationIsImmutableRestartSafeAndServerIDIgnoresRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.jsonl")
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistration(testIdentity(), testRuntime())
	if err != nil {
		t.Fatal(err)
	}
	_, created, err := r.Register(reg)
	if err != nil || !created {
		t.Fatalf("register created=%v err=%v", created, err)
	}
	_, created, err = r.Register(reg)
	if err != nil || created {
		t.Fatalf("duplicate register created=%v err=%v", created, err)
	}

	otherRuntime := testRuntime()
	otherRuntime.MaxFrameBytes *= 2
	conflict, err := NewRegistration(testIdentity(), otherRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if conflict.ServerID != reg.ServerID {
		t.Fatalf("server ID changed across runtime: %q %q", reg.ServerID, conflict.ServerID)
	}
	if _, _, err := r.Register(conflict); !errors.Is(err, ErrRegistrationConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, ok := reopened.Registration(reg.ServerID)
	if !ok || loaded.RuntimeSHA256 != reg.RuntimeSHA256 {
		t.Fatalf("registration did not survive restart: %+v", loaded)
	}
}

func TestDiscoveryCacheRuntimeDriftAndCapabilityDrift(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "registry.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistration(testIdentity(), testRuntime())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Register(reg); err != nil {
		t.Fatal(err)
	}
	d := &fakeDiscoverer{identity: testIdentity(), contract: testRuntime(), tools: []mcpbridge.Tool{{Name: "read_text_file", InputSchema: map[string]any{"type": "object"}}}}
	first, cached, err := r.Discover(context.Background(), reg.ServerID, d)
	if err != nil || cached || d.calls != 1 {
		t.Fatalf("first discover cached=%v calls=%d err=%v", cached, d.calls, err)
	}
	second, cached, err := r.Discover(context.Background(), reg.ServerID, d)
	if err != nil || !cached || d.calls != 1 || second.SchemaSHA256 != first.SchemaSHA256 {
		t.Fatalf("cache failed cached=%v calls=%d err=%v", cached, d.calls, err)
	}

	drift := *d
	drift.contract.MaxFrameBytes *= 2
	if _, _, err := r.Discover(context.Background(), reg.ServerID, &drift); !errors.Is(err, ErrRuntimeDrift) {
		t.Fatalf("expected runtime drift, got %v", err)
	}

	changed := &fakeDiscoverer{identity: testIdentity(), contract: testRuntime(), tools: []mcpbridge.Tool{{Name: "read_text_file", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}}}}
	if _, err := r.Revalidate(context.Background(), reg.ServerID, changed); !errors.Is(err, ErrCapabilityDrift) {
		t.Fatalf("expected capability drift, got %v", err)
	}
	still, ok := r.CachedDiscovery(reg.ServerID)
	if !ok || still.SchemaSHA256 != first.SchemaSHA256 {
		t.Fatal("capability drift replaced trusted cache")
	}
}
