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
var rawInvalidCapture []byte

//go:embed aerolink-usage-window-limit-capture.json
var rawUsageWindowCapture []byte

const (
	expectedInvalidCaptureSHA     = "b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3"
	expectedUsageWindowCaptureSHA = "f1b07af5a186f96709151bc70f541c2b097d9469c25281f726a9bfc44a1532f3"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                         string     `json:"proof"`
	Status                        string     `json:"status"`
	Passed                        bool       `json:"passed"`
	Scenarios                     []scenario `json:"scenarios"`
	InvalidCaptureSHA256          string     `json:"invalid_capture_sha256"`
	UsageWindowCaptureSHA256      string     `json:"usage_window_capture_sha256"`
	UsageWindowResponseBodySHA256 string     `json:"usage_window_response_body_sha256"`
	UsageWindowEvidenceFragmentID string     `json:"usage_window_evidence_fragment_id"`
	UsageWindowEvidenceSHA256     string     `json:"usage_window_evidence_sha256"`
	ObservedBehavior              string     `json:"observed_behavior"`
	PolicyStatus                  string     `json:"policy_status"`
	Limitations                   []string   `json:"limitations"`
	NextGate                      string     `json:"next_gate"`
}

func main() {
	out := report{Proof: "0.17-second-real-provider-evidence-capture", Status: "failed"}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	invalidSHA := sha256Hex(rawInvalidCapture)
	usageSHA := sha256Hex(rawUsageWindowCapture)
	out.InvalidCaptureSHA256 = invalidSHA
	out.UsageWindowCaptureSHA256 = usageSHA
	add("invalid_capture_sha256_still_matches_proof_0_16", strings.EqualFold(invalidSHA, expectedInvalidCaptureSHA), invalidSHA)
	add("usage_window_capture_sha256_matches_windows_gate", strings.EqualFold(usageSHA, expectedUsageWindowCaptureSHA), usageSHA)

	invalidCapture, err := conformance.DecodeRealProviderCapture(rawInvalidCapture)
	add("prior_invalid_key_capture_still_validates", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	usageCapture, err := conformance.DecodeRealProviderCapture(rawUsageWindowCapture)
	add("real_usage_window_capture_schema_and_body_hash_validate", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	out.UsageWindowResponseBodySHA256 = usageCapture.Response.BodySHA256

	safeRequest := usageCapture.Request.RequestCount == 1 &&
		usageCapture.Request.RetryCount == 0 &&
		usageCapture.Transport.AutomaticRetries == 0 &&
		usageCapture.Request.RealAPIKeyUsed &&
		!usageCapture.Request.SecretPersisted &&
		usageCapture.Request.MaxTokens == 1
	add("capture_used_one_real_already_limited_key_request_zero_retries_without_persisting_secret", safeRequest, map[string]any{
		"request_count":     usageCapture.Request.RequestCount,
		"retry_count":       usageCapture.Request.RetryCount,
		"automatic_retries": usageCapture.Transport.AutomaticRetries,
		"real_api_key_used": usageCapture.Request.RealAPIKeyUsed,
		"secret_persisted":  usageCapture.Request.SecretPersisted,
		"max_tokens":        usageCapture.Request.MaxTokens,
	})

	exactTarget := usageCapture.Target.Provider == "Aerolink" &&
		usageCapture.Target.APIBase == "https://capi.aerolink.lat" &&
		usageCapture.Target.Endpoint == "/v1/messages" &&
		usageCapture.Target.AnthropicVersion == "2023-06-01" &&
		usageCapture.Target.Model == "claude-opus-4-8"
	add("exact_aerolink_target_preserved", exactTarget, usageCapture.Target)

	exactResponse := usageCapture.Response.StatusCode == 402 &&
		strings.Contains(usageCapture.Response.BodyUTF8, "5-hour included-usage limit reached") &&
		strings.Contains(usageCapture.Response.BodyUTF8, "$10.00 allowance")
	add("exact_402_included_usage_window_response_preserved", exactResponse, map[string]any{
		"status_code": usageCapture.Response.StatusCode,
		"body_sha256": usageCapture.Response.BodySHA256,
	})

	invalidFragment, err := conformance.NormalizeInvalidCredentialCapture(invalidCapture, invalidSHA, 30*24*time.Hour)
	add("proof_0_16_invalid_key_fragment_still_normalizes", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	usageFragment, err := conformance.NormalizeUsageWindowLimitCapture(usageCapture, usageSHA, 30*24*time.Hour)
	add("usage_window_capture_normalizes_to_scoped_fragment", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	out.UsageWindowEvidenceFragmentID = usageFragment.FragmentID
	out.UsageWindowEvidenceSHA256 = usageFragment.EvidenceSHA256
	out.Limitations = append([]string(nil), usageFragment.Limitations...)

	add("usage_window_fragment_binds_raw_capture_provenance", usageFragment.Provenance.RawCaptureSHA256 == usageSHA, usageFragment.Provenance)
	conservativePolicy := usageFragment.Observation.Decision.Class == pool.FailureAmbiguous &&
		usageFragment.Observation.Decision.RequireInput &&
		!usageFragment.Observation.Decision.AllowOriginalReplay &&
		!usageFragment.Observation.Decision.AllowKeyRotation &&
		!usageFragment.Observation.Decision.AllowSemanticContinuation
	add("usage_window_scope_not_overclaimed_and_policy_remains_conservative", conservativePolicy, usageFragment.Observation)

	store := conformance.FragmentStore{Path: filepath.Join(os.TempDir(), "keydeck-proof17", "aerolink-usage-window-fragment.json")}
	_ = os.RemoveAll(filepath.Dir(store.Path))
	saveErr := store.Save(usageFragment, usageCapture.CapturedAtUTC)
	loaded, loadErr := store.Load(usageCapture.CapturedAtUTC)
	add("durable_usage_window_fragment_round_trip_preserves_identity", saveErr == nil && loadErr == nil && loaded.EvidenceSHA256 == usageFragment.EvidenceSHA256, map[string]any{
		"save_error":  errorString(saveErr),
		"load_error":  errorString(loadErr),
		"hash_stable": loaded.EvidenceSHA256 == usageFragment.EvidenceSHA256,
	})

	var registry conformance.FragmentRegistry
	addInvalidErr := registry.Add(invalidFragment, usageCapture.CapturedAtUTC)
	addUsageErr := registry.Add(usageFragment, usageCapture.CapturedAtUTC)
	add("multiple_real_behaviors_for_same_provider_endpoint_can_coexist", addInvalidErr == nil && addUsageErr == nil, map[string]any{
		"invalid_add_error":      errorString(addInvalidErr),
		"usage_window_add_error": errorString(addUsageErr),
	})

	invalidDecision := registry.Decide(invalidFragment.Identity, invalidFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: invalidCapture.Response.StatusCode, Body: []byte(invalidCapture.Response.BodyUTF8)}, nil, usageCapture.CapturedAtUTC)
	add("prior_401_invalid_key_behavior_still_matches_after_second_fragment", invalidDecision.Trusted && invalidDecision.Class == pool.FailureInvalidKey && invalidDecision.AllowOriginalReplay && invalidDecision.AllowKeyRotation, invalidDecision)

	usageDecision := registry.Decide(usageFragment.Identity, usageFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: usageCapture.Response.StatusCode, Body: []byte(usageCapture.Response.BodyUTF8)}, nil, usageCapture.CapturedAtUTC)
	add("exact_402_usage_window_behavior_matches_but_does_not_auto_rotate", usageDecision.Trusted && usageDecision.Class == pool.FailureAmbiguous && usageDecision.RequireInput && !usageDecision.AllowOriginalReplay && !usageDecision.AllowKeyRotation && !usageDecision.AllowSemanticContinuation, usageDecision)
	out.ObservedBehavior = "exact Aerolink HTTP 402 response proves a 5-hour included-usage window limit for the captured provider/API/model request shape"
	out.PolicyStatus = "scope is not proven key-specific, so automatic replay and key rotation remain forbidden"

	bodyMismatch := registry.Decide(usageFragment.Identity, usageFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: 402, Body: []byte(`{"error":"other"}`)}, nil, usageCapture.CapturedAtUTC)
	add("usage_window_response_body_mismatch_fails_closed", !bodyMismatch.Trusted && bodyMismatch.Class == pool.FailureAmbiguous && !bodyMismatch.AllowKeyRotation, bodyMismatch)

	endpointMismatch := registry.Decide(usageFragment.Identity, "/v1/other", conformance.PhasePreOutput, providerhttp.Response{StatusCode: 402, Body: []byte(usageCapture.Response.BodyUTF8)}, nil, usageCapture.CapturedAtUTC)
	add("usage_window_endpoint_mismatch_fails_closed", !endpointMismatch.Trusted && endpointMismatch.Class == pool.FailureAmbiguous, endpointMismatch)

	identityMismatch := usageFragment.Identity
	identityMismatch.ModelRevision = "different"
	modelMismatch := registry.Decide(identityMismatch, usageFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: 402, Body: []byte(usageCapture.Response.BodyUTF8)}, nil, usageCapture.CapturedAtUTC)
	add("usage_window_model_version_mismatch_fails_closed", !modelMismatch.Trusted && modelMismatch.Class == pool.FailureAmbiguous, modelMismatch)

	expired := registry.Decide(usageFragment.Identity, usageFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: 402, Body: []byte(usageCapture.Response.BodyUTF8)}, nil, usageFragment.ExpiresAt.Add(time.Second))
	add("expired_usage_window_fragment_requires_revalidation", !expired.Trusted && expired.RequireRevalidation && expired.Class == pool.FailureAmbiguous, expired)

	tampered := usageFragment
	tampered.ResponseSHA256 = strings.Repeat("a", 64)
	tamperErr := tampered.Validate(usageCapture.CapturedAtUTC)
	add("tampered_usage_window_fragment_rejected", errors.Is(tamperErr, conformance.ErrEvidenceTampered), errorString(tamperErr))

	noOverclaim := len(usageFragment.Limitations) >= 4 && usageFragment.Observation.ErrorScope == "unknown"
	add("limitations_preserved_and_key_scope_not_claimed", noOverclaim, usageFragment.Limitations)

	cleanRaw := !strings.Contains(string(rawUsageWindowCapture), `C:\Users\`) &&
		!strings.Contains(strings.ToLower(string(rawUsageWindowCapture)), "sk-") &&
		!usageCapture.Request.SecretPersisted
	add("source_evidence_contains_no_user_path_or_persisted_secret", cleanRaw, map[string]any{
		"user_path_present": strings.Contains(string(rawUsageWindowCapture), `C:\Users\`),
		"secret_persisted":  usageCapture.Request.SecretPersisted,
	})

	allPassed := true
	for _, s := range out.Scenarios {
		allPassed = allPassed && s.Passed
	}
	out.Passed = allPassed
	if allPassed {
		out.Status = "passed"
	}
	out.NextGate = "Prove whether the exact Aerolink 402 included-usage-window response is replacement-key/account scoped by a bounded paired capture: limited key returns the exact 402, then a separate known-usable replacement key succeeds on the same request shape. Until then, preserve backups and require input."
	emit(out, boolCode(!allPassed))
}

func sha256Hex(raw []byte) string {
	// Historical provider captures were recorded by Windows gates using CRLF.
	// Git normalizes text fixtures to LF, so reconstruct the recorded bytes
	// before checking or propagating raw-capture provenance.
	canonical := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	canonical = bytes.ReplaceAll(canonical, []byte("\n"), []byte("\r\n"))
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
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
