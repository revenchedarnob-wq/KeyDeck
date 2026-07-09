package timeline

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
)

type Domain string

const (
	DomainTask     Domain = "task"
	DomainEngine   Domain = "engine"
	DomainTool     Domain = "tool"
	DomainArtifact Domain = "artifact"
	DomainProof    Domain = "proof"
)

type Input struct {
	EventID   string `json:"event_id"`
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Domain    Domain `json:"domain"`
	Kind      string `json:"kind"`
	SourceRef string `json:"source_ref,omitempty"`
	Summary   string `json:"summary,omitempty"`
	DataHash  string `json:"data_hash,omitempty"`
}

type Event struct {
	Sequence  uint64    `json:"sequence"`
	At        time.Time `json:"at"`
	EventID   string    `json:"event_id"`
	TaskID    string    `json:"task_id"`
	SessionID string    `json:"session_id"`
	Domain    Domain    `json:"domain"`
	Kind      string    `json:"kind"`
	SourceRef string    `json:"source_ref,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	DataHash  string    `json:"data_hash,omitempty"`
}

type Store struct {
	mu     sync.Mutex
	path   string
	events []Event
	byID   map[string]Event
}

var ErrEventIDConflict = errors.New("timeline event id already exists with different content")

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("timeline path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path, byID: map[string]Event{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) AppendOnce(in Input) (Event, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateInput(in); err != nil {
		return Event{}, false, err
	}
	if existing, ok := s.byID[in.EventID]; ok {
		if fingerprint(eventInput(existing)) != fingerprint(in) {
			return Event{}, false, ErrEventIDConflict
		}
		return existing, false, nil
	}

	event := Event{
		Sequence:  uint64(len(s.events) + 1),
		At:        time.Now().UTC(),
		EventID:   in.EventID,
		TaskID:    in.TaskID,
		SessionID: in.SessionID,
		Domain:    in.Domain,
		Kind:      in.Kind,
		SourceRef: in.SourceRef,
		Summary:   in.Summary,
		DataHash:  in.DataHash,
	}
	if err := appendJSONLine(s.path, event); err != nil {
		return Event{}, false, err
	}
	s.events = append(s.events, event)
	s.byID[event.EventID] = event
	return event, true, nil
}

func (s *Store) Snapshot() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, len(s.events))
	copy(out, s.events)
	return out
}

func (s *Store) ByTask(taskID string) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Event, 0, len(s.events))
	for _, event := range s.events {
		if event.TaskID == taskID {
			out = append(out, event)
		}
	}
	return out
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

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var expected uint64 = 1
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode timeline event: %w", err)
		}
		if event.Sequence != expected {
			return fmt.Errorf("timeline sequence gap: expected %d got %d", expected, event.Sequence)
		}
		in := eventInput(event)
		if err := validateInput(in); err != nil {
			return fmt.Errorf("invalid timeline event %d: %w", event.Sequence, err)
		}
		if _, exists := s.byID[event.EventID]; exists {
			return fmt.Errorf("duplicate timeline event id %q", event.EventID)
		}
		s.events = append(s.events, event)
		s.byID[event.EventID] = event
		expected++
	}
	return scanner.Err()
}

func validateInput(in Input) error {
	if in.EventID == "" || in.TaskID == "" || in.SessionID == "" || in.Domain == "" || in.Kind == "" {
		return errors.New("event id, task id, session id, domain and kind are required")
	}
	switch in.Domain {
	case DomainTask, DomainEngine, DomainTool, DomainArtifact, DomainProof:
	default:
		return fmt.Errorf("unsupported timeline domain %q", in.Domain)
	}
	return nil
}

func eventInput(event Event) Input {
	return Input{
		EventID: event.EventID, TaskID: event.TaskID, SessionID: event.SessionID,
		Domain: event.Domain, Kind: event.Kind, SourceRef: event.SourceRef,
		Summary: event.Summary, DataHash: event.DataHash,
	}
}

func fingerprint(in Input) string {
	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func appendJSONLine(path string, value any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(value); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
