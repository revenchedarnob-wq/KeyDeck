package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

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
	state := session.New("session-proof-04", `C:\Projects\KeyDeck`, "Preserve one chat while switching engines", "api-pool")
	orch := &session.Orchestrator{State: state}

	api := demoEngine{name: "api-pool", run: func(_ session.Passport, _ string) session.EngineResult {
		return session.EngineResult{
			Text:          "I found the financially ambiguous retry path.",
			Decisions:     []string{"Never replay an ambiguous upstream outcome automatically."},
			PendingTasks:  []string{"Implement safe classifier"},
			RelevantFiles: []string{"internal/gateway/server_windows.go"},
		}
	}}
	passport1, result1, err := orch.Run(context.Background(), api, "Inspect the gateway", "initial analysis")
	if err != nil {
		panic(err)
	}

	dir, _ := os.MkdirTemp("", "keydeck-proof04-")
	defer os.RemoveAll(dir)
	statePath := filepath.Join(dir, "session.json")
	if err := session.Save(statePath, orch.State); err != nil {
		panic(err)
	}
	reloaded, err := session.Load(statePath)
	if err != nil {
		panic(err)
	}
	orch = &session.Orchestrator{State: reloaded}

	codex := demoEngine{name: "codex", run: func(p session.Passport, _ string) session.EngineResult {
		return session.EngineResult{
			Text:             "I continued from the API analysis and updated the retry classifier.",
			CompletedActions: []string{"Updated retry classifier"},
			PendingTasks:     []string{"Run failure lab"},
			Checkpoint:       "checkpoint-codex-1",
		}
	}}
	passport2, result2, err := orch.Run(context.Background(), codex, "Continue and fix it", "manual model switch")
	if err != nil {
		panic(err)
	}

	apiAgain := demoEngine{name: "api-pool", run: func(p session.Passport, _ string) session.EngineResult {
		return session.EngineResult{Text: "I can see the Codex checkpoint and will review the change."}
	}}
	passport3, result3, err := orch.Run(context.Background(), apiAgain, "Review Codex's change", "manual switch back")
	if err != nil {
		panic(err)
	}

	passportBytes, _ := json.Marshal(passport2)
	report := map[string]any{
		"proof":         "0.4-canonical-session-engine-switching",
		"passed":        len(orch.State.Transcript) == 6 && orch.State.ActiveEngine == "api-pool" && strings.Contains(string(passportBytes), "financially ambiguous") && !strings.Contains(strings.ToLower(string(passportBytes)), "secret-key"),
		"first_run":     map[string]any{"passport": passport1, "result": result1},
		"codex_handoff": map[string]any{"passport": passport2, "result": result2},
		"switch_back":   map[string]any{"passport": passport3, "result": result3},
		"final_state":   orch.State,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if !report["passed"].(bool) {
		os.Exit(1)
	}
}
