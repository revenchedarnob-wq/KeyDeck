package routing

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

	"keydeck.local/feasibilitylab/internal/proofreceipt"
)

var ErrRouteStoreConflict = errors.New("route store conflict")

type storeEvent struct {
	Sequence            uint64 `json:"sequence"`
	EventID             string `json:"event_id"`
	PreviousEventSHA256 string `json:"previous_event_sha256,omitempty"`
	EventSHA256         string `json:"event_sha256"`
	Plan                Plan   `json:"plan"`
}

type Store struct {
	mu     sync.Mutex
	path   string
	events []storeEvent
	plans  map[string]Plan
}

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("route store path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path, plans: map[string]Plan{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) SaveOnce(plan Plan) (Plan, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plan.Version != 1 || plan.RouteID == "" || plan.RouteSHA256 == "" || plan.RouteSHA256 != planDigest(plan) || plan.RouteID != "route-"+plan.RouteSHA256[:20] {
		return Plan{}, false, ErrInvalidPlan
	}
	if existing, ok := s.plans[plan.RouteID]; ok {
		if existing.RouteSHA256 != plan.RouteSHA256 {
			return Plan{}, false, ErrRouteStoreConflict
		}
		return existing, false, nil
	}
	prev := ""
	if len(s.events) > 0 {
		prev = s.events[len(s.events)-1].EventSHA256
	}
	ev := storeEvent{Sequence: uint64(len(s.events) + 1), EventID: "route-save-" + plan.RouteID, PreviousEventSHA256: prev, Plan: plan}
	ev.EventSHA256 = routeEventHash(ev)
	if err := appendRouteEvent(s.path, ev); err != nil {
		return Plan{}, false, err
	}
	s.events = append(s.events, ev)
	s.plans[plan.RouteID] = plan
	return plan, true, nil
}

func (s *Store) Plan(id string) (Plan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	return p, ok
}
func (s *Store) ReceiptArtifact() (proofreceipt.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return proofreceipt.Artifact{}, err
	}
	sum := sha256.Sum256(raw)
	return proofreceipt.Artifact{Name: "route plan store", Path: s.path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw))}, nil
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
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	expected := uint64(1)
	prev := ""
	for sc.Scan() {
		var ev storeEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return fmt.Errorf("decode route event: %w", err)
		}
		if ev.Sequence != expected || ev.PreviousEventSHA256 != prev || ev.EventSHA256 != routeEventHash(ev) {
			return ErrRouteStoreConflict
		}
		p := ev.Plan
		if p.Version != 1 || p.RouteSHA256 != planDigest(p) || p.RouteID != "route-"+p.RouteSHA256[:20] {
			return ErrRouteStoreConflict
		}
		if _, exists := s.plans[p.RouteID]; exists {
			return ErrRouteStoreConflict
		}
		s.events = append(s.events, ev)
		s.plans[p.RouteID] = p
		expected++
		prev = ev.EventSHA256
	}
	return sc.Err()
}
func routeEventHash(ev storeEvent) string {
	x := ev
	x.EventSHA256 = ""
	raw, _ := json.Marshal(x)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func appendRouteEvent(path string, ev storeEvent) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err = enc.Encode(ev); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
