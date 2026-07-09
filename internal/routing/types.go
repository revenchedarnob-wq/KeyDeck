package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/engineruntime"
)

var (
	ErrInvalidRequirements      = errors.New("invalid routing requirements")
	ErrNoQualifiedRoute         = errors.New("no qualified route")
	ErrInvalidPlan              = errors.New("invalid route plan")
	ErrRoutePackageMismatch     = errors.New("route plan does not match handoff package")
	ErrContinuationDenied       = errors.New("route continuation denied")
	ErrProviderBusySameProvider = errors.New("provider-wide busy continuation cannot target the same provider")
)

type FailureClass string

const (
	FailureKeyExhausted       FailureClass = "key_exhausted"
	FailureProviderBusy       FailureClass = "provider_busy"
	FailureAmbiguousTransport FailureClass = "ambiguous_transport"
	FailureEngineInterrupted  FailureClass = "engine_interrupted"
)

type Candidate struct {
	EngineID      string                     `json:"engine_id"`
	ProviderID    string                     `json:"provider_id"`
	Available     bool                       `json:"available"`
	Health        engineruntime.HealthState  `json:"health"`
	Capabilities  []engineruntime.Capability `json:"capabilities"`
	EvidenceScore int                        `json:"evidence_score"`
	EvidenceRefs  []string                   `json:"evidence_refs,omitempty"`
}

type Requirements struct {
	TaskID               string                     `json:"task_id"`
	SessionID            string                     `json:"session_id"`
	HandoffPackageID     string                     `json:"handoff_package_id,omitempty"`
	HandoffPackageSHA256 string                     `json:"handoff_package_sha256,omitempty"`
	RequiredCapabilities []engineruntime.Capability `json:"required_capabilities,omitempty"`
	ExcludedEngineIDs    []string                   `json:"excluded_engine_ids,omitempty"`
	ExcludedProviderIDs  []string                   `json:"excluded_provider_ids,omitempty"`
}

type Plan struct {
	Version               int                        `json:"version"`
	RouteID               string                     `json:"route_id"`
	RouteSHA256           string                     `json:"route_sha256"`
	TaskID                string                     `json:"task_id"`
	SessionID             string                     `json:"session_id"`
	HandoffPackageID      string                     `json:"handoff_package_id,omitempty"`
	HandoffPackageSHA256  string                     `json:"handoff_package_sha256,omitempty"`
	RequiredCapabilities  []engineruntime.Capability `json:"required_capabilities,omitempty"`
	SelectedEngineID      string                     `json:"selected_engine_id"`
	SelectedProviderID    string                     `json:"selected_provider_id"`
	SelectedEvidenceScore int                        `json:"selected_evidence_score"`
	SelectedEvidenceRefs  []string                   `json:"selected_evidence_refs,omitempty"`
	CandidateSetSHA256    string                     `json:"candidate_set_sha256"`
}

type ContinuationPlan struct {
	Version         int          `json:"version"`
	ContinuationID  string       `json:"continuation_id"`
	SHA256          string       `json:"sha256"`
	ResponseID      string       `json:"response_id"`
	TaskID          string       `json:"task_id"`
	SessionID       string       `json:"session_id"`
	FailureClass    FailureClass `json:"failure_class"`
	FromRouteID     string       `json:"from_route_id"`
	FromRouteSHA256 string       `json:"from_route_sha256"`
	ToRouteID       string       `json:"to_route_id"`
	ToRouteSHA256   string       `json:"to_route_sha256"`
	FromEngineID    string       `json:"from_engine_id"`
	FromProviderID  string       `json:"from_provider_id"`
	ToEngineID      string       `json:"to_engine_id"`
	ToProviderID    string       `json:"to_provider_id"`
}

func digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeCaps(v []engineruntime.Capability) []engineruntime.Capability {
	seen := map[engineruntime.Capability]bool{}
	out := make([]engineruntime.Capability, 0, len(v))
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

func normalizeStrings(v []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(v))
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

func normalizeCandidate(c Candidate) Candidate {
	c.EngineID = strings.TrimSpace(c.EngineID)
	c.ProviderID = strings.TrimSpace(c.ProviderID)
	c.Capabilities = normalizeCaps(c.Capabilities)
	c.EvidenceRefs = normalizeStrings(c.EvidenceRefs)
	return c
}

func normalizeRequirements(r Requirements) Requirements {
	r.TaskID = strings.TrimSpace(r.TaskID)
	r.SessionID = strings.TrimSpace(r.SessionID)
	r.HandoffPackageID = strings.TrimSpace(r.HandoffPackageID)
	r.HandoffPackageSHA256 = strings.TrimSpace(r.HandoffPackageSHA256)
	r.RequiredCapabilities = normalizeCaps(r.RequiredCapabilities)
	r.ExcludedEngineIDs = normalizeStrings(r.ExcludedEngineIDs)
	r.ExcludedProviderIDs = normalizeStrings(r.ExcludedProviderIDs)
	return r
}

func planDigest(p Plan) string {
	p.RouteID = ""
	p.RouteSHA256 = ""
	return digest(p)
}

func continuationDigest(p ContinuationPlan) string {
	p.ContinuationID = ""
	p.SHA256 = ""
	return digest(p)
}
