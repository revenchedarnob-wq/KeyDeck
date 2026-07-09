package codexapp

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/session"
)

func TestClientInitializeThreadTurnResume(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()
	go runFakeServer(t, serverConn)

	c := NewClient(NewStreamTransport(clientConn), ClientInfo{Name: "keydeck_lab", Title: "KeyDeck Lab", Version: "0.2.0"})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	account, err := c.AccountRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if account.Account == nil || account.Account.PlanType != "plus" {
		t.Fatalf("unexpected account: %+v", account)
	}

	thread, err := c.StartThread(ctx, StartThreadOptions{Model: "gpt-test", CWD: `C:\project`, ApprovalPolicy: "never", Sandbox: ThreadSandboxWorkspaceWrite})
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != "thr_test" {
		t.Fatalf("thread=%q", thread.ID)
	}

	turn, err := c.StartTurn(ctx, thread.ID, "Continue", `C:\project`)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := c.CollectTurn(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("status=%q", outcome.Status)
	}
	if outcome.Text != "Codex continued safely." {
		t.Fatalf("text=%q", outcome.Text)
	}

	resumed, err := c.ResumeThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ID != thread.ID {
		t.Fatalf("resume id=%q", resumed.ID)
	}
}

func TestBuildHandoffPromptExcludesSecrets(t *testing.T) {
	p := BuildHandoffPrompt(sessionFixture(), "Fix it")
	if strings.Contains(p, "sk-secret") || strings.Contains(p, "api_key") {
		t.Fatalf("handoff leaked secret-like data: %s", p)
	}
	if !strings.Contains(p, "Do not repeat completed actions") {
		t.Fatalf("missing safety rule")
	}
}

func sessionFixture() session.Passport {
	return session.Passport{
		SessionID: "kd-1", ProjectRoot: `C:\project`, Goal: "Fix gateway",
		FromEngine: "api", ToEngine: "codex", HandoffReason: "api pool exhausted",
		PendingTasks: []string{"fix retry classifier"}, RelevantFiles: []string{"gateway.go"},
	}
}

func runFakeServer(t *testing.T, conn net.Conn) {
	t.Helper()
	scanner := bufio.NewScanner(conn)
	enc := json.NewEncoder(conn)
	initialized := false
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return
		}
		method, _ := msg["method"].(string)
		id := msg["id"]
		switch method {
		case "initialize":
			initialized = true
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"userAgent": "fake"}})
		case "initialized":
			if !initialized {
				t.Errorf("initialized before initialize")
			}
		case "account/read":
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"account": map[string]any{"type": "chatgpt", "planType": "plus"}, "requiresOpenaiAuth": true}})
		case "thread/start":
			params, _ := msg["params"].(map[string]any)
			if got, _ := params["sandbox"].(string); got != "workspace-write" {
				t.Errorf("thread/start sandbox=%q, want workspace-write", got)
			}
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thr_test", "sessionId": "thr_test"}}})
			_ = enc.Encode(map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": "thr_test"}}})
		case "turn/start":
			params, _ := msg["params"].(map[string]any)
			policy, _ := params["sandboxPolicy"].(map[string]any)
			if got, _ := policy["type"].(string); got != "workspaceWrite" {
				t.Errorf("turn/start sandboxPolicy.type=%q, want workspaceWrite", got)
			}
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "inProgress"}}})
			_ = enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thr_test", "turnId": "turn_test", "delta": "Codex continued "}})
			_ = enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thr_test", "turnId": "turn_test", "delta": "safely."}})
			_ = enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": "turn_test", "status": "completed"}}})
		case "thread/resume":
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": "thr_test", "sessionId": "thr_test"}}})
		}
	}
}

func TestBuildHandoffPromptCarriesCrossEngineContinuationWithoutSecrets(t *testing.T) {
	passport := session.Passport{
		SessionID:     "session-07",
		ProjectRoot:   t.TempDir(),
		Goal:          "Continue across engines",
		FromEngine:    "api-pool",
		ToEngine:      "codex",
		HandoffReason: "all keys exhausted mid-answer",
		Continuation: &session.ContinuationState{
			OriginalPrompt:   "finish the task",
			ConfirmedOutput:  "Confirmed visible sentence.",
			UnstableFragment: "unfinished fragment ",
			SourceEngine:     "api-pool",
			Reason:           "explicit all-keys exhaustion",
		},
		CompletedActions: []session.Action{{Summary: "Preserve completed file write", Source: "api-pool"}},
	}
	prompt := BuildHandoffPrompt(passport, "finish the task")
	for _, needle := range []string{"Confirmed visible sentence.", "unfinished fragment ", "do not repeat", "Preserve completed file write"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("handoff prompt missing %q: %s", needle, prompt)
		}
	}
	for _, forbidden := range []string{"api_key", "secret-key", "aero_liv"} {
		if strings.Contains(strings.ToLower(prompt), strings.ToLower(forbidden)) {
			t.Fatalf("handoff prompt leaked forbidden marker %q", forbidden)
		}
	}
}
