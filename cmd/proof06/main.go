package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"

	"keydeck.local/feasibilitylab/internal/apiengine"
	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
	"keydeck.local/feasibilitylab/internal/session"
)

type fakeCodex struct{ runs int }

func (f *fakeCodex) Name() string { return "codex" }
func (f *fakeCodex) Run(_ context.Context, p session.Passport, _ string) (session.EngineResult, error) {
	f.runs++
	return session.EngineResult{
		Text:             "Codex continued the canonical task after API capacity was exhausted.",
		CompletedActions: []string{"Created proof artifact"},
		PendingTasks:     []string{"Resume after restart"},
		RelevantFiles:    []string{"codex-proof.txt"},
		Checkpoint:       "checkpoint-proof-06",
	}, nil
}

type scenarioReport struct {
	Name           string         `json:"name"`
	Passed         bool           `json:"passed"`
	Error          string         `json:"error,omitempty"`
	FallbackRuns   int            `json:"fallback_runs"`
	Calls          map[string]int `json:"calls"`
	TranscriptSize int            `json:"transcript_size"`
	ActiveEngine   string         `json:"active_engine"`
}

func main() {
	reports := []scenarioReport{
		runAllKeysExhausted(),
		runNoFallbackScenario("provider-busy", fakeprovider.ProviderBusy, pool.ErrProviderBusy),
		runNoFallbackScenario("ambiguous-502", fakeprovider.Ambiguous502, pool.ErrAmbiguousFailure),
	}
	passed := true
	for _, r := range reports {
		passed = passed && r.Passed
	}
	out := map[string]any{"proof": "0.6-automatic-api-pool-to-codex", "passed": passed, "scenarios": reports}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !passed {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "PASS: Proof 0.6 automatic API-pool -> Codex policy")
}

func runAllKeysExhausted() scenarioReport {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-3"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	defer srv.Close()
	api := newAPIEngine(srv.URL, plan)
	codex := &fakeCodex{}
	state := session.New("proof06", os.TempDir(), "Continue safely when all API keys are exhausted", "api-pool")
	orch := &session.Orchestrator{State: state}
	_, _, err := orch.RunWithFallback(
		context.Background(), api, codex, "Finish the canonical task", "try API pool", "all API keys exhausted; continue with Codex",
		func(err error) bool { return errors.Is(err, pool.ErrAllKeysUnavailable) },
	)
	calls := map[string]int{"key-1": plan.Calls("secret-1"), "key-2": plan.Calls("secret-2"), "key-3": plan.Calls("secret-3")}
	passed := err == nil && codex.runs == 1 && calls["key-1"] == 1 && calls["key-2"] == 1 && calls["key-3"] == 1 && len(orch.State.Transcript) == 2 && orch.State.ActiveEngine == "codex"
	return scenarioReport{Name: "all-keys-exhausted", Passed: passed, Error: errorString(err), FallbackRuns: codex.runs, Calls: calls, TranscriptSize: len(orch.State.Transcript), ActiveEngine: orch.State.ActiveEngine}
}

func runNoFallbackScenario(name string, behavior fakeprovider.Behavior, expected error) scenarioReport {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: behavior}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "must stay untouched"}}
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	defer srv.Close()
	api := newAPIEngine(srv.URL, plan)
	codex := &fakeCodex{}
	state := session.New("proof06-"+name, os.TempDir(), "Protect safe fallback boundaries", "api-pool")
	orch := &session.Orchestrator{State: state}
	_, _, err := orch.RunWithFallback(
		context.Background(), api, codex, "Continue", "try API pool", "fallback",
		func(err error) bool { return errors.Is(err, pool.ErrAllKeysUnavailable) },
	)
	calls := map[string]int{"key-1": plan.Calls("secret-1"), "key-2": plan.Calls("secret-2")}
	passed := errors.Is(err, expected) && codex.runs == 0 && calls["key-1"] == 1 && calls["key-2"] == 0 && len(orch.State.Transcript) == 1 && orch.State.ActiveEngine == "api-pool"
	return scenarioReport{Name: name, Passed: passed, Error: errorString(err), FallbackRuns: codex.runs, Calls: calls, TranscriptSize: len(orch.State.Transcript), ActiveEngine: orch.State.ActiveEngine}
}

func newAPIEngine(baseURL string, _ *fakeprovider.Plan) *apiengine.Engine {
	guard, _ := costguard.New(costguard.Config{LargeWriteMin: 100_000, LowReadMax: 5_000, ConsecutiveLimit: 2, ExtremeWriteMin: 180_000})
	p := pool.New([]pool.Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
		{ID: "key-3", Secret: "secret-3"},
	}, &providerhttp.Client{BaseURL: baseURL}, &events.Recorder{}, guard)
	return &apiengine.Engine{Pool: p}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
