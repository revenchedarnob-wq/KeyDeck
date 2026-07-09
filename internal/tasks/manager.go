package tasks

import (
	"encoding/json"
	"errors"
	"fmt"

	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type Manager struct {
	Store   *Store
	Journal *tooljournal.Journal
}

func (m *Manager) Create(taskID, sessionID string, contract Contract) (State, error) {
	if m.Store == nil {
		return State{}, errors.New("task store is required")
	}
	if m.Store.State().TaskID != "" {
		return State{}, errors.New("task already exists")
	}
	return m.Store.Append(EventTaskCreated, map[string]any{"session_id": sessionID, "contract": contract, "task_id": taskID})
}

func (m *Manager) BeginStep(stepID, operationID, tool string, args []byte, policy tooljournal.ReplayPolicy) (tooljournal.Decision, State, error) {
	if m.Store == nil || m.Journal == nil {
		return tooljournal.Decision{}, State{}, errors.New("task store and tool journal are required")
	}
	state := m.Store.State()
	if state.Status == StatusCancelled || state.Status == StatusCompleted || state.Status == StatusFailed {
		return tooljournal.Decision{}, state, fmt.Errorf("task is terminal: %s", state.Status)
	}
	decision, err := m.Journal.Begin(operationID, tool, args, policy)
	if errors.Is(err, tooljournal.ErrAmbiguousOperation) {
		step := state.Steps[stepID]
		if step.ID == "" {
			step = Step{ID: stepID, OperationID: operationID, Tool: tool}
			if _, appendErr := m.Store.Append(EventStepStarted, step); appendErr != nil {
				return tooljournal.Decision{}, m.Store.State(), appendErr
			}
		}
		blocked, appendErr := m.Store.Append(EventStepAmbiguous, Step{ID: stepID, Error: err.Error()})
		if appendErr != nil {
			return tooljournal.Decision{}, m.Store.State(), appendErr
		}
		_, _ = m.Store.Append(EventRecoveryReconciled, map[string]any{"step_id": stepID, "resolution": "blocked_ambiguous"})
		return tooljournal.Decision{}, blocked, err
	}
	if err != nil {
		return tooljournal.Decision{}, state, err
	}

	if decision.Kind == tooljournal.DecisionReturnPrevious {
		if _, ok := state.Steps[stepID]; !ok {
			if _, err := m.Store.Append(EventStepStarted, Step{ID: stepID, OperationID: operationID, Tool: tool}); err != nil {
				return decision, m.Store.State(), err
			}
		}
		next, err := m.Store.Append(EventStepCompleted, Step{ID: stepID, Result: decision.Result})
		if err != nil {
			return decision, m.Store.State(), err
		}
		_, _ = m.Store.Append(EventRecoveryReconciled, map[string]any{"step_id": stepID, "resolution": "reused_completed_tool_result"})
		return decision, next, nil
	}

	if existing, ok := state.Steps[stepID]; ok && existing.Status == "started" {
		_, _ = m.Store.Append(EventRecoveryReconciled, map[string]any{"step_id": stepID, "resolution": "retry_idempotent"})
		return decision, m.Store.State(), nil
	}
	next, err := m.Store.Append(EventStepStarted, Step{ID: stepID, OperationID: operationID, Tool: tool})
	return decision, next, err
}

func (m *Manager) CompleteStep(stepID, operationID, result string) (State, error) {
	if err := m.Journal.Complete(operationID, result); err != nil {
		return m.Store.State(), err
	}
	return m.Store.Append(EventStepCompleted, Step{ID: stepID, Result: result})
}

func (m *Manager) FailStep(stepID, operationID, message string) (State, error) {
	if err := m.Journal.Fail(operationID, message); err != nil {
		return m.Store.State(), err
	}
	return m.Store.Append(EventStepFailed, Step{ID: stepID, Error: message})
}

func (m *Manager) UpdateCheck(checkID string, status CheckStatus, evidence string) (State, error) {
	return m.Store.Append(EventAcceptanceUpdated, AcceptanceCheck{ID: checkID, Status: status, Evidence: evidence})
}

func (m *Manager) RequestInput(reason string) (State, error) {
	return m.Store.Append(EventInputRequested, map[string]string{"reason": reason})
}

func (m *Manager) ResumeWorking() (State, error) {
	return m.Store.Append(EventStatusChanged, map[string]Status{"status": StatusWorking})
}

func (m *Manager) Cancel() (State, error) {
	return m.Store.Append(EventTaskCancelled, nil)
}

func (m *Manager) MarshalState() ([]byte, error) {
	return json.MarshalIndent(m.Store.State(), "", "  ")
}
