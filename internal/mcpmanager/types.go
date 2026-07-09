package mcpmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/mcpregistry"
)

var (
	ErrBindingConflict = errors.New("MCP local runtime binding conflicts with existing binding; explicit rebind required")
	ErrNoBinding       = errors.New("MCP server has no local runtime binding")
	ErrApprovalSchema  = errors.New("MCP permission approval schema does not match trusted discovery")
	ErrUnknownTool     = errors.New("MCP permission approval references unknown tool")
	ErrInvalidHealth   = errors.New("MCP health observation does not match current binding")
	ErrManagerState    = errors.New("invalid MCP server manager state")
)

type LocalBinding struct {
	ServerID       string            `json:"server_id"`
	RuntimePath    string            `json:"runtime_path"`
	EntrypointPath string            `json:"entrypoint_path"`
	Arguments      map[string]string `json:"arguments,omitempty"`
	BindingSHA256  string            `json:"binding_sha256"`
}

func NewLocalBinding(reg mcpregistry.Registration, runtimePath, entrypointPath string, arguments map[string]string) (LocalBinding, error) {
	b := LocalBinding{
		ServerID:       reg.ServerID,
		RuntimePath:    filepath.Clean(strings.TrimSpace(runtimePath)),
		EntrypointPath: filepath.Clean(strings.TrimSpace(entrypointPath)),
		Arguments:      cloneStrings(arguments),
	}
	if err := b.validateShape(reg); err != nil {
		return LocalBinding{}, err
	}
	hash, err := b.hashWithoutDigest()
	if err != nil {
		return LocalBinding{}, err
	}
	b.BindingSHA256 = hash
	return b, nil
}

func (b LocalBinding) Validate(reg mcpregistry.Registration) error {
	if err := b.validateShape(reg); err != nil {
		return err
	}
	expected, err := b.hashWithoutDigest()
	if err != nil {
		return err
	}
	if b.BindingSHA256 != expected {
		return errors.New("MCP local runtime binding digest is invalid")
	}
	return nil
}

func (b LocalBinding) validateShape(reg mcpregistry.Registration) error {
	if err := reg.Validate(); err != nil {
		return err
	}
	if b.ServerID != reg.ServerID {
		return errors.New("MCP local runtime binding server ID mismatch")
	}
	if strings.TrimSpace(b.RuntimePath) == "" || strings.TrimSpace(b.EntrypointPath) == "" {
		return errors.New("MCP local runtime and entrypoint paths are required")
	}
	if !filepath.IsAbs(b.RuntimePath) || !filepath.IsAbs(b.EntrypointPath) {
		return errors.New("MCP local runtime and entrypoint paths must be machine-local absolute paths")
	}
	allowed := map[string]bool{}
	for _, slot := range reg.Runtime.ArgumentSlots {
		allowed[slot] = true
	}
	for key, value := range b.Arguments {
		if !allowed[key] {
			return fmt.Errorf("MCP local binding argument %q is not declared by the portable runtime contract", key)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("MCP local binding argument %q is empty", key)
		}
	}
	return nil
}

func (b LocalBinding) hashWithoutDigest() (string, error) {
	type canonical struct {
		ServerID       string            `json:"server_id"`
		RuntimePath    string            `json:"runtime_path"`
		EntrypointPath string            `json:"entrypoint_path"`
		Arguments      map[string]string `json:"arguments,omitempty"`
	}
	raw, err := json.Marshal(canonical{
		ServerID:       b.ServerID,
		RuntimePath:    filepath.Clean(strings.TrimSpace(b.RuntimePath)),
		EntrypointPath: filepath.Clean(strings.TrimSpace(b.EntrypointPath)),
		Arguments:      cloneStrings(b.Arguments),
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

type HealthStatus string

const (
	HealthUnknown     HealthStatus = "unknown"
	HealthHealthy     HealthStatus = "healthy"
	HealthUnavailable HealthStatus = "unavailable"
	HealthUnhealthy   HealthStatus = "unhealthy"
)

type HealthObservation struct {
	ServerID      string       `json:"server_id"`
	BindingSHA256 string       `json:"binding_sha256"`
	Status        HealthStatus `json:"status"`
	DetailCode    string       `json:"detail_code"`
	ToolCount     int          `json:"tool_count,omitempty"`
}

func (h HealthObservation) Validate(binding LocalBinding) error {
	if h.ServerID != binding.ServerID || h.BindingSHA256 != binding.BindingSHA256 {
		return ErrInvalidHealth
	}
	switch h.Status {
	case HealthHealthy, HealthUnavailable, HealthUnhealthy:
	default:
		return fmt.Errorf("%w: unsupported status %q", ErrInvalidHealth, h.Status)
	}
	if strings.TrimSpace(h.DetailCode) == "" || h.ToolCount < 0 {
		return ErrInvalidHealth
	}
	return nil
}

type ApprovalState struct {
	ServerID       string   `json:"server_id"`
	SchemaSHA256   string   `json:"schema_sha256"`
	ApprovedTools  []string `json:"approved_tools"`
	ApprovalSHA256 string   `json:"approval_sha256"`
}

func newApproval(snapshot mcpregistry.DiscoverySnapshot, tools []string) (ApprovalState, error) {
	known := map[string]bool{}
	for _, tool := range snapshot.Tools {
		known[tool.Name] = true
	}
	tools = normalizedStrings(tools)
	for _, tool := range tools {
		if !known[tool] {
			return ApprovalState{}, fmt.Errorf("%w: %s", ErrUnknownTool, tool)
		}
	}
	a := ApprovalState{ServerID: snapshot.ServerID, SchemaSHA256: snapshot.SchemaSHA256, ApprovedTools: tools}
	hash, err := a.hashWithoutDigest()
	if err != nil {
		return ApprovalState{}, err
	}
	a.ApprovalSHA256 = hash
	return a, nil
}

func (a ApprovalState) Validate(snapshot mcpregistry.DiscoverySnapshot) error {
	if a.ServerID != snapshot.ServerID || a.SchemaSHA256 != snapshot.SchemaSHA256 {
		return ErrApprovalSchema
	}
	expected, err := newApproval(snapshot, a.ApprovedTools)
	if err != nil {
		return err
	}
	if a.ApprovalSHA256 != expected.ApprovalSHA256 {
		return errors.New("MCP permission approval digest is invalid")
	}
	return nil
}

func (a ApprovalState) hashWithoutDigest() (string, error) {
	raw, err := json.Marshal(struct {
		ServerID      string   `json:"server_id"`
		SchemaSHA256  string   `json:"schema_sha256"`
		ApprovedTools []string `json:"approved_tools"`
	}{a.ServerID, a.SchemaSHA256, normalizedStrings(a.ApprovedTools)})
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

type ServerView struct {
	ServerID        string                         `json:"server_id"`
	Registration    mcpregistry.Registration       `json:"registration"`
	Discovery       *mcpregistry.DiscoverySnapshot `json:"discovery,omitempty"`
	Binding         *LocalBinding                  `json:"binding,omitempty"`
	Health          HealthObservation              `json:"health"`
	Enabled         bool                           `json:"enabled"`
	Approval        *ApprovalState                 `json:"approval,omitempty"`
	ApprovalCurrent bool                           `json:"approval_current"`
	Ready           bool                           `json:"ready"`
}

func cloneStrings(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
