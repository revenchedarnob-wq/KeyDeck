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

var ErrInvalidPairedScopeEvidence = errors.New("invalid paired provider scope evidence")

type ExpectedLimitedBehavior struct {
	StatusCode int    `json:"status_code"`
	BodySHA256 string `json:"body_sha256"`
}

type PairedCredentialTransport struct {
	ElapsedMS        int    `json:"elapsed_ms"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	AutomaticRetries int    `json:"automatic_retries"`
	TransportError   string `json:"transport_error"`
}

type PairedScopeCredential struct {
	KeyKind         string                    `json:"key_kind"`
	RequestCount    int                       `json:"request_count"`
	RetryCount      int                       `json:"retry_count"`
	MaxTokens       int                       `json:"max_tokens"`
	SecretPersisted bool                      `json:"secret_persisted"`
	BodySHA256      string                    `json:"body_sha256"`
	Response        CaptureResponse           `json:"response"`
	Transport       PairedCredentialTransport `json:"transport"`
}

type PairedScopeChecks struct {
	LimitedExact402Match  bool `json:"limited_exact_402_match"`
	ReplacementAttempted  bool `json:"replacement_attempted"`
	ReplacementSucceeded  bool `json:"replacement_succeeded"`
	RequestShapeIdentical bool `json:"request_shape_identical"`
	TotalRequestCount     int  `json:"total_request_count"`
	TotalRetryCount       int  `json:"total_retry_count"`
	SecretsPersisted      bool `json:"secrets_persisted"`
}

type PairedScopeCapture struct {
	ProofComponent          string                  `json:"proof_component"`
	SchemaVersion           int                     `json:"schema_version"`
	CapturedAtUTC           time.Time               `json:"captured_at_utc"`
	Passed                  bool                    `json:"passed"`
	Target                  CaptureTarget           `json:"target"`
	Ordering                string                  `json:"ordering"`
	ExpectedLimitedBehavior ExpectedLimitedBehavior `json:"expected_limited_behavior"`
	LimitedCredential       PairedScopeCredential   `json:"limited_credential"`
	ReplacementCredential   PairedScopeCredential   `json:"replacement_credential"`
	Checks                  PairedScopeChecks       `json:"checks"`
	Limitations             []string                `json:"limitations"`
}

func DecodePairedScopeCapture(raw []byte) (PairedScopeCapture, error) {
	var capture PairedScopeCapture
	if err := json.Unmarshal(raw, &capture); err != nil {
		return PairedScopeCapture{}, fmt.Errorf("decode paired scope capture: %w", err)
	}
	if err := capture.Validate(); err != nil {
		return PairedScopeCapture{}, err
	}
	return capture, nil
}

func (c PairedScopeCapture) Validate() error {
	if c.SchemaVersion != 1 || c.CapturedAtUTC.IsZero() || strings.TrimSpace(c.ProofComponent) == "" {
		return fmt.Errorf("%w: schema, proof component and capture time are required", ErrInvalidPairedScopeEvidence)
	}
	if strings.TrimSpace(c.Target.Provider) == "" || strings.TrimSpace(c.Target.APIBase) == "" || strings.TrimSpace(c.Target.Endpoint) == "" || strings.TrimSpace(c.Target.APIFormat) == "" || strings.TrimSpace(c.Target.AnthropicVersion) == "" || strings.TrimSpace(c.Target.Model) == "" {
		return fmt.Errorf("%w: exact target identity is required", ErrInvalidPairedScopeEvidence)
	}
	if c.Ordering != "limited_then_replacement" || c.ExpectedLimitedBehavior.StatusCode <= 0 || !isSHA256(c.ExpectedLimitedBehavior.BodySHA256) {
		return fmt.Errorf("%w: bounded ordering and exact expected limited behavior are required", ErrInvalidPairedScopeEvidence)
	}
	if err := validatePairedCredential(c.LimitedCredential, true); err != nil {
		return err
	}
	if err := validatePairedCredential(c.ReplacementCredential, false); err != nil {
		return err
	}
	if !c.Checks.LimitedExact402Match || !c.Checks.ReplacementAttempted || !c.Checks.RequestShapeIdentical || c.Checks.TotalRequestCount != 2 || c.Checks.TotalRetryCount != 0 || c.Checks.SecretsPersisted {
		return fmt.Errorf("%w: capture checks do not prove the bounded two-request gate", ErrInvalidPairedScopeEvidence)
	}
	if c.LimitedCredential.Response.StatusCode != c.ExpectedLimitedBehavior.StatusCode || !strings.EqualFold(c.LimitedCredential.Response.BodySHA256, c.ExpectedLimitedBehavior.BodySHA256) {
		return fmt.Errorf("%w: limited credential did not reproduce the exact expected response", ErrInvalidPairedScopeEvidence)
	}
	if !strings.EqualFold(c.LimitedCredential.BodySHA256, c.ReplacementCredential.BodySHA256) {
		return fmt.Errorf("%w: request body shape changed between credentials", ErrInvalidPairedScopeEvidence)
	}
	if c.Checks.ReplacementSucceeded {
		if c.ReplacementCredential.Response.StatusCode < 200 || c.ReplacementCredential.Response.StatusCode >= 300 || strings.TrimSpace(c.ReplacementCredential.Transport.TransportError) != "" {
			return fmt.Errorf("%w: replacement success check conflicts with response evidence", ErrInvalidPairedScopeEvidence)
		}
	} else if c.ReplacementCredential.Response.StatusCode == 0 {
		if strings.TrimSpace(c.ReplacementCredential.Transport.TransportError) == "" {
			return fmt.Errorf("%w: status 0 requires preserved transport error evidence", ErrInvalidPairedScopeEvidence)
		}
	}
	return nil
}

func validatePairedCredential(c PairedScopeCredential, limited bool) error {
	if strings.TrimSpace(c.KeyKind) == "" || c.RequestCount != 1 || c.RetryCount != 0 || c.MaxTokens != 1 || c.SecretPersisted || !isSHA256(c.BodySHA256) {
		return fmt.Errorf("%w: each credential must use one request, zero retries, max_tokens=1 and no persisted secret", ErrInvalidPairedScopeEvidence)
	}
	if c.Transport.TimeoutSeconds <= 0 || c.Transport.AutomaticRetries != 0 {
		return fmt.Errorf("%w: bounded timeout and zero automatic retries are required", ErrInvalidPairedScopeEvidence)
	}
	if limited {
		if c.Response.StatusCode <= 0 || !isSHA256(c.Response.BodySHA256) || strings.TrimSpace(c.Transport.TransportError) != "" {
			return fmt.Errorf("%w: limited credential requires an exact HTTP response", ErrInvalidPairedScopeEvidence)
		}
		bodySum := sha256.Sum256([]byte(c.Response.BodyUTF8))
		if !strings.EqualFold(c.Response.BodySHA256, hex.EncodeToString(bodySum[:])) {
			return fmt.Errorf("%w: limited response body hash mismatch", ErrInvalidPairedScopeEvidence)
		}
		return nil
	}
	if c.Response.StatusCode > 0 {
		if !isSHA256(c.Response.BodySHA256) {
			return fmt.Errorf("%w: replacement HTTP response requires body hash", ErrInvalidPairedScopeEvidence)
		}
		bodySum := sha256.Sum256([]byte(c.Response.BodyUTF8))
		if !strings.EqualFold(c.Response.BodySHA256, hex.EncodeToString(bodySum[:])) {
			return fmt.Errorf("%w: replacement response body hash mismatch", ErrInvalidPairedScopeEvidence)
		}
	}
	return nil
}

type PairedScopeEvidence struct {
	SchemaVersion            int                 `json:"schema_version"`
	EvidenceID               string              `json:"evidence_id"`
	Identity                 ProviderIdentity    `json:"identity"`
	RequestShape             CaptureRequestShape `json:"request_shape"`
	TestedAt                 time.Time           `json:"tested_at"`
	ExpiresAt                time.Time           `json:"expires_at"`
	Provenance               EvidenceProvenance  `json:"provenance"`
	LimitedResponseSHA256    string              `json:"limited_response_sha256"`
	ReplacementOutcome       string              `json:"replacement_outcome"`
	ReplacementTransportKind string              `json:"replacement_transport_kind"`
	ScopeConclusion          string              `json:"scope_conclusion"`
	Decision                 PolicyDecision      `json:"decision"`
	Limitations              []string            `json:"limitations"`
	EvidenceSHA256           string              `json:"evidence_sha256"`
}

func (e PairedScopeEvidence) unsignedBytes() ([]byte, error) {
	copy := e
	copy.EvidenceSHA256 = ""
	return json.Marshal(copy)
}

func (e PairedScopeEvidence) ComputeSHA256() (string, error) {
	raw, err := e.unsignedBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (e *PairedScopeEvidence) Seal() error {
	h, err := e.ComputeSHA256()
	if err != nil {
		return err
	}
	e.EvidenceSHA256 = h
	return nil
}

func (e PairedScopeEvidence) Validate(now time.Time) error {
	if e.SchemaVersion != 1 || strings.TrimSpace(e.EvidenceID) == "" || e.TestedAt.IsZero() || e.ExpiresAt.IsZero() || !e.ExpiresAt.After(e.TestedAt) {
		return fmt.Errorf("%w: schema, identity and validity window are required", ErrInvalidPairedScopeEvidence)
	}
	if err := e.Identity.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(e.RequestShape.Endpoint) == "" || strings.TrimSpace(e.RequestShape.Method) == "" || strings.TrimSpace(e.RequestShape.APIFormat) == "" || !isSHA256(e.RequestShape.RequestSHA256) {
		return fmt.Errorf("%w: exact request shape is required", ErrInvalidPairedScopeEvidence)
	}
	if err := e.Provenance.Validate(); err != nil {
		return err
	}
	if !isSHA256(e.LimitedResponseSHA256) || strings.TrimSpace(e.ReplacementOutcome) == "" || strings.TrimSpace(e.ReplacementTransportKind) == "" || strings.TrimSpace(e.ScopeConclusion) == "" {
		return fmt.Errorf("%w: exact limited response and replacement outcome are required", ErrInvalidPairedScopeEvidence)
	}
	if e.Decision.Class != pool.FailureAmbiguous || e.Decision.AllowOriginalReplay || e.Decision.AllowKeyRotation || e.Decision.AllowSemanticContinuation || !e.Decision.RequireInput {
		return fmt.Errorf("%w: inconclusive paired evidence must remain conservative", ErrInvalidPairedScopeEvidence)
	}
	expected, err := e.ComputeSHA256()
	if err != nil {
		return err
	}
	if !isSHA256(e.EvidenceSHA256) || !strings.EqualFold(expected, e.EvidenceSHA256) {
		return ErrEvidenceTampered
	}
	if !now.Before(e.ExpiresAt) {
		return ErrEvidenceExpired
	}
	return nil
}

func NormalizePairedScopeCapture(capture PairedScopeCapture, rawCaptureSHA string, validity time.Duration) (PairedScopeEvidence, error) {
	if err := capture.Validate(); err != nil {
		return PairedScopeEvidence{}, err
	}
	if !isSHA256(rawCaptureSHA) || validity <= 0 {
		return PairedScopeEvidence{}, fmt.Errorf("%w: raw capture hash and positive validity are required", ErrInvalidPairedScopeEvidence)
	}
	if capture.Passed || capture.Checks.ReplacementSucceeded {
		return PairedScopeEvidence{}, fmt.Errorf("%w: this normalizer is for inconclusive replacement evidence only", ErrInvalidPairedScopeEvidence)
	}
	transportKind := classifyReplacementTransport(capture.ReplacementCredential.Transport)
	if transportKind == "none" {
		return PairedScopeEvidence{}, fmt.Errorf("%w: inconclusive replacement requires preserved transport failure", ErrInvalidPairedScopeEvidence)
	}
	evidence := PairedScopeEvidence{
		SchemaVersion: 1,
		EvidenceID:    "aerolink-paired-scope-inconclusive-2026-07-07",
		Identity: ProviderIdentity{
			Provider:      capture.Target.Provider,
			APIBase:       capture.Target.APIBase,
			APIVersion:    capture.Target.AnthropicVersion,
			Model:         capture.Target.Model,
			ModelRevision: capture.Target.Model,
		},
		RequestShape: CaptureRequestShape{
			Endpoint:      capture.Target.Endpoint,
			Method:        "POST",
			APIFormat:     capture.Target.APIFormat,
			RequestSHA256: capture.LimitedCredential.BodySHA256,
		},
		TestedAt:  capture.CapturedAtUTC,
		ExpiresAt: capture.CapturedAtUTC.Add(validity),
		Provenance: EvidenceProvenance{
			CaptureID:        capture.ProofComponent,
			SourceKind:       "real-provider-bounded-paired-capture",
			SourceRef:        "Proof 0.18 Aerolink paired-scope Windows gate",
			CapturedBy:       "KeyDeck Proof 0.18",
			RawCaptureSHA256: rawCaptureSHA,
		},
		LimitedResponseSHA256:    capture.LimitedCredential.Response.BodySHA256,
		ReplacementOutcome:       "no_http_response",
		ReplacementTransportKind: transportKind,
		ScopeConclusion:          "unproven_inconclusive_replacement_transport_failure",
		Decision: PolicyDecision{
			Class:        pool.FailureAmbiguous,
			RequireInput: true,
		},
		Limitations: append([]string(nil), capture.Limitations...),
	}
	if err := evidence.Seal(); err != nil {
		return PairedScopeEvidence{}, err
	}
	if err := evidence.Validate(capture.CapturedAtUTC); err != nil {
		return PairedScopeEvidence{}, err
	}
	return evidence, nil
}

func classifyReplacementTransport(t PairedCredentialTransport) string {
	msg := strings.ToLower(strings.TrimSpace(t.TransportError))
	if msg == "" {
		return "none"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out") || strings.Contains(msg, "20 seconds elapsing") {
		return "timeout"
	}
	return "other_ambiguous_transport"
}
