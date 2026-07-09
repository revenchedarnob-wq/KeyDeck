package recovery

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

type engineEventKind string

const (
	engineExecutionStarted engineEventKind = "execution_started"
	engineResultCompleted  engineEventKind = "result_completed"
	engineResultCommitted  engineEventKind = "result_committed"
)

type engineEvent struct {
	Sequence uint64          `json:"sequence"`
	At       time.Time       `json:"at"`
	EventID  string          `json:"event_id"`
	Kind     engineEventKind `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
}

type EngineStore struct {
	mu         sync.Mutex
	path       string
	events     []engineEvent
	byEventID  map[string]engineEvent
	executions map[string]Execution
	results    map[string]Result
}

var ErrEngineEventConflict = errors.New("engine event id already exists with different content")

func OpenEngineStore(path string) (*EngineStore, error) {
	if path == "" {
		return nil, errors.New("engine ledger path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &EngineStore{
		path: path, byEventID: map[string]engineEvent{}, executions: map[string]Execution{}, results: map[string]Result{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *EngineStore) StartOnce(in Execution) (Execution, bool, error) {
	if in.ExecutionID == "" || in.TaskID == "" || in.SessionID == "" || in.Engine == "" {
		return Execution{}, false, errors.New("execution id, task id, session id and engine are required")
	}
	if in.StartedAt.IsZero() {
		in.StartedAt = time.Now().UTC()
	}
	if in.Resumable && in.ExternalThreadID == "" {
		return Execution{}, false, errors.New("resumable execution requires external thread id")
	}
	appended, err := s.appendOnce("engine-start-"+in.ExecutionID, engineExecutionStarted, in)
	if err != nil {
		return Execution{}, false, err
	}
	return s.Execution(in.ExecutionID), appended, nil
}

func (s *EngineStore) CompleteResultOnce(in Result) (Result, bool, error) {
	if in.ResultID == "" || in.ExecutionID == "" || in.TaskID == "" || in.SessionID == "" || in.Engine == "" {
		return Result{}, false, errors.New("result identity is incomplete")
	}
	if in.CompletedAt.IsZero() {
		in.CompletedAt = time.Now().UTC()
	}
	if in.CanonicalCommitted {
		return Result{}, false, errors.New("new result cannot start as canonically committed")
	}
	appended, err := s.appendOnce("engine-result-"+in.ResultID, engineResultCompleted, in)
	if err != nil {
		return Result{}, false, err
	}
	return s.Result(in.ResultID), appended, nil
}

func (s *EngineStore) MarkCommittedOnce(resultID string) (Result, bool, error) {
	if resultID == "" {
		return Result{}, false, errors.New("result id is required")
	}
	appended, err := s.appendOnce("engine-commit-"+resultID, engineResultCommitted, map[string]string{"result_id": resultID})
	if err != nil {
		return Result{}, false, err
	}
	return s.Result(resultID), appended, nil
}

func (s *EngineStore) Execution(id string) Execution {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.executions[id]
}

func (s *EngineStore) Result(id string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.results[id]
}

func (s *EngineStore) Executions() []Execution {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Execution, 0, len(s.executions))
	for _, execution := range s.executions {
		out = append(out, execution)
	}
	return out
}

func (s *EngineStore) Results() []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Result, 0, len(s.results))
	for _, result := range s.results {
		out = append(out, result)
	}
	return out
}

func (s *EngineStore) appendOnce(eventID string, kind engineEventKind, payload any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	candidate := engineEvent{EventID: eventID, Kind: kind, Payload: raw}
	if existing, ok := s.byEventID[eventID]; ok {
		if engineFingerprint(existing) != engineFingerprint(candidate) {
			return false, ErrEngineEventConflict
		}
		return false, nil
	}
	event := candidate
	event.Sequence = uint64(len(s.events) + 1)
	event.At = time.Now().UTC()
	if err := appendEngineJSONLine(s.path, event); err != nil {
		return false, err
	}
	if err := s.apply(event); err != nil {
		return false, err
	}
	s.events = append(s.events, event)
	s.byEventID[eventID] = event
	return true, nil
}

func (s *EngineStore) load() error {
	f, err := os.Open(s.path)
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
		var event engineEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode engine ledger: %w", err)
		}
		if event.Sequence != expected {
			return fmt.Errorf("engine event sequence gap: expected %d got %d", expected, event.Sequence)
		}
		if _, exists := s.byEventID[event.EventID]; exists {
			return fmt.Errorf("duplicate engine event id %q", event.EventID)
		}
		if err := s.apply(event); err != nil {
			return fmt.Errorf("apply engine event %d: %w", event.Sequence, err)
		}
		s.events = append(s.events, event)
		s.byEventID[event.EventID] = event
		expected++
	}
	return scanner.Err()
}

func (s *EngineStore) apply(event engineEvent) error {
	switch event.Kind {
	case engineExecutionStarted:
		var execution Execution
		if err := json.Unmarshal(event.Payload, &execution); err != nil {
			return err
		}
		if previous, exists := s.executions[execution.ExecutionID]; exists && previous != execution {
			return errors.New("execution id conflict")
		}
		s.executions[execution.ExecutionID] = execution
	case engineResultCompleted:
		var result Result
		if err := json.Unmarshal(event.Payload, &result); err != nil {
			return err
		}
		execution, exists := s.executions[result.ExecutionID]
		if !exists {
			return fmt.Errorf("result references unknown execution %q", result.ExecutionID)
		}
		if execution.TaskID != result.TaskID || execution.SessionID != result.SessionID || execution.Engine != result.Engine {
			return errors.New("result identity does not match execution")
		}
		if previous, exists := s.results[result.ResultID]; exists && engineResultFingerprint(previous) != engineResultFingerprint(result) {
			return errors.New("result id conflict")
		}
		s.results[result.ResultID] = result
	case engineResultCommitted:
		var payload struct {
			ResultID string `json:"result_id"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		result, exists := s.results[payload.ResultID]
		if !exists {
			return fmt.Errorf("commit references unknown result %q", payload.ResultID)
		}
		result.CanonicalCommitted = true
		s.results[payload.ResultID] = result
	default:
		return fmt.Errorf("unknown engine event kind %q", event.Kind)
	}
	return nil
}

func engineFingerprint(event engineEvent) string {
	b, _ := json.Marshal(struct {
		EventID string          `json:"event_id"`
		Kind    engineEventKind `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}{event.EventID, event.Kind, event.Payload})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func engineResultFingerprint(result Result) string {
	copy := result
	copy.CanonicalCommitted = false
	b, _ := json.Marshal(copy)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func appendEngineJSONLine(path string, value any) error {
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
