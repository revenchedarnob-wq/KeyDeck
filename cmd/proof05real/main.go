package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/codexapp"
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

type report struct {
	Proof         string                     `json:"proof"`
	Passed        bool                       `json:"passed"`
	Platform      string                     `json:"platform"`
	Project       string                     `json:"project"`
	Account       codexapp.AccountReadResult `json:"account"`
	Binding       session.EngineBinding      `json:"binding"`
	StartContent  string                     `json:"phase_1_content"`
	ResumeContent string                     `json:"phase_2_content"`
	FinalState    session.State              `json:"final_state"`
	Error         string                     `json:"error,omitempty"`
}

func main() {
	var projectFlag string
	var deviceLogin bool
	var setupSandbox bool
	flag.StringVar(&projectFlag, "project", "", "Disposable project directory. Omit to create a temporary proof project.")
	flag.BoolVar(&deviceLogin, "device-login", false, "Use ChatGPT device-code login instead of browser callback login.")
	flag.BoolVar(&setupSandbox, "setup-windows-sandbox", false, "Request Codex unelevated Windows sandbox setup before the proof.")
	flag.Parse()

	rep := report{Proof: "0.5-real-codex-app-server-handoff", Platform: runtime.GOOS + "/" + runtime.GOARCH}
	if runtime.GOOS != "windows" {
		fail(rep, errors.New("this proof executable is intended for Windows because KeyDeck is Windows-first"))
	}
	if _, err := exec.LookPath("codex"); err != nil {
		fail(rep, errors.New("official Codex CLI is not on PATH; install/sign in to Codex first, then rerun this proof"))
	}

	project, cleanup, err := prepareProject(projectFlag)
	if err != nil {
		fail(rep, err)
	}
	defer cleanup()
	rep.Project = project
	statePath := filepath.Join(project, ".keydeck-lab", "session.json")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	state := session.New("session-proof-05-real", project, "Prove one KeyDeck session can hand off to real Codex and resume after restart", "api-pool")
	orch := &session.Orchestrator{State: state}
	api := demoEngine{name: "api-pool", run: func(_ session.Passport, _ string) session.EngineResult {
		return session.EngineResult{
			Text:          "The disposable project is ready for a Codex handoff.",
			Decisions:     []string{"Do not repeat completed actions after engine restart."},
			PendingTasks:  []string{"Create Codex proof artifact"},
			RelevantFiles: []string{"gateway.go"},
		}
	}}
	if _, _, err := orch.Run(ctx, api, "Inspect the disposable proof project", "initial API analysis"); err != nil {
		fail(rep, err)
	}

	client1, err := startCodex(ctx)
	if err != nil {
		fail(rep, err)
	}
	account, err := ensureChatGPTAuth(ctx, client1, deviceLogin)
	if err != nil {
		_ = client1.Close()
		fail(rep, err)
	}
	rep.Account = account
	if setupSandbox {
		if _, err := client1.StartWindowsSandboxSetup(ctx, "unelevated"); err != nil {
			_ = client1.Close()
			fail(rep, fmt.Errorf("start Windows sandbox setup: %w", err))
		}
		fmt.Println("Windows sandbox setup requested. Waiting for completion...")
		note, err := client1.WaitForNotification(ctx, "windowsSandbox/setupCompleted")
		if err != nil {
			_ = client1.Close()
			fail(rep, err)
		}
		fmt.Println("Sandbox setup event:", string(note.Params))
	}

	codex1 := &codexapp.Engine{Client: client1}
	if _, _, err := orch.Run(ctx, codex1,
		"Create a file named codex-proof.txt containing exactly one line: KEYDECK_CODEX_PROOF_PHASE_1. Do not modify any other file.",
		"manual switch to real Codex"); err != nil {
		_ = client1.Close()
		fail(rep, fmt.Errorf("real Codex phase 1: %w", err))
	}
	phase1, err := os.ReadFile(filepath.Join(project, "codex-proof.txt"))
	if err != nil {
		_ = client1.Close()
		fail(rep, fmt.Errorf("Codex did not create proof file: %w", err))
	}
	rep.StartContent = string(phase1)
	if err := session.Save(statePath, orch.State); err != nil {
		_ = client1.Close()
		fail(rep, err)
	}
	_ = client1.Close()

	// Simulate a full KeyDeck restart: reload canonical state and launch a new
	// Codex App Server process. The persisted external thread binding must resume.
	reloaded, err := session.Load(statePath)
	if err != nil {
		fail(rep, err)
	}
	orch = &session.Orchestrator{State: reloaded}
	client2, err := startCodex(ctx)
	if err != nil {
		fail(rep, err)
	}
	defer client2.Close()
	if _, err := ensureChatGPTAuth(ctx, client2, deviceLogin); err != nil {
		fail(rep, err)
	}
	codex2 := &codexapp.Engine{Client: client2}
	if _, _, err := orch.Run(ctx, codex2,
		"Read codex-proof.txt and append a second line exactly: KEYDECK_CODEX_PROOF_PHASE_2. Do not modify any other file.",
		"resume real Codex after KeyDeck restart"); err != nil {
		fail(rep, fmt.Errorf("real Codex resume phase: %w", err))
	}
	phase2, err := os.ReadFile(filepath.Join(project, "codex-proof.txt"))
	if err != nil {
		fail(rep, err)
	}
	rep.ResumeContent = string(phase2)

	apiAgain := demoEngine{name: "api-pool", run: func(p session.Passport, _ string) session.EngineResult {
		b, _ := os.ReadFile(filepath.Join(p.ProjectRoot, "codex-proof.txt"))
		if strings.Contains(string(b), "KEYDECK_CODEX_PROOF_PHASE_1") && strings.Contains(string(b), "KEYDECK_CODEX_PROOF_PHASE_2") {
			return session.EngineResult{Text: "API engine can see both real Codex phases in the same canonical project/session."}
		}
		return session.EngineResult{Text: "API engine could not verify Codex work."}
	}}
	_, finalResult, err := orch.Run(ctx, apiAgain, "Review the real Codex work", "switch back to API")
	if err != nil {
		fail(rep, err)
	}

	rep.Binding = orch.State.EngineBindings["codex"]
	rep.FinalState = orch.State
	rep.Passed = strings.Contains(rep.StartContent, "KEYDECK_CODEX_PROOF_PHASE_1") &&
		strings.Contains(rep.ResumeContent, "KEYDECK_CODEX_PROOF_PHASE_1") &&
		strings.Contains(rep.ResumeContent, "KEYDECK_CODEX_PROOF_PHASE_2") &&
		rep.Binding.ExternalThreadID != "" &&
		strings.Contains(finalResult.Text, "same canonical project/session")
	writeReport(project, rep)
	if !rep.Passed {
		os.Exit(1)
	}
	fmt.Println("\nPASS: real Codex handoff, restart/resume, and switch-back proof succeeded.")
	fmt.Println("Report:", filepath.Join(project, ".keydeck-lab", "proof05-real-report.json"))
}

func startCodex(ctx context.Context) (*codexapp.Client, error) {
	transport, err := codexapp.NewProcessTransport(ctx, "codex", "app-server")
	if err != nil {
		return nil, err
	}
	client := codexapp.NewClient(transport, codexapp.ClientInfo{Name: "keydeck_lab", Title: "KeyDeck Feasibility Lab", Version: "0.2.0"})
	if err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func ensureChatGPTAuth(ctx context.Context, client *codexapp.Client, deviceCode bool) (codexapp.AccountReadResult, error) {
	account, err := client.AccountRead(ctx)
	if err != nil {
		return account, err
	}
	if account.Account != nil && account.Account.Type == "chatgpt" {
		fmt.Printf("Codex ChatGPT account detected (plan: %s).\n", account.Account.PlanType)
		return account, nil
	}
	login, err := client.StartChatGPTLogin(ctx, deviceCode)
	if err != nil {
		return account, err
	}
	if login.AuthURL != "" {
		fmt.Println("Opening official ChatGPT login in your browser...")
		fmt.Println(login.AuthURL)
		_ = openURL(login.AuthURL)
	} else {
		fmt.Println("Open this official verification page:", login.VerificationURL)
		fmt.Println("Enter code:", login.UserCode)
		_ = openURL(login.VerificationURL)
	}
	note, err := client.WaitForNotification(ctx, "account/login/completed")
	if err != nil {
		return account, err
	}
	var completed struct {
		Success bool `json:"success"`
		Error   any  `json:"error"`
	}
	if err := json.Unmarshal(note.Params, &completed); err != nil {
		return account, err
	}
	if !completed.Success {
		return account, fmt.Errorf("ChatGPT login failed: %v", completed.Error)
	}
	account, err = client.AccountRead(ctx)
	if err != nil {
		return account, err
	}
	if account.Account == nil || account.Account.Type != "chatgpt" {
		return account, errors.New("login completed but Codex did not report ChatGPT-managed authentication")
	}
	return account, nil
}

func prepareProject(requested string) (string, func(), error) {
	if requested != "" {
		abs, err := filepath.Abs(requested)
		return abs, func() {}, err
	}
	dir, err := os.MkdirTemp("", "keydeck-real-codex-proof-")
	if err != nil {
		return "", func() {}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.go"), []byte("package proof\n\n// Disposable KeyDeck real-Codex proof project.\n"), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", func() {}, err
	}
	if git, err := exec.LookPath("git"); err == nil {
		_ = exec.Command(git, "-C", dir, "init").Run()
	}
	return dir, func() { fmt.Println("Disposable proof project kept for inspection:", dir) }, nil
}

func openURL(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func writeReport(project string, rep report) {
	path := filepath.Join(project, ".keydeck-lab", "proof05-real-report.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	b, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(path, b, 0o600)
}

func fail(rep report, err error) {
	rep.Error = err.Error()
	if rep.Project != "" {
		writeReport(rep.Project, rep)
	}
	fmt.Fprintln(os.Stderr, "FAIL:", err)
	os.Exit(1)
}
