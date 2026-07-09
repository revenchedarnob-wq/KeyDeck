package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

type fakeEngine struct {
	name string
	run  func(Passport, string) EngineResult
}

func (f fakeEngine) Name() string { return f.name }
func (f fakeEngine) Run(_ context.Context, p Passport, prompt string) (EngineResult, error) {
	return f.run(p, prompt), nil
}

func TestOneCanonicalSessionSurvivesEngineSwitchAndRestart(t *testing.T) {
	state := New("session-1", `C:\Projects\KeyDeck`, "Fix gateway safety", "api-pool")
	orch := &Orchestrator{State: state}

	api := fakeEngine{name: "api-pool", run: func(p Passport, _ string) EngineResult {
		return EngineResult{
			Text:          "The retry policy is financially ambiguous.",
			Decisions:     []string{"Do not replay ambiguous 502/504/network failures."},
			PendingTasks:  []string{"Implement safe retry policy"},
			RelevantFiles: []string{"internal/gateway/server_windows.go"},
		}
	}}
	_, _, err := orch.Run(context.Background(), api, "Inspect the gateway", "initial analysis")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "session.json")
	if err := Save(path, orch.State); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	orch = &Orchestrator{State: reloaded}

	codex := fakeEngine{name: "codex", run: func(p Passport, _ string) EngineResult {
		b, _ := json.Marshal(p)
		if !strings.Contains(string(b), "financially ambiguous") || !strings.Contains(string(b), "Do not replay ambiguous") {
			t.Fatalf("handoff lost prior model context: %s", b)
		}
		if strings.Contains(strings.ToLower(string(b)), "api_key") || strings.Contains(string(b), "secret-key") {
			t.Fatalf("passport leaked secret material: %s", b)
		}
		return EngineResult{
			Text:             "I changed the retry policy without replaying ambiguous outcomes.",
			CompletedActions: []string{"Updated retry classifier"},
			PendingTasks:     []string{"Run failure lab"},
			Checkpoint:       "checkpoint-codex-1",
		}
	}}
	_, _, err = orch.Run(context.Background(), codex, "Continue and fix it", "manual model switch")
	if err != nil {
		t.Fatal(err)
	}

	api2 := fakeEngine{name: "api-pool", run: func(p Passport, _ string) EngineResult {
		b, _ := json.Marshal(p)
		if !strings.Contains(string(b), "Updated retry classifier") || p.Checkpoint != "checkpoint-codex-1" {
			t.Fatalf("switching back lost Codex work: %s", b)
		}
		return EngineResult{Text: "I can see the Codex change and will review it."}
	}}
	_, _, err = orch.Run(context.Background(), api2, "Review the Codex change", "manual switch back")
	if err != nil {
		t.Fatal(err)
	}

	if len(orch.State.Transcript) != 6 {
		t.Fatalf("expected one transcript with 6 messages, got %d", len(orch.State.Transcript))
	}
	if orch.State.ActiveEngine != "api-pool" {
		t.Fatalf("wrong active engine: %s", orch.State.ActiveEngine)
	}
}

type errorEngine struct {
	name string
	err  error
}

func (e errorEngine) Name() string { return e.name }
func (e errorEngine) Run(_ context.Context, _ Passport, _ string) (EngineResult, error) {
	return EngineResult{}, e.err
}

func TestRunWithFallbackRecordsUserTaskOnce(t *testing.T) {
	state := New("session-fallback", t.TempDir(), "Finish project safely", "api-pool")
	orch := &Orchestrator{State: state}
	primaryErr := errors.New("all keys unavailable")
	primary := errorEngine{name: "api-pool", err: primaryErr}
	fallback := fakeEngine{name: "codex", run: func(p Passport, _ string) EngineResult {
		if p.HandoffReason != "api pool exhausted" {
			t.Fatalf("unexpected handoff reason: %s", p.HandoffReason)
		}
		return EngineResult{Text: "continued with Codex"}
	}}

	_, _, err := orch.RunWithFallback(
		context.Background(),
		primary,
		fallback,
		"Continue the task",
		"try API pool",
		"api pool exhausted",
		func(err error) bool { return errors.Is(err, primaryErr) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(orch.State.Transcript); got != 2 {
		t.Fatalf("expected exactly one user message and one assistant message, got %d", got)
	}
	if orch.State.Transcript[0].Role != RoleUser || orch.State.Transcript[0].Text != "Continue the task" {
		t.Fatalf("unexpected user turn: %#v", orch.State.Transcript[0])
	}
	if orch.State.ActiveEngine != "codex" {
		t.Fatalf("expected codex active, got %s", orch.State.ActiveEngine)
	}
}

func TestRunWithFallbackDoesNotFallbackWhenPolicyRejectsError(t *testing.T) {
	state := New("session-no-fallback", t.TempDir(), "Protect backup keys", "api-pool")
	orch := &Orchestrator{State: state}
	primaryErr := errors.New("provider busy")
	primary := errorEngine{name: "api-pool", err: primaryErr}
	fallbackRuns := 0
	fallback := fakeEngine{name: "codex", run: func(_ Passport, _ string) EngineResult {
		fallbackRuns++
		return EngineResult{Text: "should not run"}
	}}

	_, _, err := orch.RunWithFallback(
		context.Background(), primary, fallback, "Continue", "try API", "fallback", func(error) bool { return false },
	)
	if !errors.Is(err, primaryErr) {
		t.Fatalf("expected primary error, got %v", err)
	}
	if fallbackRuns != 0 {
		t.Fatalf("fallback ran %d times", fallbackRuns)
	}
	if got := len(orch.State.Transcript); got != 1 {
		t.Fatalf("expected one canonical user message, got %d", got)
	}
}

type partialEngine struct {
	name         string
	cause        error
	continuation ContinuationState
}

func (e partialEngine) Name() string { return e.name }
func (e partialEngine) Run(_ context.Context, _ Passport, _ string) (EngineResult, error) {
	return EngineResult{}, &PartialResultError{Cause: e.cause, Continuation: e.continuation}
}

func TestInterruptibleResponseSurvivesRestartAndContinuesOnce(t *testing.T) {
	root := t.TempDir()
	cause := errors.New("all keys unavailable")
	state := New("session-interruptible", root, "Continue safely across engines", "api-pool")
	orch := &Orchestrator{State: state}
	primary := partialEngine{
		name:  "api-pool",
		cause: cause,
		continuation: ContinuationState{
			ConfirmedOutput:  "API completed one safe sentence.",
			UnstableFragment: "The unfinished thought was ",
			SourceEngine:     "api-pool",
			Reason:           "all keys exhausted mid-answer",
		},
	}

	_, _, err := orch.BeginInterruptible(context.Background(), primary, "Finish the task", "start on API")
	if !errors.Is(err, cause) {
		t.Fatalf("expected wrapped primary cause, got %v", err)
	}
	if orch.State.InFlightResponse == nil {
		t.Fatal("expected in-flight continuation state")
	}
	if got := len(orch.State.Transcript); got != 1 {
		t.Fatalf("expected only one user message before handoff, got %d", got)
	}

	path := filepath.Join(root, "state.json")
	if err := Save(path, orch.State); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	orch = &Orchestrator{State: reloaded}

	fallback := fakeEngine{name: "codex", run: func(p Passport, _ string) EngineResult {
		if p.Continuation == nil {
			t.Fatal("continuation state missing from handoff passport")
		}
		if p.Continuation.ConfirmedOutput != "API completed one safe sentence." {
			t.Fatalf("wrong confirmed output: %q", p.Continuation.ConfirmedOutput)
		}
		if p.Continuation.UnstableFragment != "The unfinished thought was " {
			t.Fatalf("wrong unstable fragment: %q", p.Continuation.UnstableFragment)
		}
		return EngineResult{Text: "Codex completed the task without repeating the API sentence."}
	}}
	_, result, err := orch.ContinueInFlight(context.Background(), fallback, "continue interrupted response")
	if err != nil {
		t.Fatal(err)
	}
	if orch.State.InFlightResponse != nil {
		t.Fatal("in-flight response should be cleared after successful continuation")
	}
	if got := len(orch.State.Transcript); got != 2 {
		t.Fatalf("expected one user and one final assistant message, got %d", got)
	}
	if strings.Count(result.Text, "API completed one safe sentence.") != 1 {
		t.Fatalf("confirmed output was not preserved exactly once: %q", result.Text)
	}
	if !strings.Contains(result.Text, "Codex completed the task") {
		t.Fatalf("fallback continuation missing: %q", result.Text)
	}
}

func TestInterruptibleResponseDoesNotStageAmbiguousError(t *testing.T) {
	state := New("session-ambiguous", t.TempDir(), "Do not guess", "api-pool")
	orch := &Orchestrator{State: state}
	primary := errorEngine{name: "api-pool", err: errors.New("ambiguous network failure")}
	_, _, err := orch.BeginInterruptible(context.Background(), primary, "Continue", "try API")
	if err == nil {
		t.Fatal("expected error")
	}
	if orch.State.InFlightResponse != nil {
		t.Fatal("ambiguous failures must not create safe continuation state")
	}
	if got := len(orch.State.Transcript); got != 1 {
		t.Fatalf("expected one user task, got %d", got)
	}
}
