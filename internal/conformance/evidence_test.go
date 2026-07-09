package conformance

import (
	"errors"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/pool"
)

func testEvidence(now time.Time) ProviderEvidenceBundle {
	b := ProviderEvidenceBundle{
		SchemaVersion: 1,
		EvidenceID:    "fixture-evidence",
		Identity:      ProviderIdentity{Provider: "fixture", APIBase: "local://fixture", APIVersion: "2026-07-07", Model: "fixture-model", ModelRevision: "r1"},
		TestedAt:      now,
		ExpiresAt:     now.Add(30 * 24 * time.Hour),
		Provenance:    EvidenceProvenance{CaptureID: "capture-1", SourceKind: "fake-provider", SourceRef: "proof15", CapturedBy: "KeyDeck Proof 0.15", RawCaptureSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Failures:      []FailureObservation{{ScenarioID: "key-empty", Phase: PhasePreOutput, StatusCode: 402, ErrorCode: "balance_empty", ErrorScope: "key", TerminalEvent: true, Decision: PolicyDecision{Class: pool.FailureKeyExhausted, AllowOriginalReplay: true, AllowKeyRotation: true}}},
		Usage:         UsageSemantics{UsageFields: []string{"input_tokens", "output_tokens"}, UsageReportedOnSuccess: true, BillingMeaning: "fixture usage is synthetic evidence only"},
	}
	_ = b.Seal()
	return b
}

func TestProviderEvidenceSealValidateAndTamperDetection(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	b := testEvidence(now)
	if err := b.Validate(now); err != nil {
		t.Fatal(err)
	}
	b.Identity.ModelRevision = "tampered"
	if !errors.Is(b.Validate(now), ErrEvidenceTampered) {
		t.Fatalf("tampered evidence was accepted")
	}
}

func TestEvidenceRegistryExactMatchAndUnknownFailClosed(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	b := testEvidence(now)
	var registry EvidenceRegistry
	if err := registry.Add(b, now); err != nil {
		t.Fatal(err)
	}
	obs := b.Failures[0]
	trusted := registry.Decide(b.Identity, obs, now)
	if !trusted.Trusted || !trusted.AllowKeyRotation || !trusted.AllowOriginalReplay {
		t.Fatalf("exact evidence did not activate: %#v", trusted)
	}
	unknownIdentity := b.Identity
	unknownIdentity.ModelRevision = "unknown"
	unknown := registry.Decide(unknownIdentity, obs, now)
	if unknown.Trusted || unknown.Class != pool.FailureAmbiguous || unknown.AllowKeyRotation || unknown.AllowOriginalReplay {
		t.Fatalf("unknown identity was not conservative: %#v", unknown)
	}
}

func TestExpiredEvidenceRequiresRevalidation(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	b := testEvidence(now)
	var registry EvidenceRegistry
	if err := registry.Add(b, now); err != nil {
		t.Fatal(err)
	}
	decision := registry.Decide(b.Identity, b.Failures[0], b.ExpiresAt.Add(time.Second))
	if decision.Trusted || !decision.RequireRevalidation || decision.Class != pool.FailureAmbiguous {
		t.Fatalf("expired evidence did not fail closed: %#v", decision)
	}
}

func TestMidstreamPartialOutputForbidsReplay(t *testing.T) {
	obs := FailureObservation{ScenarioID: "mid", Phase: PhaseMidStream, StatusCode: 200, ErrorCode: "key_exhausted", ErrorScope: "key", PartialOutput: true, TerminalEvent: true,
		Decision: PolicyDecision{Class: pool.FailureKeyExhausted, AllowOriginalReplay: true, AllowKeyRotation: true, AllowSemanticContinuation: true}}
	if err := obs.Validate(); err == nil {
		t.Fatal("unsafe mid-stream replay evidence was accepted")
	}
}

func TestProviderProfileOnlyUsesPreOutputEvidence(t *testing.T) {
	now := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	b := testEvidence(now)
	b.Failures = append(b.Failures, FailureObservation{ScenarioID: "midstream", Phase: PhaseMidStream, StatusCode: 200, ErrorCode: "balance_empty", ErrorScope: "key", PartialOutput: true, TerminalEvent: true,
		Decision: PolicyDecision{Class: pool.FailureKeyExhausted, AllowKeyRotation: true, AllowSemanticContinuation: true}})
	if err := b.Seal(); err != nil {
		t.Fatal(err)
	}
	profile, err := b.ProviderProfile(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Rules) != 1 || profile.Rules[0].ErrorCode != "balance_empty" {
		t.Fatalf("mid-stream evidence leaked into request replay classifier: %#v", profile.Rules)
	}
}
