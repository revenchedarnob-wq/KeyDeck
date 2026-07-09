package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/apiengine"
	"keydeck.local/feasibilitylab/internal/codexapp"
	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
	"keydeck.local/feasibilitylab/internal/session"
)

const (
	phase1Marker = "KEYDECK_PROOF_06_CODEX_PHASE_1"
	phase2Marker = "KEYDECK_PROOF_06_CODEX_PHASE_2"
)

type accountSummary struct {
	Type string `json:"type"`
	Plan string `json:"plan"`
}

type report struct {
	Proof                    string                `json:"proof"`
	Passed                   bool                  `json:"passed"`
	Platform                 string                `json:"platform"`
	Project                  string                `json:"project"`
	Account                  accountSummary        `json:"account"`
	APIFallbackReason        string                `json:"api_fallback_reason"`
	InitialKeyCalls          map[string]int        `json:"initial_key_calls"`
	InitialKeyStates         []pool.Key            `json:"initial_key_states"`
	InitialAPIEvents         []events.Event        `json:"initial_api_events"`
	CodexBinding             session.EngineBinding `json:"codex_binding"`
	Phase1Content            string                `json:"phase_1_content"`
	Phase2Content            string                `json:"phase_2_content"`
	RecoveredAPIRequestSawP1 bool                  `json:"recovered_api_request_saw_phase_1"`
	RecoveredAPIRequestSawP2 bool                  `json:"recovered_api_request_saw_phase_2"`
	FinalState               session.State         `json:"final_state"`
	Error                    string                `json:"error,omitempty"`
}

func main() {
	var projectFlag string
	var deviceLogin bool
	flag.StringVar(&projectFlag, "project", "", "Disposable project directory. Omit to create a temporary proof project.")
	flag.BoolVar(&deviceLogin, "device-login", false, "Use ChatGPT device-code login instead of browser callback login.")
	flag.Parse()

	rep := report{Proof: "0.6-real-automatic-api-pool-to-codex", Platform: runtime.GOOS + "/" + runtime.GOARCH}
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// The initial API provider is fully synthetic. Every key is explicitly
	// exhausted so no real credit is spent while proving automatic handoff.
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-3"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	provider := httptest.NewServer(fakeprovider.Handler(plan))
	defer provider.Close()

	guard, err := newGuard()
	if err != nil {
		fail(rep, err)
	}
	recorder := &events.Recorder{}
	apiPool := pool.New([]pool.Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
		{ID: "key-3", Secret: "secret-3"},
	}, &providerhttp.Client{BaseURL: provider.URL}, recorder, guard)
	api := &apiengine.Engine{Pool: apiPool}

	state := session.New("session-proof-06-real", project, "Prove KeyDeck can automatically fall back from an exhausted API pool to real Codex and preserve one canonical task", "api-pool")
	state.Decisions = append(state.Decisions, session.Decision{At: time.Now().UTC(), Summary: "Only explicit all-keys exhaustion may trigger this automatic cross-engine fallback proof.", Source: "keydeck"})
	state.PendingTasks = []string{"Create the Codex proof artifact after API capacity is exhausted", "Resume the same Codex thread after restart", "Return to API when capacity recovers"}
	state.RelevantFiles = []string{"gateway.go"}
	orch := &session.Orchestrator{State: state}

	fmt.Println("Launching official Codex App Server...")
	client1, err := startCodex(ctx)
	if err != nil {
		fail(rep, err)
	}
	startDiagnosticPrinter(client1)
	account, err := ensureChatGPTAuth(ctx, client1, deviceLogin)
	if err != nil {
		_ = client1.Close()
		fail(rep, err)
	}
	rep.Account = accountSummary{Type: account.Account.Type, Plan: account.Account.PlanType}

	codex1 := &codexapp.Engine{Client: client1, OnEvent: printCodexEvent}
	fmt.Println("Phase 1: exhaust the fake API pool, then automatically hand the same task to real Codex.")
	passport, _, err := orch.RunWithFallback(
		ctx,
		api,
		codex1,
		"Create a file named codex-proof-06.txt containing exactly one line: "+phase1Marker+". Do not modify any other file.",
		"attempt task with selected API pool",
		"all API keys explicitly exhausted; automatically continue with real Codex",
		func(err error) bool { return errors.Is(err, pool.ErrAllKeysUnavailable) },
	)
	if err != nil {
		_ = client1.Close()
		fail(rep, fmt.Errorf("automatic API-pool -> real Codex handoff: %w", err))
	}
	rep.APIFallbackReason = passport.HandoffReason
	rep.InitialKeyCalls = map[string]int{
		"key-1": plan.Calls("secret-1"),
		"key-2": plan.Calls("secret-2"),
		"key-3": plan.Calls("secret-3"),
	}
	rep.InitialKeyStates = apiPool.Snapshot()
	rep.InitialAPIEvents = recorder.Snapshot()

	phase1, err := os.ReadFile(filepath.Join(project, "codex-proof-06.txt"))
	if err != nil {
		_ = client1.Close()
		fail(rep, fmt.Errorf("Codex did not create proof file after API fallback: %w", err))
	}
	rep.Phase1Content = string(phase1)
	if err := session.Save(statePath, orch.State); err != nil {
		_ = client1.Close()
		fail(rep, err)
	}
	firstBinding := orch.State.EngineBindings["codex"]
	_ = client1.Close()

	fmt.Println("Phase 1 complete. Simulating full KeyDeck/App Server restart...")
	reloaded, err := session.Load(statePath)
	if err != nil {
		fail(rep, err)
	}
	orch = &session.Orchestrator{State: reloaded}

	fmt.Println("Launching fresh official Codex App Server...")
	client2, err := startCodex(ctx)
	if err != nil {
		fail(rep, err)
	}
	defer client2.Close()
	startDiagnosticPrinter(client2)
	if _, err := ensureChatGPTAuth(ctx, client2, deviceLogin); err != nil {
		fail(rep, err)
	}

	fmt.Println("Phase 2: resume the same real Codex thread after restart.")
	codex2 := &codexapp.Engine{Client: client2, OnEvent: printCodexEvent}
	if _, _, err := orch.Run(
		ctx,
		codex2,
		"Read codex-proof-06.txt and append a second line exactly: "+phase2Marker+". Do not modify any other file.",
		"resume the same real Codex thread after KeyDeck restart",
	); err != nil {
		fail(rep, fmt.Errorf("real Codex resume phase: %w", err))
	}
	phase2, err := os.ReadFile(filepath.Join(project, "codex-proof-06.txt"))
	if err != nil {
		fail(rep, err)
	}
	rep.Phase2Content = string(phase2)

	fmt.Println("Phase 3: simulate API capacity recovery and switch the same canonical task back to API.")
	recoveredPlan := fakeprovider.NewPlan()
	recoveredPlan.ByKey["secret-recovered"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "API_RECOVERY_REVIEWED_CODEX_WORK"}}
	recoveredProvider := httptest.NewServer(fakeprovider.Handler(recoveredPlan))
	defer recoveredProvider.Close()
	recoveredGuard, err := newGuard()
	if err != nil {
		fail(rep, err)
	}
	recoveredPool := pool.New([]pool.Key{{ID: "key-recovered", Secret: "secret-recovered"}}, &providerhttp.Client{BaseURL: recoveredProvider.URL}, &events.Recorder{}, recoveredGuard)
	recoveredAPI := &apiengine.Engine{Pool: recoveredPool, EvidenceFiles: []string{"codex-proof-06.txt"}, ExtraRequestFields: map[string]any{"proof_stage": "api_capacity_recovered"}}
	_, finalResult, err := orch.Run(ctx, recoveredAPI, "Review the work completed by Codex after API capacity returned.", "API capacity recovered; switch back from Codex")
	if err != nil {
		fail(rep, fmt.Errorf("recovered API review: %w", err))
	}
	recoveredBody := string(recoveredPlan.LastBody("secret-recovered"))
	rep.RecoveredAPIRequestSawP1 = strings.Contains(recoveredBody, phase1Marker)
	rep.RecoveredAPIRequestSawP2 = strings.Contains(recoveredBody, phase2Marker)
	rep.CodexBinding = orch.State.EngineBindings["codex"]
	rep.FinalState = orch.State

	callsOK := rep.InitialKeyCalls["key-1"] == 1 && rep.InitialKeyCalls["key-2"] == 1 && rep.InitialKeyCalls["key-3"] == 1
	bindingOK := firstBinding.ExternalThreadID != "" && rep.CodexBinding.ExternalThreadID == firstBinding.ExternalThreadID
	contentOK := strings.Contains(rep.Phase1Content, phase1Marker) && strings.Contains(rep.Phase2Content, phase1Marker) && strings.Contains(rep.Phase2Content, phase2Marker)
	fallbackOK := rep.APIFallbackReason == "all API keys explicitly exhausted; automatically continue with real Codex"
	transcriptOK := countUserText(rep.FinalState.Transcript, "Create a file named codex-proof-06.txt") == 1
	apiReviewOK := finalResult.Text == "API_RECOVERY_REVIEWED_CODEX_WORK" && rep.RecoveredAPIRequestSawP1 && rep.RecoveredAPIRequestSawP2
	finalEngineOK := rep.FinalState.ActiveEngine == "api-pool"

	rep.Passed = callsOK && bindingOK && contentOK && fallbackOK && transcriptOK && apiReviewOK && finalEngineOK
	writeReport(project, rep)
	if !rep.Passed {
		fmt.Fprintln(os.Stderr, "Proof checks failed. Inspect report for exact evidence.")
		os.Exit(1)
	}

	fmt.Println("\nPASS: automatic API-pool exhaustion -> real Codex -> restart/resume -> recovered API proof succeeded.")
	fmt.Println("Report:", filepath.Join(project, ".keydeck-lab", "proof06-real-report.json"))
}

func newGuard() (*costguard.Guard, error) {
	return costguard.New(costguard.Config{LargeWriteMin: 100_000, LowReadMax: 5_000, ConsecutiveLimit: 2, ExtremeWriteMin: 180_000})
}

func startCodex(ctx context.Context) (*codexapp.Client, error) {
	transport, err := codexapp.NewProcessTransport(ctx, "codex", "app-server")
	if err != nil {
		return nil, err
	}
	client := codexapp.NewClient(transport, codexapp.ClientInfo{Name: "keydeck_lab", Title: "KeyDeck Feasibility Lab", Version: "0.3.0"})
	if err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func startDiagnosticPrinter(client *codexapp.Client) {
	go func() {
		for {
			select {
			case err := <-client.Diagnostics():
				if err != nil {
					fmt.Println("  [App Server diagnostic]", err)
				}
			case <-client.Done():
				return
			}
		}
	}()
}

func printCodexEvent(note codexapp.Notification) {
	switch note.Method {
	case "turn/started", "turn/completed":
		fmt.Println("  [Codex] Event:", note.Method)
	case "item/started", "item/completed":
		var payload struct {
			Item struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"item"`
		}
		if json.Unmarshal(note.Params, &payload) == nil {
			if payload.Item.Status != "" {
				fmt.Printf("  [Codex] Event: %s %s (%s)\n", note.Method, payload.Item.Type, payload.Item.Status)
			} else {
				fmt.Printf("  [Codex] Event: %s %s\n", note.Method, payload.Item.Type)
			}
		}
	}
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
	dir, err := os.MkdirTemp("", "keydeck-real-proof-06-")
	if err != nil {
		return "", func() {}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.go"), []byte("package proof\n\n// Disposable KeyDeck Proof 0.6 project.\n"), 0o600); err != nil {
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

func countUserText(transcript []session.Message, needle string) int {
	count := 0
	for _, message := range transcript {
		if message.Role == session.RoleUser && strings.Contains(message.Text, needle) {
			count++
		}
	}
	return count
}

func writeReport(project string, rep report) {
	path := filepath.Join(project, ".keydeck-lab", "proof06-real-report.json")
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
