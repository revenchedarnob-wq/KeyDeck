package mcpregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
)

var (
	ErrRegistrationConflict = errors.New("MCP server registration conflicts with immutable record")
	ErrIdentityDrift        = errors.New("MCP server identity drift detected")
	ErrRuntimeDrift         = errors.New("MCP runtime contract drift detected")
	ErrCapabilityDrift      = errors.New("MCP capability/schema drift detected")
	ErrUnknownServer        = errors.New("MCP server is not registered")
)

const (
	TransportStdio = "stdio"
	maxFrameLimit  = 64 << 20
)

type RuntimeContract struct {
	Transport       string   `json:"transport"`
	Runtime         string   `json:"runtime"`
	Entrypoint      string   `json:"entrypoint"`
	ProtocolVersion string   `json:"protocol_version"`
	MaxFrameBytes   int      `json:"max_frame_bytes"`
	ArgumentSlots   []string `json:"argument_slots,omitempty"`
	EnvironmentKeys []string `json:"environment_keys,omitempty"`
}

func (c RuntimeContract) Normalize() RuntimeContract {
	out := c
	out.Transport = strings.TrimSpace(out.Transport)
	out.Runtime = strings.TrimSpace(out.Runtime)
	out.Entrypoint = strings.TrimSpace(out.Entrypoint)
	out.ProtocolVersion = strings.TrimSpace(out.ProtocolVersion)
	out.ArgumentSlots = normalizedStrings(out.ArgumentSlots)
	out.EnvironmentKeys = normalizedStrings(out.EnvironmentKeys)
	return out
}

func (c RuntimeContract) Validate() error {
	c = c.Normalize()
	if c.Transport != TransportStdio {
		return fmt.Errorf("unsupported MCP runtime transport %q", c.Transport)
	}
	if c.Runtime == "" || c.Entrypoint == "" || c.ProtocolVersion == "" {
		return errors.New("MCP runtime, entrypoint and protocol version are required")
	}
	if c.MaxFrameBytes <= 0 || c.MaxFrameBytes > maxFrameLimit {
		return fmt.Errorf("MCP max frame bytes must be between 1 and %d", maxFrameLimit)
	}
	for _, value := range append(append([]string(nil), c.ArgumentSlots...), c.EnvironmentKeys...) {
		if value == "" {
			return errors.New("MCP runtime contract contains an empty slot/key")
		}
	}
	return nil
}

func (c RuntimeContract) Hash() (string, error) {
	c = c.Normalize()
	if err := c.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

type Registration struct {
	ServerID       string                   `json:"server_id"`
	Identity       mcpbridge.ServerIdentity `json:"identity"`
	IdentitySHA256 string                   `json:"identity_sha256"`
	Runtime        RuntimeContract          `json:"runtime"`
	RuntimeSHA256  string                   `json:"runtime_sha256"`
}

func NewRegistration(identity mcpbridge.ServerIdentity, runtime RuntimeContract) (Registration, error) {
	identityHash, err := identity.Hash()
	if err != nil {
		return Registration{}, err
	}
	runtime = runtime.Normalize()
	runtimeHash, err := runtime.Hash()
	if err != nil {
		return Registration{}, err
	}
	return Registration{
		ServerID: "mcp-" + identityHash[:24], Identity: identity, IdentitySHA256: identityHash,
		Runtime: runtime, RuntimeSHA256: runtimeHash,
	}, nil
}

func (r Registration) Validate() error {
	expected, err := NewRegistration(r.Identity, r.Runtime)
	if err != nil {
		return err
	}
	if r.ServerID != expected.ServerID || r.IdentitySHA256 != expected.IdentitySHA256 || r.RuntimeSHA256 != expected.RuntimeSHA256 {
		return errors.New("MCP registration digest fields are invalid")
	}
	return nil
}

type ToolCapability struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type DiscoverySnapshot struct {
	ServerID       string           `json:"server_id"`
	IdentitySHA256 string           `json:"identity_sha256"`
	RuntimeSHA256  string           `json:"runtime_sha256"`
	Protocol       string           `json:"protocol"`
	Tools          []ToolCapability `json:"tools"`
	SchemaSHA256   string           `json:"schema_sha256"`
}

func snapshotFor(reg Registration, tools []mcpbridge.Tool) (DiscoverySnapshot, error) {
	if err := reg.Validate(); err != nil {
		return DiscoverySnapshot{}, err
	}
	caps := make([]ToolCapability, 0, len(tools))
	seen := map[string]bool{}
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" || seen[name] {
			return DiscoverySnapshot{}, fmt.Errorf("invalid or duplicate MCP tool name %q", name)
		}
		seen[name] = true
		caps = append(caps, ToolCapability{Name: name, Description: tool.Description, InputSchema: cloneMap(tool.InputSchema)})
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i].Name < caps[j].Name })
	raw, err := json.Marshal(caps)
	if err != nil {
		return DiscoverySnapshot{}, err
	}
	return DiscoverySnapshot{
		ServerID: reg.ServerID, IdentitySHA256: reg.IdentitySHA256, RuntimeSHA256: reg.RuntimeSHA256,
		Protocol: reg.Runtime.ProtocolVersion, Tools: caps, SchemaSHA256: sha256Hex(raw),
	}, nil
}

func (s DiscoverySnapshot) Validate(reg Registration) error {
	if err := reg.Validate(); err != nil {
		return err
	}
	if s.ServerID != reg.ServerID || s.IdentitySHA256 != reg.IdentitySHA256 || s.RuntimeSHA256 != reg.RuntimeSHA256 || s.Protocol != reg.Runtime.ProtocolVersion {
		return errors.New("MCP discovery snapshot does not match registration")
	}
	tools := make([]mcpbridge.Tool, 0, len(s.Tools))
	for _, tool := range s.Tools {
		tools = append(tools, mcpbridge.Tool{Name: tool.Name, Description: tool.Description, InputSchema: cloneMap(tool.InputSchema)})
	}
	expected, err := snapshotFor(reg, tools)
	if err != nil {
		return err
	}
	if expected.SchemaSHA256 != s.SchemaSHA256 {
		return errors.New("MCP discovery schema digest is invalid")
	}
	return nil
}

func normalizedStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	raw, _ := json.Marshal(in)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
