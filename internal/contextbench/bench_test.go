package contextbench

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRepositoryAndAcceptance(t *testing.T) {
	root := t.TempDir()
	if err := CreateRepository(root); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"internal/app/server.go",
		"internal/routing/engine.go",
		"internal/policy/cache.go",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("missing benchmark file %s: %v", rel, err)
		}
	}
	if _, err := exec.LookPath("git"); err == nil {
		out, err := exec.Command("git", "-C", root, "status", "--short").Output()
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 0 {
			t.Fatalf("benchmark repository should start clean, got %q", out)
		}
	}

	result := Result{
		Proof:     ProofMarker,
		RootCause: "The cache is keyed only by route and omits tenant identity, so tenant A's cached fallback policy can cross the tenant isolation boundary and be returned for tenant B.",
		CallPath:  []string{"HandleRequest", "Resolve", "GetOrLoad"},
		FilesToFix: []string{
			"internal/routing/engine.go",
			"internal/policy/cache.go",
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ResultFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	got := Evaluate(root)
	if !got.Passed {
		t.Fatalf("valid result should pass: %+v", got)
	}
}

func TestAcceptanceRejectsSymptomOnlyAnswer(t *testing.T) {
	root := t.TempDir()
	result := Result{
		Proof:      ProofMarker,
		RootCause:  "Tenant B can receive tenant A's fallback policy.",
		CallPath:   []string{"HandleRequest", "Resolve", "GetOrLoad"},
		FilesToFix: []string{"internal/routing/engine.go", "internal/policy/cache.go"},
	}
	b, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(root, ResultFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Evaluate(root); got.Passed || got.CacheKeyCauseOK {
		t.Fatalf("symptom-only answer must not pass cache-key root-cause check: %+v", got)
	}
}

func TestAcceptanceRejectsSourceModificationAndExtraFixFile(t *testing.T) {
	root := t.TempDir()
	if err := CreateRepository(root); err != nil {
		t.Fatal(err)
	}
	result := Result{
		Proof:     ProofMarker,
		RootCause: "The cache is keyed only by route and omits tenant identity, causing a cross-tenant isolation leak.",
		CallPath:  []string{"HandleRequest", "Resolve", "GetOrLoad"},
		FilesToFix: []string{
			"internal/routing/engine.go",
			"internal/policy/cache.go",
			"internal/app/server.go",
		},
	}
	b, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(root, ResultFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "routing", "engine.go"), []byte("package routing\n// modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Evaluate(root)
	if got.Passed || got.MinimalFixSetOK || got.SourceUnchangedOK {
		t.Fatalf("modified source and extra fix file must fail: %+v", got)
	}
}

func TestPromptMakesBenchmarkToolchainIndependent(t *testing.T) {
	p := Prompt("")
	for _, want := range []string{
		"static diagnosis benchmark",
		"Do not run builds, tests, package managers, compilers, or linters",
		"does not require a language toolchain",
		"filesystem/search tools only",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}
