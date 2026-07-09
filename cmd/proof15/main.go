package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/conformance"
	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof          string     `json:"proof"`
	Passed         bool       `json:"passed"`
	EvidenceID     string     `json:"evidence_id"`
	EvidenceSHA256 string     `json:"evidence_sha256"`
	RawCaptureSHA  string     `json:"raw_capture_sha256"`
	Claims         []string   `json:"claims"`
	Scenarios      []scenario `json:"scenarios"`
}

type httpCapture struct {
	Name         string          `json:"name"`
	StatusCode   int             `json:"status_code"`
	Body         json.RawMessage `json:"body,omitempty"`
	TransportErr string          `json:"transport_error,omitempty"`
	Usage        protocol.Usage  `json:"usage,omitempty"`
}

type streamCapture struct {
	Name         string                     `json:"name"`
	Events       []providerhttp.StreamEvent `json:"events"`
	Error        string                     `json:"error,omitempty"`
	PartialText  string                     `json:"partial_text"`
	TerminalSeen bool                       `json:"terminal_seen"`
	UsageSeen    bool                       `json:"usage_seen"`
}

type captureSet struct {
	HTTP    []httpCapture   `json:"http"`
	Streams []streamCapture `json:"streams"`
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

var fixedNow = time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)

func main() {
	captures := collectCaptures()
	rawCaptureSHA := hashJSON(captures)
	bundle := buildEvidence(captures, rawCaptureSHA)
	if err := bundle.Seal(); err != nil {
		panic(err)
	}

	tmpDir, err := os.MkdirTemp("", "keydeck-proof15-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	store := conformance.EvidenceStore{Path: filepath.Join(tmpDir, "fixture-provider-evidence.json")}

	scenarios := []scenario{
		durableEvidenceScenario(store, bundle),
		tamperScenario(bundle),
		exactDecisionScenario(bundle, "key_exhausted_pre_output", true, true, false),
		exactDecisionScenario(bundle, "invalid_credential_pre_output", true, true, false),
		exactDecisionScenario(bundle, "key_rate_limit_pre_output", true, true, false),
		exactDecisionScenario(bundle, "provider_busy_pre_output", false, false, false),
		exactDecisionScenario(bundle, "provider_outage_pre_output", false, false, false),
		exactDecisionScenario(bundle, "ambiguous_transport", false, false, false),
		exactDecisionScenario(bundle, "key_exhausted_mid_stream", false, true, true),
		exactDecisionScenario(bundle, "abrupt_eof_mid_stream", false, false, false),
		unknownBehaviorScenario(bundle),
		identityMismatchScenario(bundle),
		expiryScenario(bundle),
		usageSemanticsScenario(bundle, captures),
		derivedPoolProfileScenario(bundle),
	}

	passed := true
	for _, s := range scenarios {
		passed = passed && s.Passed
	}
	r := report{
		Proof:          "0.15-real-provider-conformance-framework",
		Passed:         passed,
		EvidenceID:     bundle.EvidenceID,
		EvidenceSHA256: bundle.EvidenceSHA256,
		RawCaptureSHA:  rawCaptureSHA,
		Claims: []string{
			"provider conformance evidence is exact by provider, API base/version, model and model revision",
			"evidence is dated, expires, requires revalidation and is cryptographically bound to raw capture provenance",
			"tampered evidence is rejected before policy activation",
			"exact pre-output key failures may authorize replay on a replacement key only when captured evidence matches",
			"provider-wide busy/outage and ambiguous transport preserve backup keys",
			"mid-stream key exhaustion may authorize semantic continuation but never original request replay",
			"abrupt partial streams remain ambiguous",
			"cache and usage semantics are captured as provider evidence instead of universal assumptions",
			"unknown provider identity/version/behavior fails closed",
			"only pre-output evidence can compile into the existing API-key-pool classifier",
		},
		Scenarios: scenarios,
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
	if !passed {
		os.Exit(1)
	}
}

func collectCaptures() captureSet {
	return captureSet{
		HTTP: []httpCapture{
			captureHTTP("key_exhausted_pre_output", fakeprovider.Outcome{Behavior: fakeprovider.CustomError, StatusCode: 402, ErrorCode: "fixture_balance_empty", ErrorScope: "key"}),
			captureHTTP("invalid_credential_pre_output", fakeprovider.Outcome{Behavior: fakeprovider.CustomError, StatusCode: 401, ErrorCode: "fixture_invalid_credential", ErrorScope: "key"}),
			captureHTTP("key_rate_limit_pre_output", fakeprovider.Outcome{Behavior: fakeprovider.CustomError, StatusCode: 429, ErrorCode: "fixture_per_key_limit", ErrorScope: "key"}),
			captureHTTP("provider_busy_pre_output", fakeprovider.Outcome{Behavior: fakeprovider.CustomError, StatusCode: 503, ErrorCode: "fixture_global_busy", ErrorScope: "provider"}),
			captureHTTP("provider_outage_pre_output", fakeprovider.Outcome{Behavior: fakeprovider.CustomError, StatusCode: 503, ErrorCode: "fixture_provider_outage", ErrorScope: "provider"}),
			captureTransportFailure(),
			captureHTTP("usage_success", fakeprovider.Outcome{Behavior: fakeprovider.Success, Output: "usage", Usage: protocol.Usage{InputTokens: 12_000, OutputTokens: 800, CacheCreationInputTokens: 50_000, CacheReadInputTokens: 6_000}}),
			captureHTTP("usage_error", fakeprovider.Outcome{Behavior: fakeprovider.CustomError, StatusCode: 429, ErrorCode: "fixture_metered_error", ErrorScope: "key", Usage: protocol.Usage{InputTokens: 4_000, OutputTokens: 120, CacheCreationInputTokens: 2_000}}),
		},
		Streams: []streamCapture{
			captureStream("key_exhausted_mid_stream", fakeprovider.StreamOutcome{Chunks: []string{"partial ", "answer"}, Terminal: fakeprovider.StreamKeyExhausted, Usage: protocol.Usage{InputTokens: 9_000, OutputTokens: 120, CacheReadInputTokens: 2_000}}),
			captureStream("abrupt_eof_mid_stream", fakeprovider.StreamOutcome{Chunks: []string{"partial ", "unknown"}, Terminal: fakeprovider.StreamAbruptEOF}),
		},
	}
}

func captureHTTP(name string, outcome fakeprovider.Outcome) httpCapture {
	plan := fakeprovider.NewPlan()
	plan.ByKey["proof-key"] = []fakeprovider.Outcome{outcome}
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	defer srv.Close()
	resp, err := (&providerhttp.Client{BaseURL: srv.URL}).Do(context.Background(), "proof-key", []byte(`{"prompt":"proof15"}`))
	capture := httpCapture{Name: name, StatusCode: resp.StatusCode, Body: append([]byte(nil), resp.Body...)}
	if err != nil {
		capture.TransportErr = err.Error()
	}
	if env, decodeErr := protocol.DecodeEnvelope(resp.Body); decodeErr == nil {
		capture.Usage = env.Usage
	}
	return capture
}

func captureTransportFailure() httpCapture {
	client := &providerhttp.Client{
		BaseURL: "http://proof15.invalid",
		HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("fixture connection reset after unknown acceptance state")
		})},
	}
	resp, err := client.Do(context.Background(), "proof-key", []byte(`{"prompt":"transport"}`))
	capture := httpCapture{Name: "ambiguous_transport", StatusCode: resp.StatusCode, Body: append([]byte(nil), resp.Body...)}
	if err != nil {
		capture.TransportErr = normalizeTransportError(err.Error())
	}
	return capture
}

func captureStream(name string, outcome fakeprovider.StreamOutcome) streamCapture {
	plan := fakeprovider.NewPlan()
	streamPlan := fakeprovider.NewStreamPlan()
	streamPlan.ByKey["proof-key"] = []fakeprovider.StreamOutcome{outcome}
	srv := httptest.NewServer(fakeprovider.Mux(plan, streamPlan))
	defer srv.Close()
	capture := streamCapture{Name: name}
	err := (&providerhttp.StreamClient{BaseURL: srv.URL}).Do(context.Background(), "proof-key", []byte(`{"prompt":"stream"}`), func(event providerhttp.StreamEvent) error {
		capture.Events = append(capture.Events, event)
		switch event.Type {
		case providerhttp.StreamTextDelta:
			capture.PartialText += event.Text
		case providerhttp.StreamUsage:
			capture.UsageSeen = true
		case providerhttp.StreamDone, providerhttp.StreamError:
			capture.TerminalSeen = true
		}
		return nil
	})
	if err != nil {
		capture.Error = err.Error()
	}
	return capture
}

func buildEvidence(captures captureSet, rawCaptureSHA string) conformance.ProviderEvidenceBundle {
	findHTTP := func(name string) httpCapture {
		for _, capture := range captures.HTTP {
			if capture.Name == name {
				return capture
			}
		}
		panic("missing HTTP capture: " + name)
	}
	findStream := func(name string) streamCapture {
		for _, capture := range captures.Streams {
			if capture.Name == name {
				return capture
			}
		}
		panic("missing stream capture: " + name)
	}
	obsHTTP := func(name string, class pool.FailureClass, replay, rotate bool) conformance.FailureObservation {
		capture := findHTTP(name)
		code, scope := decodeError(capture.Body)
		return conformance.FailureObservation{ScenarioID: name, Phase: conformance.PhasePreOutput, StatusCode: capture.StatusCode, ErrorCode: code, ErrorScope: scope, TransportError: capture.TransportErr, TerminalEvent: capture.TransportErr == "",
			UsageObserved: capture.Usage != (protocol.Usage{}), Decision: conformance.PolicyDecision{Class: class, AllowOriginalReplay: replay, AllowKeyRotation: rotate, RequireInput: class == pool.FailureAmbiguous}}
	}
	transport := findHTTP("ambiguous_transport")
	mid := findStream("key_exhausted_mid_stream")
	midCode, midScope := streamError(mid.Events)
	abrupt := findStream("abrupt_eof_mid_stream")
	usageSuccess := findHTTP("usage_success")
	usageError := findHTTP("usage_error")
	return conformance.ProviderEvidenceBundle{
		SchemaVersion: 1,
		EvidenceID:    "proof-0.15-fixture-provider-r1",
		Identity:      conformance.ProviderIdentity{Provider: "fixture-provider", APIBase: "fixture://proof15", APIVersion: "2026-07-07", Model: "fixture-model", ModelRevision: "r1"},
		TestedAt:      fixedNow,
		ExpiresAt:     fixedNow.Add(30 * 24 * time.Hour),
		Provenance:    conformance.EvidenceProvenance{CaptureID: "proof15-local-fake-provider-matrix", SourceKind: "deterministic-fake-provider", SourceRef: "cmd/proof15", CapturedBy: "KeyDeck Proof 0.15", RawCaptureSHA256: rawCaptureSHA},
		Failures: []conformance.FailureObservation{
			obsHTTP("key_exhausted_pre_output", pool.FailureKeyExhausted, true, true),
			obsHTTP("invalid_credential_pre_output", pool.FailureInvalidKey, true, true),
			obsHTTP("key_rate_limit_pre_output", pool.FailureKeyRateLimited, true, true),
			obsHTTP("provider_busy_pre_output", pool.FailureProviderBusy, false, false),
			obsHTTP("provider_outage_pre_output", pool.FailureProviderBusy, false, false),
			{ScenarioID: "ambiguous_transport", Phase: conformance.PhaseUnknown, TransportError: transport.TransportErr, Decision: conformance.PolicyDecision{Class: pool.FailureAmbiguous, RequireInput: true}},
			{ScenarioID: "key_exhausted_mid_stream", Phase: conformance.PhaseMidStream, StatusCode: 200, ErrorCode: midCode, ErrorScope: midScope, PartialOutput: mid.PartialText != "", TerminalEvent: mid.TerminalSeen, UsageObserved: mid.UsageSeen,
				Decision: conformance.PolicyDecision{Class: pool.FailureKeyExhausted, AllowKeyRotation: true, AllowSemanticContinuation: true}},
			{ScenarioID: "abrupt_eof_mid_stream", Phase: conformance.PhaseMidStream, StatusCode: 200, TransportError: abrupt.Error, PartialOutput: abrupt.PartialText != "", TerminalEvent: abrupt.TerminalSeen, UsageObserved: abrupt.UsageSeen,
				Decision: conformance.PolicyDecision{Class: pool.FailureAmbiguous, RequireInput: true}},
			{ScenarioID: "metered_error_usage", Phase: conformance.PhasePreOutput, StatusCode: usageError.StatusCode, ErrorCode: "fixture_metered_error", ErrorScope: "key", TerminalEvent: true, UsageObserved: usageError.Usage != (protocol.Usage{}),
				Decision: conformance.PolicyDecision{Class: pool.FailureAmbiguous, RequireInput: true}},
		},
		Usage: conformance.UsageSemantics{
			UsageFields:    []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"},
			CacheReadField: "cache_read_input_tokens", CacheCreationField: "cache_creation_input_tokens",
			UsageReportedOnSuccess: usageSuccess.Usage != (protocol.Usage{}), UsageReportedOnError: usageError.Usage != (protocol.Usage{}), UsageReportedMidStream: mid.UsageSeen,
			BillingMeaning: "fixture reports usage across success, exact error and mid-stream events; real-provider charge meaning remains provider-specific evidence",
		},
	}
}

func durableEvidenceScenario(store conformance.EvidenceStore, bundle conformance.ProviderEvidenceBundle) scenario {
	err := store.Save(bundle, fixedNow)
	loaded, loadErr := store.Load(fixedNow)
	passed := err == nil && loadErr == nil && loaded.EvidenceSHA256 == bundle.EvidenceSHA256
	return scenario{Name: "durable_evidence_round_trip", Passed: passed, Detail: map[string]any{"save_error": errorString(err), "load_error": errorString(loadErr), "hash_stable": loaded.EvidenceSHA256 == bundle.EvidenceSHA256}}
}

func tamperScenario(bundle conformance.ProviderEvidenceBundle) scenario {
	tampered := bundle
	tampered.Identity.ModelRevision = "tampered"
	err := tampered.Validate(fixedNow)
	return scenario{Name: "tampered_evidence_rejected", Passed: errors.Is(err, conformance.ErrEvidenceTampered), Detail: errorString(err)}
}

func exactDecisionScenario(bundle conformance.ProviderEvidenceBundle, scenarioID string, wantReplay, wantRotate, wantContinuation bool) scenario {
	obs := findObservation(bundle, scenarioID)
	var registry conformance.EvidenceRegistry
	addErr := registry.Add(bundle, fixedNow)
	decision := registry.Decide(bundle.Identity, obs, fixedNow)
	passed := addErr == nil && decision.Trusted && decision.AllowOriginalReplay == wantReplay && decision.AllowKeyRotation == wantRotate && decision.AllowSemanticContinuation == wantContinuation
	if scenarioID == "provider_busy_pre_output" || scenarioID == "provider_outage_pre_output" || scenarioID == "ambiguous_transport" || scenarioID == "abrupt_eof_mid_stream" {
		passed = passed && !decision.AllowOriginalReplay && !decision.AllowKeyRotation
	}
	return scenario{Name: "exact_decision_" + scenarioID, Passed: passed, Detail: decision}
}

func unknownBehaviorScenario(bundle conformance.ProviderEvidenceBundle) scenario {
	var registry conformance.EvidenceRegistry
	_ = registry.Add(bundle, fixedNow)
	unknown := conformance.FailureObservation{ScenarioID: "unknown", Phase: conformance.PhasePreOutput, StatusCode: 429, ErrorCode: "mystery_limit", ErrorScope: "key", TerminalEvent: true, Decision: conformance.PolicyDecision{Class: pool.FailureAmbiguous, RequireInput: true}}
	decision := registry.Decide(bundle.Identity, unknown, fixedNow)
	passed := !decision.Trusted && decision.Class == pool.FailureAmbiguous && !decision.AllowOriginalReplay && !decision.AllowKeyRotation
	return scenario{Name: "unknown_behavior_fails_closed", Passed: passed, Detail: decision}
}

func identityMismatchScenario(bundle conformance.ProviderEvidenceBundle) scenario {
	var registry conformance.EvidenceRegistry
	_ = registry.Add(bundle, fixedNow)
	identity := bundle.Identity
	identity.ModelRevision = "r2"
	decision := registry.Decide(identity, bundle.Failures[0], fixedNow)
	passed := !decision.Trusted && decision.Class == pool.FailureAmbiguous && !decision.AllowKeyRotation
	return scenario{Name: "identity_version_mismatch_fails_closed", Passed: passed, Detail: decision}
}

func expiryScenario(bundle conformance.ProviderEvidenceBundle) scenario {
	var registry conformance.EvidenceRegistry
	_ = registry.Add(bundle, fixedNow)
	decision := registry.Decide(bundle.Identity, bundle.Failures[0], bundle.ExpiresAt.Add(time.Second))
	passed := !decision.Trusted && decision.RequireRevalidation && decision.Class == pool.FailureAmbiguous && !decision.AllowKeyRotation
	return scenario{Name: "expired_evidence_requires_revalidation", Passed: passed, Detail: decision}
}

func usageSemanticsScenario(bundle conformance.ProviderEvidenceBundle, captures captureSet) scenario {
	var success, metered httpCapture
	for _, capture := range captures.HTTP {
		switch capture.Name {
		case "usage_success":
			success = capture
		case "usage_error":
			metered = capture
		}
	}
	mid := streamCapture{}
	for _, capture := range captures.Streams {
		if capture.Name == "key_exhausted_mid_stream" {
			mid = capture
		}
	}
	passed := bundle.Usage.UsageReportedOnSuccess && bundle.Usage.UsageReportedOnError && bundle.Usage.UsageReportedMidStream &&
		success.Usage.CacheCreationInputTokens == 50_000 && success.Usage.CacheReadInputTokens == 6_000 && metered.Usage.InputTokens == 4_000 && mid.UsageSeen
	return scenario{Name: "cache_billing_usage_semantics_captured", Passed: passed, Detail: map[string]any{"success_usage": success.Usage, "error_usage": metered.Usage, "mid_stream_usage_seen": mid.UsageSeen, "billing_meaning": bundle.Usage.BillingMeaning}}
}

func derivedPoolProfileScenario(bundle conformance.ProviderEvidenceBundle) scenario {
	profile, profileErr := bundle.ProviderProfile(fixedNow)
	if profileErr != nil {
		return scenario{Name: "evidence_drives_existing_pool_classifier", Passed: false, Detail: errorString(profileErr)}
	}
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.CustomError, StatusCode: 402, ErrorCode: "fixture_balance_empty", ErrorScope: "key"}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "done"}}
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	defer srv.Close()
	guard, _ := costguard.New(costguard.Config{LargeWriteMin: 100_000, LowReadMax: 5_000, ConsecutiveLimit: 2, ExtremeWriteMin: 180_000})
	p := pool.NewWithClassifier([]pool.Key{{ID: "key-1", Secret: "secret-1"}, {ID: "key-2", Secret: "secret-2"}}, &providerhttp.Client{BaseURL: srv.URL}, &events.Recorder{}, guard, profile)
	result, runErr := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	passed := runErr == nil && result.KeyID == "key-2" && plan.Calls("secret-1") == 1 && plan.Calls("secret-2") == 1
	return scenario{Name: "evidence_drives_existing_pool_classifier", Passed: passed, Detail: map[string]any{"profile_rules": len(profile.Rules), "selected_key": result.KeyID, "error": errorString(runErr)}}
}

func findObservation(bundle conformance.ProviderEvidenceBundle, id string) conformance.FailureObservation {
	for _, observation := range bundle.Failures {
		if observation.ScenarioID == id {
			return observation
		}
	}
	panic("missing observation: " + id)
}

func decodeError(body []byte) (string, string) {
	env, err := protocol.DecodeEnvelope(body)
	if err != nil || env.Error == nil {
		return "", ""
	}
	return env.Error.Code, env.Error.Scope
}

func streamError(events []providerhttp.StreamEvent) (string, string) {
	for _, event := range events {
		if event.Type == providerhttp.StreamError {
			return event.Code, event.Scope
		}
	}
	return "", ""
}

func normalizeTransportError(value string) string {
	if strings.Contains(value, "fixture connection reset after unknown acceptance state") {
		return "fixture connection reset after unknown acceptance state"
	}
	return value
}

func hashJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
