package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"keydeck.local/feasibilitylab/internal/continuity"
	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
	"keydeck.local/feasibilitylab/internal/session"
)

const (
	phase1Marker        = "KEYDECK_PROOF_07_CROSS_ENGINE_PHASE_1"
	phase2Marker        = "KEYDECK_PROOF_07_CROSS_ENGINE_PHASE_2"
	protectedMarker     = "API_COMPLETED_ACTION_MUST_NOT_BE_REPEATED_OR_MODIFIED"
	confirmedFirst      = "API inspection confirmed the protected file."
	confirmedSecond     = "The next step is to create the cross-engine artifact."
	confirmedThird      = "Additional verification requires a second engine."
	finalUnstable       = "Final recommendation "
	originalTaskSnippet = "Continue the interrupted task by creating codex-proof-07.txt"
)

type accountSummary struct {
	Type string `json:"type"`
	Plan string `json:"plan"`
}

type report struct {
	Proof                             string                    `json:"proof"`
	Passed                            bool                      `json:"passed"`
	Platform                          string                    `json:"platform"`
	Project                           string                    `json:"project"`
	Account                           accountSummary            `json:"account"`
	InitialKeyCalls                   map[string]int            `json:"initial_key_calls"`
	InitialAPIEvents                  []events.Event            `json:"initial_api_events"`
	IntraAPIContinuationRequest2      bool                      `json:"intra_api_continuation_request_2"`
	IntraAPIContinuationRequest3      bool                      `json:"intra_api_continuation_request_3"`
	StagedContinuation                session.ContinuationState `json:"staged_continuation"`
	InFlightSurvivedKeyDeckRestart    bool                      `json:"in_flight_survived_keydeck_restart"`
	FinalMergedVisibleResponse        string                    `json:"final_merged_visible_response"`
	ConfirmedOutputAppearsExactlyOnce bool                      `json:"confirmed_output_appears_exactly_once"`
	ProtectedFileHashBefore           string                    `json:"protected_file_hash_before"`
	ProtectedFileHashAfter            string                    `json:"protected_file_hash_after"`
	ProtectedCompletedActionPreserved bool                      `json:"protected_completed_action_preserved"`
	CodexBinding                      session.EngineBinding     `json:"codex_binding"`
	Phase1Content                     string                    `json:"phase_1_content"`
	Phase2Content                     string                    `json:"phase_2_content"`
	RecoveredAPIRequestSawConfirmed   bool                      `json:"recovered_api_request_saw_confirmed_output"`
	RecoveredAPIRequestSawPhase1      bool                      `json:"recovered_api_request_saw_phase_1"`
	RecoveredAPIRequestSawPhase2      bool                      `json:"recovered_api_request_saw_phase_2"`
	RecoveredAPIRequestSawProtected   bool                      `json:"recovered_api_request_saw_protected_marker"`
	FinalState                        session.State             `json:"final_state"`
	Error                             string                    `json:"error,omitempty"`
}

func main() {
	var projectFlag string
	var deviceLogin bool
	flag.StringVar(&projectFlag, "project", "", "Disposable project directory. Omit to create a temporary proof project.")
	flag.BoolVar(&deviceLogin, "device-login", false, "Use ChatGPT device-code login instead of browser callback login.")
	flag.Parse()

	rep := report{Proof: "0.7-real-cross-engine-mid-answer-continuation", Platform: runtime.GOOS + "/" + runtime.GOARCH}
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

	protectedPath := filepath.Join(project, "api-completed-action.txt")
	beforeHash, err := hashFile(protectedPath)
	if err != nil {
		fail(rep, err)
	}
	rep.ProtectedFileHashBefore = beforeHash

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()

	streamPlan := fakeprovider.NewStreamPlan()
	streamPlan.ByKey["secret-1"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{confirmedFirst + " ", "The next step is "},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	streamPlan.ByKey["secret-2"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{confirmedSecond + " ", "Additional verification requires "},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	streamPlan.ByKey["secret-3"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{confirmedThird + " ", finalUnstable},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	provider := httptest.NewServer(fakeprovider.Mux(fakeprovider.NewPlan(), streamPlan))
	defer provider.Close()

	recorder := &events.Recorder{}
	streaming := continuity.New([]pool.Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
		{ID: "key-3", Secret: "secret-3"},
	}, &providerhttp.StreamClient{BaseURL: provider.URL}, recorder)
	api := &apiengine.StreamingEngine{Continuity: streaming}

	state := session.New("session-proof-07-real", project, "Prove API mid-answer exhaustion can continue through real Codex without duplicating visible output or completed actions", "api-pool")
	state.CompletedActions = append(state.CompletedActions, session.Action{
		At:      time.Now().UTC(),
		Summary: "API already created api-completed-action.txt; preserve it byte-for-byte and do not repeat that action.",
		Source:  "api-pool",
	})
	state.Decisions = append(state.Decisions, session.Decision{
		At:      time.Now().UTC(),
		Summary: "Only explicit all-keys exhaustion after partial output may create safe cross-engine continuation state.",
		Source:  "keydeck",
	})
	state.PendingTasks = []string{"Complete the interrupted response with another engine", "Create the phase-1 proof artifact", "Resume the same Codex thread after restart", "Return to API after recovery"}
	state.RelevantFiles = []string{"api-completed-action.txt", "gateway.go"}
	orch := &session.Orchestrator{State: state}

	userPrompt := "Inspect this disposable project. Preserve api-completed-action.txt byte-for-byte. " + originalTaskSnippet + " containing exactly one line: " + phase1Marker + ". Do not modify any other file."

	fmt.Println("Phase 1: start one visible API answer, rotate safely across explicitly exhausted keys, then stage cross-engine continuation.")
	_, _, primaryErr := orch.BeginInterruptible(ctx, api, userPrompt, "start streamed API response")
	if !errors.Is(primaryErr, pool.ErrAllKeysUnavailable) {
		fail(rep, fmt.Errorf("expected explicit all-keys exhaustion, got: %w", primaryErr))
	}
	if orch.State.InFlightResponse == nil {
		fail(rep, errors.New("API exhaustion did not stage in-flight continuation state"))
	}
	rep.StagedContinuation = orch.State.InFlightResponse.Continuation
	rep.InitialAPIEvents = recorder.Snapshot()

	requests := streamPlan.SnapshotRequests()
	rep.InitialKeyCalls = countStreamCalls(requests)
	rep.IntraAPIContinuationRequest2 = continuationRequestContains(requests, 1, confirmedFirst, "The next step is ")
	rep.IntraAPIContinuationRequest3 = continuationRequestContains(requests, 2, confirmedSecond, "Additional verification requires ")

	if err := session.Save(statePath, orch.State); err != nil {
		fail(rep, err)
	}

	fmt.Println("Phase 1 complete. Simulating full KeyDeck restart while the answer is still in-flight...")
	reloaded, err := session.Load(statePath)
	if err != nil {
		fail(rep, err)
	}
	rep.InFlightSurvivedKeyDeckRestart = reloaded.InFlightResponse != nil &&
		reloaded.InFlightResponse.Continuation.ConfirmedOutput == rep.StagedContinuation.ConfirmedOutput &&
		reloaded.InFlightResponse.Continuation.UnstableFragment == rep.StagedContinuation.UnstableFragment
	orch = &session.Orchestrator{State: reloaded}

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

	fmt.Println("Phase 2: hand the persisted partial answer to real Codex and complete the same visible response.")
	codex1 := &codexapp.Engine{Client: client1, OnEvent: printCodexEvent}
	_, mergedResult, err := orch.ContinueInFlight(ctx, codex1, "all API keys exhausted mid-answer; continue the same visible response with real Codex")
	if err != nil {
		_ = client1.Close()
		fail(rep, fmt.Errorf("real Codex cross-engine continuation: %w", err))
	}
	rep.FinalMergedVisibleResponse = mergedResult.Text
	rep.ConfirmedOutputAppearsExactlyOnce = strings.Count(mergedResult.Text, confirmedFirst) == 1 &&
		strings.Count(mergedResult.Text, confirmedSecond) == 1 &&
		strings.Count(mergedResult.Text, confirmedThird) == 1

	phase1, err := os.ReadFile(filepath.Join(project, "codex-proof-07.txt"))
	if err != nil {
		_ = client1.Close()
		fail(rep, fmt.Errorf("Codex did not create phase-1 proof file: %w", err))
	}
	rep.Phase1Content = string(phase1)
	afterHash, err := hashFile(protectedPath)
	if err != nil {
		_ = client1.Close()
		fail(rep, err)
	}
	rep.ProtectedFileHashAfter = afterHash
	rep.ProtectedCompletedActionPreserved = beforeHash == afterHash

	if err := session.Save(statePath, orch.State); err != nil {
		_ = client1.Close()
		fail(rep, err)
	}
	firstBinding := orch.State.EngineBindings["codex"]
	_ = client1.Close()

	fmt.Println("Phase 2 complete. Simulating full KeyDeck/App Server restart...")
	reloaded, err = session.Load(statePath)
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

	fmt.Println("Phase 3: resume the same real Codex thread after restart.")
	codex2 := &codexapp.Engine{Client: client2, OnEvent: printCodexEvent}
	if _, _, err := orch.Run(
		ctx,
		codex2,
		"Read codex-proof-07.txt and append a second line exactly: "+phase2Marker+". Preserve api-completed-action.txt byte-for-byte. Do not modify any other file.",
		"resume the same real Codex thread after cross-engine continuation and restart",
	); err != nil {
		fail(rep, fmt.Errorf("real Codex resume phase: %w", err))
	}
	phase2, err := os.ReadFile(filepath.Join(project, "codex-proof-07.txt"))
	if err != nil {
		fail(rep, err)
	}
	rep.Phase2Content = string(phase2)
	finalProtectedHash, err := hashFile(protectedPath)
	if err != nil {
		fail(rep, err)
	}
	rep.ProtectedFileHashAfter = finalProtectedHash
	rep.ProtectedCompletedActionPreserved = beforeHash == finalProtectedHash

	fmt.Println("Phase 4: simulate API recovery and verify it receives the completed cross-engine state.")
	recoveredPlan := fakeprovider.NewPlan()
	recoveredPlan.ByKey["secret-recovered"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "API_RECOVERY_REVIEWED_CROSS_ENGINE_CONTINUITY"}}
	recoveredProvider := httptest.NewServer(fakeprovider.Handler(recoveredPlan))
	defer recoveredProvider.Close()
	recoveredGuard, err := newGuard()
	if err != nil {
		fail(rep, err)
	}
	recoveredPool := pool.New([]pool.Key{{ID: "key-recovered", Secret: "secret-recovered"}}, &providerhttp.Client{BaseURL: recoveredProvider.URL}, &events.Recorder{}, recoveredGuard)
	recoveredAPI := &apiengine.Engine{
		Pool:          recoveredPool,
		EvidenceFiles: []string{"codex-proof-07.txt", "api-completed-action.txt"},
		ExtraRequestFields: map[string]any{
			"proof_stage": "api_capacity_recovered_after_cross_engine_continuation",
		},
	}
	_, finalResult, err := orch.Run(ctx, recoveredAPI, "Review the completed cross-engine continuation after API capacity returned.", "API capacity recovered; switch back from Codex")
	if err != nil {
		fail(rep, fmt.Errorf("recovered API review: %w", err))
	}
	recoveredBody := string(recoveredPlan.LastBody("secret-recovered"))
	rep.RecoveredAPIRequestSawConfirmed = strings.Contains(recoveredBody, confirmedFirst) && strings.Contains(recoveredBody, confirmedThird)
	rep.RecoveredAPIRequestSawPhase1 = strings.Contains(recoveredBody, phase1Marker)
	rep.RecoveredAPIRequestSawPhase2 = strings.Contains(recoveredBody, phase2Marker)
	rep.RecoveredAPIRequestSawProtected = strings.Contains(recoveredBody, protectedMarker)
	rep.CodexBinding = orch.State.EngineBindings["codex"]
	rep.FinalState = orch.State

	callsOK := rep.InitialKeyCalls["key-1"] == 1 && rep.InitialKeyCalls["key-2"] == 1 && rep.InitialKeyCalls["key-3"] == 1
	continuationOK := rep.IntraAPIContinuationRequest2 && rep.IntraAPIContinuationRequest3 &&
		strings.Contains(rep.StagedContinuation.ConfirmedOutput, confirmedFirst) &&
		strings.Contains(rep.StagedContinuation.ConfirmedOutput, confirmedSecond) &&
		strings.Contains(rep.StagedContinuation.ConfirmedOutput, confirmedThird) &&
		rep.StagedContinuation.UnstableFragment == finalUnstable
	transcriptOK := countUserText(rep.FinalState.Transcript, originalTaskSnippet) == 1 &&
		countAssistantContaining(rep.FinalState.Transcript, confirmedFirst) == 1
	bindingOK := firstBinding.ExternalThreadID != "" && rep.CodexBinding.ExternalThreadID == firstBinding.ExternalThreadID
	contentOK := strings.TrimSpace(rep.Phase1Content) == phase1Marker && strings.Contains(rep.Phase2Content, phase1Marker) && strings.Contains(rep.Phase2Content, phase2Marker)
	apiReviewOK := finalResult.Text == "API_RECOVERY_REVIEWED_CROSS_ENGINE_CONTINUITY" && rep.RecoveredAPIRequestSawConfirmed && rep.RecoveredAPIRequestSawPhase1 && rep.RecoveredAPIRequestSawPhase2 && rep.RecoveredAPIRequestSawProtected
	finalStateOK := rep.FinalState.ActiveEngine == "api-pool" && rep.FinalState.InFlightResponse == nil

	rep.Passed = callsOK && continuationOK && rep.InFlightSurvivedKeyDeckRestart && rep.ConfirmedOutputAppearsExactlyOnce && rep.ProtectedCompletedActionPreserved && transcriptOK && bindingOK && contentOK && apiReviewOK && finalStateOK
	writeReport(project, rep)
	if !rep.Passed {
		fmt.Fprintln(os.Stderr, "Proof checks failed. Inspect report for exact evidence.")
		os.Exit(1)
	}

	fmt.Println("\nPASS: API mid-answer exhaustion -> persisted partial state -> real Codex continuation -> restart/resume -> recovered API succeeded.")
	fmt.Println("Report:", filepath.Join(project, ".keydeck-lab", "proof07-real-report.json"))
}

func newGuard() (*costguard.Guard, error) {
	return costguard.New(costguard.Config{LargeWriteMin: 100_000, LowReadMax: 5_000, ConsecutiveLimit: 2, ExtremeWriteMin: 180_000})
}

func startCodex(ctx context.Context) (*codexapp.Client, error) {
	transport, err := codexapp.NewProcessTransport(ctx, "codex", "app-server")
	if err != nil {
		return nil, err
	}
	client := codexapp.NewClient(transport, codexapp.ClientInfo{Name: "keydeck_lab", Title: "KeyDeck Feasibility Lab", Version: "0.4.0"})
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
	dir, err := os.MkdirTemp("", "keydeck-real-proof-07-")
	if err != nil {
		return "", func() {}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.go"), []byte("package proof\n\n// Disposable KeyDeck Proof 0.7 project.\n"), 0o600); err != nil {
		os.RemoveAll(dir)
		return "", func() {}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "api-completed-action.txt"), []byte(protectedMarker+"\n"), 0o600); err != nil {
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

func countStreamCalls(requests []fakeprovider.StreamRequestRecord) map[string]int {
	out := map[string]int{"key-1": 0, "key-2": 0, "key-3": 0}
	for _, req := range requests {
		switch req.Key {
		case "secret-1":
			out["key-1"]++
		case "secret-2":
			out["key-2"]++
		case "secret-3":
			out["key-3"]++
		}
	}
	return out
}

func continuationRequestContains(requests []fakeprovider.StreamRequestRecord, index int, confirmedNeedle, unstable string) bool {
	if index < 0 || index >= len(requests) {
		return false
	}
	var req continuity.Request
	if json.Unmarshal(requests[index].Body, &req) != nil || req.Continuation == nil {
		return false
	}
	return strings.Contains(req.Continuation.ConfirmedOutput, confirmedNeedle) && req.Continuation.UnstableFragment == unstable
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
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

func countAssistantContaining(transcript []session.Message, needle string) int {
	count := 0
	for _, message := range transcript {
		if message.Role == session.RoleAssistant && strings.Contains(message.Text, needle) {
			count++
		}
	}
	return count
}

func writeReport(project string, rep report) {
	path := filepath.Join(project, ".keydeck-lab", "proof07-real-report.json")
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
