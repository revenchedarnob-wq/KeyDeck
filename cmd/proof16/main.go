package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/conformance"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

//go:embed aerolink-invalid-key-capture.json
var rawCapture []byte

const expectedRawCaptureSHA = "b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3"

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                  string     `json:"proof"`
	Status                 string     `json:"status"`
	Passed                 bool       `json:"passed"`
	Scenarios              []scenario `json:"scenarios"`
	RawCaptureSHA256       string     `json:"raw_capture_sha256"`
	EvidenceFragmentID     string     `json:"evidence_fragment_id"`
	EvidenceFragmentSHA256 string     `json:"evidence_fragment_sha256"`
	TrustedBehavior        string     `json:"trusted_behavior"`
	Limitations            []string   `json:"limitations"`
	NextGate               string     `json:"next_gate"`
}

func main() {
	out := report{Proof: "0.16-first-real-provider-evidence-capture", Status: "failed"}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	// The original Windows capture used CRLF. Git stores text fixtures with LF,
	// so reconstruct the recorded byte form before checking historical provenance.
	canonicalRawCapture := bytes.ReplaceAll(rawCapture, []byte("\r\n"), []byte("\n"))
	canonicalRawCapture = bytes.ReplaceAll(canonicalRawCapture, []byte("\n"), []byte("\r\n"))
	rawSum := sha256.Sum256(canonicalRawCapture)
	rawSHA := hex.EncodeToString(rawSum[:])
	out.RawCaptureSHA256 = rawSHA
	add("raw_capture_sha256_matches_windows_gate", strings.EqualFold(rawSHA, expectedRawCaptureSHA), rawSHA)

	capture, err := conformance.DecodeRealProviderCapture(rawCapture)
	add("real_capture_schema_and_body_hash_validate", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}

	safeRequest := capture.Request.RequestCount == 1 && capture.Request.RetryCount == 0 && capture.Transport.AutomaticRetries == 0 && !capture.Request.RealAPIKeyUsed && capture.Request.MaxTokens == 1
	add("capture_used_one_invalid_key_request_zero_retries", safeRequest, map[string]any{"request_count": capture.Request.RequestCount, "retry_count": capture.Request.RetryCount, "automatic_retries": capture.Transport.AutomaticRetries, "real_api_key_used": capture.Request.RealAPIKeyUsed, "max_tokens": capture.Request.MaxTokens})
	add("exact_aerolink_target_preserved", capture.Target.Provider == "Aerolink" && capture.Target.APIBase == "https://capi.aerolink.lat" && capture.Target.Endpoint == "/v1/messages" && capture.Target.AnthropicVersion == "2023-06-01" && capture.Target.Model == "claude-opus-4-8", capture.Target)
	add("exact_401_invalid_token_response_preserved", capture.Response.StatusCode == 401 && strings.Contains(capture.Response.BodyUTF8, "Invalid token"), map[string]any{"status_code": capture.Response.StatusCode, "body_sha256": capture.Response.BodySHA256})

	fragment, err := conformance.NormalizeInvalidCredentialCapture(capture, rawSHA, 30*24*time.Hour)
	add("real_capture_normalizes_to_scoped_fragment", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	out.EvidenceFragmentID = fragment.FragmentID
	out.EvidenceFragmentSHA256 = fragment.EvidenceSHA256
	out.Limitations = append([]string(nil), fragment.Limitations...)

	add("normalized_fragment_binds_raw_capture_provenance", fragment.Provenance.RawCaptureSHA256 == rawSHA, fragment.Provenance)
	add("normalized_fragment_has_single_scoped_observation", fragment.Observation.Decision.Class == pool.FailureInvalidKey && fragment.Observation.Phase == conformance.PhasePreOutput && fragment.Observation.StatusCode == 401, fragment.Observation)

	store := conformance.FragmentStore{Path: filepath.Join(os.TempDir(), "keydeck-proof16", "aerolink-invalid-key-fragment.json")}
	_ = os.RemoveAll(filepath.Dir(store.Path))
	saveErr := store.Save(fragment, capture.CapturedAtUTC)
	loaded, loadErr := store.Load(capture.CapturedAtUTC)
	add("durable_fragment_round_trip_preserves_identity", saveErr == nil && loadErr == nil && loaded.EvidenceSHA256 == fragment.EvidenceSHA256, map[string]any{"save_error": errorString(saveErr), "load_error": errorString(loadErr), "hash_stable": loaded.EvidenceSHA256 == fragment.EvidenceSHA256})

	var registry conformance.FragmentRegistry
	addErr := registry.Add(fragment, capture.CapturedAtUTC)
	exact := registry.Decide(fragment.Identity, fragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: capture.Response.StatusCode, Body: []byte(capture.Response.BodyUTF8)}, nil, capture.CapturedAtUTC)
	add("exact_real_response_authorizes_invalid_key_rotation", addErr == nil && exact.Trusted && exact.Class == pool.FailureInvalidKey && exact.AllowOriginalReplay && exact.AllowKeyRotation, exact)
	out.TrustedBehavior = "exact pre-output Aerolink 401 response with the captured body hash is invalid_key; replacement-key selection and replay are allowed only for that exact evidence match"

	bodyMismatch := registry.Decide(fragment.Identity, fragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: 401, Body: []byte(`{"error":"other"}`)}, nil, capture.CapturedAtUTC)
	add("response_body_mismatch_fails_closed", !bodyMismatch.Trusted && bodyMismatch.Class == pool.FailureAmbiguous && !bodyMismatch.AllowKeyRotation, bodyMismatch)

	endpointMismatch := registry.Decide(fragment.Identity, "/v1/other", conformance.PhasePreOutput, providerhttp.Response{StatusCode: 401, Body: []byte(capture.Response.BodyUTF8)}, nil, capture.CapturedAtUTC)
	add("endpoint_mismatch_fails_closed", !endpointMismatch.Trusted && endpointMismatch.Class == pool.FailureAmbiguous, endpointMismatch)

	identityMismatch := fragment.Identity
	identityMismatch.ModelRevision = "different"
	modelMismatch := registry.Decide(identityMismatch, fragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: 401, Body: []byte(capture.Response.BodyUTF8)}, nil, capture.CapturedAtUTC)
	add("model_version_mismatch_fails_closed", !modelMismatch.Trusted && modelMismatch.Class == pool.FailureAmbiguous, modelMismatch)

	expired := registry.Decide(fragment.Identity, fragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: 401, Body: []byte(capture.Response.BodyUTF8)}, nil, fragment.ExpiresAt.Add(time.Second))
	add("expired_fragment_requires_revalidation", !expired.Trusted && expired.RequireRevalidation && expired.Class == pool.FailureAmbiguous, expired)

	tampered := fragment
	tampered.ResponseSHA256 = strings.Repeat("a", 64)
	tamperErr := tampered.Validate(capture.CapturedAtUTC)
	add("tampered_fragment_rejected", errors.Is(tamperErr, conformance.ErrEvidenceTampered), errorString(tamperErr))

	noOverclaim := len(fragment.Limitations) >= 3 && fragment.Observation.ScenarioID == "invalid_credential_pre_output"
	add("limitations_preserved_and_no_other_provider_semantics_claimed", noOverclaim, fragment.Limitations)

	cleanRaw := !strings.Contains(string(rawCapture), `C:\Users\`) && !strings.Contains(strings.ToLower(string(rawCapture)), "api_key\"")
	add("normalized_source_evidence_contains_no_user_path_or_real_key", cleanRaw, map[string]any{"user_path_present": strings.Contains(string(rawCapture), `C:\Users\`), "real_api_key_used": capture.Request.RealAPIKeyUsed})

	allPassed := true
	for _, s := range out.Scenarios {
		allPassed = allPassed && s.Passed
	}
	out.Passed = allPassed
	if allPassed {
		out.Status = "passed"
	}
	out.NextGate = "Continue real-provider evidence capture for exhaustion, key-specific rate limit, provider-wide busy/outage, ambiguous transport, streaming interruption, and cache/billing semantics before promoting Aerolink to a full trusted provider profile."
	emit(out, boolCode(!allPassed))
}

func emit(out report, code int) {
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	os.Exit(code)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func boolCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}
