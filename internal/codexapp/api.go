package codexapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Account struct {
	Type     string `json:"type"`
	Email    string `json:"email,omitempty"`
	PlanType string `json:"planType,omitempty"`
}

type AccountReadResult struct {
	Account            *Account `json:"account"`
	RequiresOpenAIAuth bool     `json:"requiresOpenaiAuth"`
}

type LoginStartResult struct {
	Type            string `json:"type"`
	LoginID         string `json:"loginId"`
	AuthURL         string `json:"authUrl,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	UserCode        string `json:"userCode,omitempty"`
}

type Thread struct {
	ID           string `json:"id"`
	SessionID    string `json:"sessionId,omitempty"`
	ForkedFromID string `json:"forkedFromId,omitempty"`
	Name         string `json:"name,omitempty"`
}

type threadResult struct {
	Thread Thread `json:"thread"`
}

type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type turnResult struct {
	Turn Turn `json:"turn"`
}

const (
	// Legacy thread/start sandbox shorthand uses kebab-case.
	ThreadSandboxWorkspaceWrite = "workspace-write"
	// Structured turn/start sandboxPolicy variants use camelCase.
	TurnSandboxWorkspaceWrite = "workspaceWrite"
)

type StartThreadOptions struct {
	Model          string
	CWD            string
	ApprovalPolicy string
	Sandbox        string
	ServiceName    string
}

func (c *Client) AccountRead(ctx context.Context) (AccountReadResult, error) {
	var out AccountReadResult
	err := c.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &out)
	return out, err
}

func (c *Client) StartChatGPTLogin(ctx context.Context, deviceCode bool) (LoginStartResult, error) {
	loginType := "chatgpt"
	if deviceCode {
		loginType = "chatgptDeviceCode"
	}
	var out LoginStartResult
	err := c.Call(ctx, "account/login/start", map[string]any{"type": loginType}, &out)
	return out, err
}

func (c *Client) StartThread(ctx context.Context, opt StartThreadOptions) (Thread, error) {
	params := map[string]any{}
	if opt.Model != "" {
		params["model"] = opt.Model
	}
	if opt.CWD != "" {
		params["cwd"] = opt.CWD
	}
	if opt.ApprovalPolicy != "" {
		params["approvalPolicy"] = opt.ApprovalPolicy
	}
	if opt.Sandbox != "" {
		params["sandbox"] = opt.Sandbox
	}
	if opt.ServiceName != "" {
		params["serviceName"] = opt.ServiceName
	}
	var out threadResult
	if err := c.Call(ctx, "thread/start", params, &out); err != nil {
		return Thread{}, err
	}
	if out.Thread.ID == "" {
		return Thread{}, errors.New("thread/start returned empty thread id")
	}
	return out.Thread, nil
}

func (c *Client) ResumeThread(ctx context.Context, threadID string) (Thread, error) {
	if strings.TrimSpace(threadID) == "" {
		return Thread{}, errors.New("thread id is required")
	}
	var out threadResult
	if err := c.Call(ctx, "thread/resume", map[string]any{"threadId": threadID}, &out); err != nil {
		return Thread{}, err
	}
	if out.Thread.ID == "" {
		return Thread{}, errors.New("thread/resume returned empty thread id")
	}
	return out.Thread, nil
}

func (c *Client) StartTurn(ctx context.Context, threadID, text, cwd string) (Turn, error) {
	if threadID == "" {
		return Turn{}, errors.New("thread id is required")
	}
	if strings.TrimSpace(text) == "" {
		return Turn{}, errors.New("turn text is required")
	}
	params := map[string]any{
		"threadId":       threadID,
		"input":          []any{map[string]any{"type": "text", "text": text}},
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type":          TurnSandboxWorkspaceWrite,
			"writableRoots": []string{cwd},
			"networkAccess": false,
		},
	}
	if cwd != "" {
		params["cwd"] = cwd
	}
	var out turnResult
	if err := c.Call(ctx, "turn/start", params, &out); err != nil {
		return Turn{}, err
	}
	if out.Turn.ID == "" {
		return Turn{}, errors.New("turn/start returned empty turn id")
	}
	return out.Turn, nil
}

type TurnOutcome struct {
	Text    string
	Status  string
	Events  []Notification
	Metrics TurnMetrics
}

func (c *Client) CollectTurn(ctx context.Context, turnID string) (TurnOutcome, error) {
	return c.CollectTurnObserved(ctx, turnID, nil)
}

func (c *Client) CollectTurnObserved(ctx context.Context, turnID string, observer func(Notification)) (TurnOutcome, error) {
	var out TurnOutcome
	for {
		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-c.done:
			return out, errors.New("codex app-server closed during turn")
		case note := <-c.notes:
			out.Events = append(out.Events, note)
			if observer != nil {
				observer(note)
			}
			switch note.Method {
			case "item/agentMessage/delta":
				var p struct {
					Delta string `json:"delta"`
				}
				if json.Unmarshal(note.Params, &p) == nil {
					out.Text += p.Delta
				}
			case "turn/completed":
				var p struct {
					Turn Turn `json:"turn"`
				}
				if err := json.Unmarshal(note.Params, &p); err != nil {
					return out, fmt.Errorf("decode turn/completed: %w", err)
				}
				if p.Turn.ID != "" && p.Turn.ID != turnID {
					continue
				}
				out.Status = p.Turn.Status
				out.Metrics = metricsFromEvents(out.Events)
				if p.Turn.Status != "" && p.Turn.Status != "completed" {
					return out, fmt.Errorf("Codex turn completed with status %s", p.Turn.Status)
				}
				return out, nil
			}
		}
	}
}

type WindowsSandboxSetupResult struct {
	Started bool `json:"started"`
}

func (c *Client) StartWindowsSandboxSetup(ctx context.Context, mode string) (WindowsSandboxSetupResult, error) {
	if mode != "elevated" && mode != "unelevated" {
		return WindowsSandboxSetupResult{}, fmt.Errorf("unsupported Windows sandbox setup mode %q", mode)
	}
	var out WindowsSandboxSetupResult
	err := c.Call(ctx, "windowsSandbox/setupStart", map[string]any{"mode": mode}, &out)
	return out, err
}
