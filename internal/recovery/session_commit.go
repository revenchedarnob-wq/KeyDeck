package recovery

import (
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/session"
)

const resultCommitSourcePrefix = "keydeck-engine-result:"

func ResultCommitMarker(resultID string) string {
	return resultCommitSourcePrefix + resultID
}

func HasCanonicalResult(state session.State, resultID string) bool {
	marker := ResultCommitMarker(resultID)
	for _, action := range state.CompletedActions {
		if action.Source == marker {
			return true
		}
	}
	return false
}

func ApplyResultToSession(state session.State, result Result) (session.State, bool) {
	if HasCanonicalResult(state, result.ResultID) {
		return state, false
	}
	now := time.Now().UTC()
	state.ActiveEngine = result.Engine
	state.Transcript = append(state.Transcript, session.Message{At: now, Role: session.RoleAssistant, Engine: result.Engine, Text: result.Output.Text})
	for _, decision := range result.Output.Decisions {
		state.Decisions = append(state.Decisions, session.Decision{At: now, Summary: decision, Source: result.Engine})
	}
	for _, action := range result.Output.CompletedActions {
		state.CompletedActions = append(state.CompletedActions, session.Action{At: now, Summary: action, Source: result.Engine})
	}
	if result.Output.PendingTasks != nil {
		state.PendingTasks = append([]string(nil), result.Output.PendingTasks...)
	}
	if result.Output.RelevantFiles != nil {
		state.RelevantFiles = mergeUnique(state.RelevantFiles, result.Output.RelevantFiles)
	}
	if result.Output.Checkpoint != "" {
		state.Checkpoint = result.Output.Checkpoint
	}
	if result.ExternalThreadID != "" {
		if state.EngineBindings == nil {
			state.EngineBindings = map[string]session.EngineBinding{}
		}
		state.EngineBindings[result.Engine] = session.EngineBinding{
			Engine: result.Engine, ExternalThreadID: result.ExternalThreadID, UpdatedAt: now,
		}
	}
	state.CompletedActions = append(state.CompletedActions, session.Action{
		At: now, Summary: "Canonical engine result committed exactly once", Source: ResultCommitMarker(result.ResultID),
	})
	return state, true
}

func mergeUnique(existing, incoming []string) []string {
	seen := make(map[string]bool, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	for _, value := range append(append([]string(nil), existing...), incoming...) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
