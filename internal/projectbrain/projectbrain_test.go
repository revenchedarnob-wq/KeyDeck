package projectbrain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/contextscout"
)

func fixture(t *testing.T) (string, contextcompiler.Packet, contextscout.Record, ContextInspection) {
	t.Helper()
	root := t.TempDir()
	packet := contextcompiler.Packet{Version: 1, CreatedAt: time.Now(), Objective: "repair router", ProjectRoot: root, StructuralEvidence: []contextcompiler.StructuralEvidence{{Tool: "search_graph", Arguments: `{"q":"router"}`, Output: "ok", Successful: true}}, SourceSnippets: []contextcompiler.SourceSnippet{{Path: "internal/router.go", StartLine: 1, EndLine: 2, Score: 10, Content: "package internal\nfunc Route(){}"}}, OmittedEvidenceCount: 3}
	raw, _ := json.Marshal(packet)
	record := contextscout.Record{PacketID: "packet-test", PacketSHA256: digestBytes(raw), ProjectFingerprint: "fp-current"}
	ins, err := BuildInspection(packet, record, "fp-current")
	if err != nil {
		t.Fatal(err)
	}
	return root, packet, record, ins
}

func input(root string, ins ContextInspection) RevisionInput {
	return RevisionInput{ProjectID: "project-test", SessionID: "session-test", ProjectRoot: root, ProjectFingerprint: "fp-current", Goal: "repair router safely", Decisions: []string{"keep exact once", "keep exact once"}, KnownFailures: []string{"old replay bug"}, CompletedWork: []string{"proof 0.26"}, PendingWork: []string{"proof 0.27"}, RelevantFiles: []string{"internal/router.go"}, Context: ins}
}

func TestInspectionAndStoreRestart(t *testing.T) {
	root, _, _, ins := fixture(t)
	path := filepath.Join(t.TempDir(), "brain.jsonl")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, created, err := s.Append(input(root, ins), nil)
	if err != nil || !created {
		t.Fatalf("append %v %+v", err, r)
	}
	if r.Context.OmittedEvidenceCount != 3 || len(r.Context.IncludedEvidence) != 2 {
		t.Fatal("inspection accounting wrong")
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Latest()
	if !ok || got.RevisionSHA256 != r.RevisionSHA256 {
		t.Fatal("restart lost revision")
	}
	again, created, err := s2.Append(input(root, ins), nil)
	if err != nil || created || again.RevisionSHA256 != r.RevisionSHA256 {
		t.Fatal("identical revision not reused")
	}
}

func TestStaleAndSecretRejected(t *testing.T) {
	root, p, r, ins := fixture(t)
	if _, err := BuildInspection(p, r, "different"); !errors.Is(err, ErrStaleContext) {
		t.Fatalf("stale got %v", err)
	}
	s, _ := Open(filepath.Join(t.TempDir(), "brain.jsonl"))
	in := input(root, ins)
	in.Decisions = []string{"token=TOPSECRET"}
	if _, _, err := s.Append(in, []string{"TOPSECRET"}); !errors.Is(err, ErrSecret) {
		t.Fatalf("secret got %v", err)
	}
	if s.Count() != 0 {
		t.Fatal("rejected input persisted")
	}
}

func TestTamperRejectedAfterRehashOuterJSONNotEnough(t *testing.T) {
	root, _, _, ins := fixture(t)
	path := filepath.Join(t.TempDir(), "brain.jsonl")
	s, _ := Open(path)
	_, _, _ = s.Append(input(root, ins), nil)
	raw, _ := os.ReadFile(path)
	var r Revision
	_ = json.Unmarshal(raw, &r)
	r.Context.IncludedEvidence[0].Reference = "forged"
	r.RevisionSHA256 = revisionDigest(r)
	forged, _ := json.Marshal(r)
	_ = os.WriteFile(path, append(forged, '\n'), 0o600)
	if _, err := Open(path); !errors.Is(err, ErrTampered) {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
}

func TestAppendRevisionChainAndNormalization(t *testing.T) {
	root, _, _, ins := fixture(t)
	path := filepath.Join(t.TempDir(), "brain.jsonl")
	s, _ := Open(path)
	first, _, _ := s.Append(input(root, ins), nil)
	in := input(root, ins)
	in.PendingWork = []string{"proof 0.28", "proof 0.27", "proof 0.28"}
	second, created, err := s.Append(in, nil)
	if err != nil || !created {
		t.Fatal(err)
	}
	if second.Sequence != 2 || second.PreviousRevisionSHA256 != first.RevisionSHA256 {
		t.Fatal("chain wrong")
	}
	if strings.Join(second.PendingWork, ",") != "proof 0.27,proof 0.28" {
		t.Fatal("normalization wrong")
	}
}

func TestFullyRehashedInspectionTamperRejectedAgainstContext(t *testing.T) {
	root, p, r, ins := fixture(t)
	path := filepath.Join(t.TempDir(), "brain.jsonl")
	s, _ := Open(path)
	rev, _, _ := s.Append(input(root, ins), nil)
	rev.Context.IncludedEvidence[0].Reference = "forged"
	rev.Context.InspectionSHA256 = inspectionDigest(rev.Context)
	rev.RevisionSHA256 = revisionDigest(rev)
	forged, _ := json.Marshal(rev)
	_ = os.WriteFile(path, append(forged, '\n'), 0o600)
	loaded, err := Open(path)
	if err != nil {
		t.Fatalf("self-consistent store should load: %v", err)
	}
	got, _ := loaded.Latest()
	if err := ValidateRevisionContext(got, p, r, "fp-current"); !errors.Is(err, ErrTampered) {
		t.Fatalf("expected context-bound tamper rejection, got %v", err)
	}
}
