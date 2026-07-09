package visualshell

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionVisualShellImportsOnlyPresentationFromKeyDeckInternals(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Clean(entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range file.Imports {
			lit := strings.Trim(spec.Path.Value, "\"")
			if strings.Contains(lit, "keydeck.local/feasibilitylab/internal/") && lit != "keydeck.local/feasibilitylab/internal/presentation" {
				t.Fatalf("production visual shell imports forbidden KeyDeck internal package %q in %s", lit, path)
			}
		}
		ast.Inspect(file, func(ast.Node) bool { return true })
	}
}
