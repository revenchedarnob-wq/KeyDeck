package conformance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvidenceStoreRoundTripAndTamperDetection(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "provider-evidence.json")
	store := EvidenceStore{Path: path}
	bundle := testEvidence(now)
	if err := store.Save(bundle, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(now)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EvidenceSHA256 != bundle.EvidenceSHA256 {
		t.Fatalf("evidence identity changed: got %s want %s", loaded.EvidenceSHA256, bundle.EvidenceSHA256)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range data {
		if data[i] == 'r' {
			data[i] = 'x'
			break
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(now); err == nil {
		t.Fatal("tampered durable evidence was accepted")
	}
}
