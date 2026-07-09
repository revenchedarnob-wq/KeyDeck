package tasks

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type EventKind string

const (
	EventTaskCreated        EventKind = "task_created"
	EventStatusChanged      EventKind = "status_changed"
	EventStepStarted        EventKind = "step_started"
	EventStepCompleted      EventKind = "step_completed"
	EventStepFailed         EventKind = "step_failed"
	EventStepAmbiguous      EventKind = "step_ambiguous"
	EventAcceptanceUpdated  EventKind = "acceptance_updated"
	EventInputRequested     EventKind = "input_requested"
	EventTaskCancelled      EventKind = "task_cancelled"
	EventRecoveryReconciled EventKind = "recovery_reconciled"
)

type Event struct {
	Sequence uint64          `json:"sequence"`
	At       time.Time       `json:"at"`
	Kind     EventKind       `json:"kind"`
	TaskID   string          `json:"task_id"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type Store struct {
	mu     sync.Mutex
	path   string
	state  State
	loaded bool
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state)
}

func (s *Store) Append(kind EventKind, payload any) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		return State{}, errors.New("task store is not loaded")
	}
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return State{}, err
		}
		raw = b
	}
	taskID := s.state.TaskID
	if kind == EventTaskCreated && taskID == "" {
		var created struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(raw, &created); err != nil {
			return State{}, err
		}
		taskID = created.TaskID
	}
	e := Event{
		Sequence: s.state.LastSequence + 1,
		At:       time.Now().UTC(),
		Kind:     kind,
		TaskID:   taskID,
		Payload:  raw,
	}
	next := cloneState(s.state)
	if err := apply(&next, e); err != nil {
		return State{}, err
	}
	if err := appendEvent(s.path, e); err != nil {
		return State{}, err
	}
	s.state = next
	return cloneState(s.state), nil
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		s.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var state State
	var expected uint64 = 1
	for scanner.Scan() {
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return fmt.Errorf("decode task event: %w", err)
		}
		if e.Sequence != expected {
			return fmt.Errorf("task event sequence gap: expected %d got %d", expected, e.Sequence)
		}
		if err := apply(&state, e); err != nil {
			return fmt.Errorf("apply task event %d: %w", e.Sequence, err)
		}
		expected++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	s.state = state
	s.loaded = true
	return nil
}

func appendEvent(path string, e Event) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err := enc.Encode(e); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func apply(state *State, e Event) error {
	if e.Sequence == 0 {
		return errors.New("event sequence is required")
	}
	if !state.CreatedAt.IsZero() && state.TaskID != e.TaskID {
		return errors.New("event task id does not match store task")
	}
	switch e.Kind {
	case EventTaskCreated:
		if !state.CreatedAt.IsZero() {
			return errors.New("task already created")
		}
		var p struct {
			SessionID string   `json:"session_id"`
			Contract  Contract `json:"contract"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if e.TaskID == "" || p.SessionID == "" || p.Contract.Goal == "" || len(p.Contract.Checks) == 0 {
			return errors.New("task creation requires task id, session id, goal and acceptance checks")
		}
		checks := append([]AcceptanceCheck(nil), p.Contract.Checks...)
		for i := range checks {
			if checks[i].Status == "" {
				checks[i].Status = CheckPending
			}
		}
		p.Contract.Checks = checks
		*state = State{
			Version: 1, TaskID: e.TaskID, SessionID: p.SessionID, Status: StatusWorking,
			Contract: p.Contract, Steps: map[string]Step{}, CreatedAt: e.At, UpdatedAt: e.At,
		}
	case EventStatusChanged:
		var p struct {
			Status Status `json:"status"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		state.Status = p.Status
		if p.Status != StatusInputRequired {
			state.NeedsInput = ""
		}
	case EventStepStarted:
		var p Step
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if p.ID == "" {
			return errors.New("step id is required")
		}
		p.Status = "started"
		p.UpdatedAt = e.At
		state.Steps[p.ID] = p
	case EventStepCompleted:
		var p Step
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		prev, ok := state.Steps[p.ID]
		if !ok {
			return errors.New("cannot complete unknown step")
		}
		prev.Status = "completed"
		prev.Result = p.Result
		prev.Error = ""
		prev.UpdatedAt = e.At
		state.Steps[p.ID] = prev
	case EventStepFailed:
		var p Step
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		prev, ok := state.Steps[p.ID]
		if !ok {
			return errors.New("cannot fail unknown step")
		}
		prev.Status = "failed"
		prev.Error = p.Error
		prev.UpdatedAt = e.At
		state.Steps[p.ID] = prev
	case EventStepAmbiguous:
		var p Step
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		prev, ok := state.Steps[p.ID]
		if !ok {
			return errors.New("cannot mark unknown step ambiguous")
		}
		prev.Status = "ambiguous"
		prev.Error = p.Error
		prev.UpdatedAt = e.At
		state.Steps[p.ID] = prev
		state.Status = StatusInputRequired
		state.NeedsInput = p.Error
	case EventAcceptanceUpdated:
		var p AcceptanceCheck
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		found := false
		for i := range state.Contract.Checks {
			if state.Contract.Checks[i].ID == p.ID {
				p.Description = state.Contract.Checks[i].Description
				p.UpdatedAt = e.At
				state.Contract.Checks[i] = p
				found = true
				break
			}
		}
		if !found {
			return errors.New("unknown acceptance check")
		}
		if state.Progress().Complete {
			state.Status = StatusCompleted
		}
	case EventInputRequested:
		var p struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		state.Status = StatusInputRequired
		state.NeedsInput = p.Reason
	case EventTaskCancelled:
		state.Status = StatusCancelled
	case EventRecoveryReconciled:
		// Audit-only event. State changes are represented by the adjacent step event.
	default:
		return fmt.Errorf("unknown task event kind %q", e.Kind)
	}
	state.LastSequence = e.Sequence
	state.UpdatedAt = e.At
	return nil
}

func cloneState(in State) State {
	out := in
	out.Contract.RequiredOutcomes = append([]string(nil), in.Contract.RequiredOutcomes...)
	out.Contract.ForbiddenScope = append([]string(nil), in.Contract.ForbiddenScope...)
	out.Contract.Checks = append([]AcceptanceCheck(nil), in.Contract.Checks...)
	out.Steps = make(map[string]Step, len(in.Steps))
	for k, v := range in.Steps {
		out.Steps[k] = v
	}
	return out
}
