package conformance

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

func verifiedProfile() ProviderProfile {
	return ProviderProfile{
		Provider: "fixture-provider", Version: "2026-07-07", TestedAt: time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC), EvidenceID: "proof-0.10-fixture",
		Rules: []FailureRule{
			{StatusCode: 402, ErrorCode: "fixture_balance_empty", ErrorScope: "key", Class: pool.FailureKeyExhausted},
			{StatusCode: 503, ErrorCode: "fixture_global_busy", ErrorScope: "provider", Class: pool.FailureProviderBusy},
		},
	}
}

func testGuard(t *testing.T) *costguard.Guard {
	t.Helper()
	g, err := costguard.New(costguard.Config{LargeWriteMin: 100_000, LowReadMax: 5_000, ConsecutiveLimit: 2, ExtremeWriteMin: 180_000})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func newProfilePool(t *testing.T, plan *fakeprovider.Plan) (*pool.Pool, func()) {
	t.Helper()
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	p := pool.NewWithClassifier([]pool.Key{{ID: "key-1", Secret: "secret-1"}, {ID: "key-2", Secret: "secret-2"}}, &providerhttp.Client{BaseURL: srv.URL}, &events.Recorder{}, testGuard(t), verifiedProfile())
	return p, srv.Close
}

func TestOptimizationOffPreservesExactBytes(t *testing.T) {
	input := []byte("{\n  \"cache_control\" : { \"type\" : \"ephemeral\" },\n  \"prompt\" : \"keep spacing\"\n}\n")
	called := false
	o := Optimizer{Transform: func(in []byte) ([]byte, error) { called = true; return append(in, '!'), nil }}
	out, activation, err := o.Apply(OptimizationOff, "any", "any", input)
	if err != nil {
		t.Fatal(err)
	}
	if called || activation.Active || !bytes.Equal(input, out) {
		t.Fatalf("Optimization OFF changed request or invoked optimizer: called=%v activation=%#v", called, activation)
	}
}

func TestOptimizationOnRequiresExactVerifiedEvidence(t *testing.T) {
	input := []byte(`{"prompt":"proof"}`)
	optimizer := Optimizer{
		Evidence:  OptimizerEvidence{Provider: "fixture-provider", Version: "2026-07-07", TestedAt: time.Now().UTC(), EvidenceID: "opt-proof", Status: EvidenceVerified, CorrectnessPreserved: true, MeasurableBenefit: true},
		Transform: func(in []byte) ([]byte, error) { return append(in, []byte("\nverified")...), nil },
	}
	out, activation, err := optimizer.Apply(OptimizationOn, "fixture-provider", "2026-07-07", input)
	if err != nil || !activation.Active || bytes.Equal(out, input) {
		t.Fatalf("verified optimizer did not activate: activation=%#v err=%v", activation, err)
	}
	blocked, blockedActivation, err := optimizer.Apply(OptimizationOn, "fixture-provider", "different-version", input)
	if !errors.Is(err, ErrOptimizationNotVerified) || blockedActivation.Active || !bytes.Equal(blocked, input) {
		t.Fatalf("mismatched optimizer changed request: activation=%#v err=%v", blockedActivation, err)
	}
}

func TestExactProviderRuleAllowsKeyRotation(t *testing.T) {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.CustomError, StatusCode: 402, ErrorCode: "fixture_balance_empty", ErrorScope: "key"}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "done"}}
	p, closeFn := newProfilePool(t, plan)
	defer closeFn()
	result, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if err != nil || result.KeyID != "key-2" {
		t.Fatalf("exact evidenced key failure did not rotate safely: result=%#v err=%v", result, err)
	}
}

func TestUnknown429RemainsAmbiguousEvenWithProfile(t *testing.T) {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.CustomError, StatusCode: 429, ErrorCode: "mystery_limit", ErrorScope: "key"}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}
	p, closeFn := newProfilePool(t, plan)
	defer closeFn()
	_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if !errors.Is(err, pool.ErrAmbiguousFailure) || plan.Calls("secret-2") != 0 {
		t.Fatalf("unknown 429 was not conservative: err=%v backup_calls=%d", err, plan.Calls("secret-2"))
	}
}

func TestProviderBusyAndAmbiguousFailuresPreserveBackups(t *testing.T) {
	cases := []struct {
		name    string
		outcome fakeprovider.Outcome
		wantErr error
	}{
		{name: "provider_busy", outcome: fakeprovider.Outcome{Behavior: fakeprovider.CustomError, StatusCode: 503, ErrorCode: "fixture_global_busy", ErrorScope: "provider"}, wantErr: pool.ErrProviderBusy},
		{name: "ambiguous_502", outcome: fakeprovider.Outcome{Behavior: fakeprovider.Ambiguous502}, wantErr: pool.ErrAmbiguousFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := fakeprovider.NewPlan()
			plan.ByKey["secret-1"] = []fakeprovider.Outcome{tc.outcome}
			plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}
			p, closeFn := newProfilePool(t, plan)
			defer closeFn()
			_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
			if !errors.Is(err, tc.wantErr) || plan.Calls("secret-1") != 1 || plan.Calls("secret-2") != 0 {
				t.Fatalf("unsafe fallback: err=%v primary=%d backup=%d", err, plan.Calls("secret-1"), plan.Calls("secret-2"))
			}
		})
	}
}

func TestCostThrashBlocksBeforeBackupConsumption(t *testing.T) {
	plan := fakeprovider.NewPlan()
	miss := protocol.Usage{CacheCreationInputTokens: 120_000, CacheReadInputTokens: 0}
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Usage: miss}, {Behavior: fakeprovider.Success, Usage: miss}}
	p, closeFn := newProfilePool(t, plan)
	defer closeFn()
	if _, err := p.Execute(context.Background(), []byte(`{"prompt":"one"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(context.Background(), []byte(`{"prompt":"two"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := p.Execute(context.Background(), []byte(`{"prompt":"three"}`))
	if !errors.Is(err, pool.ErrCostSafetyBlocked) || plan.Calls("secret-2") != 0 {
		t.Fatalf("cost-thrash protection failed: err=%v backup_calls=%d", err, plan.Calls("secret-2"))
	}
}
