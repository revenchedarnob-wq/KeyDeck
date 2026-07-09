package candidatecollection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/handoff"
	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/routing"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
)

// RouteCurrentResolver supplies the current live routing inputs for an already
// persisted route plan. It is deliberately evaluated during candidate
// collection so stale route evidence fails before persistence.
type RouteCurrentResolver func(context.Context, routing.Plan) (routing.Requirements, []routing.Candidate, error)

type KeyDeckValidator struct {
	Tasks                 *tasks.Store
	Handoffs              *handoff.Store
	HandoffCurrent        handoff.StateResolver
	Routes                *routing.Store
	RouteCurrent          RouteCurrentResolver
	Runtime               *engineruntime.Store
	CandidateEngineLedger *recovery.EngineStore
	ForbiddenExactValues  []string
}

func (v *KeyDeckValidator) ValidateCollectionInput(ctx context.Context, req CollectRequest) error {
	if v == nil || v.Tasks == nil || v.Handoffs == nil || v.HandoffCurrent == nil || v.Routes == nil || v.RouteCurrent == nil || v.Runtime == nil || v.CandidateEngineLedger == nil {
		return errors.New("candidate current-state validator is not configured")
	}
	if containsForbiddenPayload(req.ResultPayload, v.ForbiddenExactValues) {
		return errors.New("candidate result payload contains forbidden secret-like data")
	}
	task := v.Tasks.State()
	if task.TaskID != req.TaskID || task.SessionID != req.SessionID || task.LastSequence != req.TaskSequence {
		return errors.New("current task identity or sequence mismatch")
	}
	pkg, ok := v.Handoffs.Package(req.HandoffPackageID)
	if !ok || pkg.PackageSHA256 != req.HandoffPackageSHA {
		return errors.New("current handoff package identity mismatch")
	}
	if err := handoff.Validate(pkg, v.HandoffCurrent(), v.ForbiddenExactValues); err != nil {
		return fmt.Errorf("current handoff package invalid: %w", err)
	}
	plan, ok := v.Routes.Plan(req.RouteDecisionID)
	if !ok || plan.RouteSHA256 != req.RouteDecisionSHA {
		return errors.New("current route decision identity mismatch")
	}
	routeReq, candidates, err := v.RouteCurrent(ctx, plan)
	if err != nil {
		return err
	}
	if err := routing.Validate(plan, routeReq, candidates); err != nil {
		return fmt.Errorf("current route decision invalid: %w", err)
	}
	if err := validateRouteScope(plan, pkg); err != nil {
		return err
	}
	if plan.SelectedEngineID != req.EngineID {
		return errors.New("route-selected engine mismatch")
	}

	execution := v.Runtime.Execution(req.ExecutionID)
	if execution.ExecutionID == "" || execution.TaskID != req.TaskID || execution.SessionID != req.SessionID || execution.EngineID != req.EngineID || execution.ResultID != req.ResultID || execution.Disposition != engineruntime.DispositionCompleted {
		return errors.New("current runtime execution/result binding mismatch")
	}
	result := v.CandidateEngineLedger.Result(req.ResultID)
	if result.ResultID == "" || result.ExecutionID != req.ExecutionID || result.TaskID != req.TaskID || result.SessionID != req.SessionID || result.Engine != req.EngineID {
		return errors.New("current engine result binding mismatch")
	}
	payload, err := engineResultPayload(result.Output)
	if err != nil {
		return err
	}
	if payload != req.ResultPayload || sha256HexString(payload) != req.ResultPayloadSHA {
		return errors.New("engine result payload evidence mismatch")
	}
	if KeyDeckResultSHA(result) != req.ResultSHA {
		return errors.New("engine result identity SHA mismatch")
	}
	if KeyDeckEngineIdentitySHA(plan) != req.EngineIdentitySHA {
		return errors.New("engine identity SHA mismatch")
	}
	if !requiredRuntimeEvidence(req.RuntimeEvidenceIDs, plan.RouteID, req.ExecutionID, req.ResultID) {
		return errors.New("required runtime evidence references missing")
	}
	return nil
}

func BuildCollectRequest(task tasks.State, pkg handoff.Package, plan routing.Plan, execution engineruntime.Execution, result recovery.Result) (CollectRequest, error) {
	if task.TaskID == "" || task.SessionID == "" || task.LastSequence == 0 {
		return CollectRequest{}, ErrInvalidInput
	}
	if pkg.Task.TaskID != task.TaskID || pkg.Task.SessionID != task.SessionID || pkg.Task.LastSequence != task.LastSequence {
		return CollectRequest{}, ErrInvalidInput
	}
	if err := validateRouteScope(plan, pkg); err != nil {
		return CollectRequest{}, err
	}
	if execution.ExecutionID == "" || execution.TaskID != task.TaskID || execution.SessionID != task.SessionID || execution.EngineID != plan.SelectedEngineID || execution.ResultID == "" || execution.Disposition != engineruntime.DispositionCompleted {
		return CollectRequest{}, ErrInvalidInput
	}
	if result.ResultID != execution.ResultID || result.ExecutionID != execution.ExecutionID || result.TaskID != task.TaskID || result.SessionID != task.SessionID || result.Engine != plan.SelectedEngineID {
		return CollectRequest{}, ErrInvalidInput
	}
	payload, err := engineResultPayload(result.Output)
	if err != nil {
		return CollectRequest{}, err
	}
	return CollectRequest{
		TaskID: task.TaskID, SessionID: task.SessionID, TaskSequence: task.LastSequence,
		HandoffPackageID: pkg.PackageID, HandoffPackageSHA: pkg.PackageSHA256,
		RouteDecisionID: plan.RouteID, RouteDecisionSHA: plan.RouteSHA256,
		ExecutionID: execution.ExecutionID, ResultID: result.ResultID, EngineID: result.Engine,
		EngineIdentitySHA: KeyDeckEngineIdentitySHA(plan), ResultSHA: KeyDeckResultSHA(result),
		ResultPayloadSHA: sha256HexString(payload), ResultPayload: payload,
		RuntimeEvidenceIDs: []string{"result:" + result.ResultID, "route:" + plan.RouteID, "runtime:" + execution.ExecutionID},
	}, nil
}

func validateRouteScope(plan routing.Plan, pkg handoff.Package) error {
	if plan.TaskID != pkg.Task.TaskID || plan.SessionID != pkg.Task.SessionID || plan.HandoffPackageID != pkg.PackageID || plan.HandoffPackageSHA256 != pkg.PackageSHA256 {
		return routing.ErrRoutePackageMismatch
	}
	return nil
}

func KeyDeckEngineIdentitySHA(plan routing.Plan) string {
	return adapterDigest(struct {
		EngineID        string   `json:"engine_id"`
		ProviderID      string   `json:"provider_id"`
		RouteSHA256     string   `json:"route_sha256"`
		CandidateSetSHA string   `json:"candidate_set_sha256"`
		EvidenceRefs    []string `json:"evidence_refs,omitempty"`
	}{plan.SelectedEngineID, plan.SelectedProviderID, plan.RouteSHA256, plan.CandidateSetSHA256, append([]string(nil), plan.SelectedEvidenceRefs...)})
}

func KeyDeckResultSHA(result recovery.Result) string {
	return adapterDigest(struct {
		ResultID         string               `json:"result_id"`
		ExecutionID      string               `json:"execution_id"`
		TaskID           string               `json:"task_id"`
		SessionID        string               `json:"session_id"`
		Engine           string               `json:"engine"`
		ExternalThreadID string               `json:"external_thread_id,omitempty"`
		Output           session.EngineResult `json:"output"`
	}{result.ResultID, result.ExecutionID, result.TaskID, result.SessionID, result.Engine, result.ExternalThreadID, result.Output})
}

func engineResultPayload(result session.EngineResult) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func adapterDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func requiredRuntimeEvidence(ids []string, routeID, executionID, resultID string) bool {
	have := map[string]bool{}
	for _, id := range ids {
		have[id] = true
	}
	return have["route:"+routeID] && have["runtime:"+executionID] && have["result:"+resultID]
}

var adapterSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|password|access[_-]?token|secret)\s*[:=]\s*["']?[A-Za-z0-9_./+\-=]{8,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
}

func containsForbiddenPayload(payload string, exact []string) bool {
	for _, s := range exact {
		if s != "" && strings.Contains(payload, s) {
			return true
		}
	}
	for _, re := range adapterSecretPatterns {
		if re.MatchString(payload) {
			return true
		}
	}
	return false
}

// EvidenceReconciler is intentionally conservative. Exact all-candidate
// agreement may select one identical result; a 2-vs-1 majority never does.
type EvidenceReconciler struct{}

func (EvidenceReconciler) Assess(_ context.Context, candidates []CandidateRecord) (AssessmentRecord, error) {
	if len(candidates) == 0 {
		return AssessmentRecord{}, ErrNotFound
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CandidateID < candidates[j].CandidateID })
	allVerified := true
	for _, c := range candidates {
		if !hasVerificationEvidence(c.RuntimeEvidenceIDs) {
			allVerified = false
		}
	}
	if len(candidates) == 1 {
		if !allVerified {
			return AssessmentRecord{State: AssessmentNeedsReview}, nil
		}
		return AssessmentRecord{State: AssessmentSingleVerified, SelectedCandidateID: candidates[0].CandidateID, DecisiveEvidenceIDs: uniqueSorted(candidates[0].RuntimeEvidenceIDs)}, nil
	}
	same := true
	for i := 1; i < len(candidates); i++ {
		if candidates[i].ResultPayloadSHA != candidates[0].ResultPayloadSHA {
			same = false
			break
		}
	}
	if same && allVerified {
		evidence := []string{}
		for _, c := range candidates {
			evidence = append(evidence, c.RuntimeEvidenceIDs...)
		}
		return AssessmentRecord{State: AssessmentAgreement, SelectedCandidateID: candidates[0].CandidateID, DecisiveEvidenceIDs: uniqueSorted(evidence)}, nil
	}
	if !allVerified {
		return AssessmentRecord{State: AssessmentNeedsReview}, nil
	}
	return AssessmentRecord{State: AssessmentDisagreement}, nil
}

func hasVerificationEvidence(ids []string) bool {
	for _, id := range ids {
		if strings.HasPrefix(id, "verification:") && len(strings.TrimPrefix(id, "verification:")) > 0 {
			return true
		}
	}
	return false
}

type KeyDeckRecoveryPort struct {
	Coordinator  *recovery.Coordinator
	EngineLedger *recovery.EngineStore
}

func (p *KeyDeckRecoveryPort) CommitResolvedCandidate(_ context.Context, in ResolvedRecoveryInput) (RecoveryCommit, error) {
	if p == nil || p.Coordinator == nil || p.EngineLedger == nil {
		return RecoveryCommit{}, errors.New("candidate recovery port is not configured")
	}
	var output session.EngineResult
	if err := json.Unmarshal([]byte(in.ResultPayload), &output); err != nil {
		return RecoveryCommit{}, err
	}
	payload, err := engineResultPayload(output)
	if err != nil {
		return RecoveryCommit{}, err
	}
	if payload != in.ResultPayload || sha256HexString(payload) != in.ResultPayloadSHA {
		return RecoveryCommit{}, errors.New("resolved result payload mismatch")
	}

	existingExec := p.EngineLedger.Execution(in.ExecutionID)
	if existingExec.ExecutionID == "" {
		if _, _, err := p.EngineLedger.StartOnce(recovery.Execution{ExecutionID: in.ExecutionID, TaskID: in.TaskID, SessionID: in.SessionID, Engine: in.EngineID, StartedAt: time.Unix(0, 0).UTC()}); err != nil {
			return RecoveryCommit{}, err
		}
	}
	existingResult := p.EngineLedger.Result(in.ResultID)
	if existingResult.ResultID == "" {
		if _, _, err := p.EngineLedger.CompleteResultOnce(recovery.Result{ResultID: in.ResultID, ExecutionID: in.ExecutionID, TaskID: in.TaskID, SessionID: in.SessionID, Engine: in.EngineID, Output: output, CompletedAt: time.Unix(0, 0).UTC()}); err != nil {
			return RecoveryCommit{}, err
		}
	} else if KeyDeckResultSHA(existingResult) != in.ResultSHA {
		return RecoveryCommit{}, errors.New("canonical recovery ledger result identity conflict")
	}
	if _, err := p.Coordinator.Recover(); err != nil {
		return RecoveryCommit{}, err
	}
	committed := p.EngineLedger.Result(in.ResultID)
	if !committed.CanonicalCommitted {
		return RecoveryCommit{}, errors.New("resolved result was not canonically committed")
	}
	canonicalRef := recovery.ResultCommitMarker(in.ResultID)
	sha := adapterDigest(struct {
		RecoveryOperationID  string `json:"recovery_operation_id"`
		RecoveryOperationSHA string `json:"recovery_operation_sha256"`
		CandidateID          string `json:"candidate_id"`
		AssessmentID         string `json:"assessment_id"`
		ResolutionID         string `json:"resolution_id,omitempty"`
		ResultID             string `json:"result_id"`
		CanonicalRef         string `json:"canonical_ref"`
	}{in.RecoveryOperationID, in.RecoveryOperationSHA, in.CandidateID, in.AssessmentID, in.ResolutionID, in.ResultID, canonicalRef})
	return RecoveryCommit{CommitID: "commit-" + sha[:20], CommitSHA: sha, CanonicalRef: canonicalRef}, nil
}
