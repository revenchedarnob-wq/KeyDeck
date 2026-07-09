package mcpregistry

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
)

const registryVersion = 1

type event struct {
	Sequence     uint64             `json:"sequence"`
	Version      int                `json:"version"`
	Kind         string             `json:"kind"`
	Registration *Registration      `json:"registration,omitempty"`
	Discovery    *DiscoverySnapshot `json:"discovery,omitempty"`
}

type Discoverer interface {
	BoundServerIdentity() *mcpbridge.ServerIdentity
	BoundRuntimeContract() RuntimeContract
	DiscoverTools(ctx context.Context) ([]mcpbridge.Tool, error)
}

type ClientDiscoverer struct {
	Client   *mcpbridge.Client
	Identity *mcpbridge.ServerIdentity
	Contract RuntimeContract
}

func (d *ClientDiscoverer) BoundServerIdentity() *mcpbridge.ServerIdentity {
	if d == nil || d.Identity == nil {
		return nil
	}
	copy := *d.Identity
	return &copy
}

func (d *ClientDiscoverer) BoundRuntimeContract() RuntimeContract { return d.Contract }

func (d *ClientDiscoverer) DiscoverTools(ctx context.Context) ([]mcpbridge.Tool, error) {
	if d == nil || d.Client == nil {
		return nil, errors.New("MCP discoverer client is not configured")
	}
	session, err := d.Client.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if err := session.Initialize(); err != nil {
		return nil, err
	}
	return session.ListTools()
}

type Registry struct {
	mu            sync.Mutex
	path          string
	events        []event
	registrations map[string]Registration
	discoveries   map[string]DiscoverySnapshot
}

func Open(path string) (*Registry, error) {
	if path == "" {
		return nil, errors.New("MCP registry path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &Registry{path: path, registrations: map[string]Registration{}, discoveries: map[string]DiscoverySnapshot{}}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) Register(reg Registration) (Registration, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := reg.Validate(); err != nil {
		return Registration{}, false, err
	}
	if existing, ok := r.registrations[reg.ServerID]; ok {
		if existing.IdentitySHA256 != reg.IdentitySHA256 || existing.RuntimeSHA256 != reg.RuntimeSHA256 {
			return existing, false, ErrRegistrationConflict
		}
		return existing, false, nil
	}
	ev := event{Sequence: uint64(len(r.events) + 1), Version: registryVersion, Kind: "registered", Registration: &reg}
	if err := r.appendLocked(ev); err != nil {
		return Registration{}, false, err
	}
	r.registrations[reg.ServerID] = reg
	return reg, true, nil
}

func (r *Registry) Registration(serverID string) (Registration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reg, ok := r.registrations[serverID]
	return reg, ok
}

func (r *Registry) CachedDiscovery(serverID string) (DiscoverySnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, ok := r.discoveries[serverID]
	return snapshot, ok
}

func (r *Registry) Discover(ctx context.Context, serverID string, discoverer Discoverer) (DiscoverySnapshot, bool, error) {
	r.mu.Lock()
	reg, ok := r.registrations[serverID]
	if !ok {
		r.mu.Unlock()
		return DiscoverySnapshot{}, false, ErrUnknownServer
	}
	if err := bindingMatches(reg, discoverer); err != nil {
		r.mu.Unlock()
		return DiscoverySnapshot{}, false, err
	}
	if cached, ok := r.discoveries[serverID]; ok {
		r.mu.Unlock()
		return cached, true, nil
	}
	r.mu.Unlock()

	tools, err := discoverer.DiscoverTools(ctx)
	if err != nil {
		return DiscoverySnapshot{}, false, err
	}
	snapshot, err := snapshotFor(reg, tools)
	if err != nil {
		return DiscoverySnapshot{}, false, err
	}
	stored, _, err := r.storeDiscovery(snapshot)
	return stored, false, err
}

func (r *Registry) Revalidate(ctx context.Context, serverID string, discoverer Discoverer) (DiscoverySnapshot, error) {
	r.mu.Lock()
	reg, ok := r.registrations[serverID]
	if !ok {
		r.mu.Unlock()
		return DiscoverySnapshot{}, ErrUnknownServer
	}
	if err := bindingMatches(reg, discoverer); err != nil {
		r.mu.Unlock()
		return DiscoverySnapshot{}, err
	}
	r.mu.Unlock()
	tools, err := discoverer.DiscoverTools(ctx)
	if err != nil {
		return DiscoverySnapshot{}, err
	}
	snapshot, err := snapshotFor(reg, tools)
	if err != nil {
		return DiscoverySnapshot{}, err
	}
	stored, _, err := r.storeDiscovery(snapshot)
	return stored, err
}

func (r *Registry) storeDiscovery(snapshot DiscoverySnapshot) (DiscoverySnapshot, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reg, ok := r.registrations[snapshot.ServerID]
	if !ok {
		return DiscoverySnapshot{}, false, ErrUnknownServer
	}
	if err := snapshot.Validate(reg); err != nil {
		return DiscoverySnapshot{}, false, err
	}
	if existing, ok := r.discoveries[snapshot.ServerID]; ok {
		if existing.SchemaSHA256 != snapshot.SchemaSHA256 {
			return existing, false, ErrCapabilityDrift
		}
		return existing, false, nil
	}
	ev := event{Sequence: uint64(len(r.events) + 1), Version: registryVersion, Kind: "discovered", Discovery: &snapshot}
	if err := r.appendLocked(ev); err != nil {
		return DiscoverySnapshot{}, false, err
	}
	r.discoveries[snapshot.ServerID] = snapshot
	return snapshot, true, nil
}

func bindingMatches(reg Registration, discoverer Discoverer) error {
	if discoverer == nil {
		return errors.New("MCP discoverer is required")
	}
	identity := discoverer.BoundServerIdentity()
	if identity == nil {
		return ErrIdentityDrift
	}
	identityHash, err := identity.Hash()
	if err != nil || identityHash != reg.IdentitySHA256 {
		return ErrIdentityDrift
	}
	hash, err := discoverer.BoundRuntimeContract().Hash()
	if err != nil {
		return err
	}
	if hash != reg.RuntimeSHA256 {
		return ErrRuntimeDrift
	}
	return nil
}

func (r *Registry) load() error {
	f, err := os.Open(r.path)
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
			return fmt.Errorf("decode MCP registry event: %w", err)
		}
		if ev.Sequence != expected || ev.Version != registryVersion {
			return fmt.Errorf("invalid MCP registry event sequence/version at %d", expected)
		}
		switch ev.Kind {
		case "registered":
			if ev.Registration == nil || ev.Discovery != nil {
				return errors.New("invalid MCP registration event")
			}
			if err := ev.Registration.Validate(); err != nil {
				return err
			}
			if _, exists := r.registrations[ev.Registration.ServerID]; exists {
				return ErrRegistrationConflict
			}
			r.registrations[ev.Registration.ServerID] = *ev.Registration
		case "discovered":
			if ev.Discovery == nil || ev.Registration != nil {
				return errors.New("invalid MCP discovery event")
			}
			reg, ok := r.registrations[ev.Discovery.ServerID]
			if !ok {
				return ErrUnknownServer
			}
			if err := ev.Discovery.Validate(reg); err != nil {
				return err
			}
			if _, exists := r.discoveries[ev.Discovery.ServerID]; exists {
				return ErrCapabilityDrift
			}
			r.discoveries[ev.Discovery.ServerID] = *ev.Discovery
		default:
			return fmt.Errorf("unsupported MCP registry event kind %q", ev.Kind)
		}
		r.events = append(r.events, ev)
		expected++
	}
	return scanner.Err()
}

func (r *Registry) appendLocked(ev event) error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
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
	r.events = append(r.events, ev)
	return nil
}
