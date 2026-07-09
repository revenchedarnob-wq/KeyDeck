package codexapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"keydeck.local/feasibilitylab/internal/session"
)

type Engine struct {
	Client   *Client
	ThreadID string
	Model    string
	OnEvent  func(Notification)
}

func (e *Engine) Name() string { return "codex" }

func (e *Engine) Run(ctx context.Context, passport session.Passport, userPrompt string) (session.EngineResult, error) {
	if e.Client == nil {
		return session.EngineResult{}, fmt.Errorf("codex client is required")
	}
	if e.ThreadID == "" {
		thread, err := e.Client.StartThread(ctx, StartThreadOptions{
			Model:          e.Model,
			CWD:            passport.ProjectRoot,
			ApprovalPolicy: "never",
			Sandbox:        ThreadSandboxWorkspaceWrite,
			ServiceName:    "keydeck_lab",
		})
		if err != nil {
			return session.EngineResult{}, err
		}
		e.ThreadID = thread.ID
	} else {
		if _, err := e.Client.ResumeThread(ctx, e.ThreadID); err != nil {
			return session.EngineResult{}, err
		}
	}

	handoff := BuildHandoffPrompt(passport, userPrompt)
	turn, err := e.Client.StartTurn(ctx, e.ThreadID, handoff, passport.ProjectRoot)
	if err != nil {
		return session.EngineResult{}, err
	}
	outcome, err := e.Client.CollectTurnObserved(ctx, turn.ID, e.OnEvent)
	if err != nil {
		return session.EngineResult{}, err
	}
	result := session.EngineResult{Text: strings.TrimSpace(outcome.Text)}
	for _, note := range outcome.Events {
		if note.Method != "item/completed" {
			continue
		}
		var payload struct {
			Item struct {
				Type    string `json:"type"`
				Changes []struct {
					Path string `json:"path"`
					Kind string `json:"kind"`
				} `json:"changes"`
			} `json:"item"`
		}
		if json.Unmarshal(note.Params, &payload) != nil {
			continue
		}
		if payload.Item.Type == "fileChange" {
			for _, change := range payload.Item.Changes {
				if change.Path == "" {
					continue
				}
				result.CompletedActions = append(result.CompletedActions, "Codex "+change.Kind+" file: "+change.Path)
				result.RelevantFiles = append(result.RelevantFiles, change.Path)
			}
		}
	}
	return result, nil
}

func BuildHandoffPrompt(passport session.Passport, userPrompt string) string {
	// The passport is deliberately structured and secret-free. It does not
	// include provider keys, vault data, or hidden model state.
	compact := map[string]any{
		"session_id":        passport.SessionID,
		"goal":              passport.Goal,
		"from_engine":       passport.FromEngine,
		"handoff_reason":    passport.HandoffReason,
		"decisions":         passport.Decisions,
		"completed_actions": passport.CompletedActions,
		"pending_tasks":     passport.PendingTasks,
		"relevant_files":    passport.RelevantFiles,
		"checkpoint":        passport.Checkpoint,
	}
	if passport.Continuation != nil {
		compact["cross_engine_continuation"] = passport.Continuation
	}
	b, _ := json.MarshalIndent(compact, "", "  ")
	return fmt.Sprintf(`You are continuing one KeyDeck-owned project session. KeyDeck, not Codex, is the canonical state owner.

Handoff passport:
%s

Current user request:
%s

Rules:
- Inspect the current project state before acting.
- Do not repeat completed actions.
- Treat ambiguous prior tool actions as unsafe to replay.
- Keep work scoped to the project root.
- If cross_engine_continuation is present, its confirmed_output is already visible to the user: do not repeat it.
- Treat unstable_fragment only as directional context; do not present it as confirmed text.
- Continue from the next logical step and complete the user's task.
- Return a concise continuation/completion summary for KeyDeck.`, string(b), userPrompt)
}

func (e *Engine) RestoreBinding(binding session.EngineBinding) error {
	if binding.Engine != "" && binding.Engine != e.Name() {
		return fmt.Errorf("binding belongs to engine %q", binding.Engine)
	}
	e.ThreadID = binding.ExternalThreadID
	return nil
}

func (e *Engine) CurrentBinding() session.EngineBinding {
	return session.EngineBinding{Engine: e.Name(), ExternalThreadID: e.ThreadID}
}
