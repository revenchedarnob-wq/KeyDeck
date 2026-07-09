package handoff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
)

var (
	ErrInvalidPackage   = errors.New("invalid handoff package")
	ErrStaleTask        = errors.New("handoff package task state is stale")
	ErrStaleContext     = errors.New("handoff package context packet is stale")
	ErrStaleBrain       = errors.New("handoff package project brain revision is stale")
	ErrForbiddenContext = errors.New("handoff package contains forbidden exact value")
)

type CheckRef struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Status      tasks.CheckStatus `json:"status"`
	Evidence    string            `json:"evidence,omitempty"`
}
type TaskSnapshot struct {
	TaskID           string         `json:"task_id"`
	SessionID        string         `json:"session_id"`
	Status           tasks.Status   `json:"status"`
	LastSequence     uint64         `json:"last_sequence"`
	Progress         tasks.Progress `json:"progress"`
	Goal             string         `json:"goal"`
	RequiredOutcomes []string       `json:"required_outcomes"`
	PendingChecks    []CheckRef     `json:"pending_checks"`
	PassedChecks     []CheckRef     `json:"passed_checks"`
	ForbiddenScope   []string       `json:"forbidden_scope"`
}
type Package struct {
	Version                        int                               `json:"version"`
	PackageID                      string                            `json:"package_id"`
	PackageSHA256                  string                            `json:"package_sha256"`
	Task                           TaskSnapshot                      `json:"task"`
	ContextPacketID                string                            `json:"context_packet_id"`
	ContextPacketSHA256            string                            `json:"context_packet_sha256"`
	MCPServerID                    string                            `json:"mcp_server_id"`
	MCPSchemaSHA256                string                            `json:"mcp_schema_sha256"`
	ProjectSourceFingerprint       string                            `json:"project_source_fingerprint"`
	ProjectBrainRevisionSHA256     string                            `json:"project_brain_revision_sha256"`
	ContextInspectionSHA256        string                            `json:"context_inspection_sha256"`
	IncludedInspectionEvidence     []projectbrain.InspectionEvidence `json:"included_inspection_evidence"`
	OmittedInspectionEvidenceCount int                               `json:"omitted_inspection_evidence_count"`
	Passport                       session.Passport                  `json:"passport"`
	RequiredEngineCapabilities     []engineruntime.Capability        `json:"required_engine_capabilities"`
	EngineRequest                  engineruntime.Request             `json:"engine_request"`
}

type Input struct {
	Task                     tasks.State
	ContextPacketID          string
	ContextPacketSHA256      string
	MCPServerID              string
	MCPSchemaSHA256          string
	ProjectSourceFingerprint string
	Brain                    projectbrain.Revision
	Passport                 session.Passport
	EngineID                 string
	RequiredCapabilities     []engineruntime.Capability
	ForbiddenExactValues     []string
}

type CurrentState struct {
	TaskSequence               uint64
	ContextPacketID            string
	ProjectBrainRevisionSHA256 string
}

func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func normalizeStrings(v []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range v {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func normalizeCaps(v []engineruntime.Capability) []engineruntime.Capability {
	seen := map[engineruntime.Capability]bool{}
	out := []engineruntime.Capability{}
	for _, c := range v {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func packageDigest(p Package) string { p.PackageID = ""; p.PackageSHA256 = ""; return digest(p) }
