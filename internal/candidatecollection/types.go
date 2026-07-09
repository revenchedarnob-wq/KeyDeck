package candidatecollection

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput               = errors.New("candidate collection: invalid input")
	ErrStaleCurrentState          = errors.New("candidate collection: current state validation failed")
	ErrIdentityConflict           = errors.New("candidate collection: execution/result identity conflict")
	ErrNotFound                   = errors.New("candidate collection: not found")
	ErrAssessmentRequired         = errors.New("candidate collection: reconciliation assessment required")
	ErrResolutionRequired         = errors.New("candidate collection: explicit resolution required")
	ErrDirectRawRecoveryForbidden = errors.New("candidate collection: direct raw engine-result recovery is forbidden")
	ErrAlreadyCommitted           = errors.New("candidate collection: already committed")
	ErrInvalidReviewerResolution  = errors.New("candidate collection: invalid reviewer resolution")
	ErrCandidateNotInAssessment   = errors.New("candidate collection: selected candidate is not in assessment")
	ErrMissingDecisiveEvidence    = errors.New("candidate collection: decisive evidence is required")
	ErrUnresolvedAssessment       = errors.New("candidate collection: assessment is unresolved")
	ErrStaleAssessment            = errors.New("candidate collection: assessment candidate set is stale")
	ErrScopeCommitted             = errors.New("candidate collection: scope is already canonically committed")
	ErrMalformedStoreEvent        = errors.New("candidate collection: malformed durable store event")
	ErrRecoveryPrepared           = errors.New("candidate collection: exact recovery operation is already prepared")
)

type AssessmentState string

const (
	AssessmentSingleVerified AssessmentState = "single_verified"
	AssessmentAgreement      AssessmentState = "agreement"
	AssessmentDisagreement   AssessmentState = "disagreement"
	AssessmentNeedsReview    AssessmentState = "needs_review"
	AssessmentResolved       AssessmentState = "resolved"
)

type CollectRequest struct {
	TaskID             string   `json:"task_id"`
	SessionID          string   `json:"session_id"`
	TaskSequence       uint64   `json:"task_sequence"`
	HandoffPackageID   string   `json:"handoff_package_id"`
	HandoffPackageSHA  string   `json:"handoff_package_sha256"`
	RouteDecisionID    string   `json:"route_decision_id"`
	RouteDecisionSHA   string   `json:"route_decision_sha256"`
	ExecutionID        string   `json:"execution_id"`
	ResultID           string   `json:"result_id"`
	EngineID           string   `json:"engine_id"`
	EngineIdentitySHA  string   `json:"engine_identity_sha256"`
	ResultSHA          string   `json:"result_sha256"`
	ResultPayloadSHA   string   `json:"result_payload_sha256"`
	ResultPayload      string   `json:"result_payload"`
	RuntimeEvidenceIDs []string `json:"runtime_evidence_ids,omitempty"`
}

type CandidateRecord struct {
	CandidateID        string   `json:"candidate_id"`
	CandidateSHA       string   `json:"candidate_sha256"`
	TaskID             string   `json:"task_id"`
	SessionID          string   `json:"session_id"`
	TaskSequence       uint64   `json:"task_sequence"`
	HandoffPackageID   string   `json:"handoff_package_id"`
	HandoffPackageSHA  string   `json:"handoff_package_sha256"`
	RouteDecisionID    string   `json:"route_decision_id"`
	RouteDecisionSHA   string   `json:"route_decision_sha256"`
	ExecutionID        string   `json:"execution_id"`
	ResultID           string   `json:"result_id"`
	EngineID           string   `json:"engine_id"`
	EngineIdentitySHA  string   `json:"engine_identity_sha256"`
	ResultSHA          string   `json:"result_sha256"`
	ResultPayloadSHA   string   `json:"result_payload_sha256"`
	ResultPayload      string   `json:"result_payload"`
	RuntimeEvidenceIDs []string `json:"runtime_evidence_ids,omitempty"`
	CollectedAtUTC     string   `json:"collected_at_utc"`
}

type CollectOutcome struct {
	Candidate CandidateRecord `json:"candidate"`
	Reused    bool            `json:"reused"`
}

type AssessmentRecord struct {
	AssessmentID        string          `json:"assessment_id"`
	AssessmentSHA       string          `json:"assessment_sha256"`
	TaskID              string          `json:"task_id"`
	HandoffPackageID    string          `json:"handoff_package_id"`
	CandidateIDs        []string        `json:"candidate_ids"`
	CandidateSetSHA     string          `json:"candidate_set_sha256"`
	State               AssessmentState `json:"state"`
	SelectedCandidateID string          `json:"selected_candidate_id,omitempty"`
	DecisiveEvidenceIDs []string        `json:"decisive_evidence_ids,omitempty"`
	CreatedAtUTC        string          `json:"created_at_utc"`
}

type ReviewRequest struct {
	AssessmentID          string   `json:"assessment_id"`
	AssessmentSHA         string   `json:"assessment_sha256"`
	TaskID                string   `json:"task_id"`
	HandoffPackageID      string   `json:"handoff_package_id"`
	CandidateIDs          []string `json:"candidate_ids"`
	RequiredEvidenceKinds []string `json:"required_evidence_kinds,omitempty"`
}

type ReviewerResolutionRequest struct {
	AssessmentID        string   `json:"assessment_id"`
	AssessmentSHA       string   `json:"assessment_sha256"`
	SelectedCandidateID string   `json:"selected_candidate_id"`
	ReviewerRef         string   `json:"reviewer_ref"`
	DecisiveEvidenceIDs []string `json:"decisive_evidence_ids"`
	Rationale           string   `json:"rationale"`
}

type ResolutionRecord struct {
	ResolutionID        string   `json:"resolution_id"`
	ResolutionSHA       string   `json:"resolution_sha256"`
	AssessmentID        string   `json:"assessment_id"`
	AssessmentSHA       string   `json:"assessment_sha256"`
	SelectedCandidateID string   `json:"selected_candidate_id"`
	ReviewerRef         string   `json:"reviewer_ref"`
	DecisiveEvidenceIDs []string `json:"decisive_evidence_ids"`
	Rationale           string   `json:"rationale"`
	CreatedAtUTC        string   `json:"created_at_utc"`
}

type ResolvedRecoveryInput struct {
	RecoveryOperationID  string   `json:"recovery_operation_id"`
	RecoveryOperationSHA string   `json:"recovery_operation_sha256"`
	TaskID               string   `json:"task_id"`
	SessionID            string   `json:"session_id"`
	TaskSequence         uint64   `json:"task_sequence"`
	HandoffPackageID     string   `json:"handoff_package_id"`
	HandoffPackageSHA    string   `json:"handoff_package_sha256"`
	RouteDecisionID      string   `json:"route_decision_id"`
	ExecutionID          string   `json:"execution_id"`
	ResultID             string   `json:"result_id"`
	EngineID             string   `json:"engine_id"`
	CandidateID          string   `json:"candidate_id"`
	CandidateSHA         string   `json:"candidate_sha256"`
	ResultSHA            string   `json:"result_sha256"`
	ResultPayloadSHA     string   `json:"result_payload_sha256"`
	ResultPayload        string   `json:"result_payload"`
	AssessmentID         string   `json:"assessment_id"`
	AssessmentSHA        string   `json:"assessment_sha256"`
	ResolutionID         string   `json:"resolution_id,omitempty"`
	ResolutionSHA        string   `json:"resolution_sha256,omitempty"`
	DecisiveEvidenceIDs  []string `json:"decisive_evidence_ids,omitempty"`
}

type RecoveryPreparationRecord struct {
	PreparationID        string `json:"preparation_id"`
	PreparationSHA       string `json:"preparation_sha256"`
	TaskID               string `json:"task_id"`
	HandoffPackageID     string `json:"handoff_package_id"`
	CandidateID          string `json:"candidate_id"`
	CandidateSHA         string `json:"candidate_sha256"`
	AssessmentID         string `json:"assessment_id"`
	AssessmentSHA        string `json:"assessment_sha256"`
	ResolutionID         string `json:"resolution_id,omitempty"`
	ResolutionSHA        string `json:"resolution_sha256,omitempty"`
	RecoveryOperationID  string `json:"recovery_operation_id"`
	RecoveryOperationSHA string `json:"recovery_operation_sha256"`
	CreatedAtUTC         string `json:"created_at_utc"`
}

type RecoveryCommit struct {
	CommitID     string `json:"commit_id"`
	CommitSHA    string `json:"commit_sha256"`
	CanonicalRef string `json:"canonical_ref"`
}

type ReceiptEvidence struct {
	ReconciliationState    AssessmentState `json:"reconciliation_state,omitempty"`
	StoreSHA               string          `json:"collection_store_sha256"`
	CollectionEventIDs     []string        `json:"collection_event_ids"`
	CandidateIDs           []string        `json:"candidate_ids"`
	CandidateSetSHA        string          `json:"candidate_set_sha256"`
	AssessmentID           string          `json:"assessment_id,omitempty"`
	AssessmentSHA          string          `json:"assessment_sha256,omitempty"`
	ResolutionID           string          `json:"resolution_id,omitempty"`
	ResolutionSHA          string          `json:"resolution_sha256,omitempty"`
	RuntimeEvidenceIDs     []string        `json:"runtime_evidence_ids,omitempty"`
	RecoveryPreparationID  string          `json:"recovery_preparation_id,omitempty"`
	RecoveryPreparationSHA string          `json:"recovery_preparation_sha256,omitempty"`
	RecoveryOperationID    string          `json:"recovery_operation_id,omitempty"`
	RecoveryOperationSHA   string          `json:"recovery_operation_sha256,omitempty"`
	RecoveryCommitID       string          `json:"recovery_commit_id,omitempty"`
	RecoveryCommitSHA      string          `json:"recovery_commit_sha256,omitempty"`
	CanonicalCommitRef     string          `json:"canonical_commit_ref,omitempty"`
}

// CurrentStateValidator is the production integration seam for the existing
// canonical task, handoff, route, runtime, and engine identity stores.
// Validation must happen before candidate persistence.
type CurrentStateValidator interface {
	ValidateCollectionInput(context.Context, CollectRequest) error
}

// Reconciler is the existing reconciliation boundary. The coordinator never
// establishes truth by candidate count or majority vote.
type Reconciler interface {
	Assess(context.Context, []CandidateRecord) (AssessmentRecord, error)
}

// RecoveryPort is deliberately resolved-candidate-only. There is no raw
// engine-result method on this interface.
type RecoveryPort interface {
	CommitResolvedCandidate(context.Context, ResolvedRecoveryInput) (RecoveryCommit, error)
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }
