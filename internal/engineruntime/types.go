package engineruntime

import (
	"context"
	"time"

	"keydeck.local/feasibilitylab/internal/session"
)

type Capability string

const (
	CapabilityText              Capability = "text"
	CapabilityPersistentSession Capability = "persistent_session"
	CapabilityResume            Capability = "resume"
	CapabilityCancel            Capability = "cancel"
)

type HealthState string

const (
	HealthHealthy   HealthState = "healthy"
	HealthDegraded  HealthState = "degraded"
	HealthUnhealthy HealthState = "unhealthy"
)

type Health struct {
	State  HealthState `json:"state"`
	Detail string      `json:"detail,omitempty"`
}

type Operation string

const (
	OperationStart    Operation = "start"
	OperationContinue Operation = "continue"
	OperationResume   Operation = "resume"
)

type Disposition string

const (
	DispositionRunning        Disposition = "running"
	DispositionCompleted      Disposition = "completed"
	DispositionResumeRequired Disposition = "resume_required"
	DispositionInputRequired  Disposition = "input_required"
	DispositionFailed         Disposition = "failed"
	DispositionCancelled      Disposition = "cancelled"
)

type Request struct {
	ExecutionID          string           `json:"execution_id"`
	TaskID               string           `json:"task_id"`
	SessionID            string           `json:"session_id"`
	EngineID             string           `json:"engine_id"`
	Prompt               string           `json:"prompt,omitempty"`
	Passport             session.Passport `json:"passport"`
	RequiredCapabilities []Capability     `json:"required_capabilities,omitempty"`
	Binding              *Binding         `json:"binding,omitempty"`
}

type Outcome struct {
	Disposition    Disposition          `json:"disposition"`
	Result         session.EngineResult `json:"result,omitempty"`
	ExternalHandle string               `json:"external_handle,omitempty"`
	Resumable      bool                 `json:"resumable,omitempty"`
	Detail         string               `json:"detail,omitempty"`
}

type Binding struct {
	BindingID       string       `json:"binding_id"`
	TaskID          string       `json:"task_id"`
	SessionID       string       `json:"session_id"`
	ExecutionID     string       `json:"execution_id"`
	EngineID        string       `json:"engine_id"`
	ExternalHandle  string       `json:"external_handle"`
	ResumeSupported bool         `json:"resume_supported"`
	Capabilities    []Capability `json:"capabilities,omitempty"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

type Execution struct {
	ExecutionID string      `json:"execution_id"`
	TaskID      string      `json:"task_id"`
	SessionID   string      `json:"session_id"`
	EngineID    string      `json:"engine_id"`
	Operation   Operation   `json:"operation"`
	Disposition Disposition `json:"disposition"`
	BindingID   string      `json:"binding_id,omitempty"`
	ResultID    string      `json:"result_id,omitempty"`
	Detail      string      `json:"detail,omitempty"`
	StartedAt   time.Time   `json:"started_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type Adapter interface {
	ID() string
	Capabilities(context.Context) ([]Capability, error)
	Health(context.Context) (Health, error)
	Start(context.Context, Request) (Outcome, error)
	Continue(context.Context, Request) (Outcome, error)
	Resume(context.Context, Request) (Outcome, error)
	Cancel(context.Context, Binding) error
}

type Result struct {
	Execution      Execution `json:"execution"`
	Binding        *Binding  `json:"binding,omitempty"`
	AdapterInvoked bool      `json:"adapter_invoked"`
}
