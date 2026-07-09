package mcpregistry

import (
	"testing"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
)

func TestPermissionProposalNeverAutoGrantsAndRequiresExplicitApproval(t *testing.T) {
	snapshot := DiscoverySnapshot{ServerID: "mcp-test", SchemaSHA256: "schema", Tools: []ToolCapability{{Name: "read_text_file"}, {Name: "write_file"}, {Name: "mystery_power"}}}
	proposal := ProposePermissions(snapshot)
	for _, tool := range proposal.Tools {
		if tool.DefaultGranted {
			t.Fatalf("tool auto-granted: %+v", tool)
		}
	}
	policy, err := proposal.BuildPolicy(mcpbridge.ProfileSafeEdit, map[string]bool{"read_text_file": true})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Allows("read_text_file") || policy.Allows("write_file") || policy.Allows("mystery_power") {
		t.Fatalf("unexpected policy: %+v", policy)
	}
	if _, err := proposal.BuildPolicy(mcpbridge.ProfileFullControl, map[string]bool{"not-discovered": true}); err == nil {
		t.Fatal("unknown approval accepted")
	}
}
