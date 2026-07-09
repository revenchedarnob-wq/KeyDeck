package presentation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionPresentationFilesDoNotImportCanonicalStores(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(path, "keydeck.local/feasibilitylab/internal/") && path != "keydeck.local/feasibilitylab/internal/corehost" {
				t.Fatalf("production presentation file %s imports forbidden internal package %s", name, path)
			}
		}
	}
}

func TestShellTypeHasNoPersistenceMethods(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "shell.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		name := strings.ToLower(fn.Name.Name)
		if strings.Contains(name, "save") || strings.Contains(name, "persist") || strings.Contains(name, "store") || strings.Contains(name, "write") {
			t.Fatalf("presentation shell exposes persistence-like method %s", fn.Name.Name)
		}
	}
}
