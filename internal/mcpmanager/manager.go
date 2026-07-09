package mcpmanager

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpregistry"
)

const managerVersion = 1

type event struct {
	Sequence              uint64             `json:"sequence"`
	Version               int                `json:"version"`
	Kind                  string             `json:"kind"`
	ServerID              string             `json:"server_id"`
	Binding               *LocalBinding      `json:"binding,omitempty"`
	PreviousBindingSHA256 string             `json:"previous_binding_sha256,omitempty"`
	Enabled               *bool              `json:"enabled,omitempty"`
	Reason                string             `json:"reason,omitempty"`
	Approval              *ApprovalState     `json:"approval,omitempty"`
	Health                *HealthObservation `json:"health,omitempty"`
}

type HealthChecker interface {
	Check(ctx context.Context, binding LocalBinding) (HealthObservation, error)
}

type Manager struct {
	mu        sync.Mutex
	path      string
	registry  *mcpregistry.Registry
	events    []event
	bindings  map[string]LocalBinding
	enabled   map[string]bool
	approvals map[string]ApprovalState
	health    map[string]HealthObservation
}

func Open(path string, registry *mcpregistry.Registry) (*Manager, error) {
	if strings.TrimSpace(path) == "" || registry == nil {
		return nil, errors.New("MCP manager path and portable registry are required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	m := &Manager{
		path: path, registry: registry,
		bindings: map[string]LocalBinding{}, enabled: map[string]bool{},
		approvals: map[string]ApprovalState{}, health: map[string]HealthObservation{},
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Bind(binding LocalBinding) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reg, ok := m.registry.Registration(binding.ServerID)
	if !ok {
		return false, mcpregistry.ErrUnknownServer
	}
	if err := binding.Validate(reg); err != nil {
		return false, err
	}
	if existing, ok := m.bindings[binding.ServerID]; ok {
		if existing.BindingSHA256 == binding.BindingSHA256 {
			return false, nil
		}
		return false, ErrBindingConflict
	}
	ev := event{Sequence: uint64(len(m.events) + 1), Version: managerVersion, Kind: "bound", ServerID: binding.ServerID, Binding: &binding}
	if err := m.appendLocked(ev); err != nil {
		return false, err
	}
	m.bindings[binding.ServerID] = binding
	delete(m.health, binding.ServerID)
	return true, nil
}

func (m *Manager) Rebind(binding LocalBinding, reason string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false, errors.New("MCP rebind reason is required")
	}
	reg, ok := m.registry.Registration(binding.ServerID)
	if !ok {
		return false, mcpregistry.ErrUnknownServer
	}
	if err := binding.Validate(reg); err != nil {
		return false, err
	}
	existing, ok := m.bindings[binding.ServerID]
	if !ok {
		return false, ErrNoBinding
	}
	if existing.BindingSHA256 == binding.BindingSHA256 {
		return false, nil
	}
	ev := event{Sequence: uint64(len(m.events) + 1), Version: managerVersion, Kind: "rebound", ServerID: binding.ServerID, Binding: &binding, PreviousBindingSHA256: existing.BindingSHA256, Reason: reason}
	if err := m.appendLocked(ev); err != nil {
		return false, err
	}
	m.bindings[binding.ServerID] = binding
	delete(m.health, binding.ServerID)
	return true, nil
}

func (m *Manager) SetEnabled(serverID string, enabled bool, reason string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.registry.Registration(serverID); !ok {
		return false, mcpregistry.ErrUnknownServer
	}
	if enabled {
		if _, ok := m.bindings[serverID]; !ok {
			return false, ErrNoBinding
		}
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return false, errors.New("MCP enable/disable reason is required")
	}
	current := m.enabled[serverID]
	if current == enabled {
		return false, nil
	}
	value := enabled
	kind := "disabled"
	if enabled {
		kind = "enabled"
	}
	ev := event{Sequence: uint64(len(m.events) + 1), Version: managerVersion, Kind: kind, ServerID: serverID, Enabled: &value, Reason: reason}
	if err := m.appendLocked(ev); err != nil {
		return false, err
	}
	m.enabled[serverID] = enabled
	return true, nil
}

func (m *Manager) ApproveTools(serverID, schemaSHA256 string, tools []string) (ApprovalState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.registry.Registration(serverID); !ok {
		return ApprovalState{}, mcpregistry.ErrUnknownServer
	}
	snapshot, ok := m.registry.CachedDiscovery(serverID)
	if !ok || snapshot.SchemaSHA256 != schemaSHA256 {
		return ApprovalState{}, ErrApprovalSchema
	}
	approval, err := newApproval(snapshot, tools)
	if err != nil {
		return ApprovalState{}, err
	}
	if existing, ok := m.approvals[serverID]; ok && existing.ApprovalSHA256 == approval.ApprovalSHA256 {
		return existing, nil
	}
	ev := event{Sequence: uint64(len(m.events) + 1), Version: managerVersion, Kind: "approved", ServerID: serverID, Approval: &approval}
	if err := m.appendLocked(ev); err != nil {
		return ApprovalState{}, err
	}
	m.approvals[serverID] = approval
	return approval, nil
}

func (m *Manager) CheckHealth(ctx context.Context, serverID string, checker HealthChecker) (HealthObservation, error) {
	m.mu.Lock()
	binding, ok := m.bindings[serverID]
	m.mu.Unlock()
	if !ok {
		return HealthObservation{}, ErrNoBinding
	}
	if checker == nil {
		return HealthObservation{}, errors.New("MCP health checker is required")
	}
	observation, checkErr := checker.Check(ctx, binding)
	if err := observation.Validate(binding); err != nil {
		return HealthObservation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.bindings[serverID]
	if !ok || current.BindingSHA256 != binding.BindingSHA256 {
		return HealthObservation{}, ErrInvalidHealth
	}
	ev := event{Sequence: uint64(len(m.events) + 1), Version: managerVersion, Kind: "health", ServerID: serverID, Health: &observation}
	if err := m.appendLocked(ev); err != nil {
		return HealthObservation{}, err
	}
	m.health[serverID] = observation
	return observation, checkErr
}

func (m *Manager) View(serverID string) (ServerView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	reg, ok := m.registry.Registration(serverID)
	if !ok {
		return ServerView{}, mcpregistry.ErrUnknownServer
	}
	view := ServerView{ServerID: serverID, Registration: reg, Enabled: m.enabled[serverID], Health: HealthObservation{ServerID: serverID, Status: HealthUnknown, DetailCode: "not_checked"}}
	if snapshot, ok := m.registry.CachedDiscovery(serverID); ok {
		copy := snapshot
		view.Discovery = &copy
	}
	if binding, ok := m.bindings[serverID]; ok {
		copy := binding
		view.Binding = &copy
		view.Health.BindingSHA256 = binding.BindingSHA256
		if health, ok := m.health[serverID]; ok && health.BindingSHA256 == binding.BindingSHA256 {
			view.Health = health
		}
	}
	if approval, ok := m.approvals[serverID]; ok {
		copy := approval
		view.Approval = &copy
		view.ApprovalCurrent = view.Discovery != nil && approval.SchemaSHA256 == view.Discovery.SchemaSHA256
	}
	view.Ready = view.Enabled && view.Binding != nil && view.Health.Status == HealthHealthy && view.ApprovalCurrent && view.Approval != nil && len(view.Approval.ApprovedTools) > 0
	return view, nil
}

func (m *Manager) EffectivePolicy(serverID string, activeProfile mcpbridge.PermissionProfile) (*mcpbridge.PermissionPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.registry.Registration(serverID); !ok {
		return nil, mcpregistry.ErrUnknownServer
	}
	snapshot, ok := m.registry.CachedDiscovery(serverID)
	if !ok {
		policy := &mcpbridge.PermissionPolicy{Profile: activeProfile, ToolProfiles: map[string]mcpbridge.PermissionProfile{}}
		return policy, policy.Validate()
	}
	approval, ok := m.approvals[serverID]
	if !ok || approval.SchemaSHA256 != snapshot.SchemaSHA256 {
		policy := &mcpbridge.PermissionPolicy{Profile: activeProfile, ToolProfiles: map[string]mcpbridge.PermissionProfile{}}
		return policy, policy.Validate()
	}
	approved := map[string]bool{}
	for _, tool := range approval.ApprovedTools {
		approved[tool] = true
	}
	return mcpregistry.ProposePermissions(snapshot).BuildPolicy(activeProfile, approved)
}

func (m *Manager) EventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func (m *Manager) load() error {
	f, err := os.Open(m.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	var expected uint64 = 1
	for scanner.Scan() {
		var ev event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			return fmt.Errorf("decode MCP manager event: %w", err)
		}
		if ev.Sequence != expected || ev.Version != managerVersion {
			return fmt.Errorf("%w: sequence/version at %d", ErrManagerState, expected)
		}
		if _, ok := m.registry.Registration(ev.ServerID); !ok {
			return mcpregistry.ErrUnknownServer
		}
		switch ev.Kind {
		case "bound":
			if ev.Binding == nil || ev.Binding.ServerID != ev.ServerID {
				return ErrManagerState
			}
			if _, exists := m.bindings[ev.ServerID]; exists {
				return ErrBindingConflict
			}
			reg, _ := m.registry.Registration(ev.ServerID)
			if err := ev.Binding.Validate(reg); err != nil {
				return err
			}
			m.bindings[ev.ServerID] = *ev.Binding
		case "rebound":
			if ev.Binding == nil || strings.TrimSpace(ev.Reason) == "" {
				return ErrManagerState
			}
			old, exists := m.bindings[ev.ServerID]
			if !exists || old.BindingSHA256 != ev.PreviousBindingSHA256 {
				return ErrManagerState
			}
			reg, _ := m.registry.Registration(ev.ServerID)
			if err := ev.Binding.Validate(reg); err != nil {
				return err
			}
			m.bindings[ev.ServerID] = *ev.Binding
			delete(m.health, ev.ServerID)
		case "enabled", "disabled":
			if ev.Enabled == nil || strings.TrimSpace(ev.Reason) == "" || (*ev.Enabled != (ev.Kind == "enabled")) {
				return ErrManagerState
			}
			if *ev.Enabled {
				if _, ok := m.bindings[ev.ServerID]; !ok {
					return ErrManagerState
				}
			}
			m.enabled[ev.ServerID] = *ev.Enabled
		case "approved":
			if ev.Approval == nil {
				return ErrManagerState
			}
			snapshot, ok := m.registry.CachedDiscovery(ev.ServerID)
			if !ok {
				return ErrManagerState
			}
			if err := ev.Approval.Validate(snapshot); err != nil {
				return err
			}
			m.approvals[ev.ServerID] = *ev.Approval
		case "health":
			if ev.Health == nil {
				return ErrManagerState
			}
			binding, ok := m.bindings[ev.ServerID]
			if !ok || ev.Health.Validate(binding) != nil {
				return ErrManagerState
			}
			m.health[ev.ServerID] = *ev.Health
		default:
			return fmt.Errorf("%w: unsupported event kind %q", ErrManagerState, ev.Kind)
		}
		m.events = append(m.events, ev)
		expected++
	}
	return scanner.Err()
}

func (m *Manager) appendLocked(ev event) error {
	f, err := os.OpenFile(m.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(ev); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	m.events = append(m.events, ev)
	return nil
}

func ApprovedToolNames(approval ApprovalState) []string {
	out := append([]string(nil), approval.ApprovedTools...)
	sort.Strings(out)
	return out
}
