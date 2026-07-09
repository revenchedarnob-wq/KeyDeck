package projectbrain

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/proofreceipt"
)

type Store struct {
	mu        sync.Mutex
	path      string
	revisions []Revision
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("project brain store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Path() string { return s.path }
func (s *Store) Count() int   { s.mu.Lock(); defer s.mu.Unlock(); return len(s.revisions) }
func (s *Store) Latest() (Revision, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.revisions) == 0 {
		return Revision{}, false
	}
	return s.revisions[len(s.revisions)-1], true
}

func (s *Store) Append(in RevisionInput, forbiddenExactValues []string) (Revision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalized, err := normalizeInput(in, forbiddenExactValues)
	if err != nil {
		return Revision{}, false, err
	}
	if len(s.revisions) > 0 {
		latest := s.revisions[len(s.revisions)-1]
		if latest.ProjectID != normalized.ProjectID || latest.SessionID != normalized.SessionID || latest.ProjectRoot != normalized.ProjectRoot {
			return Revision{}, false, ErrInvalidState
		}
		probe := revisionFromInput(normalized, latest.Sequence, latest.PreviousRevisionSHA256)
		probe.Sequence = latest.Sequence
		probe.PreviousRevisionSHA256 = latest.PreviousRevisionSHA256
		probe.RevisionSHA256 = revisionDigest(probe)
		if sameRevisionContent(latest, probe) {
			return latest, false, nil
		}
	}
	seq := uint64(len(s.revisions) + 1)
	prev := ""
	if len(s.revisions) > 0 {
		prev = s.revisions[len(s.revisions)-1].RevisionSHA256
	}
	rev := revisionFromInput(normalized, seq, prev)
	rev.CreatedAt = time.Now().UTC()
	rev.RevisionSHA256 = revisionDigest(rev)
	if err := appendLine(s.path, rev); err != nil {
		return Revision{}, false, err
	}
	s.revisions = append(s.revisions, rev)
	return rev, true, nil
}

func (s *Store) ReceiptArtifact() (proofreceipt.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return proofreceipt.Artifact{}, err
	}
	return proofreceipt.Artifact{Name: "project brain revision store", Path: s.path, SHA256: digestBytes(raw), Size: int64(len(raw))}, nil
}

func normalizeInput(in RevisionInput, forbidden []string) (RevisionInput, error) {
	in.ProjectID = strings.TrimSpace(in.ProjectID)
	in.SessionID = strings.TrimSpace(in.SessionID)
	in.ProjectRoot = filepath.Clean(strings.TrimSpace(in.ProjectRoot))
	in.ProjectFingerprint = strings.TrimSpace(in.ProjectFingerprint)
	in.Goal = strings.TrimSpace(in.Goal)
	if in.ProjectID == "" || in.SessionID == "" || in.ProjectRoot == "." || in.ProjectFingerprint == "" || in.Goal == "" {
		return RevisionInput{}, ErrInvalidState
	}
	if in.Context.PacketID == "" || in.Context.PacketSHA256 == "" || in.Context.ProjectFingerprint != in.ProjectFingerprint || in.Context.InspectionSHA256 != inspectionDigest(in.Context) {
		return RevisionInput{}, ErrInvalidState
	}
	in.Decisions = normalizeStrings(in.Decisions)
	in.KnownFailures = normalizeStrings(in.KnownFailures)
	in.CompletedWork = normalizeStrings(in.CompletedWork)
	in.PendingWork = normalizeStrings(in.PendingWork)
	in.RelevantFiles = normalizeStrings(in.RelevantFiles)
	raw, _ := json.Marshal(in)
	text := string(raw)
	for _, v := range forbidden {
		if v != "" && strings.Contains(text, v) {
			return RevisionInput{}, ErrSecret
		}
	}
	return in, nil
}

func revisionFromInput(in RevisionInput, seq uint64, prev string) Revision {
	return Revision{Version: 1, Sequence: seq, ProjectID: in.ProjectID, SessionID: in.SessionID, ProjectRoot: in.ProjectRoot, ProjectFingerprint: in.ProjectFingerprint, Goal: in.Goal, Decisions: in.Decisions, KnownFailures: in.KnownFailures, CompletedWork: in.CompletedWork, PendingWork: in.PendingWork, RelevantFiles: in.RelevantFiles, Context: in.Context, PreviousRevisionSHA256: prev}
}
func sameRevisionContent(a, b Revision) bool {
	a.Sequence = 0
	b.Sequence = 0
	a.CreatedAt = time.Time{}
	b.CreatedAt = time.Time{}
	a.PreviousRevisionSHA256 = ""
	b.PreviousRevisionSHA256 = ""
	a.RevisionSHA256 = ""
	b.RevisionSHA256 = ""
	return digest(a) == digest(b)
}
func appendLine(path string, v any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	if err = enc.Encode(v); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
func digestBytes(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	expected := uint64(1)
	prev := ""
	for sc.Scan() {
		var r Revision
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return fmt.Errorf("%w: decode revision: %v", ErrTampered, err)
		}
		if r.Sequence != expected || r.PreviousRevisionSHA256 != prev || r.RevisionSHA256 != revisionDigest(r) || r.Context.InspectionSHA256 != inspectionDigest(r.Context) {
			return fmt.Errorf("%w: revision %d", ErrTampered, expected)
		}
		if r.ProjectID == "" || r.SessionID == "" || r.ProjectRoot == "" || r.ProjectFingerprint == "" || r.Goal == "" || r.Context.ProjectFingerprint != r.ProjectFingerprint {
			return fmt.Errorf("%w: revision fields %d", ErrTampered, expected)
		}
		s.revisions = append(s.revisions, r)
		prev = r.RevisionSHA256
		expected++
	}
	return sc.Err()
}
