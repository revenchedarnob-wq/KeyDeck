package session

import "time"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type Message struct {
	At     time.Time `json:"at"`
	Role   Role      `json:"role"`
	Engine string    `json:"engine,omitempty"`
	Text   string    `json:"text"`
}

type Decision struct {
	At      time.Time `json:"at"`
	Summary string    `json:"summary"`
	Source  string    `json:"source"`
}

type Action struct {
	At      time.Time `json:"at"`
	Summary string    `json:"summary"`
	Source  string    `json:"source"`
}

type EngineBinding struct {
	Engine            string    `json:"engine"`
	ExternalThreadID  string    `json:"external_thread_id,omitempty"`
	ExternalSessionID string    `json:"external_session_id,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type State struct {
	Version          int                      `json:"version"`
	SessionID        string                   `json:"session_id"`
	ProjectRoot      string                   `json:"project_root"`
	Goal             string                   `json:"goal"`
	ActiveEngine     string                   `json:"active_engine"`
	Transcript       []Message                `json:"transcript"`
	Decisions        []Decision               `json:"decisions"`
	CompletedActions []Action                 `json:"completed_actions"`
	PendingTasks     []string                 `json:"pending_tasks"`
	RelevantFiles    []string                 `json:"relevant_files"`
	Checkpoint       string                   `json:"checkpoint,omitempty"`
	EngineBindings   map[string]EngineBinding `json:"engine_bindings,omitempty"`
	InFlightResponse *InFlightResponse        `json:"in_flight_response,omitempty"`
	UpdatedAt        time.Time                `json:"updated_at"`
}

type Passport struct {
	SessionID        string             `json:"session_id"`
	ProjectRoot      string             `json:"project_root"`
	Goal             string             `json:"goal"`
	FromEngine       string             `json:"from_engine,omitempty"`
	ToEngine         string             `json:"to_engine"`
	HandoffReason    string             `json:"handoff_reason"`
	Transcript       []Message          `json:"transcript"`
	Decisions        []Decision         `json:"decisions"`
	CompletedActions []Action           `json:"completed_actions"`
	PendingTasks     []string           `json:"pending_tasks"`
	RelevantFiles    []string           `json:"relevant_files"`
	Checkpoint       string             `json:"checkpoint,omitempty"`
	Continuation     *ContinuationState `json:"continuation,omitempty"`
}
