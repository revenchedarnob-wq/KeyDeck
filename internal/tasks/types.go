package tasks

import "time"

type Status string

const (
	StatusWorking       Status = "working"
	StatusInputRequired Status = "input_required"
	StatusCompleted     Status = "completed"
	StatusFailed        Status = "failed"
	StatusCancelled     Status = "cancelled"
)

type CheckStatus string

const (
	CheckPending CheckStatus = "pending"
	CheckPassed  CheckStatus = "passed"
	CheckFailed  CheckStatus = "failed"
	CheckBlocked CheckStatus = "blocked"
)

type AcceptanceCheck struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Status      CheckStatus `json:"status"`
	Evidence    string      `json:"evidence,omitempty"`
	UpdatedAt   time.Time   `json:"updated_at,omitempty"`
}

type Contract struct {
	Goal             string            `json:"goal"`
	RequiredOutcomes []string          `json:"required_outcomes,omitempty"`
	ForbiddenScope   []string          `json:"forbidden_scope,omitempty"`
	Checks           []AcceptanceCheck `json:"checks"`
}

type Step struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operation_id,omitempty"`
	Tool        string    `json:"tool,omitempty"`
	Status      string    `json:"status"`
	Result      string    `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type State struct {
	Version       int             `json:"version"`
	TaskID        string          `json:"task_id"`
	SessionID     string          `json:"session_id"`
	Status        Status          `json:"status"`
	Contract      Contract        `json:"contract"`
	Steps         map[string]Step `json:"steps"`
	NeedsInput    string          `json:"needs_input,omitempty"`
	FailureReason string          `json:"failure_reason,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	LastSequence  uint64          `json:"last_sequence"`
}

type Progress struct {
	PassedChecks int     `json:"passed_checks"`
	TotalChecks  int     `json:"total_checks"`
	Percent      float64 `json:"percent"`
	Complete     bool    `json:"complete"`
}

func (s State) Progress() Progress {
	total := len(s.Contract.Checks)
	passed := 0
	for _, check := range s.Contract.Checks {
		if check.Status == CheckPassed {
			passed++
		}
	}
	percent := 0.0
	if total > 0 {
		percent = float64(passed) * 100 / float64(total)
	}
	return Progress{PassedChecks: passed, TotalChecks: total, Percent: percent, Complete: total > 0 && passed == total}
}
