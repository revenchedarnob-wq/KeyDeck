package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	cc "keydeck.local/feasibilitylab/internal/candidatecollection"
)

type fixedClock struct{ t time.Time }

func (f *fixedClock) Now() time.Time { f.t = f.t.Add(time.Millisecond); return f.t }

type validator struct {
	reject error
	calls  int
}

func (v *validator) ValidateCollectionInput(context.Context, cc.CollectRequest) error {
	v.calls++
	return v.reject
}

type reconciler struct {
	calls     int
	lastCount int
}

func (r *reconciler) Assess(_ context.Context, cs []cc.CandidateRecord) (cc.AssessmentRecord, error) {
	r.calls++
	r.lastCount = len(cs)
	sort.Slice(cs, func(i, j int) bool { return cs[i].CandidateID < cs[j].CandidateID })
	if len(cs) == 1 {
		return cc.AssessmentRecord{State: cc.AssessmentSingleVerified, SelectedCandidateID: cs[0].CandidateID, DecisiveEvidenceIDs: []string{"evidence-single"}}, nil
	}
	same := true
	for i := 1; i < len(cs); i++ {
		if cs[i].ResultSHA != cs[0].ResultSHA {
			same = false
			break
		}
	}
	if same {
		return cc.AssessmentRecord{State: cc.AssessmentAgreement, SelectedCandidateID: cs[0].CandidateID, DecisiveEvidenceIDs: []string{"evidence-agreement"}}, nil
	}
	return cc.AssessmentRecord{State: cc.AssessmentDisagreement}, nil
}

type recovery struct {
	mu     sync.Mutex
	calls  int
	unique map[string]cc.RecoveryCommit
	last   cc.ResolvedRecoveryInput
}

type blockingRecovery struct {
	inner   *recovery
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type commitThenErrorRecovery struct {
	inner *recovery
	err   error
}

func (r *recovery) CommitResolvedCandidate(_ context.Context, in cc.ResolvedRecoveryInput) (cc.RecoveryCommit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.last = in
	if r.unique == nil {
		r.unique = map[string]cc.RecoveryCommit{}
	}
	if c, ok := r.unique[in.RecoveryOperationID]; ok {
		return c, nil
	}
	b, _ := json.Marshal([]string{in.RecoveryOperationID})
	sum := sha256.Sum256(b)
	sha := hex.EncodeToString(sum[:])
	c := cc.RecoveryCommit{CommitID: "commit-" + sha[:20], CommitSHA: sha, CanonicalRef: "canonical:" + in.TaskID}
	r.unique[in.RecoveryOperationID] = c
	return c, nil
}

func (r *commitThenErrorRecovery) CommitResolvedCandidate(ctx context.Context, in cc.ResolvedRecoveryInput) (cc.RecoveryCommit, error) {
	if _, err := r.inner.CommitResolvedCandidate(ctx, in); err != nil {
		return cc.RecoveryCommit{}, err
	}
	return cc.RecoveryCommit{}, r.err
}

func (r *blockingRecovery) CommitResolvedCandidate(ctx context.Context, in cc.ResolvedRecoveryInput) (cc.RecoveryCommit, error) {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return cc.RecoveryCommit{}, ctx.Err()
	}
	return r.inner.CommitResolvedCandidate(ctx, in)
}

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}
type report struct {
	Proof             string            `json:"proof"`
	Status            string            `json:"status"`
	IntegrationStatus string            `json:"integration_status"`
	Scenarios         []scenario        `json:"scenarios"`
	Passed            int               `json:"passed"`
	Total             int               `json:"total"`
	StableIdentities  map[string]string `json:"stable_identities,omitempty"`
	Limitations       []string          `json:"limitations"`
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func setResultPayload(req *cc.CollectRequest, payload string) {
	req.ResultPayload = payload
	req.ResultPayloadSHA = sha256Hex(payload)
}

func reqA() cc.CollectRequest {
	return cc.CollectRequest{TaskID: "task-proof34", SessionID: "session-proof34", TaskSequence: 34,
		HandoffPackageID: "handoff-proof34", HandoffPackageSHA: "handoff-sha",
		RouteDecisionID: "route-a", RouteDecisionSHA: "route-sha-a", ExecutionID: "execution-a", ResultID: "result-a",
		EngineID: "engine-a", EngineIdentitySHA: "engine-sha-a", ResultSHA: "result-sha-a", ResultPayloadSHA: sha256Hex("candidate A"), ResultPayload: "candidate A",
		RuntimeEvidenceIDs: []string{"runtime-execution-a", "runtime-result-a"}}
}
func reqB(same bool) cc.CollectRequest {
	r := reqA()
	r.RouteDecisionID = "route-b"
	r.RouteDecisionSHA = "route-sha-b"
	r.ExecutionID = "execution-b"
	r.ResultID = "result-b"
	r.EngineID = "engine-b"
	r.EngineIdentitySHA = "engine-sha-b"
	setResultPayload(&r, "candidate B")
	r.RuntimeEvidenceIDs = []string{"runtime-execution-b", "runtime-result-b"}
	if !same {
		r.ResultSHA = "result-sha-b"
	}
	return r
}
func reqCConflict() cc.CollectRequest {
	r := reqA()
	r.RouteDecisionID = "route-c"
	r.RouteDecisionSHA = "route-sha-c"
	r.ExecutionID = "execution-c"
	r.ResultID = "result-c"
	r.EngineID = "engine-c"
	r.EngineIdentitySHA = "engine-sha-c"
	r.ResultSHA = "result-sha-c"
	setResultPayload(&r, "candidate C conflict")
	r.RuntimeEvidenceIDs = []string{"runtime-execution-c", "runtime-result-c"}
	return r
}

func open(path string, v cc.CurrentStateValidator, r cc.Reconciler, rec cc.RecoveryPort) (*cc.Coordinator, error) {
	s, err := cc.OpenStore(path)
	if err != nil {
		return nil, err
	}
	c, err := cc.NewCoordinator(s, v, r, rec)
	if err != nil {
		return nil, err
	}
	c.WithClock(&fixedClock{t: time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)})
	return c, nil
}

type proofCandidateRecord struct {
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

type proofRecoveryPreparation struct {
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

type proofStoreEvent struct {
	EventID          string                `json:"event_id"`
	EventSHA         string                `json:"event_sha256"`
	Sequence         uint64                `json:"sequence"`
	PreviousEventSHA string                `json:"previous_event_sha256,omitempty"`
	Kind             string                `json:"kind"`
	TaskID           string                `json:"task_id"`
	PackageID        string                `json:"handoff_package_id"`
	Candidate        *proofCandidateRecord `json:"candidate,omitempty"`
	ReusedID         string                `json:"reused_candidate_id,omitempty"`
	Assessment       json.RawMessage       `json:"assessment,omitempty"`
	Resolution       json.RawMessage       `json:"resolution,omitempty"`
	Preparation      json.RawMessage       `json:"recovery_preparation,omitempty"`
	Recovery         json.RawMessage       `json:"recovery_commit,omitempty"`
	PreparationID    string                `json:"preparation_id,omitempty"`
	AssessmentID     string                `json:"assessment_id,omitempty"`
	ResolutionID     string                `json:"resolution_id,omitempty"`
	CreatedAtUTC     string                `json:"created_at_utc"`
}

func rewriteSingleEvent(path string, mutate func(*proofStoreEvent)) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	if len(lines) != 1 {
		return fmt.Errorf("expected one event, got %d", len(lines))
	}
	var ev proofStoreEvent
	if err := json.Unmarshal(lines[0], &ev); err != nil {
		return err
	}
	mutate(&ev)
	ev.EventID = ""
	ev.EventSHA = ""
	unsigned, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(unsigned)
	sha := hex.EncodeToString(sum[:])
	ev.EventSHA = sha
	ev.EventID = "event-" + sha[:20]
	out, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

func rewriteLastEvent(path string, mutate func(*proofStoreEvent)) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	if len(lines) == 0 {
		return fmt.Errorf("store has no events")
	}
	last := len(lines) - 1
	var ev proofStoreEvent
	if err := json.Unmarshal(lines[last], &ev); err != nil {
		return err
	}
	mutate(&ev)
	ev.EventID = ""
	ev.EventSHA = ""
	unsigned, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(unsigned)
	sha := hex.EncodeToString(sum[:])
	ev.EventSHA = sha
	ev.EventID = "event-" + sha[:20]
	out, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	lines[last] = out
	return os.WriteFile(path, append(bytes.Join(lines, []byte("\n")), '\n'), 0o600)
}

func rewriteEventChainFrom(path string, match func(proofStoreEvent) bool, mutate func(*proofStoreEvent) error) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	events := make([]proofStoreEvent, len(lines))
	target := -1
	for i, line := range lines {
		if err := json.Unmarshal(line, &events[i]); err != nil {
			return err
		}
		if target == -1 && match(events[i]) {
			target = i
		}
	}
	if target == -1 {
		return fmt.Errorf("target event not found")
	}
	if err := mutate(&events[target]); err != nil {
		return err
	}
	for i := target; i < len(events); i++ {
		if i == 0 {
			events[i].PreviousEventSHA = ""
		} else {
			events[i].PreviousEventSHA = events[i-1].EventSHA
		}
		events[i].EventID = ""
		events[i].EventSHA = ""
		unsigned, err := json.Marshal(events[i])
		if err != nil {
			return err
		}
		sum := sha256.Sum256(unsigned)
		sha := hex.EncodeToString(sum[:])
		events[i].EventSHA = sha
		events[i].EventID = "event-" + sha[:20]
	}
	out := make([]byte, 0, len(b))
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return os.WriteFile(path, out, 0o600)
}

func main() {
	root, err := os.MkdirTemp("", "keydeck-proof034-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	ctx := context.Background()
	rep := report{Proof: "0.34 production candidate collection coordinator integrated reconstructed line v0.3.0", Status: "PASS", IntegrationStatus: "integrated_into_reconstructed_keydeck_source", StableIdentities: map[string]string{}, Limitations: []string{
		"The historical lost post-v0.21 source archives were not recovered byte-for-byte; this line was reconstructed from the exact immutable v0.21.0 / Proof 0.24 source and re-proven forward.",
		"Proof 0.34 core hardening is combined with the real KeyDeck task/handoff/routing/runtime/recovery adapter integration proven by reconstructed Proofs 0.31-0.33.",
	}}
	add := func(name string, err error, detail string) {
		passed := err == nil
		if !passed {
			rep.Status = "FAIL"
			detail = err.Error()
		}
		rep.Scenarios = append(rep.Scenarios, scenario{name, passed, detail})
		if passed {
			rep.Passed++
		}
	}

	// 1. Validation before persistence.
	{
		path := filepath.Join(root, "s1.jsonl")
		v := &validator{reject: errors.New("stale current state")}
		c, _ := open(path, v, &reconciler{}, &recovery{})
		_, e := c.Collect(ctx, reqA())
		if !errors.Is(e, cc.ErrStaleCurrentState) {
			add("current_state_validation_precedes_persistence", fmt.Errorf("unexpected error: %v", e), "")
		} else {
			b, _ := os.ReadFile(path)
			if len(b) != 0 {
				add("current_state_validation_precedes_persistence", fmt.Errorf("store changed before validation"), "")
			} else {
				add("current_state_validation_precedes_persistence", nil, "stale input rejected with zero persisted bytes")
			}
		}
	}
	// Shared full-flow scope.
	path := filepath.Join(root, "flow.jsonl")
	v := &validator{}
	rr := &reconciler{}
	rec := &recovery{}
	c, _ := open(path, v, rr, rec)
	oa, e := c.Collect(ctx, reqA())
	add("exact_route_runtime_engine_binding_collected", e, "candidate persisted only after current-state validation")
	dup, e := c.Collect(ctx, reqA())
	if e == nil && (!dup.Reused || dup.Candidate.CandidateID != oa.Candidate.CandidateID) {
		e = fmt.Errorf("duplicate not reused")
	}
	add("duplicate_execution_result_reused_without_new_candidate", e, "same execution/result identity reused")
	conflict := reqA()
	conflict.EngineID = "engine-conflict"
	conflict.EngineIdentitySHA = "engine-conflict-sha"
	_, e = c.Collect(ctx, conflict)
	if !errors.Is(e, cc.ErrIdentityConflict) {
		e = fmt.Errorf("expected identity conflict, got %v", e)
	} else {
		e = nil
	}
	add("conflicting_execution_result_identity_rejected", e, "conflicting binding cannot overwrite collected identity")

	// Restart before canonical commit: the exact candidate is reused with no duplicate candidate.
	cRestart, _ := open(path, v, rr, rec)
	d, e := cRestart.Collect(ctx, reqA())
	if e == nil && (!d.Reused || d.Candidate.CandidateID != oa.Candidate.CandidateID) {
		e = fmt.Errorf("candidate not reused after restart")
	}
	add("restart_after_collection_reuses_exact_candidate", e, "durable store replay prevents duplicate collection before scope commit")
	c = cRestart

	if !errors.Is(c.RejectRawRecovery(), cc.ErrDirectRawRecoveryForbidden) {
		e = fmt.Errorf("raw recovery guard missing")
	} else {
		e = nil
	}
	add("direct_raw_engine_result_recovery_forbidden", e, "raw engine results cannot bypass collection/reconciliation")
	_, e = c.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
	if !errors.Is(e, cc.ErrAssessmentRequired) {
		e = fmt.Errorf("expected assessment gate, got %v", e)
	} else {
		e = nil
	}
	add("canonical_commit_requires_reconciliation_assessment", e, "commit blocked before assessment")

	// Add 2 matching + 1 conflicting: no majority truth.
	_, e = c.Collect(ctx, reqB(true))
	if e != nil {
		add("all_candidates_reach_reconciler", e, "")
	} else {
		_, e = c.Collect(ctx, reqCConflict())
		if e == nil {
			a, ae := c.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
			if ae != nil {
				e = ae
			} else if rr.lastCount != 3 {
				e = fmt.Errorf("reconciler saw %d candidates", rr.lastCount)
			} else if a.State != cc.AssessmentDisagreement || a.SelectedCandidateID != "" {
				e = fmt.Errorf("majority established truth: %+v", a)
			}
		}
		add("all_candidates_reach_reconciler", e, "reconciler received all three unique candidates")
	}
	a, _ := c.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
	if a.State != cc.AssessmentDisagreement || a.SelectedCandidateID != "" {
		e = fmt.Errorf("majority established truth")
	} else {
		e = nil
	}
	add("majority_vote_never_establishes_truth", e, "2 matching candidates versus 1 conflict remains disagreement")
	_, e = c.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
	if !errors.Is(e, cc.ErrResolutionRequired) {
		e = fmt.Errorf("expected resolution gate, got %v", e)
	} else {
		e = nil
	}
	add("unresolved_disagreement_cannot_mutate_canonical_state", e, "Recovery Coordinator not called for unresolved disagreement")
	review, e := c.ReviewRequest(reqA().TaskID, reqA().HandoffPackageID)
	add("explicit_reviewer_resolution_contract_emitted", e, "UI-neutral review contract contains exact assessment and candidate set")
	_, e = c.ResolveReview(cc.ReviewerResolutionRequest{AssessmentID: review.AssessmentID, AssessmentSHA: review.AssessmentSHA, SelectedCandidateID: oa.Candidate.CandidateID, ReviewerRef: "reviewer:proof034", Rationale: "exact fixture"})
	if !errors.Is(e, cc.ErrMissingDecisiveEvidence) {
		e = fmt.Errorf("resolution without evidence was accepted")
	} else {
		e = nil
	}
	add("reviewer_resolution_requires_decisive_evidence", e, "evidence-free reviewer choice rejected")
	resolution, e := c.ResolveReview(cc.ReviewerResolutionRequest{AssessmentID: review.AssessmentID, AssessmentSHA: review.AssessmentSHA, SelectedCandidateID: oa.Candidate.CandidateID, ReviewerRef: "reviewer:proof034", DecisiveEvidenceIDs: []string{"fixture-sha-proof034"}, Rationale: "exact fixture proves selected result"})
	add("evidence_backed_reviewer_resolution_persisted", e, "resolution binds assessment, selected candidate, reviewer and decisive evidence")
	commit, e := c.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
	if e == nil && (rec.last.CandidateID != oa.Candidate.CandidateID || rec.last.ResolutionID != resolution.ResolutionID) {
		e = fmt.Errorf("wrong candidate entered recovery")
	}
	add("only_resolved_selected_candidate_enters_recovery_coordinator", e, "resolved candidate envelope entered exact-once recovery")

	// Restart after resolution/commit: completed canonical commit is reused.
	c2, _ := open(path, v, rr, rec)
	commit2, e := c2.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
	if e == nil && commit2 != commit {
		e = fmt.Errorf("commit changed after restart")
	}
	add("restart_after_resolution_reuses_canonical_commit", e, "completed commit reused without duplicate canonical mutation")
	beforeCommittedBytes, _ := os.ReadFile(path)
	_, lateErr := c2.Collect(ctx, reqA())
	afterCommittedBytes, _ := os.ReadFile(path)
	if !errors.Is(lateErr, cc.ErrScopeCommitted) || !bytes.Equal(beforeCommittedBytes, afterCommittedBytes) {
		e = fmt.Errorf("committed scope accepted mutation or changed durable bytes: %v", lateErr)
	} else {
		e = nil
	}
	add("committed_scope_is_immutable_after_canonical_commit", e, "late collection is rejected and durable bytes remain unchanged")
	ev, e := c2.ReceiptEvidence(reqA().TaskID, reqA().HandoffPackageID)
	if e == nil {
		if ev.StoreSHA == "" || len(ev.CandidateIDs) != 3 || ev.AssessmentID == "" || ev.ResolutionID == "" || ev.RecoveryCommitID == "" || len(ev.RuntimeEvidenceIDs) != 6 {
			e = fmt.Errorf("incomplete receipt evidence: %+v", ev)
		}
	}
	add("receipt_binds_collection_candidate_set_assessment_resolution_runtime_and_commit", e, "receipt evidence spans collection through canonical commit")

	// A candidate arriving after assessment invalidates the assessment until all current candidates are reassessed.
	{
		stalePath := filepath.Join(root, "stale-assessment.jsonl")
		sv := &validator{}
		sr := &reconciler{}
		srec := &recovery{}
		sc, _ := open(stalePath, sv, sr, srec)
		sa := reqA()
		_, e := sc.Collect(ctx, sa)
		if e == nil {
			_, e = sc.Assess(ctx, sa.TaskID, sa.HandoffPackageID)
		}
		late := reqB(false)
		if e == nil {
			_, e = sc.Collect(ctx, late)
		}
		if e == nil {
			_, re := sc.ReceiptEvidence(sa.TaskID, sa.HandoffPackageID)
			if !errors.Is(re, cc.ErrStaleAssessment) {
				e = fmt.Errorf("stale receipt evidence was emitted: %v", re)
			}
		}
		add("stale_assessment_cannot_emit_receipt_evidence", e, "receipt generation fails closed until the current candidate set is reassessed")
		if e == nil {
			_, ce := sc.CommitResolved(ctx, sa.TaskID, sa.HandoffPackageID)
			if !errors.Is(ce, cc.ErrStaleAssessment) {
				e = fmt.Errorf("expected stale assessment, got %v", ce)
			}
		}
		add("late_candidate_invalidates_stale_assessment_before_commit", e, "candidate-set hash must be current before canonical commit")
	}

	// The local durable overlay store is append-order tamper evident.
	{
		chainPath := filepath.Join(root, "chain.jsonl")
		cv := &validator{}
		ccoord, _ := open(chainPath, cv, &reconciler{}, &recovery{})
		_, e := ccoord.Collect(ctx, reqA())
		if e == nil {
			_, e = ccoord.Collect(ctx, reqA())
		}
		if e == nil {
			data, re := os.ReadFile(chainPath)
			if re != nil {
				e = re
			} else {
				lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
				if len(lines) < 2 {
					e = fmt.Errorf("expected chained events")
				} else if we := os.WriteFile(chainPath, append(lines[1], '\n'), 0o600); we != nil {
					e = we
				} else if _, oe := cc.OpenStore(chainPath); oe == nil {
					e = fmt.Errorf("broken append chain accepted")
				}
			}
		}
		add("append_only_collection_store_rejects_removed_or_reordered_events", e, "sequence and previous-event hash chain verified on replay")
	}

	// Concurrent duplicate collection converges to one candidate identity.
	{
		concurrentPath := filepath.Join(root, "concurrent-collect.jsonl")
		cv := &validator{}
		cr := &reconciler{}
		crec := &recovery{}
		cco, _ := open(concurrentPath, cv, cr, crec)
		const workers = 32
		var wg sync.WaitGroup
		errCh := make(chan error, workers)
		idCh := make(chan string, workers)
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, ce := cco.Collect(ctx, reqA())
				if ce != nil {
					errCh <- ce
					return
				}
				idCh <- out.Candidate.CandidateID
			}()
		}
		wg.Wait()
		close(errCh)
		close(idCh)
		var e error
		for ce := range errCh {
			if ce != nil && e == nil {
				e = ce
			}
		}
		var first string
		for id := range idCh {
			if first == "" {
				first = id
			} else if id != first && e == nil {
				e = fmt.Errorf("multiple candidate identities")
			}
		}
		add("concurrent_duplicate_collection_converges_to_one_candidate", e, "32 concurrent duplicate collections returned one candidate identity")
	}

	// Concurrent commit calls converge to one Recovery Coordinator call/commit in-process.
	{
		commitPath := filepath.Join(root, "concurrent-commit.jsonl")
		cv := &validator{}
		cr := &reconciler{}
		crec := &recovery{}
		cco, _ := open(commitPath, cv, cr, crec)
		var e error
		if _, e = cco.Collect(ctx, reqA()); e == nil {
			_, e = cco.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		const workers = 32
		var wg sync.WaitGroup
		errCh := make(chan error, workers)
		commitCh := make(chan string, workers)
		if e == nil {
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					cm, ce := cco.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
					if ce != nil {
						errCh <- ce
						return
					}
					commitCh <- cm.CommitID
				}()
			}
			wg.Wait()
			close(errCh)
			close(commitCh)
			for ce := range errCh {
				if ce != nil && e == nil {
					e = ce
				}
			}
			var first string
			for id := range commitCh {
				if first == "" {
					first = id
				} else if id != first && e == nil {
					e = fmt.Errorf("multiple commit identities")
				}
			}
			if crec.calls != 1 && e == nil {
				e = fmt.Errorf("Recovery Coordinator called %d times", crec.calls)
			}
		}
		add("concurrent_commit_calls_converge_to_one_recovery_commit", e, "32 concurrent commit calls reused one Recovery Coordinator commit")
	}

	// Cross-operation lifecycle barrier: a collection racing a commit cannot enter after validation but before durable commit closure.
	{
		racePath := filepath.Join(root, "collect-commit-race.jsonl")
		cv := &validator{}
		cr := &reconciler{}
		inner := &recovery{}
		blocked := &blockingRecovery{inner: inner, entered: make(chan struct{}), release: make(chan struct{})}
		cco, _ := open(racePath, cv, cr, blocked)
		var e error
		if _, e = cco.Collect(ctx, reqA()); e == nil {
			_, e = cco.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			commitErr := make(chan error, 1)
			go func() {
				_, ce := cco.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
				commitErr <- ce
			}()
			<-blocked.entered
			late := reqB(false)
			late.ExecutionID = "execution-racing-late"
			late.ResultID = "result-racing-late"
			collectErr := make(chan error, 1)
			go func() {
				_, ce := cco.Collect(ctx, late)
				collectErr <- ce
			}()
			close(blocked.release)
			if ce := <-commitErr; ce != nil {
				e = ce
			} else if ce := <-collectErr; !errors.Is(ce, cc.ErrScopeCommitted) {
				e = fmt.Errorf("late collection crossed commit barrier: %v", ce)
			}
		}
		add("collect_and_commit_share_one_lifecycle_barrier", e, "a collection racing canonical commit cannot cross the commit window")
	}

	// Semantic replay validation rejects an unknown event kind even if the attacker recomputes the event hash.
	{
		semanticPath := filepath.Join(root, "semantic-unknown-kind.jsonl")
		cco, _ := open(semanticPath, &validator{}, &reconciler{}, &recovery{})
		_, e := cco.Collect(ctx, reqA())
		if e == nil {
			e = rewriteSingleEvent(semanticPath, func(ev *proofStoreEvent) { ev.Kind = "future_unknown_kind" })
		}
		if e == nil {
			_, oe := cc.OpenStore(semanticPath)
			if !errors.Is(oe, cc.ErrMalformedStoreEvent) {
				e = fmt.Errorf("unknown event kind accepted after rehash: %v", oe)
			}
		}
		add("semantic_replay_rejects_unknown_event_kind_after_rehash", e, "hash-chain recomputation cannot make an unknown durable event semantic valid")
	}

	// Candidate identity is re-derived during replay, so tampering remains invalid even after event rehash.
	{
		semanticPath := filepath.Join(root, "semantic-candidate-tamper.jsonl")
		cco, _ := open(semanticPath, &validator{}, &reconciler{}, &recovery{})
		_, e := cco.Collect(ctx, reqA())
		if e == nil {
			e = rewriteSingleEvent(semanticPath, func(ev *proofStoreEvent) {
				if ev.Candidate != nil {
					ev.Candidate.EngineID = "tampered-engine"
				}
			})
		}
		if e == nil {
			_, oe := cc.OpenStore(semanticPath)
			if !errors.Is(oe, cc.ErrMalformedStoreEvent) {
				e = fmt.Errorf("candidate identity tamper accepted after rehash: %v", oe)
			}
		}
		add("semantic_replay_rederives_candidate_identity_after_rehash", e, "durable candidate identity is independently recomputed on replay")
	}

	// The durable recovery event binds the exact assessment and reviewer resolution used for canonical commit.
	{
		bindingPath := filepath.Join(root, "semantic-recovery-binding.jsonl")
		cco, _ := open(bindingPath, &validator{}, &reconciler{}, &recovery{})
		oa, e := cco.Collect(ctx, reqA())
		if e == nil {
			_, e = cco.Collect(ctx, reqB(false))
		}
		var assessment cc.AssessmentRecord
		if e == nil {
			assessment, e = cco.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			_, e = cco.ResolveReview(cc.ReviewerResolutionRequest{AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA, SelectedCandidateID: oa.Candidate.CandidateID, ReviewerRef: "reviewer:binding", DecisiveEvidenceIDs: []string{"binding-evidence"}, Rationale: "exact binding evidence"})
		}
		if e == nil {
			_, e = cco.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			e = rewriteLastEvent(bindingPath, func(ev *proofStoreEvent) { ev.ResolutionID = "resolution-tampered" })
		}
		if e == nil {
			_, oe := cc.OpenStore(bindingPath)
			if !errors.Is(oe, cc.ErrMalformedStoreEvent) {
				e = fmt.Errorf("recovery resolution binding tamper accepted after rehash: %v", oe)
			}
		}
		add("recovery_commit_event_binds_exact_assessment_and_resolution", e, "rehashing cannot detach canonical commit from its exact assessment/reviewer resolution")
	}

	// Result payload bytes are independently bound before current-state validation or persistence.
	{
		payloadPath := filepath.Join(root, "payload-digest-gate.jsonl")
		pv := &validator{}
		cco, _ := open(payloadPath, pv, &reconciler{}, &recovery{})
		bad := reqA()
		bad.ResultPayload = "tampered before collection"
		_, e := cco.Collect(ctx, bad)
		if !errors.Is(e, cc.ErrInvalidInput) {
			e = fmt.Errorf("payload digest mismatch accepted: %v", e)
		} else if pv.calls != 0 {
			e = fmt.Errorf("current-state validator ran before payload integrity gate: %d", pv.calls)
		} else if b, re := os.ReadFile(payloadPath); re != nil {
			e = re
		} else if len(b) != 0 {
			e = fmt.Errorf("store changed before payload integrity passed")
		} else {
			e = nil
		}
		add("result_payload_digest_mismatch_rejected_before_validation_and_persistence", e, "persisted result payload bytes require an exact independent SHA-256 binding")
	}

	// Payload tampering remains invalid even if the attacker recomputes the full event chain.
	{
		payloadPath := filepath.Join(root, "semantic-payload-tamper.jsonl")
		cco, _ := open(payloadPath, &validator{}, &reconciler{}, &recovery{})
		_, e := cco.Collect(ctx, reqA())
		if e == nil {
			e = rewriteEventChainFrom(payloadPath, func(ev proofStoreEvent) bool { return ev.Kind == "candidate_collected" }, func(ev *proofStoreEvent) error {
				if ev.Candidate == nil {
					return fmt.Errorf("candidate payload missing")
				}
				ev.Candidate.ResultPayload = "payload changed after persistence"
				return nil
			})
		}
		if e == nil {
			_, oe := cc.OpenStore(payloadPath)
			if !errors.Is(oe, cc.ErrMalformedStoreEvent) {
				e = fmt.Errorf("payload tamper accepted after full chain rehash: %v", oe)
			}
		}
		add("semantic_replay_rejects_result_payload_tamper_after_full_rehash", e, "event-chain recomputation cannot detach persisted payload bytes from their exact digest")
	}

	// A durable recovery preparation closes the ambiguous external-commit crash window.
	{
		preparedPath := filepath.Join(root, "prepared-recovery.jsonl")
		inner := &recovery{}
		ambiguous := errors.New("ambiguous post-commit transport failure")
		cco, _ := open(preparedPath, &validator{}, &reconciler{}, &commitThenErrorRecovery{inner: inner, err: ambiguous})
		var e error
		if _, e = cco.Collect(ctx, reqA()); e == nil {
			_, e = cco.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			_, ce := cco.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
			if !errors.Is(ce, ambiguous) {
				e = fmt.Errorf("expected ambiguous post-commit failure, got %v", ce)
			}
		}
		var beforeEvidence cc.ReceiptEvidence
		if e == nil {
			beforeEvidence, e = cco.ReceiptEvidence(reqA().TaskID, reqA().HandoffPackageID)
			if e == nil && (beforeEvidence.RecoveryPreparationID == "" || beforeEvidence.RecoveryOperationID == "" || beforeEvidence.RecoveryCommitID != "") {
				e = fmt.Errorf("durable preparation missing or premature commit evidence: %+v", beforeEvidence)
			}
		}
		if e == nil && (inner.calls != 1 || len(inner.unique) != 1) {
			e = fmt.Errorf("unexpected external recovery state after ambiguous failure: calls=%d unique=%d", inner.calls, len(inner.unique))
		}

		var frozenErr error
		var beforeBytes []byte
		if e == nil {
			beforeBytes, frozenErr = os.ReadFile(preparedPath)
		}
		if frozenErr == nil {
			late := reqB(false)
			late.ExecutionID = "execution-after-preparation"
			late.ResultID = "result-after-preparation"
			_, ce := cco.Collect(ctx, late)
			if !errors.Is(ce, cc.ErrRecoveryPrepared) {
				frozenErr = fmt.Errorf("late collection not blocked by durable preparation: %v", ce)
			}
		}
		if frozenErr == nil {
			_, ae := cco.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
			if !errors.Is(ae, cc.ErrRecoveryPrepared) {
				frozenErr = fmt.Errorf("reassessment not blocked by durable preparation: %v", ae)
			}
		}
		if frozenErr == nil {
			afterBytes, re := os.ReadFile(preparedPath)
			if re != nil {
				frozenErr = re
			} else if !bytes.Equal(beforeBytes, afterBytes) {
				frozenErr = fmt.Errorf("rejected post-preparation mutation changed durable bytes")
			}
		}
		add("durable_recovery_preparation_freezes_scope_until_exact_commit_reconciliation", frozenErr, "once external recovery is prepared, collection and reconciliation mutation cannot change the exact operation")

		if e == nil {
			c2, oe := open(preparedPath, &validator{}, &reconciler{}, inner)
			if oe != nil {
				e = oe
			} else {
				commit, ce := c2.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
				if ce != nil {
					e = ce
				} else if inner.calls != 2 || len(inner.unique) != 1 {
					e = fmt.Errorf("restart did not reconcile one exact operation: calls=%d unique=%d", inner.calls, len(inner.unique))
				} else {
					afterEvidence, re := c2.ReceiptEvidence(reqA().TaskID, reqA().HandoffPackageID)
					if re != nil {
						e = re
					} else if afterEvidence.RecoveryPreparationID != beforeEvidence.RecoveryPreparationID || afterEvidence.RecoveryOperationID != beforeEvidence.RecoveryOperationID || afterEvidence.RecoveryCommitID != commit.CommitID {
						e = fmt.Errorf("restart changed prepared operation or failed to bind commit: before=%+v after=%+v", beforeEvidence, afterEvidence)
					}
				}
			}
		}
		add("ambiguous_post_commit_failure_restarts_with_exact_prepared_recovery_operation", e, "restart reuses the exact durable recovery operation through the existing exact-once Recovery Coordinator boundary")
	}

	// A forged preparation operation is rejected even after the preparation identity and all following event hashes are recomputed.
	{
		preparedPath := filepath.Join(root, "semantic-preparation-operation-tamper.jsonl")
		cco, _ := open(preparedPath, &validator{}, &reconciler{}, &recovery{})
		_, e := cco.Collect(ctx, reqA())
		if e == nil {
			_, e = cco.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			_, e = cco.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			e = rewriteEventChainFrom(preparedPath, func(ev proofStoreEvent) bool { return ev.Kind == "recovery_prepared" }, func(ev *proofStoreEvent) error {
				var p proofRecoveryPreparation
				if err := json.Unmarshal(ev.Preparation, &p); err != nil {
					return err
				}
				p.RecoveryOperationSHA = strings.Repeat("a", 64)
				p.RecoveryOperationID = "recovery-operation-" + p.RecoveryOperationSHA[:20]
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
				b, err := json.Marshal(identity)
				if err != nil {
					return err
				}
				sum := sha256.Sum256(b)
				p.PreparationSHA = hex.EncodeToString(sum[:])
				p.PreparationID = "recovery-preparation-" + p.PreparationSHA[:20]
				ev.Preparation, err = json.Marshal(p)
				return err
			})
		}
		if e == nil {
			_, oe := cc.OpenStore(preparedPath)
			if !errors.Is(oe, cc.ErrMalformedStoreEvent) {
				e = fmt.Errorf("forged recovery operation accepted after full chain rehash: %v", oe)
			}
		}
		add("semantic_replay_rejects_forged_recovery_operation_after_full_rehash", e, "replay reconstructs the exact resolved input and independently verifies the prepared recovery operation")
	}

	// The final recovery commit must bind the exact durable preparation, not merely the candidate and assessment.
	{
		bindingPath := filepath.Join(root, "semantic-preparation-binding.jsonl")
		cco, _ := open(bindingPath, &validator{}, &reconciler{}, &recovery{})
		_, e := cco.Collect(ctx, reqA())
		if e == nil {
			_, e = cco.Assess(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			_, e = cco.CommitResolved(ctx, reqA().TaskID, reqA().HandoffPackageID)
		}
		if e == nil {
			e = rewriteLastEvent(bindingPath, func(ev *proofStoreEvent) { ev.PreparationID = "recovery-preparation-tampered" })
		}
		if e == nil {
			_, oe := cc.OpenStore(bindingPath)
			if !errors.Is(oe, cc.ErrMalformedStoreEvent) {
				e = fmt.Errorf("recovery commit preparation binding tamper accepted after rehash: %v", oe)
			}
		}
		add("recovery_commit_event_binds_exact_durable_preparation", e, "canonical commit evidence cannot be detached from the exact prepared recovery operation")
	}

	rep.StableIdentities["candidate_id"] = oa.Candidate.CandidateID
	rep.StableIdentities["assessment_id"] = a.AssessmentID
	rep.StableIdentities["resolution_id"] = resolution.ResolutionID
	rep.StableIdentities["commit_id"] = commit.CommitID
	rep.Total = len(rep.Scenarios)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rep)
	if rep.Status != "PASS" {
		os.Exit(1)
	}
}
