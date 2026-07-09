package candidatecollection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixedClock struct{ t time.Time }

func (f *fixedClock) Now() time.Time { f.t = f.t.Add(time.Millisecond); return f.t }

type exactValidator struct {
	expected CollectRequest
	calls    int
	reject   error
}

func (v *exactValidator) ValidateCollectionInput(_ context.Context, got CollectRequest) error {
	v.calls++
	if v.reject != nil {
		return v.reject
	}
	e := v.expected
	if got.TaskID != e.TaskID || got.SessionID != e.SessionID || got.TaskSequence != e.TaskSequence ||
		got.HandoffPackageID != e.HandoffPackageID || got.HandoffPackageSHA != e.HandoffPackageSHA ||
		got.RouteDecisionID != e.RouteDecisionID || got.RouteDecisionSHA != e.RouteDecisionSHA ||
		got.ExecutionID != e.ExecutionID || got.ResultID != e.ResultID || got.EngineID != e.EngineID ||
		got.EngineIdentitySHA != e.EngineIdentitySHA || got.ResultSHA != e.ResultSHA {
		return fmt.Errorf("exact binding mismatch")
	}
	return nil
}

type permissiveValidator struct {
	calls  int
	reject error
}

func (v *permissiveValidator) ValidateCollectionInput(_ context.Context, _ CollectRequest) error {
	v.calls++
	return v.reject
}

type deterministicReconciler struct{ calls int }

func (r *deterministicReconciler) Assess(_ context.Context, candidates []CandidateRecord) (AssessmentRecord, error) {
	r.calls++
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CandidateID < candidates[j].CandidateID })
	if len(candidates) == 1 {
		return AssessmentRecord{State: AssessmentSingleVerified, SelectedCandidateID: candidates[0].CandidateID, DecisiveEvidenceIDs: []string{"evidence-single"}}, nil
	}
	allSame := true
	for i := 1; i < len(candidates); i++ {
		if candidates[i].ResultSHA != candidates[0].ResultSHA {
			allSame = false
			break
		}
	}
	if allSame {
		return AssessmentRecord{State: AssessmentAgreement, SelectedCandidateID: candidates[0].CandidateID, DecisiveEvidenceIDs: []string{"evidence-agreement"}}, nil
	}
	// Deliberately no majority rule: 2 matching + 1 conflicting still disagrees.
	return AssessmentRecord{State: AssessmentDisagreement}, nil
}

type exactOnceRecovery struct {
	mu     sync.Mutex
	calls  int
	unique map[string]RecoveryCommit
	last   ResolvedRecoveryInput
}

type commitThenErrorRecovery struct {
	inner *exactOnceRecovery
	err   error
}

func (r *commitThenErrorRecovery) CommitResolvedCandidate(ctx context.Context, in ResolvedRecoveryInput) (RecoveryCommit, error) {
	if _, err := r.inner.CommitResolvedCandidate(ctx, in); err != nil {
		return RecoveryCommit{}, err
	}
	return RecoveryCommit{}, r.err
}

func (r *exactOnceRecovery) CommitResolvedCandidate(_ context.Context, in ResolvedRecoveryInput) (RecoveryCommit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.last = in
	if r.unique == nil {
		r.unique = map[string]RecoveryCommit{}
	}
	if err := validateResolvedRecoveryInput(in); err != nil {
		return RecoveryCommit{}, err
	}
	if existing, ok := r.unique[in.RecoveryOperationID]; ok {
		return existing, nil
	}
	sha, _ := canonicalSHA(struct{ RecoveryOperationID string }{in.RecoveryOperationID})
	commit := RecoveryCommit{CommitID: prefixedID("commit", sha), CommitSHA: sha, CanonicalRef: "canonical:" + in.TaskID}
	r.unique[in.RecoveryOperationID] = commit
	return commit, nil
}

func baseRequest() CollectRequest {
	return CollectRequest{
		TaskID: "task-proof34", SessionID: "session-proof34", TaskSequence: 34,
		HandoffPackageID: "handoff-proof34", HandoffPackageSHA: "handoff-sha",
		RouteDecisionID: "route-proof34-a", RouteDecisionSHA: "route-sha-a",
		ExecutionID: "execution-proof34-a", ResultID: "result-proof34-a",
		EngineID: "engine-a", EngineIdentitySHA: "engine-sha-a",
		ResultSHA: "result-sha-a", ResultPayloadSHA: sha256HexString("candidate A"), ResultPayload: "candidate A",
		RuntimeEvidenceIDs: []string{"runtime-result-a", "runtime-execution-a"},
	}
}

func setResultPayload(req *CollectRequest, payload string) {
	req.ResultPayload = payload
	req.ResultPayloadSHA = sha256HexString(payload)
}

func openCoordinator(t *testing.T, path string, validator CurrentStateValidator, reconciler Reconciler, recovery RecoveryPort) (*Coordinator, *Store) {
	t.Helper()
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCoordinator(store, validator, reconciler, recovery)
	if err != nil {
		t.Fatal(err)
	}
	c.WithClock(&fixedClock{t: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)})
	return c, store
}

func TestCollectValidatesBeforePersistenceAndRejectsStaleState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	validator := &permissiveValidator{reject: errors.New("stale task sequence")}
	c, store := openCoordinator(t, path, validator, &deterministicReconciler{}, &exactOnceRecovery{})
	_, err := c.Collect(context.Background(), baseRequest())
	if !errors.Is(err, ErrStaleCurrentState) {
		t.Fatalf("expected stale state, got %v", err)
	}
	if got := len(store.CandidatesForScope("task-proof34", "handoff-proof34")); got != 0 {
		t.Fatalf("persisted %d candidates before validation", got)
	}
}

func TestCollectExactDuplicateReusesWithoutNewCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	validator := &exactValidator{expected: req}
	c, store := openCoordinator(t, path, validator, &deterministicReconciler{}, &exactOnceRecovery{})
	first, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.Collect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || !second.Reused || first.Candidate.CandidateID != second.Candidate.CandidateID {
		t.Fatalf("duplicate was not reused exactly")
	}
	if got := len(store.CandidatesForScope(req.TaskID, req.HandoffPackageID)); got != 1 {
		t.Fatalf("got %d unique candidates", got)
	}
}

func TestConflictingDuplicateExecutionResultIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	validator := &permissiveValidator{}
	c, _ := openCoordinator(t, path, validator, &deterministicReconciler{}, &exactOnceRecovery{})
	if _, err := c.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	conflict := req
	conflict.EngineID = "engine-b"
	conflict.EngineIdentitySHA = "engine-sha-b"
	if _, err := c.Collect(context.Background(), conflict); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("expected identity conflict, got %v", err)
	}
}

func TestCommitRequiresAssessmentAndBlocksDirectRawRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	if _, err := c.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID); !errors.Is(err, ErrAssessmentRequired) {
		t.Fatalf("expected assessment gate, got %v", err)
	}
	if !errors.Is(c.RejectRawRecovery(), ErrDirectRawRecoveryForbidden) {
		t.Fatal("direct raw recovery path was not blocked")
	}
}

func TestNoMajorityRuleAndExplicitReviewerResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	validator := &permissiveValidator{}
	reconciler := &deterministicReconciler{}
	recovery := &exactOnceRecovery{}
	c, _ := openCoordinator(t, path, validator, reconciler, recovery)

	a := baseRequest()
	b := a
	b.RouteDecisionID = "route-b"
	b.RouteDecisionSHA = "route-sha-b"
	b.ExecutionID = "execution-b"
	b.ResultID = "result-b"
	b.EngineID = "engine-b"
	b.EngineIdentitySHA = "engine-sha-b"
	setResultPayload(&b, "candidate B") // same ResultSHA
	d := a
	d.RouteDecisionID = "route-c"
	d.RouteDecisionSHA = "route-sha-c"
	d.ExecutionID = "execution-c"
	d.ResultID = "result-c"
	d.EngineID = "engine-c"
	d.EngineIdentitySHA = "engine-sha-c"
	d.ResultSHA = "result-sha-conflict"
	setResultPayload(&d, "candidate C conflict")
	for _, req := range []CollectRequest{a, b, d} {
		if _, err := c.Collect(context.Background(), req); err != nil {
			t.Fatal(err)
		}
	}

	assessment, err := c.Assess(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != AssessmentDisagreement {
		t.Fatalf("2-vs-1 was incorrectly treated as truth: %s", assessment.State)
	}
	if assessment.SelectedCandidateID != "" {
		t.Fatal("disagreement selected a candidate by majority")
	}
	if _, err := c.CommitResolved(context.Background(), a.TaskID, a.HandoffPackageID); !errors.Is(err, ErrResolutionRequired) {
		t.Fatalf("unresolved disagreement committed: %v", err)
	}

	review, err := c.ReviewRequest(a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.CandidateIDs) != 3 {
		t.Fatalf("review request omitted candidates: %v", review.CandidateIDs)
	}
	selected := review.CandidateIDs[0]
	if _, err := c.ResolveReview(ReviewerResolutionRequest{AssessmentID: review.AssessmentID, AssessmentSHA: review.AssessmentSHA, SelectedCandidateID: selected, ReviewerRef: "reviewer:test", Rationale: "verified by exact fixture"}); !errors.Is(err, ErrMissingDecisiveEvidence) {
		t.Fatalf("resolution without evidence should fail: %v", err)
	}
	resolution, err := c.ResolveReview(ReviewerResolutionRequest{AssessmentID: review.AssessmentID, AssessmentSHA: review.AssessmentSHA, SelectedCandidateID: selected, ReviewerRef: "reviewer:test", DecisiveEvidenceIDs: []string{"fixture-sha-verified"}, Rationale: "verified by exact fixture"})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := c.CommitResolved(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if recovery.last.CandidateID != selected || recovery.last.ResolutionID != resolution.ResolutionID {
		t.Fatalf("wrong candidate/resolution entered recovery: %+v", recovery.last)
	}
	if commit.CommitID == "" {
		t.Fatal("missing commit")
	}
}

func TestRestartAfterCollectionReusesExactCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	validator := &permissiveValidator{}
	reconciler := &deterministicReconciler{}
	recovery := &exactOnceRecovery{}
	c1, _ := openCoordinator(t, path, validator, reconciler, recovery)
	first, err := c1.Collect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	c2, store2 := openCoordinator(t, path, validator, reconciler, recovery)
	second, err := c2.Collect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Reused || second.Candidate.CandidateID != first.Candidate.CandidateID {
		t.Fatal("restart did not reuse exact candidate")
	}
	if len(store2.CandidatesForScope(req.TaskID, req.HandoffPackageID)) != 1 {
		t.Fatal("restart duplicated candidate")
	}
}

func TestRestartAcrossAssessmentResolutionAndCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	validator := &permissiveValidator{}
	reconciler := &deterministicReconciler{}
	recovery := &exactOnceRecovery{}
	a := baseRequest()
	b := a
	b.ExecutionID = "execution-b"
	b.ResultID = "result-b"
	b.RouteDecisionID = "route-b"
	b.RouteDecisionSHA = "route-sha-b"
	b.EngineID = "engine-b"
	b.EngineIdentitySHA = "engine-sha-b"
	b.ResultSHA = "result-sha-b"
	setResultPayload(&b, "candidate B")

	c1, _ := openCoordinator(t, path, validator, reconciler, recovery)
	oa, err := c1.Collect(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Collect(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	assessment, err := c1.Assess(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.State != AssessmentDisagreement {
		t.Fatalf("expected disagreement, got %s", assessment.State)
	}

	c2, _ := openCoordinator(t, path, validator, reconciler, recovery)
	review, err := c2.ReviewRequest(a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := c2.ResolveReview(ReviewerResolutionRequest{AssessmentID: review.AssessmentID, AssessmentSHA: review.AssessmentSHA, SelectedCandidateID: oa.Candidate.CandidateID, ReviewerRef: "reviewer:restart", DecisiveEvidenceIDs: []string{"decisive:restart"}, Rationale: "exact evidence"})
	if err != nil {
		t.Fatal(err)
	}

	c3, _ := openCoordinator(t, path, validator, reconciler, recovery)
	commit1, err := c3.CommitResolved(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	c4, _ := openCoordinator(t, path, validator, reconciler, recovery)
	commit2, err := c4.CommitResolved(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if commit1 != commit2 {
		t.Fatalf("commit not reused after restart: %+v %+v", commit1, commit2)
	}
	if len(recovery.unique) != 1 {
		t.Fatalf("canonical recovery committed %d unique candidates", len(recovery.unique))
	}
	if recovery.last.ResolutionID != resolution.ResolutionID {
		t.Fatal("resolution binding lost across restart")
	}
}

func TestCrashAfterRecoveryCommitReconcilesViaExactOnceRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	validator := &permissiveValidator{}
	reconciler := &deterministicReconciler{}
	recovery := &exactOnceRecovery{}
	c1, _ := openCoordinator(t, path, validator, reconciler, recovery)
	if _, err := c1.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Assess(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	crash := errors.New("simulated crash after recovery commit before local commit event")
	c1.afterRecoveryCommit = func() error { return crash }
	if _, err := c1.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID); !errors.Is(err, crash) {
		t.Fatalf("expected crash window, got %v", err)
	}
	if len(recovery.unique) != 1 {
		t.Fatalf("recovery did not commit exactly once before crash")
	}

	c2, _ := openCoordinator(t, path, validator, reconciler, recovery)
	commit, err := c2.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if commit.CommitID == "" || len(recovery.unique) != 1 {
		t.Fatal("restart failed exact-once recovery reconciliation")
	}
}

func TestReceiptEvidenceBindsCollectionCandidatesAssessmentResolutionRuntimeAndCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	validator := &permissiveValidator{}
	reconciler := &deterministicReconciler{}
	recovery := &exactOnceRecovery{}
	c, _ := openCoordinator(t, path, validator, reconciler, recovery)
	a := baseRequest()
	b := a
	b.ExecutionID = "execution-b"
	b.ResultID = "result-b"
	b.RouteDecisionID = "route-b"
	b.RouteDecisionSHA = "route-sha-b"
	b.EngineID = "engine-b"
	b.EngineIdentitySHA = "engine-sha-b"
	b.ResultSHA = "result-sha-b"
	setResultPayload(&b, "candidate B")
	b.RuntimeEvidenceIDs = []string{"runtime-execution-b", "runtime-result-b"}
	oa, err := c.Collect(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Collect(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	assessment, err := c.Assess(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := c.ResolveReview(ReviewerResolutionRequest{AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA, SelectedCandidateID: oa.Candidate.CandidateID, ReviewerRef: "reviewer:receipt", DecisiveEvidenceIDs: []string{"decisive-receipt"}, Rationale: "exact receipt evidence"})
	if err != nil {
		t.Fatal(err)
	}
	commit, err := c.CommitResolved(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := c.ReceiptEvidence(a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.StoreSHA == "" || len(ev.CollectionEventIDs) < 5 || len(ev.CandidateIDs) != 2 || ev.CandidateSetSHA == "" {
		t.Fatalf("incomplete collection evidence: %+v", ev)
	}
	if ev.AssessmentID != assessment.AssessmentID || ev.ResolutionID != resolution.ResolutionID || ev.RecoveryCommitID != commit.CommitID || ev.CanonicalCommitRef == "" {
		t.Fatalf("receipt chain mismatch: %+v", ev)
	}
	if len(ev.RuntimeEvidenceIDs) != 4 {
		t.Fatalf("runtime evidence not bound: %v", ev.RuntimeEvidenceIDs)
	}
}

func TestLateCandidateMakesAssessmentStaleUntilReassessed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	validator := &permissiveValidator{}
	reconciler := &deterministicReconciler{}
	recovery := &exactOnceRecovery{}
	c, _ := openCoordinator(t, path, validator, reconciler, recovery)
	a := baseRequest()
	if _, err := c.Collect(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Assess(context.Background(), a.TaskID, a.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	b := a
	b.ExecutionID = "execution-late"
	b.ResultID = "result-late"
	b.RouteDecisionID = "route-late"
	b.RouteDecisionSHA = "route-sha-late"
	b.EngineID = "engine-late"
	b.EngineIdentitySHA = "engine-sha-late"
	b.ResultSHA = "result-sha-late"
	setResultPayload(&b, "late candidate")
	if _, err := c.Collect(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitResolved(context.Background(), a.TaskID, a.HandoffPackageID); !errors.Is(err, ErrStaleAssessment) {
		t.Fatalf("late candidate bypassed reconciliation: %v", err)
	}
	if _, err := c.Assess(context.Background(), a.TaskID, a.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsBrokenAppendChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	a := baseRequest()
	if _, err := c.Collect(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Collect(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) < 2 {
		t.Fatal("expected at least two events")
	}
	if err := os.WriteFile(path, append(lines[1], '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path); err == nil {
		t.Fatal("broken append chain was accepted")
	}
}

func TestConcurrentDuplicateCollectionCreatesOneCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	validator := &permissiveValidator{}
	c, store := openCoordinator(t, path, validator, &deterministicReconciler{}, &exactOnceRecovery{})
	req := baseRequest()
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	ids := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := c.Collect(context.Background(), req)
			if err != nil {
				errs <- err
				return
			}
			ids <- out.Candidate.CandidateID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		} else if id != first {
			t.Fatalf("different candidate IDs: %s %s", first, id)
		}
	}
	if got := len(store.CandidatesForScope(req.TaskID, req.HandoffPackageID)); got != 1 {
		t.Fatalf("concurrent duplicate collection created %d candidates", got)
	}
}

type blockingRecovery struct {
	inner   *exactOnceRecovery
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingRecovery) CommitResolvedCandidate(ctx context.Context, in ResolvedRecoveryInput) (RecoveryCommit, error) {
	r.once.Do(func() { close(r.entered) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return RecoveryCommit{}, ctx.Err()
	}
	return r.inner.CommitResolvedCandidate(ctx, in)
}

func rewriteSingleEvent(t *testing.T, path string, mutate func(*storeEvent)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected one event, got %d", len(lines))
	}
	var ev storeEvent
	if err := json.Unmarshal(lines[0], &ev); err != nil {
		t.Fatal(err)
	}
	mutate(&ev)
	ev.EventID = ""
	ev.EventSHA = ""
	sha, err := canonicalSHA(ev)
	if err != nil {
		t.Fatal(err)
	}
	ev.EventSHA = sha
	ev.EventID = prefixedID("event", sha)
	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCommittedScopeRejectsLateMutationAndKeepsReceiptStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	if _, err := c.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	assessment, err := c.Assess(context.Background(), req.TaskID, req.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeReceipt, err := c.ReceiptEvidence(req.TaskID, req.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}

	late := req
	late.ExecutionID = "execution-late-after-commit"
	late.ResultID = "result-late-after-commit"
	late.RouteDecisionID = "route-late-after-commit"
	late.RouteDecisionSHA = "route-sha-late-after-commit"
	late.EngineID = "engine-late-after-commit"
	late.EngineIdentitySHA = "engine-sha-late-after-commit"
	late.ResultSHA = "result-sha-late-after-commit"
	setResultPayload(&late, "late after commit")
	if _, err := c.Collect(context.Background(), late); !errors.Is(err, ErrScopeCommitted) {
		t.Fatalf("late candidate changed committed scope: %v", err)
	}
	if _, err := c.Assess(context.Background(), req.TaskID, req.HandoffPackageID); !errors.Is(err, ErrScopeCommitted) {
		t.Fatalf("post-commit assessment changed committed scope: %v", err)
	}
	if _, err := c.ReviewRequest(req.TaskID, req.HandoffPackageID); !errors.Is(err, ErrScopeCommitted) {
		t.Fatalf("post-commit review changed committed scope: %v", err)
	}
	if _, err := c.ResolveReview(ReviewerResolutionRequest{
		AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA,
		SelectedCandidateID: assessment.SelectedCandidateID, ReviewerRef: "reviewer:late",
		DecisiveEvidenceIDs: []string{"evidence-late"}, Rationale: "must not mutate committed scope",
	}); !errors.Is(err, ErrScopeCommitted) {
		t.Fatalf("post-commit resolution changed committed scope: %v", err)
	}

	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterReceipt, err := c.ReceiptEvidence(req.TaskID, req.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("rejected post-commit mutation changed durable store bytes")
	}
	if !reflect.DeepEqual(beforeReceipt, afterReceipt) {
		t.Fatalf("committed receipt changed after rejected mutations:\nbefore=%+v\nafter=%+v", beforeReceipt, afterReceipt)
	}
}

func TestConcurrentCommitAndLateCollectionCannotCrossLifecycleBarrier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	inner := &exactOnceRecovery{}
	recovery := &blockingRecovery{inner: inner, entered: make(chan struct{}), release: make(chan struct{})}
	c, store := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, recovery)
	if _, err := c.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Assess(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}

	commitErr := make(chan error, 1)
	go func() {
		_, err := c.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID)
		commitErr <- err
	}()
	<-recovery.entered

	late := req
	late.ExecutionID = "execution-racing-late"
	late.ResultID = "result-racing-late"
	late.RouteDecisionID = "route-racing-late"
	late.RouteDecisionSHA = "route-sha-racing-late"
	late.EngineID = "engine-racing-late"
	late.EngineIdentitySHA = "engine-sha-racing-late"
	late.ResultSHA = "result-sha-racing-late"
	setResultPayload(&late, "racing late candidate")
	collectErr := make(chan error, 1)
	go func() {
		_, err := c.Collect(context.Background(), late)
		collectErr <- err
	}()

	close(recovery.release)
	if err := <-commitErr; err != nil {
		t.Fatal(err)
	}
	if err := <-collectErr; !errors.Is(err, ErrScopeCommitted) {
		t.Fatalf("late collection crossed canonical commit barrier: %v", err)
	}
	if got := len(store.CandidatesForScope(req.TaskID, req.HandoffPackageID)); got != 1 {
		t.Fatalf("race inserted %d candidates", got)
	}
}

func TestReceiptRejectsStaleAssessmentUntilCurrentCandidateSetIsReassessed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	a := baseRequest()
	if _, err := c.Collect(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Assess(context.Background(), a.TaskID, a.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	b := a
	b.ExecutionID = "execution-receipt-late"
	b.ResultID = "result-receipt-late"
	b.RouteDecisionID = "route-receipt-late"
	b.RouteDecisionSHA = "route-sha-receipt-late"
	b.EngineID = "engine-receipt-late"
	b.EngineIdentitySHA = "engine-sha-receipt-late"
	b.ResultSHA = "result-sha-receipt-late"
	setResultPayload(&b, "receipt late candidate")
	if _, err := c.Collect(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReceiptEvidence(a.TaskID, a.HandoffPackageID); !errors.Is(err, ErrStaleAssessment) {
		t.Fatalf("stale receipt evidence was emitted: %v", err)
	}
	assessment, err := c.Assess(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := c.ReceiptEvidence(a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if ev.CandidateSetSHA != assessment.CandidateSetSHA || len(ev.CandidateIDs) != 2 {
		t.Fatalf("receipt did not bind current candidate set: %+v", ev)
	}
}

func TestStoreReplayRejectsUnknownEventKindEvenWithRecomputedEventHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	if _, err := c.Collect(context.Background(), baseRequest()); err != nil {
		t.Fatal(err)
	}
	rewriteSingleEvent(t, path, func(ev *storeEvent) { ev.Kind = eventKind("future_unknown_kind") })
	if _, err := OpenStore(path); !errors.Is(err, ErrMalformedStoreEvent) {
		t.Fatalf("unknown event kind was accepted: %v", err)
	}
}

func TestStoreReplayRejectsCandidateIdentityTamperEvenWithRecomputedEventHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	if _, err := c.Collect(context.Background(), baseRequest()); err != nil {
		t.Fatal(err)
	}
	rewriteSingleEvent(t, path, func(ev *storeEvent) { ev.Candidate.EngineID = "tampered-engine" })
	if _, err := OpenStore(path); !errors.Is(err, ErrMalformedStoreEvent) {
		t.Fatalf("candidate identity tamper was accepted: %v", err)
	}
}

func TestStoreReplayRejectsRecoveryCommitResolutionBindingTamperAfterRehash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	a := baseRequest()
	b := a
	b.ExecutionID = "execution-binding-b"
	b.ResultID = "result-binding-b"
	b.RouteDecisionID = "route-binding-b"
	b.RouteDecisionSHA = "route-sha-binding-b"
	b.EngineID = "engine-binding-b"
	b.EngineIdentitySHA = "engine-sha-binding-b"
	b.ResultSHA = "result-sha-binding-b"
	setResultPayload(&b, "candidate binding B")
	oa, err := c.Collect(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Collect(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	assessment, err := c.Assess(context.Background(), a.TaskID, a.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := c.ResolveReview(ReviewerResolutionRequest{
		AssessmentID: assessment.AssessmentID, AssessmentSHA: assessment.AssessmentSHA,
		SelectedCandidateID: oa.Candidate.CandidateID, ReviewerRef: "reviewer:binding",
		DecisiveEvidenceIDs: []string{"decisive-binding"}, Rationale: "exact binding evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitResolved(context.Background(), a.TaskID, a.HandoffPackageID); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	var ev storeEvent
	if err := json.Unmarshal(lines[len(lines)-1], &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Kind != eventRecoveryCommitted || ev.ResolutionID != resolution.ResolutionID {
		t.Fatalf("unexpected final event: %+v", ev)
	}
	ev.ResolutionID = "resolution-tampered"
	ev.EventID = ""
	ev.EventSHA = ""
	sha, err := canonicalSHA(ev)
	if err != nil {
		t.Fatal(err)
	}
	ev.EventSHA = sha
	ev.EventID = prefixedID("event", sha)
	last, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	lines[len(lines)-1] = last
	if err := os.WriteFile(path, append(bytes.Join(lines, []byte("\n")), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path); !errors.Is(err, ErrMalformedStoreEvent) {
		t.Fatalf("recovery resolution binding tamper was accepted after rehash: %v", err)
	}
}

func rewriteEventChainFrom(t *testing.T, path string, match func(storeEvent) bool, mutate func(*storeEvent)) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	events := make([]storeEvent, len(lines))
	target := -1
	for i, line := range lines {
		if err := json.Unmarshal(line, &events[i]); err != nil {
			t.Fatal(err)
		}
		if target == -1 && match(events[i]) {
			target = i
		}
	}
	if target == -1 {
		t.Fatal("target event not found")
	}
	mutate(&events[target])
	for i := target; i < len(events); i++ {
		if i == 0 {
			events[i].PreviousEventSHA = ""
		} else {
			events[i].PreviousEventSHA = events[i-1].EventSHA
		}
		events[i].EventID = ""
		events[i].EventSHA = ""
		sha, err := canonicalSHA(events[i])
		if err != nil {
			t.Fatal(err)
		}
		events[i].EventSHA = sha
		events[i].EventID = prefixedID("event", sha)
	}
	out := make([]byte, 0, len(data))
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCollectRejectsResultPayloadDigestMismatchBeforeValidationAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	validator := &permissiveValidator{}
	c, store := openCoordinator(t, path, validator, &deterministicReconciler{}, &exactOnceRecovery{})
	req := baseRequest()
	req.ResultPayload = "tampered before collection"
	if _, err := c.Collect(context.Background(), req); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("payload digest mismatch was accepted: %v", err)
	}
	if validator.calls != 0 {
		t.Fatalf("current-state validator ran before local payload integrity gate: %d", validator.calls)
	}
	if got := len(store.CandidatesForScope(req.TaskID, req.HandoffPackageID)); got != 0 {
		t.Fatalf("persisted %d candidates with invalid payload digest", got)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 0 {
		t.Fatalf("store changed before payload integrity passed: %q", string(b))
	}
}

func TestStoreReplayRejectsResultPayloadTamperEvenWithRecomputedEventChain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	if _, err := c.Collect(context.Background(), baseRequest()); err != nil {
		t.Fatal(err)
	}
	rewriteEventChainFrom(t, path, func(ev storeEvent) bool { return ev.Kind == eventCandidateCollected }, func(ev *storeEvent) {
		ev.Candidate.ResultPayload = "payload changed after persistence"
	})
	if _, err := OpenStore(path); !errors.Is(err, ErrMalformedStoreEvent) {
		t.Fatalf("payload tamper was accepted after full event-chain rehash: %v", err)
	}
}

func TestDurableRecoveryPreparationFreezesScopeAndRestartReusesExactOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	req := baseRequest()
	recovery := &exactOnceRecovery{}
	ambiguous := errors.New("ambiguous post-commit transport failure")
	c1, store1 := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &commitThenErrorRecovery{inner: recovery, err: ambiguous})
	if _, err := c1.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := c1.Assess(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	if _, err := c1.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID); !errors.Is(err, ambiguous) {
		t.Fatalf("expected ambiguous error after external commit, got %v", err)
	}
	preparation, ok := store1.RecoveryPreparationForScope(req.TaskID, req.HandoffPackageID)
	if !ok || preparation.RecoveryOperationID == "" || preparation.RecoveryOperationSHA == "" {
		t.Fatalf("missing durable recovery preparation after crash: %+v", preparation)
	}
	if len(recovery.unique) != 1 {
		t.Fatalf("recovery did not record one exact operation before crash: %d", len(recovery.unique))
	}
	if _, ok := recovery.unique[preparation.RecoveryOperationID]; !ok {
		t.Fatalf("recovery operation differs from durable preparation: %+v", preparation)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	late := req
	late.ExecutionID = "execution-after-preparation"
	late.ResultID = "result-after-preparation"
	late.RouteDecisionID = "route-after-preparation"
	late.RouteDecisionSHA = "route-sha-after-preparation"
	setResultPayload(&late, "late after preparation")
	if _, err := c1.Collect(context.Background(), late); !errors.Is(err, ErrRecoveryPrepared) {
		t.Fatalf("late collection was not blocked by recovery preparation: %v", err)
	}
	if _, err := c1.Assess(context.Background(), req.TaskID, req.HandoffPackageID); !errors.Is(err, ErrRecoveryPrepared) {
		t.Fatalf("reassessment was not blocked by recovery preparation: %v", err)
	}
	if _, err := c1.ReviewRequest(req.TaskID, req.HandoffPackageID); !errors.Is(err, ErrRecoveryPrepared) {
		t.Fatalf("review request was not blocked by recovery preparation: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected post-preparation mutation changed durable bytes")
	}

	c2, store2 := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, recovery)
	commit, err := c2.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if commit.CommitID == "" || len(recovery.unique) != 1 || recovery.calls != 2 {
		t.Fatalf("restart did not reconcile the exact prepared operation: commit=%+v calls=%d unique=%d", commit, recovery.calls, len(recovery.unique))
	}
	persisted, ok := store2.RecoveryPreparationForScope(req.TaskID, req.HandoffPackageID)
	if !ok || persisted != preparation {
		t.Fatalf("restart changed durable preparation: before=%+v after=%+v", preparation, persisted)
	}
	evidence, err := c2.ReceiptEvidence(req.TaskID, req.HandoffPackageID)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.RecoveryPreparationID != preparation.PreparationID || evidence.RecoveryOperationID != preparation.RecoveryOperationID || evidence.RecoveryCommitID != commit.CommitID {
		t.Fatalf("receipt did not bind preparation -> operation -> commit: %+v", evidence)
	}
}

func TestStoreReplayRejectsRecoveryPreparationOperationTamperAfterFullRehash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	req := baseRequest()
	if _, err := c.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Assess(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	rewriteEventChainFrom(t, path, func(ev storeEvent) bool { return ev.Kind == eventRecoveryPrepared }, func(ev *storeEvent) {
		p := ev.Preparation
		p.RecoveryOperationSHA = strings.Repeat("a", 64)
		p.RecoveryOperationID = prefixedID("recovery-operation", p.RecoveryOperationSHA)
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
			t.Fatal(err)
		}
		p.PreparationSHA = sha
		p.PreparationID = prefixedID("recovery-preparation", sha)
	})
	if _, err := OpenStore(path); !errors.Is(err, ErrMalformedStoreEvent) {
		t.Fatalf("forged recovery operation was accepted after full chain rehash: %v", err)
	}
}

func TestStoreReplayRejectsRecoveryCommitPreparationBindingTamperAfterRehash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collection.jsonl")
	c, _ := openCoordinator(t, path, &permissiveValidator{}, &deterministicReconciler{}, &exactOnceRecovery{})
	req := baseRequest()
	if _, err := c.Collect(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Assess(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CommitResolved(context.Background(), req.TaskID, req.HandoffPackageID); err != nil {
		t.Fatal(err)
	}
	rewriteEventChainFrom(t, path, func(ev storeEvent) bool { return ev.Kind == eventRecoveryCommitted }, func(ev *storeEvent) {
		ev.PreparationID = "recovery-preparation-tampered"
	})
	if _, err := OpenStore(path); !errors.Is(err, ErrMalformedStoreEvent) {
		t.Fatalf("recovery commit preparation binding tamper was accepted after rehash: %v", err)
	}
}
