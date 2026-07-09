package engineruntime

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type eventKind string

const (
	eventExecutionStarted   eventKind = "execution_started"
	eventBindingSaved       eventKind = "binding_saved"
	eventDispositionChanged eventKind = "disposition_changed"
)

type event struct {
	Sequence uint64          `json:"sequence"`
	At       time.Time       `json:"at"`
	EventID  string          `json:"event_id"`
	Kind     eventKind       `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
}

type transition struct {
	ExecutionID string      `json:"execution_id"`
	Disposition Disposition `json:"disposition"`
	BindingID   string      `json:"binding_id,omitempty"`
	ResultID    string      `json:"result_id,omitempty"`
	Detail      string      `json:"detail,omitempty"`
}

type Store struct {
	mu         sync.Mutex
	path       string
	events     []event
	byEventID  map[string]event
	executions map[string]Execution
	bindings   map[string]Binding
}

var ErrEventConflict = errors.New("runtime event id already exists with different content")

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("runtime ledger path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		path:       path,
		byEventID:  map[string]event{},
		executions: map[string]Execution{},
		bindings:   map[string]Binding{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) BeginOnce(in Execution) (Execution, bool, error) {
	if in.ExecutionID == "" || in.TaskID == "" || in.SessionID == "" || in.EngineID == "" || in.Operation == "" {
		return Execution{}, false, errors.New("execution identity and operation are required")
	}
	if in.Disposition == "" {
		in.Disposition = DispositionRunning
	}
	if in.Disposition != DispositionRunning {
		return Execution{}, false, errors.New("new runtime execution must start as running")
	}
	if in.StartedAt.IsZero() {
		in.StartedAt = time.Now().UTC()
	}
	in.UpdatedAt = in.StartedAt
	appended, err := s.appendOnce("runtime-start-"+in.ExecutionID, eventExecutionStarted, in)
	if err != nil {
		return Execution{}, false, err
	}
	return s.Execution(in.ExecutionID), appended, nil
}

func (s *Store) SaveBindingOnce(in Binding) (Binding, bool, error) {
	if in.BindingID == "" || in.TaskID == "" || in.SessionID == "" || in.ExecutionID == "" || in.EngineID == "" || in.ExternalHandle == "" {
		return Binding{}, false, errors.New("binding identity and external handle are required")
	}
	canonicalizeCapabilities(&in.Capabilities)
	if existing := s.Binding(in.BindingID); existing.BindingID != "" {
		if bindingFingerprint(existing) != bindingFingerprint(in) {
			return Binding{}, false, ErrEventConflict
		}
		return existing, false, nil
	}
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = time.Now().UTC()
	}
	eventID := "runtime-binding-" + in.BindingID + "-" + shortFingerprint(in)
	appended, err := s.appendOnce(eventID, eventBindingSaved, in)
	if err != nil {
		return Binding{}, false, err
	}
	return s.Binding(in.BindingID), appended, nil
}

func (s *Store) SetDispositionOnce(executionID string, disposition Disposition, bindingID, resultID, detail string) (Execution, bool, error) {
	if executionID == "" || disposition == "" || disposition == DispositionRunning {
		return Execution{}, false, errors.New("execution id and non-running disposition are required")
	}
	payload := transition{ExecutionID: executionID, Disposition: disposition, BindingID: bindingID, ResultID: resultID, Detail: detail}
	eventID := "runtime-transition-" + executionID + "-" + string(disposition)
	appended, err := s.appendOnce(eventID, eventDispositionChanged, payload)
	if err != nil {
		return Execution{}, false, err
	}
	return s.Execution(executionID), appended, nil
}

func (s *Store) Execution(id string) Execution {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.executions[id]
}

func (s *Store) Binding(id string) Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bindings[id]
}

func (s *Store) BindingForExecution(executionID string) (Binding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	execution, ok := s.executions[executionID]
	if !ok || execution.BindingID == "" {
		return Binding{}, false
	}
	binding, ok := s.bindings[execution.BindingID]
	return binding, ok
}

func (s *Store) Executions() []Execution {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Execution, 0, len(s.executions))
	for _, execution := range s.executions {
		out = append(out, execution)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExecutionID < out[j].ExecutionID })
	return out
}

func (s *Store) appendOnce(eventID string, kind eventKind, payload any) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	candidate := event{EventID: eventID, Kind: kind, Payload: raw}
	if existing, ok := s.byEventID[eventID]; ok {
		if fingerprintEvent(existing) != fingerprintEvent(candidate) {
			return false, ErrEventConflict
		}
		return false, nil
	}
	candidate.Sequence = uint64(len(s.events) + 1)
	candidate.At = time.Now().UTC()
	nextExecutions := cloneExecutions(s.executions)
	nextBindings := cloneBindings(s.bindings)
	if err := applyEvent(nextExecutions, nextBindings, candidate); err != nil {
		return false, err
	}
	if err := appendJSONLine(s.path, candidate); err != nil {
		return false, err
	}
	s.executions = nextExecutions
	s.bindings = nextBindings
	s.events = append(s.events, candidate)
	s.byEventID[eventID] = candidate
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
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	var expected uint64 = 1
	for scanner.Scan() {
		var e event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return fmt.Errorf("decode runtime event: %w", err)
		}
		if e.Sequence != expected {
			return fmt.Errorf("runtime event sequence gap: expected %d got %d", expected, e.Sequence)
		}
		if _, exists := s.byEventID[e.EventID]; exists {
			return fmt.Errorf("duplicate runtime event id %q", e.EventID)
		}
		if err := applyEvent(s.executions, s.bindings, e); err != nil {
			return fmt.Errorf("apply runtime event %d: %w", e.Sequence, err)
		}
		s.events = append(s.events, e)
		s.byEventID[e.EventID] = e
		expected++
	}
	return scanner.Err()
}

func applyEvent(executions map[string]Execution, bindings map[string]Binding, e event) error {
	switch e.Kind {
	case eventExecutionStarted:
		var execution Execution
		if err := json.Unmarshal(e.Payload, &execution); err != nil {
			return err
		}
		if previous, exists := executions[execution.ExecutionID]; exists && fingerprint(previous) != fingerprint(execution) {
			return errors.New("runtime execution id conflict")
		}
		executions[execution.ExecutionID] = execution
	case eventBindingSaved:
		var binding Binding
		if err := json.Unmarshal(e.Payload, &binding); err != nil {
			return err
		}
		execution, exists := executions[binding.ExecutionID]
		if !exists {
			return fmt.Errorf("binding references unknown execution %q", binding.ExecutionID)
		}
		if execution.TaskID != binding.TaskID || execution.SessionID != binding.SessionID || execution.EngineID != binding.EngineID {
			return errors.New("binding identity does not match execution")
		}
		if previous, exists := bindings[binding.BindingID]; exists && bindingFingerprint(previous) != bindingFingerprint(binding) {
			return errors.New("runtime binding id conflict")
		}
		bindings[binding.BindingID] = binding
		execution.BindingID = binding.BindingID
		execution.UpdatedAt = e.At
		executions[execution.ExecutionID] = execution
	case eventDispositionChanged:
		var p transition
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		execution, exists := executions[p.ExecutionID]
		if !exists {
			return fmt.Errorf("transition references unknown execution %q", p.ExecutionID)
		}
		if !allowedTransition(execution.Disposition, p.Disposition) {
			return fmt.Errorf("unsafe runtime transition %q -> %q", execution.Disposition, p.Disposition)
		}
		if p.BindingID != "" {
			binding, exists := bindings[p.BindingID]
			if !exists || binding.ExecutionID != execution.ExecutionID {
				return errors.New("transition references invalid binding")
			}
			execution.BindingID = p.BindingID
		}
		execution.Disposition = p.Disposition
		execution.ResultID = p.ResultID
		execution.Detail = p.Detail
		execution.UpdatedAt = e.At
		executions[p.ExecutionID] = execution
	default:
		return fmt.Errorf("unknown runtime event kind %q", e.Kind)
	}
	return nil
}

func allowedTransition(from, to Disposition) bool {
	if from == to {
		return true
	}
	switch from {
	case DispositionRunning:
		return to == DispositionCompleted || to == DispositionResumeRequired || to == DispositionInputRequired || to == DispositionFailed || to == DispositionCancelled
	case DispositionResumeRequired:
		return to == DispositionCompleted || to == DispositionInputRequired || to == DispositionFailed || to == DispositionCancelled
	default:
		return false
	}
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

func fingerprintEvent(e event) string {
	return fingerprint(struct {
		EventID string          `json:"event_id"`
		Kind    eventKind       `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}{e.EventID, e.Kind, e.Payload})
}

func fingerprint(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shortFingerprint(value any) string {
	full := fingerprint(value)
	if len(full) > 24 {
		return full[:24]
	}
	return full
}

func bindingFingerprint(binding Binding) string {
	copy := binding
	copy.UpdatedAt = time.Time{}
	return fingerprint(copy)
}

func cloneExecutions(in map[string]Execution) map[string]Execution {
	out := make(map[string]Execution, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneBindings(in map[string]Binding) map[string]Binding {
	out := make(map[string]Binding, len(in))
	for k, v := range in {
		copy := v
		copy.Capabilities = append([]Capability(nil), v.Capabilities...)
		out[k] = copy
	}
	return out
}

func canonicalizeCapabilities(values *[]Capability) {
	seen := map[Capability]bool{}
	out := make([]Capability, 0, len(*values))
	for _, value := range *values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	*values = out
}
