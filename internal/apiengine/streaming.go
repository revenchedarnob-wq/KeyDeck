package apiengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"keydeck.local/feasibilitylab/internal/continuity"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/session"
)

// StreamingEngine adapts the same-model streaming continuity engine to the
// canonical KeyDeck session interface. Explicit exhaustion of every API key is
// surfaced as a PartialResultError so another engine can continue safely.
type StreamingEngine struct {
	Continuity *continuity.Engine
	NameValue  string
}

func (e *StreamingEngine) Name() string {
	if strings.TrimSpace(e.NameValue) == "" {
		return "api-pool"
	}
	return e.NameValue
}

func (e *StreamingEngine) Run(ctx context.Context, passport session.Passport, userPrompt string) (session.EngineResult, error) {
	if e.Continuity == nil {
		return session.EngineResult{}, fmt.Errorf("streaming continuity engine is required")
	}
	request, err := json.Marshal(map[string]any{
		"session_id":        passport.SessionID,
		"goal":              passport.Goal,
		"from_engine":       passport.FromEngine,
		"handoff_reason":    passport.HandoffReason,
		"user_prompt":       userPrompt,
		"decisions":         passport.Decisions,
		"completed_actions": passport.CompletedActions,
		"transcript":        passport.Transcript,
		"pending_tasks":     passport.PendingTasks,
		"relevant_files":    passport.RelevantFiles,
		"checkpoint":        passport.Checkpoint,
	})
	if err != nil {
		return session.EngineResult{}, err
	}

	result, err := e.Continuity.Execute(ctx, request)
	if err == nil {
		return session.EngineResult{Text: strings.TrimSpace(result.Text)}, nil
	}
	if errors.Is(err, pool.ErrAllKeysUnavailable) {
		continuation := session.ContinuationState{
			OriginalPrompt:   userPrompt,
			ConfirmedOutput:  result.ConfirmedOutput,
			UnstableFragment: result.UnstableFragment,
			SourceEngine:     e.Name(),
			Reason:           "all API keys explicitly exhausted after partial streamed output",
		}
		return session.EngineResult{}, &session.PartialResultError{
			Cause:        err,
			Continuation: continuation,
		}
	}
	return session.EngineResult{}, err
}
