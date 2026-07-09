package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
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
	Proof     string     `json:"proof"`
	Passed    bool       `json:"passed"`
	Claims    []string   `json:"claims"`
	Scenarios []scenario `json:"scenarios"`
}

func profile() conformance.ProviderProfile {
	return conformance.ProviderProfile{
		Provider:   "fixture-provider",
		Version:    "2026-07-07",
		TestedAt:   time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		EvidenceID: "proof-0.10-provider-fixture",
		Rules: []conformance.FailureRule{
			{StatusCode: 402, ErrorCode: "fixture_balance_empty", ErrorScope: "key", Class: pool.FailureKeyExhausted},
			{StatusCode: 503, ErrorCode: "fixture_global_busy", ErrorScope: "provider", Class: pool.FailureProviderBusy},
		},
	}
}

func guard() *costguard.Guard {
	g, err := costguard.New(costguard.Config{
		LargeWriteMin:    100_000,
		LowReadMax:       5_000,
		ConsecutiveLimit: 2,
		ExtremeWriteMin:  180_000,
	})
	if err != nil {
		panic(err)
	}
	return g
}

func runPool(plan *fakeprovider.Plan, execute func(*pool.Pool) error) error {
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	defer srv.Close()
	p := pool.NewWithClassifier(
		[]pool.Key{{ID: "key-1", Secret: "secret-1"}, {ID: "key-2", Secret: "secret-2"}},
		&providerhttp.Client{BaseURL: srv.URL},
		&events.Recorder{},
		guard(),
		profile(),
	)
	return execute(p)
}

func main() {
	scenarios := []scenario{
		optimizationOffScenario(),
		verifiedOptimizerScenario(),
		exactProviderRotationScenario(),
		unknown429Scenario(),
		providerBusyScenario(),
		ambiguous502Scenario(),
		costThrashScenario(),
	}
	passed := true
	for _, s := range scenarios {
		passed = passed && s.Passed
	}
	r := report{
		Proof:  "0.10-provider-optimizer-conformance",
		Passed: passed,
		Claims: []string{
			"Optimization OFF preserves request bytes exactly and never invokes an optimizer",
			"Optimization ON activates only for exact provider/version evidence marked verified with correctness and measured benefit",
			"provider-specific safe key rotation requires exact dated evidence",
			"unknown 429 behavior remains ambiguous and preserves backup keys",
			"provider-wide busy preserves backup keys",
			"ambiguous 502 outcomes are neither replayed nor rotated",
			"cost-thrash protection blocks before a backup key can be consumed",
		},
		Scenarios: scenarios,
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
	if !passed {
		os.Exit(1)
	}
}

func optimizationOffScenario() scenario {
	input := []byte("{\n  \"cache_control\" : { \"type\" : \"ephemeral\" },\n  \"prompt\" : \"preserve exact bytes\"\n}\n")
	called := false
	optimizer := conformance.Optimizer{Transform: func(in []byte) ([]byte, error) {
		called = true
		return append(in, '!'), nil
	}}
	out, activation, err := optimizer.Apply(conformance.OptimizationOff, "unknown", "unknown", input)
	passed := err == nil && !called && !activation.Active && bytes.Equal(input, out)
	return scenario{Name: "optimization_off_byte_preserving", Passed: passed, Detail: map[string]any{"transform_called": called, "activation": activation, "bytes_equal": bytes.Equal(input, out)}}
}

func verifiedOptimizerScenario() scenario {
	input := []byte(`{"prompt":"proof"}`)
	optimizer := conformance.Optimizer{
		Evidence: conformance.OptimizerEvidence{
			Provider: "fixture-provider", Version: "2026-07-07", TestedAt: time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
			EvidenceID: "proof-0.10-optimizer-fixture", Status: conformance.EvidenceVerified, CorrectnessPreserved: true, MeasurableBenefit: true,
		},
		Transform: func(in []byte) ([]byte, error) { return append(in, []byte("\nverified")...), nil },
	}
	optimized, activation, err := optimizer.Apply(conformance.OptimizationOn, "fixture-provider", "2026-07-07", input)
	blocked, blockedActivation, blockedErr := optimizer.Apply(conformance.OptimizationOn, "fixture-provider", "wrong-version", input)
	passed := err == nil && activation.Active && !bytes.Equal(optimized, input) && errors.Is(blockedErr, conformance.ErrOptimizationNotVerified) && !blockedActivation.Active && bytes.Equal(blocked, input)
	return scenario{Name: "verified_optimizer_exact_gate", Passed: passed, Detail: map[string]any{"verified_activation": activation, "mismatched_activation": blockedActivation, "mismatched_error": errorString(blockedErr)}}
}

func exactProviderRotationScenario() scenario {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.CustomError, StatusCode: 402, ErrorCode: "fixture_balance_empty", ErrorScope: "key"}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "done"}}
	var keyID string
	err := runPool(plan, func(p *pool.Pool) error {
		result, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
		keyID = result.KeyID
		return err
	})
	passed := err == nil && keyID == "key-2" && plan.Calls("secret-1") == 1 && plan.Calls("secret-2") == 1
	return scenario{Name: "exact_provider_rule_safe_rotation", Passed: passed, Detail: map[string]any{"selected_key": keyID, "primary_calls": plan.Calls("secret-1"), "backup_calls": plan.Calls("secret-2"), "error": errorString(err)}}
}

func unknown429Scenario() scenario {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.CustomError, StatusCode: 429, ErrorCode: "mystery_limit", ErrorScope: "key"}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}
	err := runPool(plan, func(p *pool.Pool) error {
		_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
		return err
	})
	passed := errors.Is(err, pool.ErrAmbiguousFailure) && plan.Calls("secret-1") == 1 && plan.Calls("secret-2") == 0
	return scenario{Name: "unknown_429_stays_ambiguous", Passed: passed, Detail: map[string]any{"primary_calls": plan.Calls("secret-1"), "backup_calls": plan.Calls("secret-2"), "error": errorString(err)}}
}

func providerBusyScenario() scenario {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.CustomError, StatusCode: 503, ErrorCode: "fixture_global_busy", ErrorScope: "provider"}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}
	err := runPool(plan, func(p *pool.Pool) error {
		_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
		return err
	})
	passed := errors.Is(err, pool.ErrProviderBusy) && plan.Calls("secret-1") == 1 && plan.Calls("secret-2") == 0
	return scenario{Name: "provider_busy_preserves_backups", Passed: passed, Detail: map[string]any{"primary_calls": plan.Calls("secret-1"), "backup_calls": plan.Calls("secret-2"), "error": errorString(err)}}
}

func ambiguous502Scenario() scenario {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Ambiguous502}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}
	err := runPool(plan, func(p *pool.Pool) error {
		_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
		return err
	})
	passed := errors.Is(err, pool.ErrAmbiguousFailure) && plan.Calls("secret-1") == 1 && plan.Calls("secret-2") == 0
	return scenario{Name: "ambiguous_502_no_replay_or_rotation", Passed: passed, Detail: map[string]any{"primary_calls": plan.Calls("secret-1"), "backup_calls": plan.Calls("secret-2"), "error": errorString(err)}}
}

func costThrashScenario() scenario {
	plan := fakeprovider.NewPlan()
	miss := protocol.Usage{CacheCreationInputTokens: 120_000, CacheReadInputTokens: 0}
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Usage: miss}, {Behavior: fakeprovider.Success, Usage: miss}}
	var thirdErr error
	err := runPool(plan, func(p *pool.Pool) error {
		if _, err := p.Execute(context.Background(), []byte(`{"prompt":"one"}`)); err != nil {
			return err
		}
		if _, err := p.Execute(context.Background(), []byte(`{"prompt":"two"}`)); err != nil {
			return err
		}
		_, thirdErr = p.Execute(context.Background(), []byte(`{"prompt":"three"}`))
		return nil
	})
	passed := err == nil && errors.Is(thirdErr, pool.ErrCostSafetyBlocked) && plan.Calls("secret-2") == 0
	return scenario{Name: "cost_thrash_blocks_before_backup", Passed: passed, Detail: map[string]any{"primary_calls": plan.Calls("secret-1"), "backup_calls": plan.Calls("secret-2"), "third_error": errorString(thirdErr), "setup_error": errorString(err)}}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
