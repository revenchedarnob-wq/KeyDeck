package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/codexapp"
	"keydeck.local/feasibilitylab/internal/fakecodexapp"
	"keydeck.local/feasibilitylab/internal/session"
)

type demoEngine struct {
	name string
	run  func(session.Passport, string) session.EngineResult
}

func (d demoEngine) Name() string { return d.name }
func (d demoEngine) Run(_ context.Context, p session.Passport, prompt string) (session.EngineResult, error) {
	return d.run(p, prompt), nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	project, err := os.MkdirTemp("", "keydeck-proof05-project-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(project)
	if err := os.WriteFile(filepath.Join(project, "gateway.go"), []byte("package demo\n"), 0o600); err != nil {
		panic(err)
	}

	statePath := filepath.Join(project, ".keydeck", "session.json")
	state := session.New("session-proof-05", project, "Prove one KeyDeck session can hand off to Codex and resume after restart", "api-pool")
	orch := &session.Orchestrator{State: state}

	api := demoEngine{name: "api-pool", run: func(_ session.Passport, _ string) session.EngineResult {
		return session.EngineResult{
			Text:          "The retry classifier is the next safe task.",
			Decisions:     []string{"Do not replay ambiguous upstream outcomes."},
			PendingTasks:  []string{"Update retry classifier"},
			RelevantFiles: []string{"gateway.go"},
		}
	}}
	if _, _, err := orch.Run(ctx, api, "Inspect the disposable project", "initial analysis"); err != nil {
		panic(err)
	}

	fakeState := &fakecodexapp.State{}
	client1, close1 := newFakeClient(ctx, fakeState)
	if err := client1.Initialize(ctx); err != nil {
		panic(err)
	}
	account, err := client1.AccountRead(ctx)
	if err != nil {
		panic(err)
	}
	codex1 := &codexapp.Engine{Client: client1, Model: "gpt-proof"}
	passport1, result1, err := orch.Run(ctx, codex1, "Continue and update the disposable project", "manual engine switch")
	if err != nil {
		panic(err)
	}
	if err := session.Save(statePath, orch.State); err != nil {
		panic(err)
	}
	close1()

	reloaded, err := session.Load(statePath)
	if err != nil {
		panic(err)
	}
	orch = &session.Orchestrator{State: reloaded}

	client2, close2 := newFakeClient(ctx, fakeState)
	defer close2()
	if err := client2.Initialize(ctx); err != nil {
		panic(err)
	}
	codex2 := &codexapp.Engine{Client: client2, Model: "gpt-proof"}
	passport2, result2, err := orch.Run(ctx, codex2, "Reopen the same Codex work after KeyDeck restart", "resume after restart")
	if err != nil {
		panic(err)
	}

	apiAgain := demoEngine{name: "api-pool", run: func(p session.Passport, _ string) session.EngineResult {
		b, _ := json.Marshal(p)
		if !strings.Contains(string(b), "codex-proof.txt") {
			return session.EngineResult{Text: "Could not see Codex work."}
		}
		return session.EngineResult{Text: "I can see the Codex file change and continue from the same canonical session."}
	}}
	passport3, result3, err := orch.Run(ctx, apiAgain, "Review the Codex work", "switch back to API")
	if err != nil {
		panic(err)
	}

	snap := fakeState.Snapshot()
	binding := orch.State.EngineBindings["codex"]
	_, fileErr := os.Stat(filepath.Join(project, "codex-proof.txt"))
	stateBytes, _ := json.Marshal(orch.State)
	passed := account.Account != nil && account.Account.PlanType == "plus" &&
		snap.StartCalls == 1 && snap.ResumeCalls >= 1 && snap.TurnCalls == 2 &&
		binding.ExternalThreadID == "thr_keydeck_proof05" && fileErr == nil &&
		strings.Contains(snap.LastPrompt, "Do not repeat completed actions") &&
		strings.Contains(result3.Text, "same canonical session") &&
		!strings.Contains(strings.ToLower(string(stateBytes)), "api_key") &&
		!strings.Contains(string(stateBytes), "sk-")

	report := map[string]any{
		"proof":             "0.5-codex-app-server-session-binding",
		"passed":            passed,
		"scope":             "Protocol/session proof uses a deterministic fake Codex App Server. Real ChatGPT Plus login and real Codex execution require the user's Windows PC.",
		"account":           account,
		"first_codex_run":   map[string]any{"passport": passport1, "result": result1},
		"restart_resume":    map[string]any{"passport": passport2, "result": result2},
		"switch_back":       map[string]any{"passport": passport3, "result": result3},
		"external_server":   snap,
		"persisted_binding": binding,
		"final_state":       orch.State,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if !passed {
		os.Exit(1)
	}
}

func newFakeClient(ctx context.Context, state *fakecodexapp.State) (*codexapp.Client, func()) {
	clientConn, serverConn := net.Pipe()
	go func() { _ = fakecodexapp.Serve(serverConn, state) }()
	c := codexapp.NewClient(codexapp.NewStreamTransport(clientConn), codexapp.ClientInfo{Name: "keydeck_lab", Title: "KeyDeck Feasibility Lab", Version: "0.2.0"})
	return c, func() { _ = c.Close() }
}
