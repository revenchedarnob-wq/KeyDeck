package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

const (
	taskID    = "proof20-task"
	sessionID = "proof20-session"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                   string     `json:"proof"`
	Status                  string     `json:"status"`
	Passed                  bool       `json:"passed"`
	Scenarios               []scenario `json:"scenarios"`
	PermissionProfiles      []string   `json:"permission_profiles"`
	FrameLimitBytes         int        `json:"frame_limit_bytes"`
	CancellationDisposition string     `json:"cancellation_disposition"`
	AdapterSeam             string     `json:"adapter_seam"`
	LocalServerProcess      string     `json:"local_server_process"`
	FinalServerStateSHA256  string     `json:"final_server_state_sha256"`
	NextGate                string     `json:"next_gate"`
}

type serverState struct {
	RPCCounts   map[string]int    `json:"rpc_counts"`
	ToolCalls   map[string]int    `json:"tool_calls"`
	KV          map[string]string `json:"kv"`
	DeleteCount int               `json:"delete_count"`
	SlowEffects []string          `json:"slow_effects"`
}

type countingAdapter struct {
	Inner mcpbridge.Adapter
	Calls int
}

func (a *countingAdapter) Invoke(ctx context.Context, tool string, args map[string]any) (mcpbridge.CallToolResult, error) {
	a.Calls++
	return a.Inner.Invoke(ctx, tool, args)
}

type stubAdapter struct{ Calls int }

func (a *stubAdapter) Invoke(_ context.Context, tool string, args map[string]any) (mcpbridge.CallToolResult, error) {
	a.Calls++
	return mcpbridge.CallToolResult{Content: []mcpbridge.Content{{Type: "text", Text: "stub:" + tool + ":" + fmt.Sprint(args["value"])}}}, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := report{
		Proof:                   "0.20-mcp-bridge-hardening",
		Status:                  "failed",
		PermissionProfiles:      []string{string(mcpbridge.ProfileReadOnly), string(mcpbridge.ProfileSafeEdit), string(mcpbridge.ProfileFullControl)},
		FrameLimitBytes:         512,
		CancellationDisposition: "non-repeatable cancellation remains ambiguous and blocked",
		AdapterSeam:             "mcpbridge.Adapter",
		LocalServerProcess:      "independent proof20 MCP stdio server process",
	}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	root := filepath.Join(os.TempDir(), "keydeck-proof20")
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	statePath := filepath.Join(root, "server-state.json")
	journalPath := filepath.Join(root, "tool-journal.jsonl")
	timelinePath := filepath.Join(root, "timeline.jsonl")
	taskPath := filepath.Join(root, "task-events.jsonl")
	receiptPath := filepath.Join(root, "receipts.jsonl")

	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		return err
	}
	timelineStore, err := timeline.Open(timelinePath)
	if err != nil {
		return err
	}
	taskStore, err := tasks.Open(taskPath)
	if err != nil {
		return err
	}
	manager := &tasks.Manager{Store: taskStore, Journal: journal}
	contract := tasks.Contract{
		Goal:           "Harden the MCP bridge with permission profiles, bounded frames, cancellation safety, an adapter seam, and a separate local MCP server process.",
		ForbiddenScope: []string{"no external network", "no secret", "no broad SaaS integration", "no unsafe replay"},
		Checks: []tasks.AcceptanceCheck{
			{ID: "permissions", Description: "Read Only, Safe Edit and Full Control profiles enforce tool access before adapter invocation"},
			{ID: "frame_bound", Description: "Oversized MCP frame is rejected deterministically"},
			{ID: "cancellation", Description: "Cancelled non-repeatable effect remains ambiguous and blocked after restart"},
			{ID: "adapter_seam", Description: "Bridge executes through an explicit transport adapter interface"},
			{ID: "local_server", Description: "Bridge communicates with a separate local MCP server process"},
			{ID: "timeline", Description: "Hardening events remain visible in durable timeline evidence"},
			{ID: "receipt", Description: "Proof Receipt binds the hardening evidence and final server artifact"},
		},
	}
	if _, err := manager.Create(taskID, sessionID, contract); err != nil {
		return err
	}

	cfg, err := serverConfig(statePath, false)
	if err != nil {
		return err
	}
	baseAdapter := mcpbridge.NewCommandAdapter(mcpbridge.NewClient(cfg))
	counted := &countingAdapter{Inner: baseAdapter}
	toolProfiles := map[string]mcpbridge.PermissionProfile{
		"readonly.echo": mcpbridge.ProfileReadOnly,
		"safe.write":    mcpbridge.ProfileSafeEdit,
		"admin.delete":  mcpbridge.ProfileFullControl,
		"slow.commit":   mcpbridge.ProfileFullControl,
	}

	readBridge := newBridge(journal, timelineStore, counted, mcpbridge.ProfileReadOnly, toolProfiles)
	readResult, readErr := readBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "perm-read", Tool: "readonly.echo", Arguments: map[string]any{"value": "alpha"}, Policy: tooljournal.ReplayForbidden})
	callsAfterRead := counted.Calls
	_, deniedSafeErr := readBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "perm-read-denied", Tool: "safe.write", Arguments: map[string]any{"key": "x", "value": "y"}, Policy: tooljournal.ReplayIdempotent})
	readBlockNoCall := counted.Calls == callsAfterRead

	safeBridge := newBridge(journal, timelineStore, counted, mcpbridge.ProfileSafeEdit, toolProfiles)
	safeResult, safeErr := safeBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "perm-safe", Tool: "safe.write", Arguments: map[string]any{"key": "alpha", "value": "one"}, Policy: tooljournal.ReplayIdempotent})
	callsAfterSafe := counted.Calls
	_, deniedAdminErr := safeBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "perm-safe-denied", Tool: "admin.delete", Arguments: map[string]any{"target": "x"}, Policy: tooljournal.ReplayForbidden})
	safeBlockNoCall := counted.Calls == callsAfterSafe

	fullBridge := newBridge(journal, timelineStore, counted, mcpbridge.ProfileFullControl, toolProfiles)
	adminResult, adminErr := fullBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "perm-full", Tool: "admin.delete", Arguments: map[string]any{"target": "x"}, Policy: tooljournal.ReplayForbidden})
	permissionPass := readErr == nil && readResult.Text == "echo:alpha" && errors.Is(deniedSafeErr, mcpbridge.ErrToolNotAllowed) && readBlockNoCall && safeErr == nil && safeResult.Text == "write:ok" && errors.Is(deniedAdminErr, mcpbridge.ErrToolNotAllowed) && safeBlockNoCall && adminErr == nil && adminResult.Text == "delete:ok"
	add("permission_profiles_block_before_adapter_invocation", permissionPass, map[string]any{"adapter_calls": counted.Calls, "read_block_no_call": readBlockNoCall, "safe_block_no_call": safeBlockNoCall, "errors": []string{errString(deniedSafeErr), errString(deniedAdminErr)}})
	mark(manager, "permissions", permissionPass, "three profiles enforced before adapter invocation")

	overCfg, err := serverConfig(filepath.Join(root, "oversize-state.json"), true)
	if err != nil {
		return err
	}
	overCfg.MaxFrameBytes = out.FrameLimitBytes
	_, oversizedErr := mcpbridge.NewCommandAdapter(mcpbridge.NewClient(overCfg)).Invoke(context.Background(), "readonly.echo", map[string]any{"value": "x"})
	framePass := errors.Is(oversizedErr, mcpbridge.ErrFrameTooLarge)
	add("oversized_mcp_frame_rejected_at_configured_bound", framePass, errString(oversizedErr))
	mark(manager, "frame_bound", framePass, "oversized initialize response rejected with ErrFrameTooLarge")

	cancelOp := mcpbridge.Operation{OperationID: "cancel-slow", Tool: "slow.commit", Arguments: map[string]any{"value": "committed-before-cancel"}, Policy: tooljournal.ReplayForbidden}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	_, cancelErr := fullBridge.Execute(ctx, cancelOp)
	cancel()
	journal, _ = tooljournal.Open(journalPath)
	timelineStore, _ = timeline.Open(timelinePath)
	fullBridge = newBridge(journal, timelineStore, baseAdapter, mcpbridge.ProfileFullControl, toolProfiles)
	_, restartErr := fullBridge.Execute(context.Background(), cancelOp)
	stateAfterCancel, _ := loadState(statePath)
	record := journal.Snapshot()[cancelOp.OperationID]
	cancelPass := errors.Is(cancelErr, context.DeadlineExceeded) && errors.Is(restartErr, tooljournal.ErrAmbiguousOperation) && record.State == tooljournal.StateStarted && len(stateAfterCancel.SlowEffects) == 1 && stateAfterCancel.ToolCalls["slow.commit"] == 1
	add("cancelled_nonrepeatable_tool_remains_ambiguous_and_blocked", cancelPass, map[string]any{"cancel_error": errString(cancelErr), "restart_error": errString(restartErr), "journal_state": record.State, "effects": len(stateAfterCancel.SlowEffects), "tool_calls": stateAfterCancel.ToolCalls["slow.commit"]})
	mark(manager, "cancellation", cancelPass, "effect committed once; cancellation left operation started/ambiguous; restart blocked")

	stubJournal, _ := tooljournal.Open(filepath.Join(root, "stub-journal.jsonl"))
	stubTimeline, _ := timeline.Open(filepath.Join(root, "stub-timeline.jsonl"))
	stub := &stubAdapter{}
	stubBridge := newBridge(stubJournal, stubTimeline, stub, mcpbridge.ProfileReadOnly, map[string]mcpbridge.PermissionProfile{"readonly.echo": mcpbridge.ProfileReadOnly})
	stubResult, stubErr := stubBridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "adapter-seam", Tool: "readonly.echo", Arguments: map[string]any{"value": "seam"}, Policy: tooljournal.ReplayForbidden})
	adapterPass := stubErr == nil && stub.Calls == 1 && stubResult.Text == "stub:readonly.echo:seam"
	add("explicit_adapter_seam_executes_without_command_client", adapterPass, map[string]any{"calls": stub.Calls, "result": stubResult.Text, "error": errString(stubErr)})
	mark(manager, "adapter_seam", adapterPass, "Bridge used mcpbridge.Adapter without Client")

	stateNow, _ := loadState(statePath)
	localServerPass := stateNow.RPCCounts["initialize"] >= 3 && stateNow.RPCCounts["tools/list"] >= 3 && stateNow.RPCCounts["tools/call"] >= 4 && stateNow.ToolCalls["readonly.echo"] == 1 && stateNow.ToolCalls["safe.write"] == 1 && stateNow.ToolCalls["admin.delete"] == 1
	add("separate_local_mcp_server_process_handled_real_wire_calls", localServerPass, map[string]any{"rpc_counts": stateNow.RPCCounts, "tool_calls": stateNow.ToolCalls})
	mark(manager, "local_server", localServerPass, "separate MCP server process handled initialize/list/call")

	timelineStore, _ = timeline.Open(timelinePath)
	events := timelineStore.ByTask(taskID)
	kinds := map[string]int{}
	for _, event := range events {
		kinds[event.Kind]++
	}
	timelinePass := kinds["mcp_tool_started"] >= 4 && kinds["mcp_tool_completed"] >= 3 && kinds["mcp_tool_cancelled_ambiguous"] == 1
	add("hardening_events_entered_durable_timeline", timelinePass, kinds)
	mark(manager, "timeline", timelinePass, fmt.Sprintf("%d durable hardening timeline events", len(events)))

	stateRaw, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	stateHash := sha256Hex(stateRaw)
	out.FinalServerStateSHA256 = stateHash
	mark(manager, "receipt", len(events) > 0 && stateHash != "", "timeline and server-state artifact ready")
	finalTask := taskStore.State()
	receipt, receiptErr := proofreceipt.Build(finalTask, events, []proofreceipt.Artifact{{Name: "proof20-server-state", Path: "proof20-server-state.json", SHA256: stateHash, Size: int64(len(stateRaw))}})
	receipts, err := proofreceipt.Open(receiptPath)
	if err != nil {
		return err
	}
	_, created, saveErr := receipts.SaveOnce(receipt)
	_, createdAgain, saveAgainErr := receipts.SaveOnce(receipt)
	receiptPass := receiptErr == nil && saveErr == nil && saveAgainErr == nil && created && !createdAgain && len(receipt.Artifacts) == 1 && receipt.Artifacts[0].SHA256 == stateHash && finalTask.Progress().Complete
	add("proof_receipt_binds_hardening_evidence_exactly_once", receiptPass, map[string]any{"receipt_id": receipt.ReceiptID, "created_once": created && !createdAgain, "artifact_sha256": stateHash, "error": errString(errors.Join(receiptErr, saveErr, saveAgainErr))})

	out.Passed = true
	for _, s := range out.Scenarios {
		out.Passed = out.Passed && s.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	out.NextGate = "Connect the hardened bridge to one external third-party local MCP server when an immutable package is available, then add Secret Broker scoping and schema-aware tool permissions before broad SaaS integrations."
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !out.Passed {
		return errors.New("Proof 0.20 failed")
	}
	return nil
}

func newBridge(j *tooljournal.Journal, t *timeline.Store, a mcpbridge.Adapter, profile mcpbridge.PermissionProfile, tools map[string]mcpbridge.PermissionProfile) *mcpbridge.Bridge {
	return &mcpbridge.Bridge{Journal: j, Timeline: t, TaskID: taskID, SessionID: sessionID, Adapter: a, Permissions: &mcpbridge.PermissionPolicy{Profile: profile, ToolProfiles: tools}}
}

func serverConfig(statePath string, oversize bool) (mcpbridge.CommandConfig, error) {
	if explicit := os.Getenv("KEYDECK_PROOF20_SERVER"); explicit != "" {
		args := []string{"--state", statePath}
		if oversize {
			args = append(args, "--oversize-init")
		}
		return mcpbridge.CommandConfig{Path: explicit, Args: args, MaxFrameBytes: 1 << 20}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return mcpbridge.CommandConfig{}, err
	}
	name := "KeyDeck-Proof-0.20-MCP-Server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	sibling := filepath.Join(filepath.Dir(exe), name)
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		args := []string{"--state", statePath}
		if oversize {
			args = append(args, "--oversize-init")
		}
		return mcpbridge.CommandConfig{Path: sibling, Args: args, MaxFrameBytes: 1 << 20}, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return mcpbridge.CommandConfig{}, err
	}
	localName := "proof20-local-mcp-server"
	if runtime.GOOS == "windows" {
		localName += ".exe"
	}
	localServer := filepath.Join(filepath.Dir(statePath), localName)
	if _, statErr := os.Stat(localServer); os.IsNotExist(statErr) {
		build := exec.Command("go", "build", "-trimpath", "-o", localServer, "./cmd/proof20server")
		build.Dir = cwd
		if output, buildErr := build.CombinedOutput(); buildErr != nil {
			return mcpbridge.CommandConfig{}, fmt.Errorf("build proof20 local MCP server: %w: %s", buildErr, output)
		}
	}
	args := []string{"--state", statePath}
	if oversize {
		args = append(args, "--oversize-init")
	}
	return mcpbridge.CommandConfig{Path: localServer, Args: args, MaxFrameBytes: 1 << 20}, nil
}

func loadState(path string) (serverState, error) {
	var s serverState
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(raw, &s)
	return s, err
}

func mark(m *tasks.Manager, id string, passed bool, evidence string) {
	status := tasks.CheckFailed
	if passed {
		status = tasks.CheckPassed
	}
	_, _ = m.UpdateCheck(id, status, evidence)
}
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func sha256Hex(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
