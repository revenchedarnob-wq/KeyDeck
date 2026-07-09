package candidatecollection

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Coordinator struct {
	lifecycleMu         sync.Mutex
	store               *Store
	validator           CurrentStateValidator
	reconciler          Reconciler
	recovery            RecoveryPort
	clock               Clock
	afterRecoveryCommit func() error
}

func NewCoordinator(store *Store, validator CurrentStateValidator, reconciler Reconciler, recovery RecoveryPort) (*Coordinator, error) {
	if store == nil || validator == nil || reconciler == nil || recovery == nil {
		return nil, fmt.Errorf("%w: coordinator dependencies must be non-nil", ErrInvalidInput)
	}
	return &Coordinator{store: store, validator: validator, reconciler: reconciler, recovery: recovery, clock: realClock{}}, nil
}

func (c *Coordinator) WithClock(clock Clock) *Coordinator {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if clock != nil {
		c.clock = clock
	}
	return c
}

func (c *Coordinator) Collect(ctx context.Context, req CollectRequest) (CollectOutcome, error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if err := validateCollectRequest(req); err != nil {
		return CollectOutcome{}, err
	}
	if _, _, ok := c.store.CommittedCandidate(req.TaskID, req.HandoffPackageID); ok {
		return CollectOutcome{}, ErrScopeCommitted
	}
	if _, ok := c.store.RecoveryPreparationForScope(req.TaskID, req.HandoffPackageID); ok {
		return CollectOutcome{}, ErrRecoveryPrepared
	}
	// The exact current task/package/route/runtime/engine binding is checked
	// before any candidate is persisted or reuse event is appended.
	if err := c.validator.ValidateCollectionInput(ctx, req); err != nil {
		return CollectOutcome{}, fmt.Errorf("%w: %v", ErrStaleCurrentState, err)
	}
	candidate, err := candidateFromRequest(req, c.clock.Now().UTC().Format(timeLayout))
	if err != nil {
		return CollectOutcome{}, err
	}
	if existing, ok := c.store.FindByExecutionResult(req.ExecutionID, req.ResultID); ok {
		if !sameExecutionResultBinding(existing, candidate) {
			return CollectOutcome{}, ErrIdentityConflict
		}
		if _, err := c.store.AddReuse(existing, c.clock.Now().UTC().Format(timeLayout)); err != nil {
			return CollectOutcome{}, err
		}
		return CollectOutcome{Candidate: existing, Reused: true}, nil
	}
	if _, err := c.store.AddCandidate(candidate, c.clock.Now().UTC().Format(timeLayout)); err != nil {
		return CollectOutcome{}, err
	}
	return CollectOutcome{Candidate: candidate, Reused: false}, nil
}

const timeLayout = "2006-01-02T15:04:05.000000000Z"

func sameExecutionResultBinding(a, b CandidateRecord) bool {
	return a.CandidateSHA == b.CandidateSHA &&
		a.TaskID == b.TaskID && a.SessionID == b.SessionID && a.TaskSequence == b.TaskSequence &&
		a.HandoffPackageID == b.HandoffPackageID && a.HandoffPackageSHA == b.HandoffPackageSHA &&
		a.RouteDecisionID == b.RouteDecisionID && a.RouteDecisionSHA == b.RouteDecisionSHA &&
		a.ExecutionID == b.ExecutionID && a.ResultID == b.ResultID && a.EngineID == b.EngineID &&
		a.EngineIdentitySHA == b.EngineIdentitySHA && a.ResultSHA == b.ResultSHA && a.ResultPayloadSHA == b.ResultPayloadSHA && a.ResultPayload == b.ResultPayload
}

func (c *Coordinator) Assess(ctx context.Context, taskID, packageID string) (AssessmentRecord, error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if _, _, ok := c.store.CommittedCandidate(taskID, packageID); ok {
		return AssessmentRecord{}, ErrScopeCommitted
	}
	if _, ok := c.store.RecoveryPreparationForScope(taskID, packageID); ok {
		return AssessmentRecord{}, ErrRecoveryPrepared
	}
	candidates := c.store.CandidatesForScope(taskID, packageID)
	if len(candidates) == 0 {
		return AssessmentRecord{}, ErrNotFound
	}
	assessed, err := c.reconciler.Assess(ctx, append([]CandidateRecord(nil), candidates...))
	if err != nil {
		return AssessmentRecord{}, err
	}
	setSHA, ids, err := candidateSetSHA(candidates)
	if err != nil {
		return AssessmentRecord{}, err
	}
	if assessed.TaskID != "" && assessed.TaskID != taskID {
		return AssessmentRecord{}, fmt.Errorf("%w: reconciler task mismatch", ErrInvalidInput)
	}
	if assessed.HandoffPackageID != "" && assessed.HandoffPackageID != packageID {
		return AssessmentRecord{}, fmt.Errorf("%w: reconciler package mismatch", ErrInvalidInput)
	}
	if assessed.State != AssessmentSingleVerified && assessed.State != AssessmentAgreement && assessed.State != AssessmentDisagreement && assessed.State != AssessmentNeedsReview && assessed.State != AssessmentResolved {
		return AssessmentRecord{}, fmt.Errorf("%w: invalid assessment state %q", ErrInvalidInput, assessed.State)
	}
	assessed.TaskID = taskID
	assessed.HandoffPackageID = packageID
	assessed.CandidateIDs = ids
	assessed.CandidateSetSHA = setSHA
	assessed.DecisiveEvidenceIDs = uniqueSorted(assessed.DecisiveEvidenceIDs)
	assessed.CreatedAtUTC = c.clock.Now().UTC().Format(timeLayout)

	if assessed.SelectedCandidateID != "" && !contains(ids, assessed.SelectedCandidateID) {
		return AssessmentRecord{}, ErrCandidateNotInAssessment
	}
	if (assessed.State == AssessmentSingleVerified || assessed.State == AssessmentAgreement || assessed.State == AssessmentResolved) && assessed.SelectedCandidateID == "" {
		return AssessmentRecord{}, fmt.Errorf("%w: selected candidate required for state %s", ErrInvalidInput, assessed.State)
	}
	// Disagreement can never be auto-resolved by candidate count.
	if assessed.State == AssessmentDisagreement || assessed.State == AssessmentNeedsReview {
		assessed.SelectedCandidateID = ""
	}

	identity := struct {
		TaskID              string          `json:"task_id"`
		HandoffPackageID    string          `json:"handoff_package_id"`
		CandidateIDs        []string        `json:"candidate_ids"`
		CandidateSetSHA     string          `json:"candidate_set_sha256"`
		State               AssessmentState `json:"state"`
		SelectedCandidateID string          `json:"selected_candidate_id,omitempty"`
		DecisiveEvidenceIDs []string        `json:"decisive_evidence_ids,omitempty"`
	}{assessed.TaskID, assessed.HandoffPackageID, assessed.CandidateIDs, assessed.CandidateSetSHA, assessed.State, assessed.SelectedCandidateID, assessed.DecisiveEvidenceIDs}
	sha, err := canonicalSHA(identity)
	if err != nil {
		return AssessmentRecord{}, err
	}
	assessed.AssessmentSHA = sha
	assessed.AssessmentID = prefixedID("assessment", sha)
	if _, err := c.store.AddAssessment(assessed, assessed.CreatedAtUTC); err != nil {
		return AssessmentRecord{}, err
	}
	return assessed, nil
}

func (c *Coordinator) ensureAssessmentCurrent(assessment AssessmentRecord) error {
	candidates := c.store.CandidatesForScope(assessment.TaskID, assessment.HandoffPackageID)
	if len(candidates) == 0 {
		return ErrNotFound
	}
	setSHA, _, err := candidateSetSHA(candidates)
	if err != nil {
		return err
	}
	if setSHA != assessment.CandidateSetSHA {
		return ErrStaleAssessment
	}
	return nil
}

func (c *Coordinator) ReviewRequest(taskID, packageID string) (ReviewRequest, error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if _, _, ok := c.store.CommittedCandidate(taskID, packageID); ok {
		return ReviewRequest{}, ErrScopeCommitted
	}
	if _, ok := c.store.RecoveryPreparationForScope(taskID, packageID); ok {
		return ReviewRequest{}, ErrRecoveryPrepared
	}
	assessment, ok := c.store.LatestAssessment(taskID, packageID)
	if !ok {
		return ReviewRequest{}, ErrAssessmentRequired
	}
	if err := c.ensureAssessmentCurrent(assessment); err != nil {
		return ReviewRequest{}, err
	}
	if assessment.State != AssessmentDisagreement && assessment.State != AssessmentNeedsReview {
		return ReviewRequest{}, ErrResolutionRequired
	}
	return ReviewRequest{
		AssessmentID:          assessment.AssessmentID,
		AssessmentSHA:         assessment.AssessmentSHA,
		TaskID:                taskID,
		HandoffPackageID:      packageID,
		CandidateIDs:          append([]string(nil), assessment.CandidateIDs...),
		RequiredEvidenceKinds: []string{"exact_decisive_evidence_or_explicit_reviewer_resolution"},
	}, nil
}

func (c *Coordinator) ResolveReview(req ReviewerResolutionRequest) (ResolutionRecord, error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if strings.TrimSpace(req.AssessmentID) == "" || strings.TrimSpace(req.AssessmentSHA) == "" || strings.TrimSpace(req.SelectedCandidateID) == "" || strings.TrimSpace(req.ReviewerRef) == "" || strings.TrimSpace(req.Rationale) == "" {
		return ResolutionRecord{}, ErrInvalidReviewerResolution
	}
	assessment, ok := c.store.Assessment(req.AssessmentID)
	if !ok || assessment.AssessmentSHA != req.AssessmentSHA {
		return ResolutionRecord{}, ErrInvalidReviewerResolution
	}
	if _, _, committed := c.store.CommittedCandidate(assessment.TaskID, assessment.HandoffPackageID); committed {
		return ResolutionRecord{}, ErrScopeCommitted
	}
	if _, prepared := c.store.RecoveryPreparationForScope(assessment.TaskID, assessment.HandoffPackageID); prepared {
		return ResolutionRecord{}, ErrRecoveryPrepared
	}
	if err := c.ensureAssessmentCurrent(assessment); err != nil {
		return ResolutionRecord{}, err
	}
	if assessment.State != AssessmentDisagreement && assessment.State != AssessmentNeedsReview {
		return ResolutionRecord{}, ErrInvalidReviewerResolution
	}
	if !contains(assessment.CandidateIDs, req.SelectedCandidateID) {
		return ResolutionRecord{}, ErrCandidateNotInAssessment
	}
	evidence := uniqueSorted(req.DecisiveEvidenceIDs)
	if len(evidence) == 0 {
		return ResolutionRecord{}, ErrMissingDecisiveEvidence
	}
	identity := struct {
		AssessmentID        string   `json:"assessment_id"`
		AssessmentSHA       string   `json:"assessment_sha256"`
		SelectedCandidateID string   `json:"selected_candidate_id"`
		ReviewerRef         string   `json:"reviewer_ref"`
		DecisiveEvidenceIDs []string `json:"decisive_evidence_ids"`
		Rationale           string   `json:"rationale"`
	}{req.AssessmentID, req.AssessmentSHA, req.SelectedCandidateID, req.ReviewerRef, evidence, req.Rationale}
	sha, err := canonicalSHA(identity)
	if err != nil {
		return ResolutionRecord{}, err
	}
	resolution := ResolutionRecord{
		ResolutionID: prefixedID("resolution", sha), ResolutionSHA: sha,
		AssessmentID: req.AssessmentID, AssessmentSHA: req.AssessmentSHA,
		SelectedCandidateID: req.SelectedCandidateID, ReviewerRef: req.ReviewerRef,
		DecisiveEvidenceIDs: evidence, Rationale: req.Rationale,
		CreatedAtUTC: c.clock.Now().UTC().Format(timeLayout),
	}
	if existing, ok := c.store.ResolutionForAssessment(req.AssessmentID); ok {
		if existing.ResolutionSHA != resolution.ResolutionSHA {
			return ResolutionRecord{}, ErrIdentityConflict
		}
		return existing, nil
	}
	if _, err := c.store.AddResolution(resolution, assessment.TaskID, assessment.HandoffPackageID, resolution.CreatedAtUTC); err != nil {
		return ResolutionRecord{}, err
	}
	return resolution, nil
}

func (c *Coordinator) resolvedInputForPreparation(preparation RecoveryPreparationRecord) (ResolvedRecoveryInput, error) {
	candidate, ok := c.store.Candidate(preparation.CandidateID)
	if !ok || candidate.CandidateSHA != preparation.CandidateSHA {
		return ResolvedRecoveryInput{}, ErrNotFound
	}
	assessment, ok := c.store.Assessment(preparation.AssessmentID)
	if !ok || assessment.AssessmentSHA != preparation.AssessmentSHA {
		return ResolvedRecoveryInput{}, ErrAssessmentRequired
	}
	if err := c.ensureAssessmentCurrent(assessment); err != nil {
		return ResolvedRecoveryInput{}, err
	}
	var resolution *ResolutionRecord
	if preparation.ResolutionID != "" {
		r, ok := c.store.ResolutionForAssessment(preparation.AssessmentID)
		if !ok || r.ResolutionID != preparation.ResolutionID || r.ResolutionSHA != preparation.ResolutionSHA {
			return ResolvedRecoveryInput{}, ErrResolutionRequired
		}
		resolution = &r
	}
	input, err := resolvedRecoveryInputFromRecords(candidate, assessment, resolution)
	if err != nil {
		return ResolvedRecoveryInput{}, err
	}
	if input.RecoveryOperationID != preparation.RecoveryOperationID || input.RecoveryOperationSHA != preparation.RecoveryOperationSHA {
		return ResolvedRecoveryInput{}, fmt.Errorf("%w: prepared recovery operation mismatch", ErrIdentityConflict)
	}
	return input, nil
}

func (c *Coordinator) currentResolvedInput(taskID, packageID string) (ResolvedRecoveryInput, error) {
	assessment, ok := c.store.LatestAssessment(taskID, packageID)
	if !ok {
		return ResolvedRecoveryInput{}, ErrAssessmentRequired
	}
	if err := c.ensureAssessmentCurrent(assessment); err != nil {
		return ResolvedRecoveryInput{}, err
	}

	selectedID := assessment.SelectedCandidateID
	var resolution *ResolutionRecord
	switch assessment.State {
	case AssessmentSingleVerified, AssessmentAgreement, AssessmentResolved:
		if selectedID == "" {
			return ResolvedRecoveryInput{}, ErrUnresolvedAssessment
		}
	case AssessmentDisagreement, AssessmentNeedsReview:
		r, ok := c.store.ResolutionForAssessment(assessment.AssessmentID)
		if !ok {
			return ResolvedRecoveryInput{}, ErrResolutionRequired
		}
		selectedID = r.SelectedCandidateID
		resolution = &r
	default:
		return ResolvedRecoveryInput{}, ErrUnresolvedAssessment
	}
	candidate, ok := c.store.Candidate(selectedID)
	if !ok {
		return ResolvedRecoveryInput{}, ErrNotFound
	}
	return resolvedRecoveryInputFromRecords(candidate, assessment, resolution)
}

func (c *Coordinator) CommitResolved(ctx context.Context, taskID, packageID string) (RecoveryCommit, error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if _, commit, ok := c.store.CommittedCandidate(taskID, packageID); ok {
		return commit, nil
	}

	preparation, prepared := c.store.RecoveryPreparationForScope(taskID, packageID)
	var input ResolvedRecoveryInput
	var err error
	if prepared {
		input, err = c.resolvedInputForPreparation(preparation)
		if err != nil {
			return RecoveryCommit{}, err
		}
	} else {
		input, err = c.currentResolvedInput(taskID, packageID)
		if err != nil {
			return RecoveryCommit{}, err
		}
		preparation, err = recoveryPreparationFromInput(input, c.clock.Now().UTC().Format(timeLayout))
		if err != nil {
			return RecoveryCommit{}, err
		}
		if _, err := c.store.AddRecoveryPreparation(preparation, preparation.CreatedAtUTC); err != nil {
			return RecoveryCommit{}, err
		}
	}

	commit, err := c.recovery.CommitResolvedCandidate(ctx, input)
	if err != nil {
		return RecoveryCommit{}, err
	}
	if strings.TrimSpace(commit.CommitID) == "" || strings.TrimSpace(commit.CommitSHA) == "" || strings.TrimSpace(commit.CanonicalRef) == "" {
		return RecoveryCommit{}, fmt.Errorf("%w: recovery returned incomplete commit evidence", ErrInvalidInput)
	}
	if c.afterRecoveryCommit != nil {
		if err := c.afterRecoveryCommit(); err != nil {
			return RecoveryCommit{}, err
		}
	}
	if _, err := c.store.AddRecoveryCommit(taskID, packageID, preparation.CandidateID, preparation.PreparationID, preparation.AssessmentID, preparation.ResolutionID, commit, c.clock.Now().UTC().Format(timeLayout)); err != nil {
		return RecoveryCommit{}, err
	}
	return commit, nil
}

func (c *Coordinator) ReceiptEvidence(taskID, packageID string) (ReceiptEvidence, error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	return c.store.Evidence(taskID, packageID)
}

// RejectRawRecovery is an explicit integration guard for callers migrating from
// the pre-coordinator path. It exists to make accidental direct handoff fail
// loudly while the production candidate-collection path is active.
func (c *Coordinator) RejectRawRecovery() error { return ErrDirectRawRecoveryForbidden }

func contains(values []string, target string) bool {
	i := sort.SearchStrings(values, target)
	return i < len(values) && values[i] == target
}

// EffectiveState reports the current user-visible reconciliation state without
// rewriting the immutable assessment record. An explicit evidence-backed
// reviewer resolution promotes disagreement/needs_review to resolved.
func (c *Coordinator) EffectiveState(taskID, packageID string) (AssessmentState, string, error) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	assessment, ok := c.store.LatestAssessment(taskID, packageID)
	if !ok {
		return "", "", ErrAssessmentRequired
	}
	if err := c.ensureAssessmentCurrent(assessment); err != nil {
		return "", "", err
	}
	if assessment.State == AssessmentDisagreement || assessment.State == AssessmentNeedsReview {
		if resolution, ok := c.store.ResolutionForAssessment(assessment.AssessmentID); ok {
			return AssessmentResolved, resolution.SelectedCandidateID, nil
		}
	}
	return assessment.State, assessment.SelectedCandidateID, nil
}
