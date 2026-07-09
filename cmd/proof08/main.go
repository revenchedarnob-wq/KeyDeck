package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/contextbench"
	"keydeck.local/feasibilitylab/internal/contextcompiler"
)

type fakeStructural struct{}

func (*fakeStructural) Name() string                   { return "fake-codebase-memory" }
func (*fakeStructural) Version(context.Context) string { return "proof-fixture" }
func (*fakeStructural) Call(_ context.Context, tool string, _ map[string]any) ([]byte, error) {
	switch tool {
	case "index_repository":
		return []byte(`{"project":"proof08"}`), nil
	case "get_architecture":
		return []byte(`{"languages":["Go"],"files":[{"file_path":"internal/routing/engine.go"},{"file_path":"internal/policy/cache.go"}]}`), nil
	case "search_graph":
		return []byte(`{"results":[{"name":"Resolve","file_path":"internal/routing/engine.go"},{"name":"GetOrLoad","file_path":"internal/policy/cache.go"},{"name":"HandleRequest","file_path":"internal/app/server.go"}]}`), nil
	case "trace_path":
		return []byte(`{"path":["HandleRequest","Resolve","GetOrLoad"],"files":[{"file_path":"internal/app/server.go"}]}`), nil
	default:
		return []byte(`{}`), nil
	}
}

type report struct {
	Proof            string                 `json:"proof"`
	Passed           bool                   `json:"passed"`
	Packet           contextcompiler.Packet `json:"packet"`
	ContainsRouting  bool                   `json:"contains_routing"`
	ContainsCache    bool                   `json:"contains_cache"`
	ContainsCallPath bool                   `json:"contains_call_path"`
}

func main() {
	root, err := os.MkdirTemp("", "keydeck-proof-08-policy-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	project := filepath.Join(root, "repo")
	if err := contextbench.CreateRepository(project); err != nil {
		panic(err)
	}
	packet, err := (&contextcompiler.Compiler{Runner: &fakeStructural{}}).Compile(context.Background(), contextcompiler.CompileOptions{ProjectRoot: project, Objective: contextbench.Objective(), MaxChars: 12000, MaxFiles: 6})
	if err != nil {
		panic(err)
	}
	rendered := packet.Render()
	rep := report{Proof: "0.8-context-compiler-policy", Packet: packet, ContainsRouting: strings.Contains(rendered, "internal/routing/engine.go"), ContainsCache: strings.Contains(rendered, "internal/policy/cache.go"), ContainsCallPath: strings.Contains(rendered, "HandleRequest") && strings.Contains(rendered, "Resolve") && strings.Contains(rendered, "GetOrLoad")}
	rep.Passed = rep.ContainsRouting && rep.ContainsCache && rep.ContainsCallPath && packet.StructuralProvider != "none" && packet.RenderedChars <= 12000
	b, _ := json.MarshalIndent(rep, "", "  ")
	path := filepath.Join("docs", "KeyDeck-Proof-0.8-policy-report.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		panic(err)
	}
	if !rep.Passed {
		fmt.Println(string(b))
		os.Exit(1)
	}
	fmt.Println("PASS: hybrid context compiler produced a bounded structural + exact-source packet.")
	fmt.Println("Report:", path)
}
