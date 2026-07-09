package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/pool"
)

func testPairedScopeCapture() PairedScopeCapture {
	limitedBody := "{\"error\":\"5-hour included-usage limit reached. You have used the $10.00 allowance for this window. Wait for the 5-hour reset, add balance, or upgrade to continue.\"}\n"
	limitedSum := sha256.Sum256([]byte(limitedBody))
	emptySum := sha256.Sum256(nil)
	return PairedScopeCapture{
		ProofComponent: "aerolink-real-paired-scope-capture",
		SchemaVersion:  1,
		CapturedAtUTC:  time.Date(2026, 7, 7, 14, 23, 39, 711473300, time.UTC),
		Passed:         false,
		Target: CaptureTarget{
			Provider: "Aerolink", APIBase: "https://capi.aerolink.lat", Endpoint: "/v1/messages", APIFormat: "Anthropic Messages", AnthropicVersion: "2023-06-01", Model: "claude-opus-4-8",
		},
		Ordering:                "limited_then_replacement",
		ExpectedLimitedBehavior: ExpectedLimitedBehavior{StatusCode: 402, BodySHA256: hex.EncodeToString(limitedSum[:])},
		LimitedCredential: PairedScopeCredential{
			KeyKind: "real_already_limited_pool_member", RequestCount: 1, RetryCount: 0, MaxTokens: 1, SecretPersisted: false,
			BodySHA256: "f4c230cdca73fcdbab1878b67bcf297011d7f1c19195af41fada91f9881f3c51",
			Response:   CaptureResponse{StatusCode: 402, ReasonPhrase: "Payment Required", BodyUTF8: limitedBody, BodySHA256: hex.EncodeToString(limitedSum[:])},
			Transport:  PairedCredentialTransport{ElapsedMS: 386, TimeoutSeconds: 20, AutomaticRetries: 0},
		},
		ReplacementCredential: PairedScopeCredential{
			KeyKind: "real_known_usable_replacement_pool_member", RequestCount: 1, RetryCount: 0, MaxTokens: 1, SecretPersisted: false,
			BodySHA256: "f4c230cdca73fcdbab1878b67bcf297011d7f1c19195af41fada91f9881f3c51",
			Response:   CaptureResponse{StatusCode: 0, BodyUTF8: "", BodySHA256: hex.EncodeToString(emptySum[:])},
			Transport:  PairedCredentialTransport{ElapsedMS: 20021, TimeoutSeconds: 20, AutomaticRetries: 0, TransportError: `Exception calling "Send" with "2" argument(s): "The request was canceled due to the configured HttpClient.Timeout of 20 seconds elapsing."`},
		},
		Checks:      PairedScopeChecks{LimitedExact402Match: true, ReplacementAttempted: true, ReplacementSucceeded: false, RequestShapeIdentical: true, TotalRequestCount: 2, TotalRetryCount: 0, SecretsPersisted: false},
		Limitations: []string{"exact paired capture only", "do not rerun automatically"},
	}
}

func TestNormalizePairedScopeTimeoutRemainsConservative(t *testing.T) {
	capture := testPairedScopeCapture()
	evidence, err := NormalizePairedScopeCapture(capture, "ee2ebaa92ab8bf97661a1a594939ede3f0109442b28190f5f21080a9fdbf81b8", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ReplacementTransportKind != "timeout" || evidence.ScopeConclusion != "unproven_inconclusive_replacement_transport_failure" {
		t.Fatalf("unexpected paired conclusion: %+v", evidence)
	}
	if evidence.Decision.Class != pool.FailureAmbiguous || evidence.Decision.AllowOriginalReplay || evidence.Decision.AllowKeyRotation || evidence.Decision.AllowSemanticContinuation || !evidence.Decision.RequireInput {
		t.Fatalf("paired timeout evidence must remain conservative: %+v", evidence.Decision)
	}
}

func TestPairedScopeStoreAndTamper(t *testing.T) {
	capture := testPairedScopeCapture()
	evidence, err := NormalizePairedScopeCapture(capture, "ee2ebaa92ab8bf97661a1a594939ede3f0109442b28190f5f21080a9fdbf81b8", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store := PairedScopeEvidenceStore{Path: filepath.Join(t.TempDir(), "paired-scope.json")}
	if err := store.Save(evidence, capture.CapturedAtUTC); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(capture.CapturedAtUTC)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.EvidenceSHA256 != evidence.EvidenceSHA256 {
		t.Fatal("paired evidence identity changed across save/load")
	}
	tampered := loaded
	tampered.ScopeConclusion = "key_specific"
	if err := tampered.Validate(capture.CapturedAtUTC); !errors.Is(err, ErrEvidenceTampered) {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
}

func TestPairedScopeCaptureRejectsRetryOrSecretPersistence(t *testing.T) {
	capture := testPairedScopeCapture()
	capture.ReplacementCredential.RetryCount = 1
	if err := capture.Validate(); !errors.Is(err, ErrInvalidPairedScopeEvidence) {
		t.Fatalf("expected retry rejection, got %v", err)
	}
	capture = testPairedScopeCapture()
	capture.ReplacementCredential.SecretPersisted = true
	if err := capture.Validate(); !errors.Is(err, ErrInvalidPairedScopeEvidence) {
		t.Fatalf("expected persisted secret rejection, got %v", err)
	}
}

func TestPairedScopeCaptureRequiresExactLimitedMatch(t *testing.T) {
	capture := testPairedScopeCapture()
	capture.LimitedCredential.Response.BodyUTF8 = strings.Replace(capture.LimitedCredential.Response.BodyUTF8, "$10.00", "$9.00", 1)
	if err := capture.Validate(); !errors.Is(err, ErrInvalidPairedScopeEvidence) {
		t.Fatalf("expected exact limited match rejection, got %v", err)
	}
}
