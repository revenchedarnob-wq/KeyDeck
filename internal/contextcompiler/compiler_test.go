package contextcompiler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls []string
	args  map[string]map[string]any
}

func (f *fakeRunner) Name() string                   { return "fake-structural" }
func (f *fakeRunner) Version(context.Context) string { return "test" }
func (f *fakeRunner) Call(_ context.Context, tool string, args map[string]any) ([]byte, error) {
	f.calls = append(f.calls, tool)
	if f.args == nil {
		f.args = map[string]map[string]any{}
	}
	f.args[tool] = args
	switch tool {
	case "index_repository":
		return []byte(`{"project":"bench"}`), nil
	case "get_architecture":
		return []byte(`{"files":[{"file_path":"internal/routing/engine.go"}]}`), nil
	case "search_graph":
		return []byte(`{"results":[{"name":"Resolve","file_path":"internal/routing/engine.go"},{"name":"GetOrLoad","file_path":"internal/policy/cache.go"}]}`), nil
	case "trace_path":
		return []byte(`{"callers":[{"file_path":"internal/app/server.go"}]}`), nil
	default:
		return []byte(`{}`), nil
	}
}

func TestCompilerBuildsBudgetedHybridPacket(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"internal/routing/engine.go": "package routing\nfunc Resolve(tenant, route string) { cache.GetOrLoad(route) }\n",
		"internal/policy/cache.go":   "package policy\nfunc GetOrLoad(route string) { /* cache key omits tenant */ }\n",
		"internal/app/server.go":     "package app\nfunc HandleRequest() { routing.Resolve() }\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := &fakeRunner{}
	p, err := (&Compiler{Runner: r}).Compile(context.Background(), CompileOptions{ProjectRoot: root, Objective: "Why can tenant B receive tenant A fallback policy after active tenant changes? Identify cache call path.", MaxChars: 9000, MaxFiles: 4})
	if err != nil {
		t.Fatal(err)
	}
	if p.ProjectID != "bench" || len(p.SourceSnippets) < 2 || p.RenderedChars > 9000 {
		t.Fatalf("unexpected packet: %+v", p)
	}
	rendered := p.Render()
	for _, want := range []string{"Resolve", "GetOrLoad", "tenant", "STRUCTURAL"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("packet missing %q", want)
		}
	}
	if _, err := json.Marshal(p); err != nil {
		t.Fatal(err)
	}
	searchArgs := r.args["search_graph"]
	if searchArgs["name_pattern"] == nil || searchArgs["query"] != nil {
		t.Fatalf("search_graph must use its documented name_pattern argument, got %#v", searchArgs)
	}
	indexArgs := r.args["index_repository"]
	if indexArgs["mode"] != nil {
		t.Fatalf("index_repository should not send undocumented mode argument, got %#v", indexArgs)
	}
}

func TestBoundGitStatus(t *testing.T) {
	status := strings.Repeat("A  internal/decoy/file.go\n", 100)
	got := boundGitStatus(strings.TrimSpace(status), 5, 4000)
	if strings.Count(got, "\n") > 5 || !strings.Contains(got, "additional git status entries omitted") {
		t.Fatalf("git status was not bounded: %q", got)
	}
}

func TestChecksumFor(t *testing.T) {
	content := "abc123  other.zip\n0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  codebase-memory-mcp-windows-amd64.zip\n"
	got := checksumFor(content, PinnedWindowsArchiveName)
	if len(got) != 64 {
		t.Fatalf("unexpected checksum %q", got)
	}
}

func TestSymbolSearchPatternUsesPortableRegex(t *testing.T) {
	got := symbolSearchPattern([]string{"tenant", "fallback"})
	if strings.Contains(got, "(?i)") {
		t.Fatalf("portable search pattern must not use inline flags: %q", got)
	}
	for _, want := range []string{"tenant", "Tenant", "fallback", "Fallback"} {
		if !strings.Contains(got, want) {
			t.Fatalf("pattern %q missing case variant %q", got, want)
		}
	}
}

func TestStructuralSuccessSurvivesBudgetCompaction(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "internal", "routing", "engine.go")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("package routing\nfunc ResolveTenantFallback() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	type noisyRunner struct{ fakeRunner }
	_ = noisyRunner{}
	r := &budgetRunner{}
	packet, err := (&Compiler{Runner: r}).Compile(context.Background(), CompileOptions{
		ProjectRoot: root,
		Objective:   "Find the tenant fallback routing bug",
		MaxChars:    2200,
		MaxFiles:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !packet.StructuralIndexSucceeded || !packet.StructuralSearchSucceeded {
		t.Fatalf("structural success receipts were lost: %+v", packet)
	}
	seen := map[string]bool{}
	for _, e := range packet.StructuralEvidence {
		seen[e.Tool] = true
	}
	if !seen["index_repository"] || !seen["search_graph"] {
		t.Fatalf("budget compaction deleted structural proof receipts: %#v", seen)
	}
}

type budgetRunner struct{}

func (b *budgetRunner) Name() string                   { return "budget-structural" }
func (b *budgetRunner) Version(context.Context) string { return "test" }
func (b *budgetRunner) Call(_ context.Context, tool string, _ map[string]any) ([]byte, error) {
	switch tool {
	case "index_repository":
		return []byte(`{"project":"bench"}`), nil
	case "get_architecture":
		return []byte(`{"architecture":"` + strings.Repeat("very-large-architecture-evidence-", 500) + `"}`), nil
	case "search_graph":
		return []byte(`{"results":[{"name":"ResolveTenantFallback","file_path":"internal/routing/engine.go"}]}`), nil
	case "trace_path":
		return []byte(`{"paths":[]}`), nil
	default:
		return []byte(`{}`), nil
	}
}
