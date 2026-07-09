package candidatecollection

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"keydeck.local/feasibilitylab/internal/proofreceipt"
)

type eventKind string

const (
	eventCandidateCollected eventKind = "candidate_collected"
	eventCandidateReused    eventKind = "candidate_reused"
	eventAssessmentRecorded eventKind = "assessment_recorded"
	eventResolutionRecorded eventKind = "resolution_recorded"
	eventRecoveryPrepared   eventKind = "recovery_prepared"
	eventRecoveryCommitted  eventKind = "recovery_committed"
)

type storeEvent struct {
	EventID          string                     `json:"event_id"`
	EventSHA         string                     `json:"event_sha256"`
	Sequence         uint64                     `json:"sequence"`
	PreviousEventSHA string                     `json:"previous_event_sha256,omitempty"`
	Kind             eventKind                  `json:"kind"`
	TaskID           string                     `json:"task_id"`
	PackageID        string                     `json:"handoff_package_id"`
	Candidate        *CandidateRecord           `json:"candidate,omitempty"`
	ReusedID         string                     `json:"reused_candidate_id,omitempty"`
	Assessment       *AssessmentRecord          `json:"assessment,omitempty"`
	Resolution       *ResolutionRecord          `json:"resolution,omitempty"`
	Preparation      *RecoveryPreparationRecord `json:"recovery_preparation,omitempty"`
	Recovery         *RecoveryCommit            `json:"recovery_commit,omitempty"`
	PreparationID    string                     `json:"preparation_id,omitempty"`
	AssessmentID     string                     `json:"assessment_id,omitempty"`
	ResolutionID     string                     `json:"resolution_id,omitempty"`
	CreatedAtUTC     string                     `json:"created_at_utc"`
}

type Store struct {
	mu                      sync.Mutex
	path                    string
	events                  []storeEvent
	candidates              map[string]CandidateRecord
	byExecResult            map[string]string
	assessments             map[string]AssessmentRecord
	latestAssessmentByScope map[string]string
	resolutions             map[string]ResolutionRecord
	resolutionByAssessment  map[string]string
	preparations            map[string]RecoveryPreparationRecord
	preparationByScope      map[string]string
	commits                 map[string]RecoveryCommit
	commitCandidateByScope  map[string]string
}

func OpenStore(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: empty store path", ErrInvalidInput)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := newStore(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReader(f))
	var expectedSequence uint64 = 1
	var expectedPrevious string
	for {
		var ev storeEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode store: %w", err)
		}
		if err := s.verifyEvent(ev, expectedSequence, expectedPrevious); err != nil {
			return nil, err
		}
		if err := s.validateEventAgainstState(ev); err != nil {
			return nil, err
		}
		s.events = append(s.events, ev)
		s.apply(ev)
		expectedSequence++
		expectedPrevious = ev.EventSHA
	}
	return s, nil
}

func newStore(path string) *Store {
	return &Store{
		path:                    path,
		candidates:              make(map[string]CandidateRecord),
		byExecResult:            make(map[string]string),
		assessments:             make(map[string]AssessmentRecord),
		latestAssessmentByScope: make(map[string]string),
		resolutions:             make(map[string]ResolutionRecord),
		resolutionByAssessment:  make(map[string]string),
		preparations:            make(map[string]RecoveryPreparationRecord),
		preparationByScope:      make(map[string]string),
		commits:                 make(map[string]RecoveryCommit),
		commitCandidateByScope:  make(map[string]string),
	}
}

func execResultKey(executionID, resultID string) string { return executionID + "\x00" + resultID }
func scopeKey(taskID, packageID string) string          { return taskID + "\x00" + packageID }

func (s *Store) verifyEvent(ev storeEvent, expectedSequence uint64, expectedPrevious string) error {
	if ev.Sequence != expectedSequence {
		return fmt.Errorf("store event sequence failure for %s: got %d want %d", ev.EventID, ev.Sequence, expectedSequence)
	}
	if ev.PreviousEventSHA != expectedPrevious {
		return fmt.Errorf("store event chain failure for %s", ev.EventID)
	}
	unsigned := ev
	unsigned.EventID = ""
	unsigned.EventSHA = ""
	sha, err := canonicalSHA(unsigned)
	if err != nil {
		return err
	}
	if sha != ev.EventSHA || prefixedID("event", sha) != ev.EventID {
		return fmt.Errorf("store event integrity failure for %s", ev.EventID)
	}
	return nil
}

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformedStoreEvent, fmt.Sprintf(format, args...))
}

func (s *Store) validateEventAgainstState(ev storeEvent) error {
	if strings.TrimSpace(ev.TaskID) == "" || strings.TrimSpace(ev.PackageID) == "" || strings.TrimSpace(ev.CreatedAtUTC) == "" {
		return malformed("event has empty scope or timestamp")
	}
	scope := scopeKey(ev.TaskID, ev.PackageID)
	if _, committed := s.commitCandidateByScope[scope]; committed {
		return fmt.Errorf("%w: durable event %s appears after canonical commit", ErrScopeCommitted, ev.EventID)
	}
	if preparationID, prepared := s.preparationByScope[scope]; prepared && ev.Kind != eventRecoveryCommitted {
		return fmt.Errorf("%w: durable event %s appears after recovery preparation %s", ErrRecoveryPrepared, ev.EventID, preparationID)
	}

	hasCandidate := ev.Candidate != nil
	hasAssessment := ev.Assessment != nil
	hasResolution := ev.Resolution != nil
	hasPreparation := ev.Preparation != nil
	hasRecovery := ev.Recovery != nil

	switch ev.Kind {
	case eventCandidateCollected:
		if !hasCandidate || hasAssessment || hasResolution || hasPreparation || hasRecovery || ev.ReusedID != "" || ev.PreparationID != "" || ev.AssessmentID != "" || ev.ResolutionID != "" {
			return malformed("candidate_collected event has invalid payload shape")
		}
		if err := validateCandidateRecord(*ev.Candidate); err != nil {
			return err
		}
		c := *ev.Candidate
		if c.TaskID != ev.TaskID || c.HandoffPackageID != ev.PackageID {
			return malformed("candidate scope does not match event scope")
		}
		if existingID, ok := s.byExecResult[execResultKey(c.ExecutionID, c.ResultID)]; ok {
			return fmt.Errorf("%w: execution/result already belongs to %s", ErrIdentityConflict, existingID)
		}
		if _, ok := s.candidates[c.CandidateID]; ok {
			return fmt.Errorf("%w: duplicate collected candidate %s", ErrIdentityConflict, c.CandidateID)
		}

	case eventCandidateReused:
		if hasCandidate || hasAssessment || hasResolution || hasPreparation || hasRecovery || strings.TrimSpace(ev.ReusedID) == "" || ev.PreparationID != "" || ev.AssessmentID != "" || ev.ResolutionID != "" {
			return malformed("candidate_reused event has invalid payload shape")
		}
		c, ok := s.candidates[ev.ReusedID]
		if !ok || c.TaskID != ev.TaskID || c.HandoffPackageID != ev.PackageID {
			return malformed("reused candidate is missing or out of scope")
		}

	case eventAssessmentRecorded:
		if hasCandidate || !hasAssessment || hasResolution || hasPreparation || hasRecovery || ev.ReusedID != "" || ev.PreparationID != "" || ev.AssessmentID != "" || ev.ResolutionID != "" {
			return malformed("assessment_recorded event has invalid payload shape")
		}
		a := *ev.Assessment
		if err := validateAssessmentRecord(a); err != nil {
			return err
		}
		if a.TaskID != ev.TaskID || a.HandoffPackageID != ev.PackageID {
			return malformed("assessment scope does not match event scope")
		}
		candidates := s.candidatesForScopeUnlocked(ev.TaskID, ev.PackageID)
		setSHA, ids, err := candidateSetSHA(candidates)
		if err != nil {
			return err
		}
		if setSHA != a.CandidateSetSHA || !slices.Equal(ids, a.CandidateIDs) {
			return fmt.Errorf("%w: assessment does not bind the current candidate set", ErrStaleAssessment)
		}
		if existing, ok := s.assessments[a.AssessmentID]; ok && existing.AssessmentSHA != a.AssessmentSHA {
			return fmt.Errorf("%w: conflicting assessment identity", ErrIdentityConflict)
		}

	case eventResolutionRecorded:
		if hasCandidate || hasAssessment || !hasResolution || hasPreparation || hasRecovery || ev.ReusedID != "" || ev.PreparationID != "" || ev.AssessmentID != "" || ev.ResolutionID != "" {
			return malformed("resolution_recorded event has invalid payload shape")
		}
		r := *ev.Resolution
		if err := validateResolutionRecord(r); err != nil {
			return err
		}
		a, ok := s.assessments[r.AssessmentID]
		if !ok || a.AssessmentSHA != r.AssessmentSHA || a.TaskID != ev.TaskID || a.HandoffPackageID != ev.PackageID {
			return malformed("resolution does not bind an exact in-scope assessment")
		}
		if a.State != AssessmentDisagreement && a.State != AssessmentNeedsReview {
			return malformed("resolution attached to assessment that does not require review")
		}
		if !contains(a.CandidateIDs, r.SelectedCandidateID) {
			return ErrCandidateNotInAssessment
		}
		if err := s.ensureAssessmentCurrentUnlocked(a); err != nil {
			return err
		}
		if _, ok := s.resolutionByAssessment[r.AssessmentID]; ok {
			return fmt.Errorf("%w: assessment already has a durable resolution", ErrIdentityConflict)
		}

	case eventRecoveryPrepared:
		if hasCandidate || hasAssessment || hasResolution || !hasPreparation || hasRecovery || ev.ReusedID != "" || ev.PreparationID != "" || ev.AssessmentID != "" || ev.ResolutionID != "" {
			return malformed("recovery_prepared event has invalid payload shape")
		}
		preparation := *ev.Preparation
		if err := validateRecoveryPreparation(preparation); err != nil {
			return err
		}
		if preparation.TaskID != ev.TaskID || preparation.HandoffPackageID != ev.PackageID {
			return malformed("recovery preparation scope does not match event scope")
		}
		candidate, ok := s.candidates[preparation.CandidateID]
		if !ok || candidate.CandidateSHA != preparation.CandidateSHA || candidate.TaskID != ev.TaskID || candidate.HandoffPackageID != ev.PackageID {
			return malformed("recovery preparation candidate is missing, mismatched, or out of scope")
		}
		assessment, ok := s.assessments[preparation.AssessmentID]
		if !ok || assessment.AssessmentSHA != preparation.AssessmentSHA || assessment.TaskID != ev.TaskID || assessment.HandoffPackageID != ev.PackageID {
			return malformed("recovery preparation assessment is missing, mismatched, or out of scope")
		}
		if err := s.ensureAssessmentCurrentUnlocked(assessment); err != nil {
			return err
		}
		var resolution *ResolutionRecord
		if preparation.ResolutionID != "" {
			r, ok := s.resolutions[preparation.ResolutionID]
			if !ok || r.ResolutionSHA != preparation.ResolutionSHA || r.AssessmentID != assessment.AssessmentID {
				return malformed("recovery preparation resolution is missing or mismatched")
			}
			resolution = &r
		} else if preparation.ResolutionSHA != "" {
			return malformed("recovery preparation has resolution SHA without resolution ID")
		}
		input, err := resolvedRecoveryInputFromRecords(candidate, assessment, resolution)
		if err != nil {
			return err
		}
		if input.RecoveryOperationID != preparation.RecoveryOperationID || input.RecoveryOperationSHA != preparation.RecoveryOperationSHA {
			return malformed("recovery preparation does not bind the exact resolved recovery input")
		}
		if _, exists := s.preparations[preparation.PreparationID]; exists {
			return fmt.Errorf("%w: duplicate recovery preparation identity", ErrIdentityConflict)
		}

	case eventRecoveryCommitted:
		if hasCandidate || hasAssessment || hasResolution || hasPreparation || !hasRecovery || strings.TrimSpace(ev.ReusedID) == "" || strings.TrimSpace(ev.PreparationID) == "" || strings.TrimSpace(ev.AssessmentID) == "" {
			return malformed("recovery_committed event has invalid payload shape")
		}
		if err := validateRecoveryCommit(*ev.Recovery); err != nil {
			return err
		}
		preparation, ok := s.preparations[ev.PreparationID]
		if !ok || preparation.TaskID != ev.TaskID || preparation.HandoffPackageID != ev.PackageID {
			return malformed("recovery commit is missing exact durable preparation")
		}
		if preparation.CandidateID != ev.ReusedID || preparation.AssessmentID != ev.AssessmentID || preparation.ResolutionID != ev.ResolutionID {
			return malformed("recovery commit does not match exact durable preparation bindings")
		}
		candidate, ok := s.candidates[ev.ReusedID]
		if !ok || candidate.CandidateSHA != preparation.CandidateSHA || candidate.TaskID != ev.TaskID || candidate.HandoffPackageID != ev.PackageID {
			return malformed("recovery commit candidate is missing or out of scope")
		}
		assessment, ok := s.assessments[ev.AssessmentID]
		if !ok || assessment.AssessmentSHA != preparation.AssessmentSHA || assessment.TaskID != ev.TaskID || assessment.HandoffPackageID != ev.PackageID {
			return malformed("recovery commit assessment is missing or out of scope")
		}
		if err := s.ensureAssessmentCurrentUnlocked(assessment); err != nil {
			return err
		}
		var resolution *ResolutionRecord
		if ev.ResolutionID != "" {
			r, ok := s.resolutions[ev.ResolutionID]
			if !ok || r.ResolutionSHA != preparation.ResolutionSHA || r.AssessmentID != assessment.AssessmentID {
				return malformed("recovery commit resolution is missing or mismatched")
			}
			resolution = &r
		}
		input, err := resolvedRecoveryInputFromRecords(candidate, assessment, resolution)
		if err != nil {
			return err
		}
		if input.RecoveryOperationID != preparation.RecoveryOperationID || input.RecoveryOperationSHA != preparation.RecoveryOperationSHA {
			return malformed("recovery commit preparation does not bind exact recovery operation")
		}
		if _, ok := s.commits[ev.Recovery.CommitID]; ok {
			return fmt.Errorf("%w: duplicate recovery commit identity", ErrIdentityConflict)
		}

	default:
		return malformed("unknown event kind %q", ev.Kind)
	}
	return nil
}

func (s *Store) ensureAssessmentCurrentUnlocked(a AssessmentRecord) error {
	candidates := s.candidatesForScopeUnlocked(a.TaskID, a.HandoffPackageID)
	if len(candidates) == 0 {
		return ErrNotFound
	}
	setSHA, ids, err := candidateSetSHA(candidates)
	if err != nil {
		return err
	}
	if setSHA != a.CandidateSetSHA || !slices.Equal(ids, a.CandidateIDs) {
		return ErrStaleAssessment
	}
	return nil
}

func (s *Store) candidatesForScopeUnlocked(taskID, packageID string) []CandidateRecord {
	out := make([]CandidateRecord, 0)
	for _, c := range s.candidates {
		if c.TaskID == taskID && c.HandoffPackageID == packageID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CandidateID < out[j].CandidateID })
	return out
}

func (s *Store) apply(ev storeEvent) {
	switch ev.Kind {
	case eventCandidateCollected:
		c := *ev.Candidate
		s.candidates[c.CandidateID] = c
		s.byExecResult[execResultKey(c.ExecutionID, c.ResultID)] = c.CandidateID
	case eventAssessmentRecorded:
		a := *ev.Assessment
		s.assessments[a.AssessmentID] = a
		s.latestAssessmentByScope[scopeKey(a.TaskID, a.HandoffPackageID)] = a.AssessmentID
	case eventResolutionRecorded:
		r := *ev.Resolution
		s.resolutions[r.ResolutionID] = r
		s.resolutionByAssessment[r.AssessmentID] = r.ResolutionID
	case eventRecoveryPrepared:
		p := *ev.Preparation
		s.preparations[p.PreparationID] = p
		s.preparationByScope[scopeKey(p.TaskID, p.HandoffPackageID)] = p.PreparationID
	case eventRecoveryCommitted:
		c := *ev.Recovery
		s.commits[c.CommitID] = c
		s.commitCandidateByScope[scopeKey(ev.TaskID, ev.PackageID)] = ev.ReusedID
	}
}

func (s *Store) appendEvent(ev storeEvent) (storeEvent, error) {
	ev.Sequence = uint64(len(s.events) + 1)
	if len(s.events) > 0 {
		ev.PreviousEventSHA = s.events[len(s.events)-1].EventSHA
	}
	if err := s.validateEventAgainstState(ev); err != nil {
		return storeEvent{}, err
	}
	unsigned := ev
	unsigned.EventID = ""
	unsigned.EventSHA = ""
	sha, err := canonicalSHA(unsigned)
	if err != nil {
		return storeEvent{}, err
	}
	ev.EventSHA = sha
	ev.EventID = prefixedID("event", sha)
	b, err := json.Marshal(ev)
	if err != nil {
		return storeEvent{}, err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return storeEvent{}, err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return storeEvent{}, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return storeEvent{}, err
	}
	if err := f.Close(); err != nil {
		return storeEvent{}, err
	}
	s.events = append(s.events, ev)
	s.apply(ev)
	return ev, nil
}

func (s *Store) ReceiptArtifact() (proofreceipt.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return proofreceipt.Artifact{}, err
	}
	sum := sha256.Sum256(raw)
	return proofreceipt.Artifact{Name: "candidate collection store", Path: s.path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw))}, nil
}

func (s *Store) FindByExecutionResult(executionID, resultID string) (CandidateRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byExecResult[execResultKey(executionID, resultID)]
	if !ok {
		return CandidateRecord{}, false
	}
	c, ok := s.candidates[id]
	return c, ok
}

func (s *Store) AddCandidate(candidate CandidateRecord, now string) (storeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEvent(storeEvent{Kind: eventCandidateCollected, TaskID: candidate.TaskID, PackageID: candidate.HandoffPackageID, Candidate: &candidate, CreatedAtUTC: now})
}

func (s *Store) AddReuse(candidate CandidateRecord, now string) (storeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEvent(storeEvent{Kind: eventCandidateReused, TaskID: candidate.TaskID, PackageID: candidate.HandoffPackageID, ReusedID: candidate.CandidateID, CreatedAtUTC: now})
}

func (s *Store) CandidatesForScope(taskID, packageID string) []CandidateRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.candidatesForScopeUnlocked(taskID, packageID)
}

func (s *Store) Candidate(id string) (CandidateRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.candidates[id]
	return c, ok
}

func (s *Store) AddAssessment(a AssessmentRecord, now string) (storeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEvent(storeEvent{Kind: eventAssessmentRecorded, TaskID: a.TaskID, PackageID: a.HandoffPackageID, Assessment: &a, CreatedAtUTC: now})
}

func (s *Store) LatestAssessment(taskID, packageID string) (AssessmentRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.latestAssessmentByScope[scopeKey(taskID, packageID)]
	if !ok {
		return AssessmentRecord{}, false
	}
	a, ok := s.assessments[id]
	return a, ok
}

func (s *Store) Assessment(id string) (AssessmentRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assessments[id]
	return a, ok
}

func (s *Store) AddResolution(r ResolutionRecord, taskID, packageID, now string) (storeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEvent(storeEvent{Kind: eventResolutionRecorded, TaskID: taskID, PackageID: packageID, Resolution: &r, CreatedAtUTC: now})
}

func (s *Store) ResolutionForAssessment(assessmentID string) (ResolutionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.resolutionByAssessment[assessmentID]
	if !ok {
		return ResolutionRecord{}, false
	}
	r, ok := s.resolutions[id]
	return r, ok
}

func (s *Store) AddRecoveryPreparation(preparation RecoveryPreparationRecord, now string) (storeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEvent(storeEvent{
		Kind: eventRecoveryPrepared, TaskID: preparation.TaskID, PackageID: preparation.HandoffPackageID,
		Preparation: &preparation, CreatedAtUTC: now,
	})
}

func (s *Store) RecoveryPreparationForScope(taskID, packageID string) (RecoveryPreparationRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.preparationByScope[scopeKey(taskID, packageID)]
	if !ok {
		return RecoveryPreparationRecord{}, false
	}
	p, ok := s.preparations[id]
	return p, ok
}

func (s *Store) AddRecoveryCommit(taskID, packageID, candidateID, preparationID, assessmentID, resolutionID string, commit RecoveryCommit, now string) (storeEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendEvent(storeEvent{
		Kind: eventRecoveryCommitted, TaskID: taskID, PackageID: packageID,
		ReusedID: candidateID, PreparationID: preparationID, AssessmentID: assessmentID, ResolutionID: resolutionID,
		Recovery: &commit, CreatedAtUTC: now,
	})
}

func (s *Store) CommittedCandidate(taskID, packageID string) (string, RecoveryCommit, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.commitCandidateByScope[scopeKey(taskID, packageID)]
	if !ok {
		return "", RecoveryCommit{}, false
	}
	for i := len(s.events) - 1; i >= 0; i-- {
		ev := s.events[i]
		if ev.Kind == eventRecoveryCommitted && ev.TaskID == taskID && ev.PackageID == packageID && ev.ReusedID == id && ev.Recovery != nil {
			return id, *ev.Recovery, true
		}
	}
	return "", RecoveryCommit{}, false
}

func (s *Store) Evidence(taskID, packageID string) (ReceiptEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates := s.candidatesForScopeUnlocked(taskID, packageID)
	if len(candidates) == 0 {
		return ReceiptEvidence{}, ErrNotFound
	}
	setSHA, candidateIDs, err := candidateSetSHA(candidates)
	if err != nil {
		return ReceiptEvidence{}, err
	}
	ev := ReceiptEvidence{CandidateIDs: candidateIDs, CandidateSetSHA: setSHA}
	for _, c := range candidates {
		ev.RuntimeEvidenceIDs = append(ev.RuntimeEvidenceIDs, c.RuntimeEvidenceIDs...)
	}
	for _, e := range s.events {
		if e.TaskID == taskID && e.PackageID == packageID {
			ev.CollectionEventIDs = append(ev.CollectionEventIDs, e.EventID)
		}
	}

	if assessmentID, ok := s.latestAssessmentByScope[scopeKey(taskID, packageID)]; ok {
		a := s.assessments[assessmentID]
		if err := s.ensureAssessmentCurrentUnlocked(a); err != nil {
			return ReceiptEvidence{}, err
		}
		ev.AssessmentID = a.AssessmentID
		ev.AssessmentSHA = a.AssessmentSHA
		ev.ReconciliationState = a.State
		if resolutionID, ok := s.resolutionByAssessment[a.AssessmentID]; ok {
			r := s.resolutions[resolutionID]
			ev.ResolutionID = r.ResolutionID
			ev.ResolutionSHA = r.ResolutionSHA
			ev.ReconciliationState = AssessmentResolved
		}
	}

	if preparationID, ok := s.preparationByScope[scopeKey(taskID, packageID)]; ok {
		p := s.preparations[preparationID]
		if ev.AssessmentID == "" || p.AssessmentID != ev.AssessmentID || p.ResolutionID != ev.ResolutionID {
			return ReceiptEvidence{}, malformed("receipt preparation binding does not match current assessment/resolution")
		}
		ev.RecoveryPreparationID = p.PreparationID
		ev.RecoveryPreparationSHA = p.PreparationSHA
		ev.RecoveryOperationID = p.RecoveryOperationID
		ev.RecoveryOperationSHA = p.RecoveryOperationSHA
	}

	if candidateID, ok := s.commitCandidateByScope[scopeKey(taskID, packageID)]; ok {
		for i := len(s.events) - 1; i >= 0; i-- {
			e := s.events[i]
			if e.Kind == eventRecoveryCommitted && e.TaskID == taskID && e.PackageID == packageID && e.ReusedID == candidateID && e.Recovery != nil {
				if ev.AssessmentID == "" || e.AssessmentID != ev.AssessmentID || e.ResolutionID != ev.ResolutionID || e.PreparationID != ev.RecoveryPreparationID {
					return ReceiptEvidence{}, malformed("receipt commit binding does not match current preparation/assessment/resolution")
				}
				ev.RecoveryCommitID = e.Recovery.CommitID
				ev.RecoveryCommitSHA = e.Recovery.CommitSHA
				ev.CanonicalCommitRef = e.Recovery.CanonicalRef
				break
			}
		}
	}

	ev.RuntimeEvidenceIDs = uniqueSorted(ev.RuntimeEvidenceIDs)
	b, err := os.ReadFile(s.path)
	if err != nil {
		return ReceiptEvidence{}, err
	}
	sum := sha256.Sum256(b)
	ev.StoreSHA = hex.EncodeToString(sum[:])
	return ev, nil
}
