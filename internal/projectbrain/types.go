package projectbrain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidState = errors.New("invalid project brain state")
	ErrStaleContext = errors.New("project brain context is stale")
	ErrTampered     = errors.New("project brain durable state failed integrity validation")
	ErrSecret       = errors.New("project brain input contains forbidden exact value")
)

type InspectionEvidence struct {
	Kind       string `json:"kind"`
	Reference  string `json:"reference"`
	SHA256     string `json:"sha256"`
	Successful bool   `json:"successful"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type ContextInspection struct {
	PacketID             string               `json:"packet_id"`
	PacketSHA256         string               `json:"packet_sha256"`
	ProjectFingerprint   string               `json:"project_fingerprint"`
	IncludedEvidence     []InspectionEvidence `json:"included_evidence"`
	OmittedEvidenceCount int                  `json:"omitted_evidence_count"`
	InspectionSHA256     string               `json:"inspection_sha256"`
}

type Revision struct {
	Version                int               `json:"version"`
	Sequence               uint64            `json:"sequence"`
	CreatedAt              time.Time         `json:"created_at"`
	ProjectID              string            `json:"project_id"`
	SessionID              string            `json:"session_id"`
	ProjectRoot            string            `json:"project_root"`
	ProjectFingerprint     string            `json:"project_fingerprint"`
	Goal                   string            `json:"goal"`
	Decisions              []string          `json:"decisions,omitempty"`
	KnownFailures          []string          `json:"known_failures,omitempty"`
	CompletedWork          []string          `json:"completed_work,omitempty"`
	PendingWork            []string          `json:"pending_work,omitempty"`
	RelevantFiles          []string          `json:"relevant_files,omitempty"`
	Context                ContextInspection `json:"context"`
	PreviousRevisionSHA256 string            `json:"previous_revision_sha256,omitempty"`
	RevisionSHA256         string            `json:"revision_sha256"`
}

type RevisionInput struct {
	ProjectID          string
	SessionID          string
	ProjectRoot        string
	ProjectFingerprint string
	Goal               string
	Decisions          []string
	KnownFailures      []string
	CompletedWork      []string
	PendingWork        []string
	RelevantFiles      []string
	Context            ContextInspection
}

func normalizeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func inspectionDigest(in ContextInspection) string {
	clone := in
	clone.InspectionSHA256 = ""
	clone.IncludedEvidence = append([]InspectionEvidence(nil), in.IncludedEvidence...)
	sort.Slice(clone.IncludedEvidence, func(i, j int) bool {
		a, b := clone.IncludedEvidence[i], clone.IncludedEvidence[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Reference != b.Reference {
			return a.Reference < b.Reference
		}
		return a.SHA256 < b.SHA256
	})
	return digest(clone)
}

func revisionDigest(in Revision) string {
	clone := in
	clone.RevisionSHA256 = ""
	clone.CreatedAt = time.Time{}
	return digest(clone)
}
