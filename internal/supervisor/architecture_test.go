package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupervisorDoesNotImportCanonicalStores(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"internal/tasks", "internal/timeline", "internal/session", "internal/recovery",
		"internal/tooljournal", "internal/candidatecollection",
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, needle := range forbidden {
			if strings.Contains(text, needle) {
				t.Fatalf("%s imports forbidden canonical layer %q", entry.Name(), needle)
			}
		}
	}
}
