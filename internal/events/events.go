package events

import (
	"sync"
	"time"
)

type Kind string

const (
	RequestAttempt      Kind = "request_attempt"
	KeyStateChanged     Kind = "key_state_changed"
	ProviderBusy        Kind = "provider_busy"
	AmbiguousFailure    Kind = "ambiguous_failure"
	RequestSucceeded    Kind = "request_succeeded"
	CostSafetyTriggered Kind = "cost_safety_triggered"
	RequestBlocked      Kind = "request_blocked"
)

type Event struct {
	At           time.Time `json:"at"`
	Kind         Kind      `json:"kind"`
	KeyID        string    `json:"key_id,omitempty"`
	Attempt      int       `json:"attempt,omitempty"`
	FailureClass string    `json:"failure_class,omitempty"`
	Detail       string    `json:"detail,omitempty"`
}

type Recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *Recorder) Add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	r.events = append(r.events, e)
}

func (r *Recorder) Snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}
