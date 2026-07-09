package candidatecollection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

func canonicalSHA(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func prefixedID(prefix, sha string) string {
	if len(sha) > 20 {
		sha = sha[:20]
	}
	return prefix + "-" + sha
}

func sha256HexString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type candidateIdentity struct {
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
	RuntimeEvidenceIDs []string `json:"runtime_evidence_ids,omitempty"`
}

func candidateIdentityFromRequest(req CollectRequest) candidateIdentity {
	ids := append([]string(nil), req.RuntimeEvidenceIDs...)
	sort.Strings(ids)
	return candidateIdentity{
		TaskID: req.TaskID, SessionID: req.SessionID, TaskSequence: req.TaskSequence,
		HandoffPackageID: req.HandoffPackageID, HandoffPackageSHA: req.HandoffPackageSHA,
		RouteDecisionID: req.RouteDecisionID, RouteDecisionSHA: req.RouteDecisionSHA,
		ExecutionID: req.ExecutionID, ResultID: req.ResultID, EngineID: req.EngineID,
		EngineIdentitySHA: req.EngineIdentitySHA, ResultSHA: req.ResultSHA, ResultPayloadSHA: req.ResultPayloadSHA,
		RuntimeEvidenceIDs: ids,
	}
}

func candidateIdentityFromRecord(c CandidateRecord) candidateIdentity {
	return candidateIdentityFromRequest(CollectRequest{
		TaskID: c.TaskID, SessionID: c.SessionID, TaskSequence: c.TaskSequence,
		HandoffPackageID: c.HandoffPackageID, HandoffPackageSHA: c.HandoffPackageSHA,
		RouteDecisionID: c.RouteDecisionID, RouteDecisionSHA: c.RouteDecisionSHA,
		ExecutionID: c.ExecutionID, ResultID: c.ResultID, EngineID: c.EngineID,
		EngineIdentitySHA: c.EngineIdentitySHA, ResultSHA: c.ResultSHA, ResultPayloadSHA: c.ResultPayloadSHA,
		RuntimeEvidenceIDs: c.RuntimeEvidenceIDs,
	})
}

func candidateFromRequest(req CollectRequest, now string) (CandidateRecord, error) {
	sha, err := canonicalSHA(candidateIdentityFromRequest(req))
	if err != nil {
		return CandidateRecord{}, err
	}
	return CandidateRecord{
		CandidateID: prefixedID("candidate", sha), CandidateSHA: sha,
		TaskID: req.TaskID, SessionID: req.SessionID, TaskSequence: req.TaskSequence,
		HandoffPackageID: req.HandoffPackageID, HandoffPackageSHA: req.HandoffPackageSHA,
		RouteDecisionID: req.RouteDecisionID, RouteDecisionSHA: req.RouteDecisionSHA,
		ExecutionID: req.ExecutionID, ResultID: req.ResultID, EngineID: req.EngineID,
		EngineIdentitySHA: req.EngineIdentitySHA, ResultSHA: req.ResultSHA, ResultPayloadSHA: req.ResultPayloadSHA,
		ResultPayload: req.ResultPayload, RuntimeEvidenceIDs: append([]string(nil), req.RuntimeEvidenceIDs...),
		CollectedAtUTC: now,
	}, nil
}

func validateCandidateRecord(c CandidateRecord) error {
	if err := validateCollectRequest(CollectRequest{
		TaskID: c.TaskID, SessionID: c.SessionID, TaskSequence: c.TaskSequence,
		HandoffPackageID: c.HandoffPackageID, HandoffPackageSHA: c.HandoffPackageSHA,
		RouteDecisionID: c.RouteDecisionID, RouteDecisionSHA: c.RouteDecisionSHA,
		ExecutionID: c.ExecutionID, ResultID: c.ResultID, EngineID: c.EngineID,
		EngineIdentitySHA: c.EngineIdentitySHA, ResultSHA: c.ResultSHA, ResultPayloadSHA: c.ResultPayloadSHA, ResultPayload: c.ResultPayload,
	}); err != nil {
		return fmt.Errorf("%w: invalid candidate record: %v", ErrMalformedStoreEvent, err)
	}
	if strings.TrimSpace(c.CandidateID) == "" || strings.TrimSpace(c.CandidateSHA) == "" || strings.TrimSpace(c.CollectedAtUTC) == "" {
		return fmt.Errorf("%w: incomplete candidate record", ErrMalformedStoreEvent)
	}
	sha, err := canonicalSHA(candidateIdentityFromRecord(c))
	if err != nil {
		return err
	}
	if c.CandidateSHA != sha || c.CandidateID != prefixedID("candidate", sha) {
		return fmt.Errorf("%w: candidate identity mismatch", ErrMalformedStoreEvent)
	}
	return nil
}

func assessmentIdentitySHA(a AssessmentRecord) (string, error) {
	identity := struct {
		TaskID              string          `json:"task_id"`
		HandoffPackageID    string          `json:"handoff_package_id"`
		CandidateIDs        []string        `json:"candidate_ids"`
		CandidateSetSHA     string          `json:"candidate_set_sha256"`
		State               AssessmentState `json:"state"`
		SelectedCandidateID string          `json:"selected_candidate_id,omitempty"`
		DecisiveEvidenceIDs []string        `json:"decisive_evidence_ids,omitempty"`
	}{a.TaskID, a.HandoffPackageID, a.CandidateIDs, a.CandidateSetSHA, a.State, a.SelectedCandidateID, a.DecisiveEvidenceIDs}
	return canonicalSHA(identity)
}

func validateAssessmentRecord(a AssessmentRecord) error {
	if strings.TrimSpace(a.AssessmentID) == "" || strings.TrimSpace(a.AssessmentSHA) == "" || strings.TrimSpace(a.TaskID) == "" || strings.TrimSpace(a.HandoffPackageID) == "" || strings.TrimSpace(a.CandidateSetSHA) == "" || strings.TrimSpace(a.CreatedAtUTC) == "" {
		return fmt.Errorf("%w: incomplete assessment record", ErrMalformedStoreEvent)
	}
	if len(a.CandidateIDs) == 0 || !slices.Equal(a.CandidateIDs, uniqueSorted(a.CandidateIDs)) {
		return fmt.Errorf("%w: assessment candidate IDs must be unique and sorted", ErrMalformedStoreEvent)
	}
	if !slices.Equal(a.DecisiveEvidenceIDs, uniqueSorted(a.DecisiveEvidenceIDs)) {
		return fmt.Errorf("%w: assessment evidence IDs must be unique and sorted", ErrMalformedStoreEvent)
	}
	switch a.State {
	case AssessmentSingleVerified, AssessmentAgreement, AssessmentResolved:
		if strings.TrimSpace(a.SelectedCandidateID) == "" || !contains(a.CandidateIDs, a.SelectedCandidateID) {
			return fmt.Errorf("%w: selected candidate missing from resolved assessment", ErrMalformedStoreEvent)
		}
	case AssessmentDisagreement, AssessmentNeedsReview:
		if a.SelectedCandidateID != "" {
			return fmt.Errorf("%w: unresolved assessment selected a candidate", ErrMalformedStoreEvent)
		}
	default:
		return fmt.Errorf("%w: invalid assessment state", ErrMalformedStoreEvent)
	}
	sha, err := assessmentIdentitySHA(a)
	if err != nil {
		return err
	}
	if a.AssessmentSHA != sha || a.AssessmentID != prefixedID("assessment", sha) {
		return fmt.Errorf("%w: assessment identity mismatch", ErrMalformedStoreEvent)
	}
	return nil
}

func resolutionIdentitySHA(r ResolutionRecord) (string, error) {
	identity := struct {
		AssessmentID        string   `json:"assessment_id"`
		AssessmentSHA       string   `json:"assessment_sha256"`
		SelectedCandidateID string   `json:"selected_candidate_id"`
		ReviewerRef         string   `json:"reviewer_ref"`
		DecisiveEvidenceIDs []string `json:"decisive_evidence_ids"`
		Rationale           string   `json:"rationale"`
	}{r.AssessmentID, r.AssessmentSHA, r.SelectedCandidateID, r.ReviewerRef, r.DecisiveEvidenceIDs, r.Rationale}
	return canonicalSHA(identity)
}

func validateResolutionRecord(r ResolutionRecord) error {
	if strings.TrimSpace(r.ResolutionID) == "" || strings.TrimSpace(r.ResolutionSHA) == "" || strings.TrimSpace(r.AssessmentID) == "" || strings.TrimSpace(r.AssessmentSHA) == "" || strings.TrimSpace(r.SelectedCandidateID) == "" || strings.TrimSpace(r.ReviewerRef) == "" || strings.TrimSpace(r.Rationale) == "" || strings.TrimSpace(r.CreatedAtUTC) == "" {
		return fmt.Errorf("%w: incomplete reviewer resolution", ErrMalformedStoreEvent)
	}
	if len(r.DecisiveEvidenceIDs) == 0 || !slices.Equal(r.DecisiveEvidenceIDs, uniqueSorted(r.DecisiveEvidenceIDs)) {
		return fmt.Errorf("%w: resolution decisive evidence must be unique, sorted, and non-empty", ErrMalformedStoreEvent)
	}
	sha, err := resolutionIdentitySHA(r)
	if err != nil {
		return err
	}
	if r.ResolutionSHA != sha || r.ResolutionID != prefixedID("resolution", sha) {
		return fmt.Errorf("%w: resolution identity mismatch", ErrMalformedStoreEvent)
	}
	return nil
}

func validateRecoveryCommit(c RecoveryCommit) error {
	if strings.TrimSpace(c.CommitID) == "" || strings.TrimSpace(c.CommitSHA) == "" || strings.TrimSpace(c.CanonicalRef) == "" {
		return fmt.Errorf("%w: incomplete recovery commit", ErrMalformedStoreEvent)
	}
	return nil
}

func validateCollectRequest(req CollectRequest) error {
	fields := map[string]string{
		"task_id": req.TaskID, "session_id": req.SessionID, "handoff_package_id": req.HandoffPackageID,
		"handoff_package_sha256": req.HandoffPackageSHA, "route_decision_id": req.RouteDecisionID,
		"route_decision_sha256": req.RouteDecisionSHA, "execution_id": req.ExecutionID,
		"result_id": req.ResultID, "engine_id": req.EngineID, "engine_identity_sha256": req.EngineIdentitySHA,
		"result_sha256": req.ResultSHA, "result_payload_sha256": req.ResultPayloadSHA,
	}
	for name, value := range fields {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: missing %s", ErrInvalidInput, name)
		}
	}
	if req.TaskSequence == 0 {
		return fmt.Errorf("%w: task_sequence must be non-zero", ErrInvalidInput)
	}
	payloadSHA := sha256HexString(req.ResultPayload)
	if payloadSHA != req.ResultPayloadSHA {
		return fmt.Errorf("%w: result payload SHA-256 mismatch", ErrInvalidInput)
	}
	return nil
}

type recoveryOperationIdentity struct {
	TaskID              string   `json:"task_id"`
	SessionID           string   `json:"session_id"`
	TaskSequence        uint64   `json:"task_sequence"`
	HandoffPackageID    string   `json:"handoff_package_id"`
	HandoffPackageSHA   string   `json:"handoff_package_sha256"`
	RouteDecisionID     string   `json:"route_decision_id"`
	ExecutionID         string   `json:"execution_id"`
	ResultID            string   `json:"result_id"`
	EngineID            string   `json:"engine_id"`
	CandidateID         string   `json:"candidate_id"`
	CandidateSHA        string   `json:"candidate_sha256"`
	ResultSHA           string   `json:"result_sha256"`
	ResultPayloadSHA    string   `json:"result_payload_sha256"`
	ResultPayload       string   `json:"result_payload"`
	AssessmentID        string   `json:"assessment_id"`
	AssessmentSHA       string   `json:"assessment_sha256"`
	ResolutionID        string   `json:"resolution_id,omitempty"`
	ResolutionSHA       string   `json:"resolution_sha256,omitempty"`
	DecisiveEvidenceIDs []string `json:"decisive_evidence_ids,omitempty"`
}

func resolvedRecoveryInputFromRecords(candidate CandidateRecord, assessment AssessmentRecord, resolution *ResolutionRecord) (ResolvedRecoveryInput, error) {
	if candidate.TaskID != assessment.TaskID || candidate.HandoffPackageID != assessment.HandoffPackageID {
		return ResolvedRecoveryInput{}, fmt.Errorf("%w: candidate and assessment scope mismatch", ErrMalformedStoreEvent)
	}
	selectedID := assessment.SelectedCandidateID
	evidence := append([]string(nil), assessment.DecisiveEvidenceIDs...)
	resolutionID, resolutionSHA := "", ""

	switch assessment.State {
	case AssessmentSingleVerified, AssessmentAgreement, AssessmentResolved:
		if resolution != nil || selectedID == "" || selectedID != candidate.CandidateID {
			return ResolvedRecoveryInput{}, fmt.Errorf("%w: assessment-selected recovery candidate mismatch", ErrMalformedStoreEvent)
		}
	case AssessmentDisagreement, AssessmentNeedsReview:
		if resolution == nil || resolution.AssessmentID != assessment.AssessmentID || resolution.AssessmentSHA != assessment.AssessmentSHA || resolution.SelectedCandidateID != candidate.CandidateID {
			return ResolvedRecoveryInput{}, fmt.Errorf("%w: reviewer resolution does not select recovery candidate", ErrMalformedStoreEvent)
		}
		selectedID = resolution.SelectedCandidateID
		evidence = append(evidence, resolution.DecisiveEvidenceIDs...)
		resolutionID, resolutionSHA = resolution.ResolutionID, resolution.ResolutionSHA
	default:
		return ResolvedRecoveryInput{}, ErrUnresolvedAssessment
	}
	if selectedID != candidate.CandidateID {
		return ResolvedRecoveryInput{}, ErrCandidateNotInAssessment
	}
	return bindRecoveryOperation(ResolvedRecoveryInput{
		TaskID: candidate.TaskID, SessionID: candidate.SessionID, TaskSequence: candidate.TaskSequence,
		HandoffPackageID: candidate.HandoffPackageID, HandoffPackageSHA: candidate.HandoffPackageSHA,
		RouteDecisionID: candidate.RouteDecisionID, ExecutionID: candidate.ExecutionID, ResultID: candidate.ResultID,
		EngineID: candidate.EngineID, CandidateID: candidate.CandidateID, CandidateSHA: candidate.CandidateSHA,
		ResultSHA: candidate.ResultSHA, ResultPayloadSHA: candidate.ResultPayloadSHA, ResultPayload: candidate.ResultPayload,
		AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA,
		ResolutionID: resolutionID, ResolutionSHA: resolutionSHA,
		DecisiveEvidenceIDs: uniqueSorted(evidence),
	})
}

func recoveryOperationIdentityFromInput(in ResolvedRecoveryInput) recoveryOperationIdentity {
	return recoveryOperationIdentity{
		TaskID: in.TaskID, SessionID: in.SessionID, TaskSequence: in.TaskSequence,
		HandoffPackageID: in.HandoffPackageID, HandoffPackageSHA: in.HandoffPackageSHA,
		RouteDecisionID: in.RouteDecisionID, ExecutionID: in.ExecutionID, ResultID: in.ResultID,
		EngineID: in.EngineID, CandidateID: in.CandidateID, CandidateSHA: in.CandidateSHA,
		ResultSHA: in.ResultSHA, ResultPayloadSHA: in.ResultPayloadSHA, ResultPayload: in.ResultPayload,
		AssessmentID: in.AssessmentID, AssessmentSHA: in.AssessmentSHA,
		ResolutionID: in.ResolutionID, ResolutionSHA: in.ResolutionSHA,
		DecisiveEvidenceIDs: uniqueSorted(in.DecisiveEvidenceIDs),
	}
}

func bindRecoveryOperation(in ResolvedRecoveryInput) (ResolvedRecoveryInput, error) {
	in.DecisiveEvidenceIDs = uniqueSorted(in.DecisiveEvidenceIDs)
	sha, err := canonicalSHA(recoveryOperationIdentityFromInput(in))
	if err != nil {
		return ResolvedRecoveryInput{}, err
	}
	in.RecoveryOperationSHA = sha
	in.RecoveryOperationID = prefixedID("recovery-operation", sha)
	return in, nil
}

func validateResolvedRecoveryInput(in ResolvedRecoveryInput) error {
	if strings.TrimSpace(in.RecoveryOperationID) == "" || strings.TrimSpace(in.RecoveryOperationSHA) == "" {
		return fmt.Errorf("%w: missing recovery operation identity", ErrMalformedStoreEvent)
	}
	if sha256HexString(in.ResultPayload) != in.ResultPayloadSHA {
		return fmt.Errorf("%w: recovery input result payload SHA-256 mismatch", ErrMalformedStoreEvent)
	}
	bound, err := bindRecoveryOperation(in)
	if err != nil {
		return err
	}
	if bound.RecoveryOperationID != in.RecoveryOperationID || bound.RecoveryOperationSHA != in.RecoveryOperationSHA {
		return fmt.Errorf("%w: recovery operation identity mismatch", ErrMalformedStoreEvent)
	}
	return nil
}

func recoveryPreparationFromInput(in ResolvedRecoveryInput, now string) (RecoveryPreparationRecord, error) {
	if err := validateResolvedRecoveryInput(in); err != nil {
		return RecoveryPreparationRecord{}, err
	}
	identity := struct {
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
	}{in.TaskID, in.HandoffPackageID, in.CandidateID, in.CandidateSHA, in.AssessmentID, in.AssessmentSHA, in.ResolutionID, in.ResolutionSHA, in.RecoveryOperationID, in.RecoveryOperationSHA}
	sha, err := canonicalSHA(identity)
	if err != nil {
		return RecoveryPreparationRecord{}, err
	}
	return RecoveryPreparationRecord{
		PreparationID: prefixedID("recovery-preparation", sha), PreparationSHA: sha,
		TaskID: in.TaskID, HandoffPackageID: in.HandoffPackageID,
		CandidateID: in.CandidateID, CandidateSHA: in.CandidateSHA,
		AssessmentID: in.AssessmentID, AssessmentSHA: in.AssessmentSHA,
		ResolutionID: in.ResolutionID, ResolutionSHA: in.ResolutionSHA,
		RecoveryOperationID: in.RecoveryOperationID, RecoveryOperationSHA: in.RecoveryOperationSHA,
		CreatedAtUTC: now,
	}, nil
}

func validateRecoveryPreparation(p RecoveryPreparationRecord) error {
	if strings.TrimSpace(p.PreparationID) == "" || strings.TrimSpace(p.PreparationSHA) == "" ||
		strings.TrimSpace(p.TaskID) == "" || strings.TrimSpace(p.HandoffPackageID) == "" ||
		strings.TrimSpace(p.CandidateID) == "" || strings.TrimSpace(p.CandidateSHA) == "" ||
		strings.TrimSpace(p.AssessmentID) == "" || strings.TrimSpace(p.AssessmentSHA) == "" ||
		strings.TrimSpace(p.RecoveryOperationID) == "" || strings.TrimSpace(p.RecoveryOperationSHA) == "" ||
		strings.TrimSpace(p.CreatedAtUTC) == "" {
		return fmt.Errorf("%w: incomplete recovery preparation", ErrMalformedStoreEvent)
	}
	identity := struct {
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
	}{p.TaskID, p.HandoffPackageID, p.CandidateID, p.CandidateSHA, p.AssessmentID, p.AssessmentSHA, p.ResolutionID, p.ResolutionSHA, p.RecoveryOperationID, p.RecoveryOperationSHA}
	sha, err := canonicalSHA(identity)
	if err != nil {
		return err
	}
	if p.PreparationSHA != sha || p.PreparationID != prefixedID("recovery-preparation", sha) {
		return fmt.Errorf("%w: recovery preparation identity mismatch", ErrMalformedStoreEvent)
	}
	return nil
}

func candidateSetSHA(candidates []CandidateRecord) (string, []string, error) {
	ids := make([]string, 0, len(candidates))
	for _, c := range candidates {
		ids = append(ids, c.CandidateID)
	}
	sort.Strings(ids)
	sha, err := canonicalSHA(ids)
	return sha, ids, err
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			set[v] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
