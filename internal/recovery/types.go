package recovery

import (
	"time"

	"keydeck.local/feasibilitylab/internal/session"
)

type Disposition string

const (
	DispositionReusedCompletedToolResult Disposition = "reused_completed_tool_result"
	DispositionBlockedAmbiguous          Disposition = "blocked_ambiguous"
	DispositionRetryIdempotent           Disposition = "retry_idempotent"
	DispositionCanonicalCommitApplied    Disposition = "canonical_commit_applied"
	DispositionCanonicalCommitReconciled Disposition = "canonical_commit_reconciled"
	DispositionResumeRequired            Disposition = "resume_required"
	DispositionInputRequired             Disposition = "input_required"
	DispositionProofReceiptReady         Disposition = "proof_receipt_ready"
)

type Decision struct {
	Domain      string      `json:"domain"`
	Reference   string      `json:"reference"`
	Disposition Disposition `json:"disposition"`
	EventID     string      `json:"event_id"`
	Appended    bool        `json:"appended"`
}

type Report struct {
	TaskID      string     `json:"task_id"`
	SessionID   string     `json:"session_id"`
	RecoveredAt time.Time  `json:"recovered_at"`
	Decisions   []Decision `json:"decisions"`
	ReceiptID   string     `json:"receipt_id,omitempty"`
}

type Execution struct {
	ExecutionID      string    `json:"execution_id"`
	TaskID           string    `json:"task_id"`
	SessionID        string    `json:"session_id"`
	Engine           string    `json:"engine"`
	Resumable        bool      `json:"resumable"`
	ExternalThreadID string    `json:"external_thread_id,omitempty"`
	StartedAt        time.Time `json:"started_at"`
}

type Result struct {
	ResultID           string               `json:"result_id"`
	ExecutionID        string               `json:"execution_id"`
	TaskID             string               `json:"task_id"`
	SessionID          string               `json:"session_id"`
	Engine             string               `json:"engine"`
	ExternalThreadID   string               `json:"external_thread_id,omitempty"`
	Output             session.EngineResult `json:"output"`
	CompletedAt        time.Time            `json:"completed_at"`
	CanonicalCommitted bool                 `json:"canonical_committed"`
}

type ArtifactRecord struct {
	ArtifactID string `json:"artifact_id"`
	TaskID     string `json:"task_id"`
	SessionID  string `json:"session_id"`
	Name       string `json:"name"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
}
