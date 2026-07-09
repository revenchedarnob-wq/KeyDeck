package handoff

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/proofreceipt"
)

type EventKind string

const (
	EventPackageSaved     EventKind = "package_saved"
	EventExecutionBound   EventKind = "execution_bound"
	EventPackageCancelled EventKind = "package_cancelled"
)

type durableEvent struct {
	Sequence            uint64          `json:"sequence"`
	At                  time.Time       `json:"at"`
	EventID             string          `json:"event_id"`
	Kind                EventKind       `json:"kind"`
	Payload             json.RawMessage `json:"payload"`
	PreviousEventSHA256 string          `json:"previous_event_sha256,omitempty"`
	EventSHA256         string          `json:"event_sha256"`
}
type ExecutionBinding struct {
	PackageID     string `json:"package_id"`
	PackageSHA256 string `json:"package_sha256"`
	ExecutionID   string `json:"execution_id"`
}
type Store struct {
	mu        sync.Mutex
	path      string
	events    []durableEvent
	packages  map[string]Package
	bindings  map[string]ExecutionBinding
	cancelled map[string]bool
}

var ErrStoreConflict = errors.New("handoff durable store conflict")

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("handoff store path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path, packages: map[string]Package{}, bindings: map[string]ExecutionBinding{}, cancelled: map[string]bool{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) Path() string { return s.path }
func (s *Store) SaveOnce(p Package) (Package, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := Validate(p, CurrentState{TaskSequence: p.Task.LastSequence, ContextPacketID: p.ContextPacketID, ProjectBrainRevisionSHA256: p.ProjectBrainRevisionSHA256}, nil); err != nil {
		return Package{}, false, err
	}
	if x, ok := s.packages[p.PackageID]; ok {
		if x.PackageSHA256 != p.PackageSHA256 {
			return Package{}, false, ErrStoreConflict
		}
		return x, false, nil
	}
	app, err := s.appendLocked("handoff-save-"+p.PackageID, EventPackageSaved, p)
	if err != nil {
		return Package{}, false, err
	}
	return s.packages[p.PackageID], app, nil
}
func (s *Store) BindExecutionOnce(packageID, executionID string) (ExecutionBinding, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packages[packageID]
	if !ok {
		return ExecutionBinding{}, false, errors.New("handoff package not persisted")
	}
	if executionID == "" || executionID != p.EngineRequest.ExecutionID {
		return ExecutionBinding{}, false, ErrStoreConflict
	}
	if x, ok := s.bindings[packageID]; ok {
		if x.ExecutionID != executionID || x.PackageSHA256 != p.PackageSHA256 {
			return ExecutionBinding{}, false, ErrStoreConflict
		}
		return x, false, nil
	}
	b := ExecutionBinding{PackageID: packageID, PackageSHA256: p.PackageSHA256, ExecutionID: executionID}
	app, err := s.appendLocked("handoff-bind-"+packageID, EventExecutionBound, b)
	if err != nil {
		return ExecutionBinding{}, false, err
	}
	return s.bindings[packageID], app, nil
}
func (s *Store) CancelOnce(packageID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.packages[packageID]; !ok {
		return false, errors.New("handoff package not persisted")
	}
	if s.cancelled[packageID] {
		return false, nil
	}
	return s.appendLocked("handoff-cancel-"+packageID, EventPackageCancelled, map[string]string{"package_id": packageID})
}
func (s *Store) Package(id string) (Package, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.packages[id]
	return p, ok
}
func (s *Store) Binding(id string) (ExecutionBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.bindings[id]
	return b, ok
}
func (s *Store) Cancelled(id string) bool { s.mu.Lock(); defer s.mu.Unlock(); return s.cancelled[id] }
func (s *Store) ReceiptArtifact() (proofreceipt.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return proofreceipt.Artifact{}, err
	}
	sum := sha256.Sum256(raw)
	return proofreceipt.Artifact{Name: "handoff package store", Path: s.path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw))}, nil
}
func (s *Store) appendLocked(id string, kind EventKind, payload any) (bool, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	for _, e := range s.events {
		if e.EventID == id {
			probe := durableEvent{EventID: id, Kind: kind, Payload: raw}
			if eventFingerprint(e) != eventFingerprint(probe) {
				return false, ErrStoreConflict
			}
			return false, nil
		}
	}
	prev := ""
	if len(s.events) > 0 {
		prev = s.events[len(s.events)-1].EventSHA256
	}
	e := durableEvent{Sequence: uint64(len(s.events) + 1), At: time.Now().UTC(), EventID: id, Kind: kind, Payload: raw, PreviousEventSHA256: prev}
	e.EventSHA256 = eventHash(e)
	nextP := clonePackages(s.packages)
	nextB := cloneBindings(s.bindings)
	nextC := cloneCancelled(s.cancelled)
	if err := applyDurable(nextP, nextB, nextC, e); err != nil {
		return false, err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	enc := json.NewEncoder(f)
	if err = enc.Encode(e); err != nil {
		_ = f.Close()
		return false, err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return false, err
	}
	if err = f.Close(); err != nil {
		return false, err
	}
	s.packages, s.bindings, s.cancelled = nextP, nextB, nextC
	s.events = append(s.events, e)
	return true, nil
}
func (s *Store) load() error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	expected := uint64(1)
	prev := ""
	for sc.Scan() {
		var e durableEvent
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return fmt.Errorf("decode handoff event: %w", err)
		}
		if e.Sequence != expected || e.PreviousEventSHA256 != prev || e.EventSHA256 != eventHash(e) {
			return ErrStoreConflict
		}
		if err := applyDurable(s.packages, s.bindings, s.cancelled, e); err != nil {
			return err
		}
		s.events = append(s.events, e)
		prev = e.EventSHA256
		expected++
	}
	return sc.Err()
}
func applyDurable(p map[string]Package, b map[string]ExecutionBinding, c map[string]bool, e durableEvent) error {
	switch e.Kind {
	case EventPackageSaved:
		var x Package
		if err := json.Unmarshal(e.Payload, &x); err != nil {
			return err
		}
		if err := Validate(x, CurrentState{TaskSequence: x.Task.LastSequence, ContextPacketID: x.ContextPacketID, ProjectBrainRevisionSHA256: x.ProjectBrainRevisionSHA256}, nil); err != nil {
			return err
		}
		if old, ok := p[x.PackageID]; ok && old.PackageSHA256 != x.PackageSHA256 {
			return ErrStoreConflict
		}
		p[x.PackageID] = x
	case EventExecutionBound:
		var x ExecutionBinding
		if err := json.Unmarshal(e.Payload, &x); err != nil {
			return err
		}
		pkg, ok := p[x.PackageID]
		if !ok || pkg.PackageSHA256 != x.PackageSHA256 || pkg.EngineRequest.ExecutionID != x.ExecutionID {
			return ErrStoreConflict
		}
		if old, ok := b[x.PackageID]; ok && old != x {
			return ErrStoreConflict
		}
		b[x.PackageID] = x
	case EventPackageCancelled:
		var x map[string]string
		if err := json.Unmarshal(e.Payload, &x); err != nil {
			return err
		}
		id := x["package_id"]
		if _, ok := p[id]; !ok {
			return ErrStoreConflict
		}
		c[id] = true
	default:
		return ErrStoreConflict
	}
	return nil
}
func eventHash(e durableEvent) string {
	x := e
	x.EventSHA256 = ""
	raw, _ := json.Marshal(x)
	s := sha256.Sum256(raw)
	return hex.EncodeToString(s[:])
}
func eventFingerprint(e durableEvent) string {
	raw, _ := json.Marshal(struct {
		EventID string          `json:"event_id"`
		Kind    EventKind       `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}{e.EventID, e.Kind, e.Payload})
	s := sha256.Sum256(raw)
	return hex.EncodeToString(s[:])
}
func clonePackages(in map[string]Package) map[string]Package {
	out := map[string]Package{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneBindings(in map[string]ExecutionBinding) map[string]ExecutionBinding {
	out := map[string]ExecutionBinding{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func cloneCancelled(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
