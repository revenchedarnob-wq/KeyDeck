package mcpbridge

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

func testServerIdentity() ServerIdentity {
	return ServerIdentity{
		Name:             "Official MCP Filesystem reference server",
		Version:          "2026.7.4",
		Registry:         "npm",
		Package:          "@modelcontextprotocol/server-filesystem",
		PackageIntegrity: "sha512-example",
		PackageSHA256:    "7ced44bb52a64349e12217a8d90d349b9d941a0560b3f0e3df05aeee8ed4da54",
		EntryPoint:       "dist/index.js",
	}
}

func TestServerIdentityCanonicalRefAndStableHash(t *testing.T) {
	id := testServerIdentity()
	if err := id.Validate(); err != nil {
		t.Fatal(err)
	}
	if got, want := id.CanonicalRef(), "npm:@modelcontextprotocol/server-filesystem@2026.7.4"; got != want {
		t.Fatalf("CanonicalRef()=%q want %q", got, want)
	}
	first, err := id.Hash()
	if err != nil {
		t.Fatal(err)
	}
	second, err := id.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("identity hash is not stable: %q %q", first, second)
	}
}

func TestServerIdentityRejectsUnsealedOrMalformedIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*ServerIdentity){
		"missing version": func(id *ServerIdentity) { id.Version = "" },
		"uppercase sha": func(id *ServerIdentity) {
			id.PackageSHA256 = "7CED44BB52A64349E12217A8D90D349B9D941A0560B3F0E3DF05AEEE8ED4DA54"
		},
		"short sha": func(id *ServerIdentity) { id.PackageSHA256 = "abc" },
		"separator": func(id *ServerIdentity) { id.Package += "#other" },
	} {
		t.Run(name, func(t *testing.T) {
			id := testServerIdentity()
			mutate(&id)
			if err := id.Validate(); !errors.Is(err, ErrServerIdentityInvalid) {
				t.Fatalf("Validate() error=%v", err)
			}
		})
	}
}

type identifiedCaptureAdapter struct {
	captureAdapter
	identity *ServerIdentity
}

func (a *identifiedCaptureAdapter) BoundServerIdentity() *ServerIdentity {
	if a.identity == nil {
		return nil
	}
	copy := *a.identity
	return &copy
}

func TestBridgeRejectsMismatchedAdapterIdentityBeforeInvocation(t *testing.T) {
	root := t.TempDir()
	journal, err := tooljournal.Open(filepath.Join(root, "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	timelineStore, err := timeline.Open(filepath.Join(root, "timeline.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	expected := testServerIdentity()
	other := expected
	other.Version = "2026.7.5"
	adapter := &identifiedCaptureAdapter{captureAdapter: captureAdapter{text: "must not run"}, identity: &other}
	bridge := &Bridge{
		Journal: journal, Timeline: timelineStore, TaskID: "task", SessionID: "session",
		Permissions: &PermissionPolicy{Profile: ProfileReadOnly, ToolProfiles: map[string]PermissionProfile{"read_text_file": ProfileReadOnly}},
		Adapter:     adapter, ServerIdentity: &expected,
	}
	_, err = bridge.Execute(context.Background(), Operation{OperationID: "identity-mismatch", Tool: "read_text_file", Arguments: map[string]any{"path": "x"}, Policy: tooljournal.ReplayForbidden})
	if !errors.Is(err, ErrServerIdentityMismatch) {
		t.Fatalf("expected identity mismatch, got %v", err)
	}
	if adapter.calls != 0 || len(journal.Snapshot()) != 0 {
		t.Fatalf("identity mismatch crossed execution boundary: calls=%d journal=%d", adapter.calls, len(journal.Snapshot()))
	}
}
