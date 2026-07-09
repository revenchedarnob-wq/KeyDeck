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
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

var ErrInvalidCaptureFragment = errors.New("invalid provider capture evidence fragment")

type CaptureTarget struct {
	Provider         string `json:"provider"`
	APIBase          string `json:"api_base"`
	Endpoint         string `json:"endpoint"`
	APIFormat        string `json:"api_format"`
	AnthropicVersion string `json:"anthropic_version"`
	Model            string `json:"model"`
}

type CaptureRequest struct {
	Method          string `json:"method"`
	RequestCount    int    `json:"request_count"`
	RetryCount      int    `json:"retry_count"`
	MaxTokens       int    `json:"max_tokens"`
	KeyKind         string `json:"key_kind"`
	RealAPIKeyUsed  bool   `json:"real_api_key_used"`
	SecretPersisted bool   `json:"secret_persisted"`
	BodySHA256      string `json:"body_sha256"`
}

type CaptureResponse struct {
	StatusCode   int      `json:"status_code"`
	ReasonPhrase string   `json:"reason_phrase"`
	Headers      []string `json:"headers"`
	BodyUTF8     string   `json:"body_utf8"`
	BodySHA256   string   `json:"body_sha256"`
}

type CaptureTransport struct {
	ElapsedMS        int `json:"elapsed_ms"`
	TimeoutSeconds   int `json:"timeout_seconds"`
	AutomaticRetries int `json:"automatic_retries"`
}

type RealProviderCapture struct {
	ProofComponent string           `json:"proof_component"`
	SchemaVersion  int              `json:"schema_version"`
	CapturedAtUTC  time.Time        `json:"captured_at_utc"`
	Passed         bool             `json:"passed"`
	Target         CaptureTarget    `json:"target"`
	Request        CaptureRequest   `json:"request"`
	Response       CaptureResponse  `json:"response"`
	Transport      CaptureTransport `json:"transport"`
	Limitations    []string         `json:"limitations"`
}

func DecodeRealProviderCapture(raw []byte) (RealProviderCapture, error) {
	var capture RealProviderCapture
	if err := json.Unmarshal(raw, &capture); err != nil {
		return RealProviderCapture{}, fmt.Errorf("decode real provider capture: %w", err)
	}
	if err := capture.Validate(); err != nil {
		return RealProviderCapture{}, err
	}
	return capture, nil
}

func (c RealProviderCapture) Validate() error {
	if c.SchemaVersion != 1 || !c.Passed || c.CapturedAtUTC.IsZero() {
		return fmt.Errorf("%w: schema, passed state and capture time are required", ErrInvalidCaptureFragment)
	}
	if strings.TrimSpace(c.Target.Provider) == "" || strings.TrimSpace(c.Target.APIBase) == "" || strings.TrimSpace(c.Target.Endpoint) == "" || strings.TrimSpace(c.Target.APIFormat) == "" || strings.TrimSpace(c.Target.AnthropicVersion) == "" || strings.TrimSpace(c.Target.Model) == "" {
		return fmt.Errorf("%w: exact target identity and endpoint are required", ErrInvalidCaptureFragment)
	}
	if strings.TrimSpace(c.Request.Method) == "" || c.Request.RequestCount <= 0 || c.Request.RetryCount < 0 || c.Request.MaxTokens <= 0 || strings.TrimSpace(c.Request.KeyKind) == "" || !isSHA256(c.Request.BodySHA256) {
		return fmt.Errorf("%w: complete request evidence is required", ErrInvalidCaptureFragment)
	}
	if c.Response.StatusCode <= 0 || !isSHA256(c.Response.BodySHA256) {
		return fmt.Errorf("%w: exact response status and body hash are required", ErrInvalidCaptureFragment)
	}
	bodySum := sha256.Sum256([]byte(c.Response.BodyUTF8))
	if !strings.EqualFold(c.Response.BodySHA256, hex.EncodeToString(bodySum[:])) {
		return fmt.Errorf("%w: response body hash mismatch", ErrInvalidCaptureFragment)
	}
	if c.Transport.TimeoutSeconds <= 0 || c.Transport.AutomaticRetries < 0 {
		return fmt.Errorf("%w: transport timeout/retry evidence is required", ErrInvalidCaptureFragment)
	}
	return nil
}

type CaptureRequestShape struct {
	Endpoint      string `json:"endpoint"`
	Method        string `json:"method"`
	APIFormat     string `json:"api_format"`
	RequestSHA256 string `json:"request_sha256"`
}

type ProviderObservationFragment struct {
	SchemaVersion  int                 `json:"schema_version"`
	FragmentID     string              `json:"fragment_id"`
	Identity       ProviderIdentity    `json:"identity"`
	RequestShape   CaptureRequestShape `json:"request_shape"`
	TestedAt       time.Time           `json:"tested_at"`
	ExpiresAt      time.Time           `json:"expires_at"`
	Provenance     EvidenceProvenance  `json:"provenance"`
	Observation    FailureObservation  `json:"observation"`
	ResponseSHA256 string              `json:"response_sha256"`
	Limitations    []string            `json:"limitations"`
	EvidenceSHA256 string              `json:"evidence_sha256"`
}

func (f ProviderObservationFragment) unsignedBytes() ([]byte, error) {
	copy := f
	copy.EvidenceSHA256 = ""
	return json.Marshal(copy)
}

func (f ProviderObservationFragment) ComputeSHA256() (string, error) {
	raw, err := f.unsignedBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (f *ProviderObservationFragment) Seal() error {
	h, err := f.ComputeSHA256()
	if err != nil {
		return err
	}
	f.EvidenceSHA256 = h
	return nil
}

func (f ProviderObservationFragment) Validate(now time.Time) error {
	if f.SchemaVersion != 1 || strings.TrimSpace(f.FragmentID) == "" || f.TestedAt.IsZero() || f.ExpiresAt.IsZero() || !f.ExpiresAt.After(f.TestedAt) {
		return fmt.Errorf("%w: schema, fragment id and validity window are required", ErrInvalidCaptureFragment)
	}
	if err := f.Identity.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(f.RequestShape.Endpoint) == "" || strings.TrimSpace(f.RequestShape.Method) == "" || strings.TrimSpace(f.RequestShape.APIFormat) == "" || !isSHA256(f.RequestShape.RequestSHA256) {
		return fmt.Errorf("%w: exact request shape is required", ErrInvalidCaptureFragment)
	}
	if err := f.Provenance.Validate(); err != nil {
		return err
	}
	if err := f.Observation.Validate(); err != nil {
		return err
	}
	if !isSHA256(f.ResponseSHA256) {
		return fmt.Errorf("%w: exact response body hash is required", ErrInvalidCaptureFragment)
	}
	expected, err := f.ComputeSHA256()
	if err != nil {
		return err
	}
	if !isSHA256(f.EvidenceSHA256) || !strings.EqualFold(expected, f.EvidenceSHA256) {
		return ErrEvidenceTampered
	}
	if !now.Before(f.ExpiresAt) {
		return ErrEvidenceExpired
	}
	return nil
}

func NormalizeInvalidCredentialCapture(capture RealProviderCapture, rawCaptureSHA string, validity time.Duration) (ProviderObservationFragment, error) {
	if err := capture.Validate(); err != nil {
		return ProviderObservationFragment{}, err
	}
	if !isSHA256(rawCaptureSHA) || validity <= 0 {
		return ProviderObservationFragment{}, fmt.Errorf("%w: raw capture hash and positive validity are required", ErrInvalidCaptureFragment)
	}
	if capture.Request.RequestCount != 1 || capture.Request.RetryCount != 0 || capture.Transport.AutomaticRetries != 0 || capture.Request.RealAPIKeyUsed || !strings.EqualFold(capture.Request.KeyKind, "intentionally_invalid_fixture") {
		return ProviderObservationFragment{}, fmt.Errorf("%w: invalid-key proof must use one request, zero retries and no real key", ErrInvalidCaptureFragment)
	}
	if capture.Response.StatusCode != 401 || !strings.Contains(strings.ToLower(capture.Response.BodyUTF8), "invalid token") {
		return ProviderObservationFragment{}, fmt.Errorf("%w: capture does not prove the expected invalid-token behavior", ErrInvalidCaptureFragment)
	}
	fragment := ProviderObservationFragment{
		SchemaVersion: 1,
		FragmentID:    "aerolink-invalid-key-2026-07-07",
		Identity: ProviderIdentity{
			Provider:      capture.Target.Provider,
			APIBase:       capture.Target.APIBase,
			APIVersion:    capture.Target.AnthropicVersion,
			Model:         capture.Target.Model,
			ModelRevision: capture.Target.Model,
		},
		RequestShape: CaptureRequestShape{
			Endpoint:      capture.Target.Endpoint,
			Method:        capture.Request.Method,
			APIFormat:     capture.Target.APIFormat,
			RequestSHA256: capture.Request.BodySHA256,
		},
		TestedAt:  capture.CapturedAtUTC,
		ExpiresAt: capture.CapturedAtUTC.Add(validity),
		Provenance: EvidenceProvenance{
			CaptureID:        capture.ProofComponent,
			SourceKind:       "real-provider-one-request-capture",
			SourceRef:        "Proof 0.16 Aerolink invalid-key Windows gate",
			CapturedBy:       "KeyDeck Proof 0.16",
			RawCaptureSHA256: rawCaptureSHA,
		},
		Observation: FailureObservation{
			ScenarioID:    "invalid_credential_pre_output",
			Phase:         PhasePreOutput,
			StatusCode:    capture.Response.StatusCode,
			ErrorCode:     "invalid_token",
			ErrorScope:    "key",
			TerminalEvent: true,
			Decision: PolicyDecision{
				Class:               pool.FailureInvalidKey,
				AllowOriginalReplay: true,
				AllowKeyRotation:    true,
			},
		},
		ResponseSHA256: capture.Response.BodySHA256,
		Limitations:    append([]string(nil), capture.Limitations...),
	}
	if err := fragment.Seal(); err != nil {
		return ProviderObservationFragment{}, err
	}
	if err := fragment.Validate(capture.CapturedAtUTC); err != nil {
		return ProviderObservationFragment{}, err
	}
	return fragment, nil
}

func NormalizeUsageWindowLimitCapture(capture RealProviderCapture, rawCaptureSHA string, validity time.Duration) (ProviderObservationFragment, error) {
	if err := capture.Validate(); err != nil {
		return ProviderObservationFragment{}, err
	}
	if !isSHA256(rawCaptureSHA) || validity <= 0 {
		return ProviderObservationFragment{}, fmt.Errorf("%w: raw capture hash and positive validity are required", ErrInvalidCaptureFragment)
	}
	if capture.Request.RequestCount != 1 || capture.Request.RetryCount != 0 || capture.Transport.AutomaticRetries != 0 || !capture.Request.RealAPIKeyUsed || capture.Request.SecretPersisted || !strings.EqualFold(capture.Request.KeyKind, "real_already_exhausted_key") {
		return ProviderObservationFragment{}, fmt.Errorf("%w: usage-window proof must use one request, zero retries, a real already-exhausted key and no persisted secret", ErrInvalidCaptureFragment)
	}
	bodyLower := strings.ToLower(capture.Response.BodyUTF8)
	if capture.Response.StatusCode != 402 || !strings.Contains(bodyLower, "5-hour included-usage limit reached") || !strings.Contains(bodyLower, "$10.00 allowance") {
		return ProviderObservationFragment{}, fmt.Errorf("%w: capture does not prove the expected Aerolink included-usage window limit behavior", ErrInvalidCaptureFragment)
	}
	fragment := ProviderObservationFragment{
		SchemaVersion: 1,
		FragmentID:    "aerolink-included-usage-window-limit-2026-07-07",
		Identity: ProviderIdentity{
			Provider:      capture.Target.Provider,
			APIBase:       capture.Target.APIBase,
			APIVersion:    capture.Target.AnthropicVersion,
			Model:         capture.Target.Model,
			ModelRevision: capture.Target.Model,
		},
		RequestShape: CaptureRequestShape{
			Endpoint:      capture.Target.Endpoint,
			Method:        capture.Request.Method,
			APIFormat:     capture.Target.APIFormat,
			RequestSHA256: capture.Request.BodySHA256,
		},
		TestedAt:  capture.CapturedAtUTC,
		ExpiresAt: capture.CapturedAtUTC.Add(validity),
		Provenance: EvidenceProvenance{
			CaptureID:        capture.ProofComponent,
			SourceKind:       "real-provider-one-request-capture",
			SourceRef:        "Proof 0.17 Aerolink usage-window-limit Windows gate",
			CapturedBy:       "KeyDeck Proof 0.17",
			RawCaptureSHA256: rawCaptureSHA,
		},
		Observation: FailureObservation{
			ScenarioID:    "included_usage_window_limit_pre_output",
			Phase:         PhasePreOutput,
			StatusCode:    capture.Response.StatusCode,
			ErrorCode:     "included_usage_window_limit",
			ErrorScope:    "unknown",
			TerminalEvent: true,
			Decision: PolicyDecision{
				Class:        pool.FailureAmbiguous,
				RequireInput: true,
			},
		},
		ResponseSHA256: capture.Response.BodySHA256,
		Limitations:    append([]string(nil), capture.Limitations...),
	}
	if err := fragment.Seal(); err != nil {
		return ProviderObservationFragment{}, err
	}
	if err := fragment.Validate(capture.CapturedAtUTC); err != nil {
		return ProviderObservationFragment{}, err
	}
	return fragment, nil
}

type FragmentRegistry struct {
	fragments []ProviderObservationFragment
}

func (r *FragmentRegistry) Add(fragment ProviderObservationFragment, now time.Time) error {
	if err := fragment.Validate(now); err != nil {
		return err
	}
	for _, existing := range r.fragments {
		if existing.FragmentID == fragment.FragmentID {
			return fmt.Errorf("%w: duplicate fragment id", ErrInvalidCaptureFragment)
		}
	}
	r.fragments = append(r.fragments, fragment)
	return nil
}

func (r FragmentRegistry) Decide(identity ProviderIdentity, endpoint string, phase ObservationPhase, resp providerhttp.Response, transportErr error, now time.Time) ConformanceDecision {
	if transportErr != nil {
		return conservativeDecision("transport failure is ambiguous", false)
	}
	bodySum := sha256.Sum256(resp.Body)
	bodySHA := hex.EncodeToString(bodySum[:])

	matchedIdentityEndpoint := false
	exactExpired := false
	invalidEvidenceSeen := false

	for _, fragment := range r.fragments {
		if !sameIdentity(fragment.Identity, identity) || !strings.EqualFold(strings.TrimSpace(fragment.RequestShape.Endpoint), strings.TrimSpace(endpoint)) {
			continue
		}
		matchedIdentityEndpoint = true

		exactResponse := phase == fragment.Observation.Phase &&
			resp.StatusCode == fragment.Observation.StatusCode &&
			strings.EqualFold(bodySHA, fragment.ResponseSHA256)

		if err := fragment.Validate(now); err != nil {
			if exactResponse && errors.Is(err, ErrEvidenceExpired) {
				exactExpired = true
			} else if !errors.Is(err, ErrEvidenceExpired) {
				invalidEvidenceSeen = true
			}
			continue
		}
		if !exactResponse {
			continue
		}

		d := fragment.Observation.Decision
		return ConformanceDecision{
			Trusted: true, Class: d.Class, AllowOriginalReplay: d.AllowOriginalReplay, AllowKeyRotation: d.AllowKeyRotation,
			AllowSemanticContinuation: d.AllowSemanticContinuation, RequireInput: d.RequireInput,
			EvidenceID: fragment.FragmentID, EvidenceSHA256: fragment.EvidenceSHA256, Reason: "exact real-provider capture fragment match",
		}
	}

	if exactExpired {
		return conservativeDecision("exact capture fragment expired; revalidation required", true)
	}
	if invalidEvidenceSeen {
		return conservativeDecision("matching provider evidence included invalid or tampered fragments", true)
	}
	if matchedIdentityEndpoint {
		return conservativeDecision("provider identity matched but exact captured response did not", false)
	}
	return conservativeDecision("no exact real-provider capture fragment", false)
}
