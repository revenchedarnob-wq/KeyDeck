package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func New(id, projectRoot, goal, initialEngine string) State {
	now := time.Now().UTC()
	return State{
		Version:          1,
		SessionID:        id,
		ProjectRoot:      projectRoot,
		Goal:             goal,
		ActiveEngine:     initialEngine,
		Transcript:       []Message{},
		Decisions:        []Decision{},
		CompletedActions: []Action{},
		PendingTasks:     []string{},
		RelevantFiles:    []string{},
		EngineBindings:   map[string]EngineBinding{},
		UpdatedAt:        now,
	}
}

func Save(path string, state State) error {
	state.UpdatedAt = time.Now().UTC()
	if err := validate(state); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Load(path string) (State, error) {
	var state State
	b, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return state, err
	}
	if state.EngineBindings == nil {
		state.EngineBindings = map[string]EngineBinding{}
	}
	if err := validate(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func validate(state State) error {
	if state.SessionID == "" || state.ProjectRoot == "" || state.Goal == "" {
		return errors.New("session requires id, project root and goal")
	}
	return nil
}
