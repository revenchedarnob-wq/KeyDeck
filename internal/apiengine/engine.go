package apiengine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/session"
)

// Engine is the thin KeyDeck-owned adapter between canonical state and an API
// pool. The pool owns only key selection/failure safety; this layer compiles the
// request context and converts a successful provider envelope into an engine
// result.
type Engine struct {
	Pool               *pool.Pool
	NameValue          string
	EvidenceFiles      []string
	ExtraRequestFields map[string]any
}

func (e *Engine) Name() string {
	if strings.TrimSpace(e.NameValue) == "" {
		return "api-pool"
	}
	return e.NameValue
}

func (e *Engine) Run(ctx context.Context, passport session.Passport, userPrompt string) (session.EngineResult, error) {
	if e.Pool == nil {
		return session.EngineResult{}, fmt.Errorf("API pool is required")
	}
	request, err := e.buildRequest(passport, userPrompt)
	if err != nil {
		return session.EngineResult{}, err
	}
	result, err := e.Pool.Execute(ctx, request)
	if err != nil {
		return session.EngineResult{}, err
	}
	env, err := protocol.DecodeEnvelope(result.Output)
	if err != nil {
		return session.EngineResult{}, fmt.Errorf("decode API engine response: %w", err)
	}
	return session.EngineResult{Text: strings.TrimSpace(env.Output)}, nil
}

func (e *Engine) buildRequest(passport session.Passport, userPrompt string) ([]byte, error) {
	projectEvidence := map[string]string{}
	for _, name := range e.EvidenceFiles {
		clean := filepath.Clean(name)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return nil, fmt.Errorf("evidence file must stay inside project root: %s", name)
		}
		b, err := os.ReadFile(filepath.Join(passport.ProjectRoot, clean))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		projectEvidence[filepath.ToSlash(clean)] = string(b)
	}

	payload := map[string]any{
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
		"project_evidence":  projectEvidence,
	}
	for k, v := range e.ExtraRequestFields {
		payload[k] = v
	}
	return json.Marshal(payload)
}
