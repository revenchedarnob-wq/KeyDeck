package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/pool"
)

var (
	ErrInvalidEvidence  = errors.New("invalid provider conformance evidence")
	ErrEvidenceExpired  = errors.New("provider conformance evidence expired")
	ErrEvidenceTampered = errors.New("provider conformance evidence hash mismatch")
	ErrEvidenceNotFound = errors.New("exact provider conformance evidence not found")
)

type ObservationPhase string

const (
	PhasePreOutput  ObservationPhase = "pre_output"
	PhaseMidStream  ObservationPhase = "mid_stream"
	PhasePostOutput ObservationPhase = "post_output"
	PhaseUnknown    ObservationPhase = "unknown"
)

type ProviderIdentity struct {
	Provider      string `json:"provider"`
	APIBase       string `json:"api_base"`
	APIVersion    string `json:"api_version"`
	Model         string `json:"model"`
	ModelRevision string `json:"model_revision"`
}

func (i ProviderIdentity) Validate() error {
	if strings.TrimSpace(i.Provider) == "" || strings.TrimSpace(i.APIBase) == "" || strings.TrimSpace(i.APIVersion) == "" || strings.TrimSpace(i.Model) == "" || strings.TrimSpace(i.ModelRevision) == "" {
		return fmt.Errorf("%w: exact provider, API base/version, model and model revision are required", ErrInvalidEvidence)
	}
	return nil
}

type EvidenceProvenance struct {
	CaptureID        string `json:"capture_id"`
	SourceKind       string `json:"source_kind"`
	SourceRef        string `json:"source_ref"`
	CapturedBy       string `json:"captured_by"`
	RawCaptureSHA256 string `json:"raw_capture_sha256"`
}

func (p EvidenceProvenance) Validate() error {
	if strings.TrimSpace(p.CaptureID) == "" || strings.TrimSpace(p.SourceKind) == "" || strings.TrimSpace(p.SourceRef) == "" || strings.TrimSpace(p.CapturedBy) == "" || !isSHA256(p.RawCaptureSHA256) {
		return fmt.Errorf("%w: complete provenance with raw capture SHA-256 is required", ErrInvalidEvidence)
	}
	return nil
}

type PolicyDecision struct {
	Class                     pool.FailureClass `json:"class"`
	AllowOriginalReplay       bool              `json:"allow_original_replay"`
	AllowKeyRotation          bool              `json:"allow_key_rotation"`
	AllowSemanticContinuation bool              `json:"allow_semantic_continuation"`
	RequireInput              bool              `json:"require_input"`
}

type FailureObservation struct {
	ScenarioID     string           `json:"scenario_id"`
	Phase          ObservationPhase `json:"phase"`
	StatusCode     int              `json:"status_code,omitempty"`
	ErrorCode      string           `json:"error_code,omitempty"`
	ErrorScope     string           `json:"error_scope,omitempty"`
	TransportError string           `json:"transport_error,omitempty"`
	PartialOutput  bool             `json:"partial_output"`
	TerminalEvent  bool             `json:"terminal_event"`
	UsageObserved  bool             `json:"usage_observed"`
	Decision       PolicyDecision   `json:"decision"`
}

func (o FailureObservation) Validate() error {
	if strings.TrimSpace(o.ScenarioID) == "" {
		return fmt.Errorf("%w: observation scenario id is required", ErrInvalidEvidence)
	}
	switch o.Phase {
	case PhasePreOutput, PhaseMidStream, PhasePostOutput, PhaseUnknown:
	default:
		return fmt.Errorf("%w: unsupported observation phase %q", ErrInvalidEvidence, o.Phase)
	}
	if o.StatusCode == 0 && strings.TrimSpace(o.TransportError) == "" && strings.TrimSpace(o.ErrorCode) == "" {
		return fmt.Errorf("%w: observation needs exact HTTP, provider-error or transport evidence", ErrInvalidEvidence)
	}
	return validateDecision(o)
}

func validateDecision(o FailureObservation) error {
	d := o.Decision
	if o.PartialOutput && d.AllowOriginalReplay {
		return fmt.Errorf("%w: partial output forbids original request replay", ErrInvalidEvidence)
	}
	switch d.Class {
	case pool.FailureKeyExhausted, pool.FailureInvalidKey, pool.FailureKeyRateLimited:
		if !strings.EqualFold(strings.TrimSpace(o.ErrorScope), "key") {
			return fmt.Errorf("%w: key-scoped decision requires exact scope=key", ErrInvalidEvidence)
		}
		if o.Phase == PhasePreOutput {
			if !d.AllowKeyRotation || !d.AllowOriginalReplay {
				return fmt.Errorf("%w: exact pre-output key failure must explicitly allow rotation and replay", ErrInvalidEvidence)
			}
		} else if o.PartialOutput {
			if d.AllowOriginalReplay || !d.AllowSemanticContinuation {
				return fmt.Errorf("%w: mid-stream key failure must continue semantically without replay", ErrInvalidEvidence)
			}
		}
	case pool.FailureProviderBusy:
		if !strings.EqualFold(strings.TrimSpace(o.ErrorScope), "provider") || d.AllowKeyRotation || d.AllowOriginalReplay {
			return fmt.Errorf("%w: provider-wide failure must preserve backup keys and forbid replay", ErrInvalidEvidence)
		}
	case pool.FailureAmbiguous:
		if d.AllowKeyRotation || d.AllowOriginalReplay || d.AllowSemanticContinuation {
			return fmt.Errorf("%w: ambiguous failure forbids automatic replay, rotation and continuation", ErrInvalidEvidence)
		}
	case pool.FailureNonRetryable:
		if d.AllowKeyRotation || d.AllowOriginalReplay {
			return fmt.Errorf("%w: non-retryable failure forbids replay and rotation", ErrInvalidEvidence)
		}
	default:
		return fmt.Errorf("%w: unsupported decision class %q", ErrInvalidEvidence, d.Class)
	}
	return nil
}

type UsageSemantics struct {
	UsageFields            []string `json:"usage_fields"`
	CacheReadField         string   `json:"cache_read_field,omitempty"`
	CacheCreationField     string   `json:"cache_creation_field,omitempty"`
	UsageReportedOnSuccess bool     `json:"usage_reported_on_success"`
	UsageReportedOnError   bool     `json:"usage_reported_on_error"`
	UsageReportedMidStream bool     `json:"usage_reported_mid_stream"`
	BillingMeaning         string   `json:"billing_meaning"`
}

func (u UsageSemantics) Validate() error {
	if len(u.UsageFields) == 0 || strings.TrimSpace(u.BillingMeaning) == "" {
		return fmt.Errorf("%w: usage fields and billing meaning are required", ErrInvalidEvidence)
	}
	return nil
}

type ProviderEvidenceBundle struct {
	SchemaVersion  int                  `json:"schema_version"`
	EvidenceID     string               `json:"evidence_id"`
	Identity       ProviderIdentity     `json:"identity"`
	TestedAt       time.Time            `json:"tested_at"`
	ExpiresAt      time.Time            `json:"expires_at"`
	Provenance     EvidenceProvenance   `json:"provenance"`
	Failures       []FailureObservation `json:"failures"`
	Usage          UsageSemantics       `json:"usage"`
	EvidenceSHA256 string               `json:"evidence_sha256"`
}

func (b ProviderEvidenceBundle) unsignedBytes() ([]byte, error) {
	copy := b
	copy.EvidenceSHA256 = ""
	return json.Marshal(copy)
}

func (b ProviderEvidenceBundle) ComputeSHA256() (string, error) {
	raw, err := b.unsignedBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (b *ProviderEvidenceBundle) Seal() error {
	h, err := b.ComputeSHA256()
	if err != nil {
		return err
	}
	b.EvidenceSHA256 = h
	return nil
}

func (b ProviderEvidenceBundle) Validate(now time.Time) error {
	if b.SchemaVersion != 1 || strings.TrimSpace(b.EvidenceID) == "" || b.TestedAt.IsZero() || b.ExpiresAt.IsZero() || !b.ExpiresAt.After(b.TestedAt) {
		return fmt.Errorf("%w: schema, evidence id and validity window are required", ErrInvalidEvidence)
	}
	if err := b.Identity.Validate(); err != nil {
		return err
	}
	if err := b.Provenance.Validate(); err != nil {
		return err
	}
	if len(b.Failures) == 0 {
		return fmt.Errorf("%w: at least one failure observation is required", ErrInvalidEvidence)
	}
	seen := map[string]struct{}{}
	for _, observation := range b.Failures {
		if err := observation.Validate(); err != nil {
			return err
		}
		if _, exists := seen[observation.ScenarioID]; exists {
			return fmt.Errorf("%w: duplicate scenario id %q", ErrInvalidEvidence, observation.ScenarioID)
		}
		seen[observation.ScenarioID] = struct{}{}
	}
	if err := b.Usage.Validate(); err != nil {
		return err
	}
	expected, err := b.ComputeSHA256()
	if err != nil {
		return err
	}
	if !isSHA256(b.EvidenceSHA256) || !strings.EqualFold(expected, b.EvidenceSHA256) {
		return ErrEvidenceTampered
	}
	if !now.Before(b.ExpiresAt) {
		return ErrEvidenceExpired
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type ConformanceDecision struct {
	Trusted                   bool              `json:"trusted"`
	Class                     pool.FailureClass `json:"class"`
	AllowOriginalReplay       bool              `json:"allow_original_replay"`
	AllowKeyRotation          bool              `json:"allow_key_rotation"`
	AllowSemanticContinuation bool              `json:"allow_semantic_continuation"`
	RequireInput              bool              `json:"require_input"`
	RequireRevalidation       bool              `json:"require_revalidation"`
	EvidenceID                string            `json:"evidence_id,omitempty"`
	EvidenceSHA256            string            `json:"evidence_sha256,omitempty"`
	Reason                    string            `json:"reason"`
}

func conservativeDecision(reason string, revalidate bool) ConformanceDecision {
	return ConformanceDecision{Class: pool.FailureAmbiguous, RequireInput: true, RequireRevalidation: revalidate, Reason: reason}
}

type EvidenceRegistry struct {
	bundles []ProviderEvidenceBundle
}

func (r *EvidenceRegistry) Add(bundle ProviderEvidenceBundle, now time.Time) error {
	if err := bundle.Validate(now); err != nil {
		return err
	}
	for _, existing := range r.bundles {
		if existing.EvidenceID == bundle.EvidenceID || sameIdentity(existing.Identity, bundle.Identity) {
			return fmt.Errorf("%w: duplicate evidence or exact identity", ErrInvalidEvidence)
		}
	}
	r.bundles = append(r.bundles, bundle)
	return nil
}

func (r EvidenceRegistry) Decide(identity ProviderIdentity, observed FailureObservation, now time.Time) ConformanceDecision {
	for _, bundle := range r.bundles {
		if !sameIdentity(bundle.Identity, identity) {
			continue
		}
		if err := bundle.Validate(now); err != nil {
			if errors.Is(err, ErrEvidenceExpired) {
				return conservativeDecision("exact evidence expired; revalidation required", true)
			}
			return conservativeDecision("exact evidence invalid or tampered", true)
		}
		for _, known := range bundle.Failures {
			if sameObservedFailure(known, observed) {
				d := known.Decision
				return ConformanceDecision{
					Trusted: true, Class: d.Class, AllowOriginalReplay: d.AllowOriginalReplay, AllowKeyRotation: d.AllowKeyRotation,
					AllowSemanticContinuation: d.AllowSemanticContinuation, RequireInput: d.RequireInput,
					EvidenceID: bundle.EvidenceID, EvidenceSHA256: bundle.EvidenceSHA256, Reason: "exact provider evidence match",
				}
			}
		}
		return conservativeDecision("provider identity matched but failure behavior was not observed", false)
	}
	return conservativeDecision("no exact provider/model/version evidence", false)
}

func sameIdentity(a, b ProviderIdentity) bool {
	return strings.EqualFold(strings.TrimSpace(a.Provider), strings.TrimSpace(b.Provider)) &&
		strings.EqualFold(strings.TrimSpace(a.APIBase), strings.TrimSpace(b.APIBase)) &&
		a.APIVersion == b.APIVersion && a.Model == b.Model && a.ModelRevision == b.ModelRevision
}

func sameObservedFailure(a, b FailureObservation) bool {
	return a.Phase == b.Phase && a.StatusCode == b.StatusCode && strings.EqualFold(strings.TrimSpace(a.ErrorCode), strings.TrimSpace(b.ErrorCode)) &&
		strings.EqualFold(strings.TrimSpace(a.ErrorScope), strings.TrimSpace(b.ErrorScope)) && strings.TrimSpace(a.TransportError) == strings.TrimSpace(b.TransportError) &&
		a.PartialOutput == b.PartialOutput && a.TerminalEvent == b.TerminalEvent && a.UsageObserved == b.UsageObserved
}

// ProviderProfile converts still-valid exact evidence into the narrow classifier
// used by the existing API-key pool. Only pre-output HTTP/provider errors are
// eligible: stream continuation remains owned by the continuity layer and may
// never be collapsed into blind request replay.
func (b ProviderEvidenceBundle) ProviderProfile(now time.Time) (ProviderProfile, error) {
	if err := b.Validate(now); err != nil {
		return ProviderProfile{}, err
	}
	profile := ProviderProfile{
		Provider:   b.Identity.Provider,
		Version:    b.Identity.APIVersion + "/" + b.Identity.ModelRevision,
		TestedAt:   b.TestedAt,
		EvidenceID: b.EvidenceID,
	}
	for _, observation := range b.Failures {
		if observation.Phase != PhasePreOutput || observation.StatusCode <= 0 || strings.TrimSpace(observation.ErrorCode) == "" || strings.TrimSpace(observation.ErrorScope) == "" {
			continue
		}
		switch observation.Decision.Class {
		case pool.FailureKeyExhausted, pool.FailureInvalidKey, pool.FailureKeyRateLimited, pool.FailureProviderBusy, pool.FailureAmbiguous:
			profile.Rules = append(profile.Rules, FailureRule{
				StatusCode: observation.StatusCode,
				ErrorCode:  observation.ErrorCode,
				ErrorScope: observation.ErrorScope,
				Class:      observation.Decision.Class,
			})
		}
	}
	if err := profile.Validate(); err != nil {
		return ProviderProfile{}, err
	}
	return profile, nil
}
