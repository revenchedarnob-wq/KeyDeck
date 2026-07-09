package mcpregistry

import (
	"errors"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
)

type ToolPermissionProposal struct {
	Tool             string                      `json:"tool"`
	SuggestedProfile mcpbridge.PermissionProfile `json:"suggested_profile"`
	DefaultGranted   bool                        `json:"default_granted"`
	Reason           string                      `json:"reason"`
}

type PermissionProposal struct {
	ServerID     string                   `json:"server_id"`
	SchemaSHA256 string                   `json:"schema_sha256"`
	Tools        []ToolPermissionProposal `json:"tools"`
}

func ProposePermissions(snapshot DiscoverySnapshot) PermissionProposal {
	proposal := PermissionProposal{ServerID: snapshot.ServerID, SchemaSHA256: snapshot.SchemaSHA256}
	for _, tool := range snapshot.Tools {
		profile, reason := suggestedProfile(tool.Name)
		proposal.Tools = append(proposal.Tools, ToolPermissionProposal{Tool: tool.Name, SuggestedProfile: profile, DefaultGranted: false, Reason: reason})
	}
	sort.Slice(proposal.Tools, func(i, j int) bool { return proposal.Tools[i].Tool < proposal.Tools[j].Tool })
	return proposal
}

func (p PermissionProposal) BuildPolicy(activeProfile mcpbridge.PermissionProfile, approvals map[string]bool) (*mcpbridge.PermissionPolicy, error) {
	known := map[string]mcpbridge.PermissionProfile{}
	for _, tool := range p.Tools {
		if tool.DefaultGranted {
			return nil, errors.New("MCP permission proposal must not auto-grant tools")
		}
		known[tool.Tool] = tool.SuggestedProfile
	}
	profiles := map[string]mcpbridge.PermissionProfile{}
	for tool, approved := range approvals {
		if !approved {
			continue
		}
		profile, ok := known[tool]
		if !ok {
			return nil, errors.New("approval references unknown MCP tool")
		}
		profiles[tool] = profile
	}
	policy := &mcpbridge.PermissionPolicy{Profile: activeProfile, ToolProfiles: profiles}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return policy, nil
}

func suggestedProfile(name string) (mcpbridge.PermissionProfile, string) {
	lower := strings.ToLower(name)
	for _, token := range []string{"delete", "remove", "chmod", "permission", "execute", "shell"} {
		if strings.Contains(lower, token) {
			return mcpbridge.ProfileFullControl, "destructive or execution-like capability; explicit full-control approval required"
		}
	}
	for _, token := range []string{"write", "edit", "create", "move", "copy", "rename", "mkdir"} {
		if strings.Contains(lower, token) {
			return mcpbridge.ProfileSafeEdit, "state-changing capability; explicit safe-edit approval required"
		}
	}
	for _, token := range []string{"read", "list", "search", "get", "info", "stat", "tree"} {
		if strings.Contains(lower, token) {
			return mcpbridge.ProfileReadOnly, "read-oriented capability; explicit read-only approval required"
		}
	}
	return mcpbridge.ProfileFullControl, "unknown capability shape; conservative full-control approval required"
}
