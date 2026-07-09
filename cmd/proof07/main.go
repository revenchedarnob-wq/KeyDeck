package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/apiengine"
	"keydeck.local/feasibilitylab/internal/continuity"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
	"keydeck.local/feasibilitylab/internal/session"
)

type continuationEngine struct{}

func (continuationEngine) Name() string { return "codex" }
func (continuationEngine) Run(_ context.Context, passport session.Passport, _ string) (session.EngineResult, error) {
	if passport.Continuation == nil {
		return session.EngineResult{}, errors.New("missing continuation state")
	}
	return session.EngineResult{Text: "Codex continued from the next logical step and completed the task."}, nil
}

func main() {
	project, err := os.MkdirTemp("", "keydeck-proof-07-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(project)

	streamPlan := fakeprovider.NewStreamPlan()
	streamPlan.ByKey["secret-1"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"API inspection confirmed the protected file. ", "The next step is "},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	streamPlan.ByKey["secret-2"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"The next step is to create the cross-engine artifact. ", "Additional verification requires "},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	streamPlan.ByKey["secret-3"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"Additional verification requires a second engine. ", "Final recommendation "},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	srv := httptest.NewServer(fakeprovider.Mux(fakeprovider.NewPlan(), streamPlan))
	defer srv.Close()

	recorder := &events.Recorder{}
	streaming := continuity.New([]pool.Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
		{ID: "key-3", Secret: "secret-3"},
	}, &providerhttp.StreamClient{BaseURL: srv.URL}, recorder)
	api := &apiengine.StreamingEngine{Continuity: streaming}

	state := session.New("session-proof-07", project, "Prove cross-engine mid-answer continuation", "api-pool")
	orch := &session.Orchestrator{State: state}
	prompt := "Continue this one task across API exhaustion and another engine."
	_, _, primaryErr := orch.BeginInterruptible(context.Background(), api, prompt, "start streamed API response")
	staged := orch.State.InFlightResponse != nil

	statePath := filepath.Join(project, "state.json")
	if err := session.Save(statePath, orch.State); err != nil {
		panic(err)
	}
	reloaded, err := session.Load(statePath)
	if err != nil {
		panic(err)
	}
	orch = &session.Orchestrator{State: reloaded}

	_, finalResult, fallbackErr := orch.ContinueInFlight(context.Background(), continuationEngine{}, "all API keys exhausted after partial output")
	confirmed := ""
	unstable := ""
	if reloaded.InFlightResponse != nil {
		confirmed = reloaded.InFlightResponse.Continuation.ConfirmedOutput
		unstable = reloaded.InFlightResponse.Continuation.UnstableFragment
	}

	passed := errors.Is(primaryErr, pool.ErrAllKeysUnavailable) &&
		staged && fallbackErr == nil &&
		strings.Contains(confirmed, "API inspection confirmed the protected file.") &&
		strings.Contains(confirmed, "Additional verification requires a second engine.") &&
		unstable == "Final recommendation " &&
		strings.Count(finalResult.Text, "API inspection confirmed the protected file.") == 1 &&
		strings.Contains(finalResult.Text, "Codex continued from the next logical step") &&
		orch.State.InFlightResponse == nil &&
		len(orch.State.Transcript) == 2

	report := map[string]any{
		"proof":              "0.7-cross-engine-mid-answer-continuation-policy",
		"passed":             passed,
		"primary_error":      errorString(primaryErr),
		"fallback_error":     errorString(fallbackErr),
		"confirmed_output":   confirmed,
		"unstable_fragment":  unstable,
		"final_visible_text": finalResult.Text,
		"events":             recorder.Snapshot(),
		"stream_requests":    streamPlan.SnapshotRequests(),
		"final_state":        orch.State,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if !passed {
		os.Exit(1)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
