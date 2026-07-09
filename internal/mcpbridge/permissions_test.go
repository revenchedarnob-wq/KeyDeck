package mcpbridge

import "testing"

func TestPermissionPolicyProfiles(t *testing.T) {
	rules := map[string]PermissionProfile{
		"read":  ProfileReadOnly,
		"write": ProfileSafeEdit,
		"admin": ProfileFullControl,
	}
	cases := []struct {
		profile PermissionProfile
		allowed []string
		denied  []string
	}{
		{ProfileReadOnly, []string{"read"}, []string{"write", "admin"}},
		{ProfileSafeEdit, []string{"read", "write"}, []string{"admin"}},
		{ProfileFullControl, []string{"read", "write", "admin"}, nil},
	}
	for _, tc := range cases {
		p := PermissionPolicy{Profile: tc.profile, ToolProfiles: rules}
		if err := p.Validate(); err != nil {
			t.Fatalf("validate %s: %v", tc.profile, err)
		}
		for _, tool := range tc.allowed {
			if !p.Allows(tool) {
				t.Fatalf("profile %s should allow %s", tc.profile, tool)
			}
		}
		for _, tool := range tc.denied {
			if p.Allows(tool) {
				t.Fatalf("profile %s should deny %s", tc.profile, tool)
			}
		}
	}
}
