package session

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	InFlightAwaitingHandoff = "awaiting_handoff"
)

// ContinuationState is KeyDeck-owned, model-agnostic visible-response state.
// It contains only user-visible or canonical task context. It never contains
// hidden model state, provider secrets, or credentials.
type ContinuationState struct {
	OriginalPrompt   string `json:"original_prompt"`
	ConfirmedOutput  string `json:"confirmed_output"`
	UnstableFragment string `json:"unstable_fragment,omitempty"`
	SourceEngine     string `json:"source_engine"`
	Reason           string `json:"reason"`
}

type InFlightResponse struct {
	ResponseID   string            `json:"response_id"`
	Status       string            `json:"status"`
	Continuation ContinuationState `json:"continuation"`
	StartedAt    time.Time         `json:"started_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// PartialResultError carries explicit, safe-to-handoff visible response state
// across an engine failure. The wrapped cause still controls fallback policy.
type PartialResultError struct {
	Cause        error
	Partial      EngineResult
	Continuation ContinuationState
}

func (e *PartialResultError) Error() string {
	if e == nil {
		return "partial engine result"
	}
	if e.Cause == nil {
		return "partial engine result"
	}
	return e.Cause.Error()
}

func (e *PartialResultError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func AsPartialResult(err error) (*PartialResultError, bool) {
	var partial *PartialResultError
	if !errors.As(err, &partial) || partial == nil {
		return nil, false
	}
	return partial, true
}

func mergeContinuationOutput(confirmed, continuation string) string {
	confirmed = strings.TrimRight(confirmed, " \t\r\n")
	continuation = strings.TrimSpace(continuation)
	if confirmed == "" {
		return continuation
	}
	if continuation == "" {
		return confirmed
	}

	trimmedConfirmed := strings.TrimSpace(confirmed)
	if strings.HasPrefix(continuation, trimmedConfirmed) {
		continuation = strings.TrimSpace(strings.TrimPrefix(continuation, trimmedConfirmed))
		if continuation == "" {
			return confirmed
		}
	}
	return fmt.Sprintf("%s\n\n%s", confirmed, continuation)
}
