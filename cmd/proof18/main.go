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

//go:embed aerolink-paired-scope-capture.json
var rawPairedCapture []byte

//go:embed aerolink-invalid-key-capture.json
var rawInvalidCapture []byte

//go:embed aerolink-usage-window-limit-capture.json
var rawUsageWindowCapture []byte

const (
	expectedPairedCaptureSHA  = "ee2ebaa92ab8bf97661a1a594939ede3f0109442b28190f5f21080a9fdbf81b8"
	expectedInvalidCaptureSHA = "b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3"
	expectedUsageCaptureSHA   = "f1b07af5a186f96709151bc70f541c2b097d9469c25281f726a9bfc44a1532f3"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                    string     `json:"proof"`
	Status                   string     `json:"status"`
	Passed                   bool       `json:"passed"`
	Scenarios                []scenario `json:"scenarios"`
	PairedCaptureSHA256      string     `json:"paired_capture_sha256"`
	EvidenceID               string     `json:"evidence_id"`
	EvidenceSHA256           string     `json:"evidence_sha256"`
	LimitedResponseSHA256    string     `json:"limited_response_sha256"`
	ReplacementOutcome       string     `json:"replacement_outcome"`
	ReplacementTransportKind string     `json:"replacement_transport_kind"`
	ScopeConclusion          string     `json:"scope_conclusion"`
	PolicyStatus             string     `json:"policy_status"`
	Limitations              []string   `json:"limitations"`
	NextGate                 string     `json:"next_gate"`
}

func main() {
	out := report{Proof: "0.18-bounded-paired-scope-capture-inconclusive-safe-handling", Status: "failed"}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	pairedSHA := sha256Hex(rawPairedCapture)
	invalidSHA := sha256Hex(rawInvalidCapture)
	usageSHA := sha256Hex(rawUsageWindowCapture)
	out.PairedCaptureSHA256 = pairedSHA
	add("paired_capture_sha256_matches_windows_gate", strings.EqualFold(pairedSHA, expectedPairedCaptureSHA), pairedSHA)
	add("prior_real_capture_hashes_remain_unchanged", strings.EqualFold(invalidSHA, expectedInvalidCaptureSHA) && strings.EqualFold(usageSHA, expectedUsageCaptureSHA), map[string]string{
		"invalid_capture_sha256": invalidSHA,
		"usage_capture_sha256":   usageSHA,
	})

	capture, err := conformance.DecodePairedScopeCapture(rawPairedCapture)
	add("real_paired_capture_schema_and_bounded_sequence_validate", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}

	exactTarget := capture.Target.Provider == "Aerolink" &&
		capture.Target.APIBase == "https://capi.aerolink.lat" &&
		capture.Target.Endpoint == "/v1/messages" &&
		capture.Target.AnthropicVersion == "2023-06-01" &&
		capture.Target.Model == "claude-opus-4-8"
	add("exact_aerolink_target_preserved", exactTarget, capture.Target)

	boundedSafety := capture.Checks.TotalRequestCount == 2 && capture.Checks.TotalRetryCount == 0 && !capture.Checks.SecretsPersisted &&
		capture.LimitedCredential.RequestCount == 1 && capture.LimitedCredential.RetryCount == 0 && capture.LimitedCredential.MaxTokens == 1 && !capture.LimitedCredential.SecretPersisted &&
		capture.ReplacementCredential.RequestCount == 1 && capture.ReplacementCredential.RetryCount == 0 && capture.ReplacementCredential.MaxTokens == 1 && !capture.ReplacementCredential.SecretPersisted &&
		capture.LimitedCredential.Transport.AutomaticRetries == 0 && capture.ReplacementCredential.Transport.AutomaticRetries == 0
	add("paired_gate_used_two_requests_zero_retries_max_tokens_one_and_persisted_no_secrets", boundedSafety, capture.Checks)

	exactLimited := capture.Checks.LimitedExact402Match &&
		capture.LimitedCredential.Response.StatusCode == 402 &&
		strings.EqualFold(capture.LimitedCredential.Response.BodySHA256, capture.ExpectedLimitedBehavior.BodySHA256) &&
		strings.Contains(capture.LimitedCredential.Response.BodyUTF8, "5-hour included-usage limit reached")
	add("credential_a_reproduced_exact_known_402_before_backup_was_used", exactLimited, map[string]any{
		"status_code": capture.LimitedCredential.Response.StatusCode,
		"body_sha256": capture.LimitedCredential.Response.BodySHA256,
	})

	replacementTimeout := capture.Checks.ReplacementAttempted && !capture.Checks.ReplacementSucceeded && capture.ReplacementCredential.Response.StatusCode == 0 &&
		capture.ReplacementCredential.Transport.ElapsedMS >= capture.ReplacementCredential.Transport.TimeoutSeconds*1000 &&
		strings.Contains(strings.ToLower(capture.ReplacementCredential.Transport.TransportError), "timeout")
	add("credential_b_was_attempted_once_but_produced_ambiguous_timeout_without_http_response", replacementTimeout, map[string]any{
		"status_code":     capture.ReplacementCredential.Response.StatusCode,
		"elapsed_ms":      capture.ReplacementCredential.Transport.ElapsedMS,
		"timeout_seconds": capture.ReplacementCredential.Transport.TimeoutSeconds,
		"transport_error": capture.ReplacementCredential.Transport.TransportError,
	})

	add("paired_capture_correctly_did_not_claim_pass", !capture.Passed && !capture.Checks.ReplacementSucceeded, map[string]bool{
		"capture_passed":        capture.Passed,
		"replacement_succeeded": capture.Checks.ReplacementSucceeded,
	})
	add("request_shape_remained_identical_across_credentials", capture.Checks.RequestShapeIdentical && strings.EqualFold(capture.LimitedCredential.BodySHA256, capture.ReplacementCredential.BodySHA256), capture.LimitedCredential.BodySHA256)

	evidence, err := conformance.NormalizePairedScopeCapture(capture, pairedSHA, 30*24*time.Hour)
	add("inconclusive_pair_normalizes_to_sha256_sealed_evidence", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	out.EvidenceID = evidence.EvidenceID
	out.EvidenceSHA256 = evidence.EvidenceSHA256
	out.LimitedResponseSHA256 = evidence.LimitedResponseSHA256
	out.ReplacementOutcome = evidence.ReplacementOutcome
	out.ReplacementTransportKind = evidence.ReplacementTransportKind
	out.ScopeConclusion = evidence.ScopeConclusion
	out.Limitations = append([]string(nil), evidence.Limitations...)
	add("paired_evidence_binds_raw_capture_provenance", strings.EqualFold(evidence.Provenance.RawCaptureSHA256, pairedSHA), evidence.Provenance)
	add("replacement_transport_is_classified_as_timeout_not_http_failure", evidence.ReplacementTransportKind == "timeout" && evidence.ReplacementOutcome == "no_http_response", map[string]string{
		"outcome":        evidence.ReplacementOutcome,
		"transport_kind": evidence.ReplacementTransportKind,
	})
	add("scope_remains_unproven_after_replacement_timeout", evidence.ScopeConclusion == "unproven_inconclusive_replacement_transport_failure", evidence.ScopeConclusion)

	conservativePolicy := evidence.Decision.Class == pool.FailureAmbiguous && evidence.Decision.RequireInput &&
		!evidence.Decision.AllowOriginalReplay && !evidence.Decision.AllowKeyRotation && !evidence.Decision.AllowSemanticContinuation
	add("paired_timeout_cannot_authorize_replay_rotation_or_semantic_continuation", conservativePolicy, evidence.Decision)
	out.PolicyStatus = "replacement transport timeout is ambiguous; scope remains unproven and automatic replay/rotation stay forbidden"

	store := conformance.PairedScopeEvidenceStore{Path: filepath.Join(os.TempDir(), "keydeck-proof18", "aerolink-paired-scope-evidence.json")}
	_ = os.RemoveAll(filepath.Dir(store.Path))
	saveErr := store.Save(evidence, capture.CapturedAtUTC)
	loaded, loadErr := store.Load(capture.CapturedAtUTC)
	add("durable_paired_evidence_round_trip_preserves_identity", saveErr == nil && loadErr == nil && loaded.EvidenceSHA256 == evidence.EvidenceSHA256, map[string]any{
		"save_error":  errorString(saveErr),
		"load_error":  errorString(loadErr),
		"hash_stable": loaded.EvidenceSHA256 == evidence.EvidenceSHA256,
	})

	expiredErr := evidence.Validate(evidence.ExpiresAt.Add(time.Second))
	add("expired_paired_evidence_requires_revalidation", errors.Is(expiredErr, conformance.ErrEvidenceExpired), errorString(expiredErr))
	tampered := evidence
	tampered.ScopeConclusion = "key_specific"
	tamperErr := tampered.Validate(capture.CapturedAtUTC)
	add("tampered_paired_evidence_is_rejected", errors.Is(tamperErr, conformance.ErrEvidenceTampered), errorString(tamperErr))

	invalidCapture, err := conformance.DecodeRealProviderCapture(rawInvalidCapture)
	add("prior_invalid_key_capture_still_validates", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	usageCapture, err := conformance.DecodeRealProviderCapture(rawUsageWindowCapture)
	add("prior_usage_window_capture_still_validates", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	invalidFragment, err := conformance.NormalizeInvalidCredentialCapture(invalidCapture, invalidSHA, 30*24*time.Hour)
	add("prior_invalid_key_fragment_still_normalizes", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}
	usageFragment, err := conformance.NormalizeUsageWindowLimitCapture(usageCapture, usageSHA, 30*24*time.Hour)
	add("prior_usage_window_fragment_still_normalizes", err == nil, errorString(err))
	if err != nil {
		emit(out, 1)
	}

	var registry conformance.FragmentRegistry
	addInvalidErr := registry.Add(invalidFragment, capture.CapturedAtUTC)
	addUsageErr := registry.Add(usageFragment, capture.CapturedAtUTC)
	add("prior_401_and_402_fragments_still_coexist", addInvalidErr == nil && addUsageErr == nil, map[string]string{
		"invalid_add_error": errorString(addInvalidErr),
		"usage_add_error":   errorString(addUsageErr),
	})

	invalidDecision := registry.Decide(invalidFragment.Identity, invalidFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: invalidCapture.Response.StatusCode, Body: []byte(invalidCapture.Response.BodyUTF8)}, nil, capture.CapturedAtUTC)
	add("prior_exact_401_behavior_still_allows_invalid_key_rotation", invalidDecision.Trusted && invalidDecision.Class == pool.FailureInvalidKey && invalidDecision.AllowOriginalReplay && invalidDecision.AllowKeyRotation, invalidDecision)
	usageDecision := registry.Decide(usageFragment.Identity, usageFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{StatusCode: usageCapture.Response.StatusCode, Body: []byte(usageCapture.Response.BodyUTF8)}, nil, capture.CapturedAtUTC)
	add("prior_exact_402_behavior_remains_trusted_but_conservative", usageDecision.Trusted && usageDecision.Class == pool.FailureAmbiguous && usageDecision.RequireInput && !usageDecision.AllowOriginalReplay && !usageDecision.AllowKeyRotation, usageDecision)

	transportErr := errors.New(capture.ReplacementCredential.Transport.TransportError)
	replacementDecision := registry.Decide(usageFragment.Identity, usageFragment.RequestShape.Endpoint, conformance.PhasePreOutput, providerhttp.Response{}, transportErr, capture.CapturedAtUTC)
	add("replacement_timeout_fails_closed_before_any_fragment_match", !replacementDecision.Trusted && replacementDecision.Class == pool.FailureAmbiguous && !replacementDecision.AllowOriginalReplay && !replacementDecision.AllowKeyRotation && !replacementDecision.AllowSemanticContinuation, replacementDecision)

	noScopeOverclaim := evidence.ScopeConclusion != "key_specific" && evidence.ScopeConclusion != "account_specific" && evidence.ScopeConclusion != "provider_wide"
	add("timeout_does_not_overclaim_key_account_or_provider_scope", noScopeOverclaim, evidence.ScopeConclusion)
	add("limitations_preserved_for_future_revalidation", len(evidence.Limitations) >= 5, evidence.Limitations)

	rawLower := strings.ToLower(string(rawPairedCapture))
	userPathPrefix := "C:" + `\Users\`
	credentialPrefix := "s" + "k-"
	cleanRaw := !strings.Contains(string(rawPairedCapture), userPathPrefix) && !strings.Contains(rawLower, credentialPrefix) && !strings.Contains(rawLower, "api_key") && !capture.Checks.SecretsPersisted
	add("source_evidence_contains_no_user_path_or_persisted_credential", cleanRaw, map[string]any{
		"user_path_present": strings.Contains(string(rawPairedCapture), userPathPrefix),
		"secrets_persisted": capture.Checks.SecretsPersisted,
	})

	allPassed := true
	for _, s := range out.Scenarios {
		allPassed = allPassed && s.Passed
	}
	out.Passed = allPassed
	if allPassed {
		out.Status = "passed"
	}
	out.NextGate = "Do not rerun the same paired test automatically. Preserve the exact 402 as scope-unknown and the replacement timeout as ambiguous. Continue local product work; only repeat a real paired scope capture later under a separately justified bounded gate when transport conditions or provider evidence make it high value."
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
