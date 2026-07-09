package mcpbridge

import "fmt"

type PermissionProfile string

const (
	ProfileReadOnly    PermissionProfile = "read_only"
	ProfileSafeEdit    PermissionProfile = "safe_edit"
	ProfileFullControl PermissionProfile = "full_control"
)

type PermissionPolicy struct {
	Profile      PermissionProfile
	ToolProfiles map[string]PermissionProfile
}

func (p PermissionPolicy) Allows(tool string) bool {
	required, ok := p.ToolProfiles[tool]
	if !ok {
		return false
	}
	return profileRank(p.Profile) >= profileRank(required)
}

func (p PermissionPolicy) Validate() error {
	if profileRank(p.Profile) < 0 {
		return fmt.Errorf("unsupported permission profile %q", p.Profile)
	}
	for tool, required := range p.ToolProfiles {
		if tool == "" {
			return fmt.Errorf("permission policy contains an empty tool name")
		}
		if profileRank(required) < 0 {
			return fmt.Errorf("tool %q requires unsupported permission profile %q", tool, required)
		}
	}
	return nil
}

func profileRank(profile PermissionProfile) int {
	switch profile {
	case ProfileReadOnly:
		return 0
	case ProfileSafeEdit:
		return 1
	case ProfileFullControl:
		return 2
	default:
		return -1
	}
}
