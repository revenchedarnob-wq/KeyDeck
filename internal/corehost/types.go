package corehost

import (
	"errors"
	"io"
	"net/http"
	"time"

	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

const (
	DefaultAPIVersion   = "v1"
	DefaultMaxBodyBytes = int64(64 << 10)
)

var (
	ErrAlreadyRunning      = errors.New("KeyDeck core is already running")
	ErrInvalidConfig       = errors.New("invalid core host configuration")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrIdempotencyConflict = errors.New("idempotency key already used for different request")
	ErrTaskConflict        = errors.New("task id already exists with different canonical content")
	ErrNotFound            = errors.New("not found")
	ErrIdentityMismatch    = errors.New("core host identity mismatch")
	ErrRequestTooLarge     = errors.New("request body too large")
)

type Layout struct {
	DataDir        string `json:"data_dir"`
	CredentialPath string `json:"credential_path"`
	RuntimePath    string `json:"runtime_path"`
	LeaseDir       string `json:"lease_dir"`
	TaskDir        string `json:"task_dir"`
	TimelinePath   string `json:"timeline_path"`
	RequestJournal string `json:"request_journal"`
}

type Config struct {
	DataDir              string
	ListenAddress        string
	BuildID              string
	APIVersion           string
	SupervisorInstanceID string
	MaxBodyBytes         int64
	StaleLeaseAfter      time.Duration
	HeartbeatEvery       time.Duration
	Random               io.Reader
	Now                  func() time.Time
	Backend              Backend
	HTTPClient           *http.Client
}

type Credential struct {
	Version   int    `json:"version"`
	InstallID string `json:"install_id"`
	Token     string `json:"token"`
}

type RuntimeInfo struct {
	Version              int    `json:"version"`
	InstanceID           string `json:"instance_id"`
	InstallID            string `json:"install_id"`
	Address              string `json:"address"`
	BuildID              string `json:"build_id"`
	APIVersion           string `json:"api_version"`
	PID                  int    `json:"pid"`
	SupervisorInstanceID string `json:"supervisor_instance_id,omitempty"`
}

type Identity struct {
	Product    string `json:"product"`
	BuildID    string `json:"build_id"`
	APIVersion string `json:"api_version"`
	InstallID  string `json:"install_id"`
	InstanceID string `json:"instance_id"`
}

type TimelineEvent = timeline.Event

type TaskCreateRequest struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id"`
	Contract  tasks.Contract `json:"contract"`
}

type TaskCreateResult struct {
	State      tasks.State `json:"state"`
	Reused     bool        `json:"reused"`
	Reconciled bool        `json:"reconciled"`
}

type TaskSummary struct {
	TaskID       string         `json:"task_id"`
	SessionID    string         `json:"session_id"`
	Status       tasks.Status   `json:"status"`
	LastSequence uint64         `json:"last_sequence"`
	Progress     tasks.Progress `json:"progress"`
}

type ProjectionSnapshot struct {
	Identity  Identity        `json:"identity"`
	Status    Status          `json:"status"`
	Tasks     []TaskSummary   `json:"tasks"`
	Timeline  []TimelineEvent `json:"timeline"`
	After     uint64          `json:"after"`
	NextAfter uint64          `json:"next_after"`
}

type Status struct {
	Product        string `json:"product"`
	BuildID        string `json:"build_id"`
	APIVersion     string `json:"api_version"`
	TaskCount      int    `json:"task_count"`
	TimelineEvents int    `json:"timeline_events"`
	RequestRecords int    `json:"request_records"`
}

type Backend interface {
	CreateTask(TaskCreateRequest, string) (TaskCreateResult, error)
	GetTask(string) (tasks.State, error)
	ListTasks() ([]TaskSummary, error)
	Timeline(after uint64, limit int) ([]timeline.Event, error)
	Status() (Status, error)
}
