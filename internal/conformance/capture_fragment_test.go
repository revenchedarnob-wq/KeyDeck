package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

func testRealCapture() RealProviderCapture {
	body := "{\"error\":\"Unauthorized - Invalid token\"}\n"
	sum := sha256.Sum256([]byte(body))
	return RealProviderCapture{
		ProofComponent: "aerolink-real-invalid-key-capture",
		SchemaVersion:  1,
		CapturedAtUTC:  time.Date(2026, 7, 7, 12, 55, 34, 147010300, time.UTC),
		Passed:         true,
		Target:         CaptureTarget{Provider: "Aerolink", APIBase: "https://capi.aerolink.lat", Endpoint: "/v1/messages", APIFormat: "Anthropic Messages", AnthropicVersion: "2023-06-01", Model: "claude-opus-4-8"},
		Request:        CaptureRequest{Method: "POST", RequestCount: 1, RetryCount: 0, MaxTokens: 1, KeyKind: "intentionally_invalid_fixture", RealAPIKeyUsed: false, BodySHA256: "f4c230cdca73fcdbab1878b67bcf297011d7f1c19195af41fada91f9881f3c51"},
		Response:       CaptureResponse{StatusCode: 401, ReasonPhrase: "Unauthorized", BodyUTF8: body, BodySHA256: hex.EncodeToString(sum[:])},
		Transport:      CaptureTransport{ElapsedMS: 456, TimeoutSeconds: 20, AutomaticRetries: 0},
		Limitations:    []string{"invalid credential only"},
	}
}

func TestNormalizeAndDecideRealCapture(t *testing.T) {
	capture := testRealCapture()
	fragment, err := NormalizeInvalidCredentialCapture(capture, "b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var registry FragmentRegistry
	if err := registry.Add(fragment, capture.CapturedAtUTC); err != nil {
		t.Fatal(err)
	}
	decision := registry.Decide(fragment.Identity, fragment.RequestShape.Endpoint, PhasePreOutput, providerhttp.Response{StatusCode: capture.Response.StatusCode, Body: []byte(capture.Response.BodyUTF8)}, nil, capture.CapturedAtUTC)
	if !decision.Trusted || decision.Class != pool.FailureInvalidKey || !decision.AllowOriginalReplay || !decision.AllowKeyRotation {
		t.Fatalf("unexpected decision: %+v", decision)
	}
}

func TestRealCaptureMismatchFailsClosed(t *testing.T) {
	capture := testRealCapture()
	fragment, err := NormalizeInvalidCredentialCapture(capture, "b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var registry FragmentRegistry
	_ = registry.Add(fragment, capture.CapturedAtUTC)
	decision := registry.Decide(fragment.Identity, fragment.RequestShape.Endpoint, PhasePreOutput, providerhttp.Response{StatusCode: 401, Body: []byte(`{"error":"different"}`)}, nil, capture.CapturedAtUTC)
	if decision.Trusted || decision.Class != pool.FailureAmbiguous || decision.AllowKeyRotation {
		t.Fatalf("mismatch should fail closed: %+v", decision)
	}
}

func TestRealCaptureFragmentStoreAndTamper(t *testing.T) {
	capture := testRealCapture()
	fragment, err := NormalizeInvalidCredentialCapture(capture, "b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := FragmentStore{Path: filepath.Join(t.TempDir(), "fragment.json")}
	if err := store.Save(fragment, capture.CapturedAtUTC); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(capture.CapturedAtUTC)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EvidenceSHA256 != fragment.EvidenceSHA256 {
		t.Fatal("fragment identity changed across durable save/load")
	}
	tampered := loaded
	tampered.ResponseSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := tampered.Validate(capture.CapturedAtUTC); !errors.Is(err, ErrEvidenceTampered) {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
}

func testUsageWindowLimitCapture() RealProviderCapture {
	body := "{\"error\":\"5-hour included-usage limit reached. You have used the $10.00 allowance for this window. Wait for the 5-hour reset, add balance, or upgrade to continue.\"}\n"
	sum := sha256.Sum256([]byte(body))
	return RealProviderCapture{
		ProofComponent: "aerolink-real-exhaustion-capture",
		SchemaVersion:  1,
		CapturedAtUTC:  time.Date(2026, 7, 7, 13, 52, 28, 953574600, time.UTC),
		Passed:         true,
		Target:         CaptureTarget{Provider: "Aerolink", APIBase: "https://capi.aerolink.lat", Endpoint: "/v1/messages", APIFormat: "Anthropic Messages", AnthropicVersion: "2023-06-01", Model: "claude-opus-4-8"},
		Request:        CaptureRequest{Method: "POST", RequestCount: 1, RetryCount: 0, MaxTokens: 1, KeyKind: "real_already_exhausted_key", RealAPIKeyUsed: true, SecretPersisted: false, BodySHA256: "f4c230cdca73fcdbab1878b67bcf297011d7f1c19195af41fada91f9881f3c51"},
		Response:       CaptureResponse{StatusCode: 402, ReasonPhrase: "Payment Required", BodyUTF8: body, BodySHA256: hex.EncodeToString(sum[:])},
		Transport:      CaptureTransport{ElapsedMS: 395, TimeoutSeconds: 20, AutomaticRetries: 0},
		Limitations:    []string{"exact usage-window response only"},
	}
}

func TestNormalizeUsageWindowLimitCaptureRemainsConservative(t *testing.T) {
	capture := testUsageWindowLimitCapture()
	fragment, err := NormalizeUsageWindowLimitCapture(capture, "f1b07af5a186f96709151bc70f541c2b097d9469c25281f726a9bfc44a1532f3", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if fragment.Observation.Decision.Class != pool.FailureAmbiguous || fragment.Observation.Decision.AllowKeyRotation || fragment.Observation.Decision.AllowOriginalReplay || !fragment.Observation.Decision.RequireInput {
		t.Fatalf("usage-window evidence must stay conservative until scope is proven: %+v", fragment.Observation.Decision)
	}
}

func TestFragmentRegistrySupportsMultipleBehaviorsForSameProviderEndpoint(t *testing.T) {
	invalidCapture := testRealCapture()
	invalidFragment, err := NormalizeInvalidCredentialCapture(invalidCapture, "b27b21c13925937cc50b0c75641a46c6e2cbeb24dfdf0bccf3b03d7262c800e3", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	limitCapture := testUsageWindowLimitCapture()
	limitFragment, err := NormalizeUsageWindowLimitCapture(limitCapture, "f1b07af5a186f96709151bc70f541c2b097d9469c25281f726a9bfc44a1532f3", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var registry FragmentRegistry
	if err := registry.Add(invalidFragment, limitCapture.CapturedAtUTC); err != nil {
		t.Fatal(err)
	}
	if err := registry.Add(limitFragment, limitCapture.CapturedAtUTC); err != nil {
		t.Fatal(err)
	}

	invalidDecision := registry.Decide(invalidFragment.Identity, invalidFragment.RequestShape.Endpoint, PhasePreOutput, providerhttp.Response{StatusCode: invalidCapture.Response.StatusCode, Body: []byte(invalidCapture.Response.BodyUTF8)}, nil, limitCapture.CapturedAtUTC)
	if !invalidDecision.Trusted || invalidDecision.Class != pool.FailureInvalidKey {
		t.Fatalf("invalid-key fragment stopped matching after second behavior was added: %+v", invalidDecision)
	}

	limitDecision := registry.Decide(limitFragment.Identity, limitFragment.RequestShape.Endpoint, PhasePreOutput, providerhttp.Response{StatusCode: limitCapture.Response.StatusCode, Body: []byte(limitCapture.Response.BodyUTF8)}, nil, limitCapture.CapturedAtUTC)
	if !limitDecision.Trusted || limitDecision.Class != pool.FailureAmbiguous || !limitDecision.RequireInput || limitDecision.AllowKeyRotation {
		t.Fatalf("usage-window fragment did not match conservatively: %+v", limitDecision)
	}
}
