package contextscout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpmanager"
	"keydeck.local/feasibilitylab/internal/mcpregistry"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type testDiscoverer struct {
	identity mcpbridge.ServerIdentity
	contract mcpregistry.RuntimeContract
	tools    []mcpbridge.Tool
}

func (d *testDiscoverer) BoundServerIdentity() *mcpbridge.ServerIdentity    { x := d.identity; return &x }
func (d *testDiscoverer) BoundRuntimeContract() mcpregistry.RuntimeContract { return d.contract }
func (d *testDiscoverer) DiscoverTools(context.Context) ([]mcpbridge.Tool, error) {
	return append([]mcpbridge.Tool(nil), d.tools...), nil
}

type staticHealth struct{ status mcpmanager.HealthStatus }

func (h staticHealth) Check(_ context.Context, b mcpmanager.LocalBinding) (mcpmanager.HealthObservation, error) {
	return mcpmanager.HealthObservation{ServerID: b.ServerID, BindingSHA256: b.BindingSHA256, Status: h.status, DetailCode: "test", ToolCount: 4}, nil
}

type providerAdapter struct {
	identity mcpbridge.ServerIdentity
	calls    *int
	mu       *sync.Mutex
	secret   string
}

func (a *providerAdapter) BoundServerIdentity() *mcpbridge.ServerIdentity { x := a.identity; return &x }
func (a *providerAdapter) Invoke(_ context.Context, tool string, _ map[string]any) (mcpbridge.CallToolResult, error) {
	a.mu.Lock()
	*a.calls++
	a.mu.Unlock()
	var text string
	switch tool {
	case "index_repository":
		text = `{"project_id":"proof26-project"}`
	case "get_architecture":
		text = `{"files":["internal/router.go","internal/cache.go"]}`
	case "search_graph":
		text = `{"symbols":[{"name":"RouteRequest","path":"internal/router.go"},{"name":"CacheLookup","path":"internal/cache.go"}]}`
	case "trace_path":
		text = `{"path":"internal/router.go"}`
	default:
		return mcpbridge.CallToolResult{}, errors.New("unknown test tool")
	}
	if a.secret != "" {
		text += a.secret
	}
	return mcpbridge.CallToolResult{Content: []mcpbridge.Content{{Type: "text", Text: text}}}, nil
}

type countFactory struct {
	identity mcpbridge.ServerIdentity
	builds   int
	calls    int
	mu       sync.Mutex
	secret   string
}

func (f *countFactory) Build(context.Context, mcpmanager.ExecutionPlan) (mcpbridge.Adapter, error) {
	f.mu.Lock()
	f.builds++
	f.mu.Unlock()
	return &providerAdapter{identity: f.identity, calls: &f.calls, mu: &f.mu, secret: f.secret}, nil
}
func (f *countFactory) Counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds, f.calls
}

type fixture struct {
	root         string
	registryPath string
	managerPath  string
	journalPath  string
	timelinePath string
	storePath    string
	reg          mcpregistry.Registration
	snapshot     mcpregistry.DiscoverySnapshot
	manager      *mcpmanager.Manager
	factory      *countFactory
	router       *mcpmanager.ExecutionRouter
	timeline     *timeline.Store
	store        *Store
}

func newFixture(t *testing.T, providerSecret string) *fixture {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	mustWrite(t, filepath.Join(project, "internal/router.go"), "package internal\nfunc RouteRequest(){ /* route provider cache invalidation target */ }\n")
	mustWrite(t, filepath.Join(project, "internal/cache.go"), "package internal\nfunc CacheLookup(){ /* cache provider route target */ }\n")
	for i := 0; i < 3; i++ {
		mustWrite(t, filepath.Join(project, "internal", "noise"+string(rune('a'+i))+".go"), "package internal\n// route provider cache invalidation lower ranked noise\n")
	}
	mustWrite(t, filepath.Join(project, ".env"), "PROOF26_SECRET=hidden\n")

	identity := mcpbridge.ServerIdentity{Name: "Proof 0.26 context provider", Version: "1.0.0", Registry: "fixture", Package: "keydeck/context-provider", PackageIntegrity: "sha512-proof26", PackageSHA256: strings.Repeat("a", 64), EntryPoint: "provider"}
	contract := mcpregistry.RuntimeContract{Transport: mcpregistry.TransportStdio, Runtime: "fixture", Entrypoint: "provider", ProtocolVersion: mcpbridge.ProtocolVersion, MaxFrameBytes: 1 << 20}
	reg, err := mcpregistry.NewRegistration(identity, contract)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "registry.jsonl")
	registry, err := mcpregistry.Open(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = registry.Register(reg); err != nil {
		t.Fatal(err)
	}
	tools := make([]mcpbridge.Tool, 0, len(DefaultProviderTools))
	for _, name := range DefaultProviderTools {
		tools = append(tools, mcpbridge.Tool{Name: name, InputSchema: map[string]any{"type": "object"}})
	}
	snap, _, err := registry.Discover(context.Background(), reg.ServerID, &testDiscoverer{identity: identity, contract: contract, tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	managerPath := filepath.Join(root, "manager.jsonl")
	manager, err := mcpmanager.Open(managerPath, registry)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "provider-entry")
	mustWrite(t, entry, "fixture")
	binding, err := mcpmanager.NewLocalBinding(reg, exe, entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Bind(binding); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.SetEnabled(reg.ServerID, true, "proof fixture"); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.ApproveTools(reg.ServerID, snap.SchemaSHA256, DefaultProviderTools); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.CheckHealth(context.Background(), reg.ServerID, staticHealth{mcpmanager.HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "journal.jsonl")
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	timelinePath := filepath.Join(root, "timeline.jsonl")
	tl, err := timeline.Open(timelinePath)
	if err != nil {
		t.Fatal(err)
	}
	factory := &countFactory{identity: identity, secret: providerSecret}
	schemas := &mcpbridge.SchemaPolicy{Tools: map[string]mcpbridge.ArgumentSchema{}}
	for _, name := range DefaultProviderTools {
		schemas.Tools[name] = mcpbridge.ArgumentSchema{AllowUnknown: true}
	}
	router := &mcpmanager.ExecutionRouter{Manager: manager, Factory: factory, Journal: journal, Timeline: tl, TaskID: "proof26-task", SessionID: "proof26-session", ActiveProfile: mcpbridge.ProfileFullControl, Schemas: schemas}
	storePath := filepath.Join(root, "context.jsonl")
	store, err := OpenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{root: project, registryPath: registryPath, managerPath: managerPath, journalPath: journalPath, timelinePath: timelinePath, storePath: storePath, reg: reg, snapshot: snap, manager: manager, factory: factory, router: router, timeline: tl, store: store}
}

func (f *fixture) coordinator() *Coordinator {
	return &Coordinator{Router: f.router, Store: f.store, Timeline: f.timeline, TaskID: "proof26-task", SessionID: "proof26-session"}
}
func (f *fixture) options(secret string) BuildOptions {
	return BuildOptions{ProjectRoot: f.root, Objective: "route provider cache invalidation target", MaxChars: 12000, MaxFiles: 2, ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snapshot.SchemaSHA256, ForbiddenExactValues: []string{secret}}
}

func TestFingerprintIgnoresEnvAndTracksSource(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")
	mustWrite(t, filepath.Join(root, ".env"), "A=1\n")
	a, err := FingerprintProject(root)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, ".env"), "A=2\n")
	b, _ := FingerprintProject(root)
	if a != b {
		t.Fatal(".env changed source fingerprint")
	}
	mustWrite(t, filepath.Join(root, "a.go"), "package a\nvar X=1\n")
	c, _ := FingerprintProject(root)
	if c == b {
		t.Fatal("source change did not change fingerprint")
	}
}

func TestBuildRestartReuseDisabledReuseInvalidationAndRebuild(t *testing.T) {
	f := newFixture(t, "")
	secret := "PROOF26_SECRET_MUST_NEVER_PERSIST"
	out1, err := f.coordinator().Build(context.Background(), f.options(secret))
	if err != nil {
		t.Fatal(err)
	}
	if out1.Reused || out1.ProviderCallCount == 0 || f.store.Count() != 1 {
		t.Fatalf("unexpected first build %+v count=%d", out1, f.store.Count())
	}
	builds, calls := f.factory.Counts()
	store2, err := OpenStore(f.storePath)
	if err != nil {
		t.Fatal(err)
	}
	tl2, err := timeline.Open(f.timelinePath)
	if err != nil {
		t.Fatal(err)
	}
	c2 := &Coordinator{Router: f.router, Store: store2, Timeline: tl2, TaskID: "proof26-task", SessionID: "proof26-session"}
	out2, err := c2.Build(context.Background(), f.options(secret))
	if err != nil {
		t.Fatal(err)
	}
	b2, calls2 := f.factory.Counts()
	if !out2.Reused || out2.ProviderCallCount != 0 || b2 != builds || calls2 != calls {
		t.Fatalf("restart reuse invoked provider: %+v builds %d/%d calls %d/%d", out2, builds, b2, calls, calls2)
	}
	if _, err = f.manager.SetEnabled(f.reg.ServerID, false, "offline reuse"); err != nil {
		t.Fatal(err)
	}
	out3, err := c2.Build(context.Background(), f.options(secret))
	if err != nil || !out3.Reused {
		t.Fatalf("disabled fresh reuse failed out=%+v err=%v", out3, err)
	}
	mustWrite(t, filepath.Join(f.root, "internal/router.go"), "package internal\nfunc RouteRequest(){ /* changed route provider cache invalidation target */ }\n")
	beforeCount := store2.Count()
	beforeBuilds, beforeCalls := f.factory.Counts()
	_, err = c2.Build(context.Background(), f.options(secret))
	if !errors.Is(err, mcpmanager.ErrExecutionDisabled) {
		t.Fatalf("expected disabled preflight, got %v", err)
	}
	afterBuilds, afterCalls := f.factory.Counts()
	if store2.Count() != beforeCount || afterBuilds != beforeBuilds || afterCalls != beforeCalls {
		t.Fatal("disabled stale rebuild crossed provider/persistence boundary")
	}
	if _, err = f.manager.SetEnabled(f.reg.ServerID, true, "resume"); err != nil {
		t.Fatal(err)
	}
	out4, err := c2.Build(context.Background(), f.options(secret))
	if err != nil {
		t.Fatal(err)
	}
	if out4.Reused || out4.Record.PacketID == out1.Record.PacketID || out4.ProjectFingerprint == out1.ProjectFingerprint {
		t.Fatalf("expected fresh source-bound rebuild: first=%s rebuilt=%s", out1.Record.PacketID, out4.Record.PacketID)
	}
}

func TestSecretBearingProviderOutputRejectedBeforePersistence(t *testing.T) {
	secret := "PROOF26_SECRET_MUST_NEVER_PERSIST"
	f := newFixture(t, secret)
	_, err := f.coordinator().Build(context.Background(), f.options(secret))
	if !errors.Is(err, ErrHygiene) {
		t.Fatalf("expected hygiene denial, got %v", err)
	}
	if f.store.Count() != 0 {
		t.Fatal("secret-bearing packet persisted")
	}
}

func TestTamperedPacketArtifactRejected(t *testing.T) {
	f := newFixture(t, "")
	out, err := f.coordinator().Build(context.Background(), f.options("sentinel"))
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(f.store.artifacts, out.Record.PacketJSONPath)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("tamper")...)
	if err = os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = f.store.FindFresh(out.CacheKey, out.ProjectFingerprint)
	if !errors.Is(err, ErrArtifactTampered) {
		t.Fatalf("expected tamper error, got %v", err)
	}
}

func TestHygieneRejectsDuplicateAndEscapingSnippet(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), "package a\n")
	p := contextcompiler.Packet{StructuralProvider: "fixture", StructuralEvidence: []contextcompiler.StructuralEvidence{{Tool: "x", Arguments: "{}", Successful: true}}, SourceSnippets: []contextcompiler.SourceSnippet{{Path: "a.go", StartLine: 1, EndLine: 1, Content: "package a"}, {Path: "a.go", StartLine: 1, EndLine: 1, Content: "package a"}}}
	p.RenderedChars = len(p.Render())
	if _, err := ValidateHygiene(p, root, 12000, 3, nil); !errors.Is(err, ErrHygiene) {
		t.Fatalf("expected duplicate denial, got %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	mustWrite(t, outside, "package outside\n")
	rel, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	p.SourceSnippets = []contextcompiler.SourceSnippet{{Path: filepath.ToSlash(rel), StartLine: 1, EndLine: 1, Content: "package outside"}}
	p.RenderedChars = len(p.Render())
	if _, err := ValidateHygiene(p, root, 12000, 3, nil); !errors.Is(err, ErrHygiene) {
		t.Fatalf("expected path escape denial, got %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
