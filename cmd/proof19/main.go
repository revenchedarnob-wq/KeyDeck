package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

const (
	taskID    = "proof19-task"
	sessionID = "proof19-session"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof                  string     `json:"proof"`
	Status                 string     `json:"status"`
	Passed                 bool       `json:"passed"`
	Scenarios              []scenario `json:"scenarios"`
	ProtocolVersion        string     `json:"protocol_version"`
	Transport              string     `json:"transport"`
	ToolJournalOperations  int        `json:"tool_journal_operations"`
	TimelineEvents         int        `json:"timeline_events"`
	ReceiptID              string     `json:"receipt_id"`
	ReceiptTimelineRefs    int        `json:"receipt_timeline_refs"`
	FinalServerStateSHA256 string     `json:"final_server_state_sha256"`
	NextGate               string     `json:"next_gate"`
}

type serverState struct {
	Counter             int               `json:"counter"`
	Appends             []string          `json:"appends"`
	KV                  map[string]string `json:"kv"`
	IdempotentCrashDone map[string]bool   `json:"idempotent_crash_done"`
	ToolCalls           map[string]int    `json:"tool_calls"`
	RPCCounts           map[string]int    `json:"rpc_counts"`
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--mcp-server" {
		if err := runMCPServer(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := runProof(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runProof() error {
	out := report{Proof: "0.19-real-mcp-tool-execution-durable-tool-journal-bridge", Status: "failed", ProtocolVersion: mcpbridge.ProtocolVersion, Transport: "stdio-newline-delimited-json-rpc"}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	root := filepath.Join(os.TempDir(), "keydeck-proof19")
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	serverStatePath := filepath.Join(root, "mcp-server-state.json")
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
		Goal:           "Prove real MCP stdio tool execution is governed by KeyDeck Tool Journal replay safety and produces durable timeline/receipt evidence.",
		ForbiddenScope: []string{"no paid service", "no external network", "no secret", "no blind replay"},
		Checks: []tasks.AcceptanceCheck{
			{ID: "mcp_wire", Description: "Real MCP initialize, tool discovery and tool call succeed over stdio"},
			{ID: "permission", Description: "Disallowed MCP tool is blocked before server execution"},
			{ID: "completed_once", Description: "Completed non-repeatable MCP operation is returned from the journal after restart without replay"},
			{ID: "ambiguous_blocked", Description: "Ambiguous non-repeatable MCP operation remains blocked after restart"},
			{ID: "idempotent_retry", Description: "Declared idempotent MCP operation may retry safely after interrupted transport"},
			{ID: "timeline", Description: "MCP lifecycle events enter the Universal Activity Timeline"},
			{ID: "receipt", Description: "Proof Receipt references MCP tool evidence and artifact hash"},
		},
	}
	if _, err := manager.Create(taskID, sessionID, contract); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	client := mcpbridge.NewClient(mcpbridge.CommandConfig{Path: exe, Args: []string{"--mcp-server", serverStatePath}, MaxFrameBytes: 1 << 20})
	bridge := newBridge(journal, timelineStore, client)

	// Permission gate must block before any MCP process is invoked.
	_, deniedErr := bridge.Execute(context.Background(), mcpbridge.Operation{OperationID: "op-denied", Tool: "admin.delete", Arguments: map[string]any{"target": "x"}, Policy: tooljournal.ReplayForbidden})
	stateBefore, _ := loadServerState(serverStatePath)
	permissionPass := errors.Is(deniedErr, mcpbridge.ErrToolNotAllowed) && totalRPCs(stateBefore) == 0
	add("disallowed_tool_blocked_before_server_execution", permissionPass, errorString(deniedErr))
	markCheck(manager, "permission", permissionPass, "permission gate blocked before journal/server execution")

	// Completed non-repeatable operation.
	counterOp := mcpbridge.Operation{OperationID: "op-counter-1", Tool: "counter.increment", Arguments: map[string]any{"amount": 1}, Policy: tooljournal.ReplayForbidden}
	counterResult, counterErr := bridge.Execute(context.Background(), counterOp)
	stateAfterCounter, _ := loadServerState(serverStatePath)
	wirePass := counterErr == nil && counterResult.Text == "counter=1" && stateAfterCounter.RPCCounts["initialize"] >= 1 && stateAfterCounter.RPCCounts["tools/list"] >= 1 && stateAfterCounter.RPCCounts["tools/call"] >= 1
	add("real_mcp_initialize_list_and_call_succeeded_over_stdio", wirePass, map[string]any{"result": counterResult.Text, "rpc_counts": stateAfterCounter.RPCCounts, "error": errorString(counterErr)})
	markCheck(manager, "mcp_wire", wirePass, "initialize/tools-list/tools-call observed over stdio")

	// Simulate KeyDeck restart: reopen journal/timeline and repeat completed operation.
	journal, err = tooljournal.Open(journalPath)
	if err != nil {
		return err
	}
	timelineStore, err = timeline.Open(timelinePath)
	if err != nil {
		return err
	}
	bridge = newBridge(journal, timelineStore, client)
	reused, reuseErr := bridge.Execute(context.Background(), counterOp)
	stateAfterReuse, _ := loadServerState(serverStatePath)
	completedOncePass := reuseErr == nil && reused.Reused && reused.Text == "counter=1" && stateAfterReuse.Counter == 1 && stateAfterReuse.ToolCalls["counter.increment"] == 1
	add("completed_nonrepeatable_mcp_operation_reused_after_restart_without_replay", completedOncePass, map[string]any{"reused": reused.Reused, "counter": stateAfterReuse.Counter, "tool_calls": stateAfterReuse.ToolCalls["counter.increment"], "error": errorString(reuseErr)})
	markCheck(manager, "completed_once", completedOncePass, "counter side effect remained exactly once after restart")

	// Ambiguous non-repeatable operation: server commits effect then exits before response.
	appendOp := mcpbridge.Operation{OperationID: "op-append-1", Tool: "ambiguous.append", Arguments: map[string]any{"value": "alpha"}, Policy: tooljournal.ReplayForbidden}
	_, appendErr := bridge.Execute(context.Background(), appendOp)
	stateAfterAppend, _ := loadServerState(serverStatePath)
	journal, _ = tooljournal.Open(journalPath)
	timelineStore, _ = timeline.Open(timelinePath)
	bridge = newBridge(journal, timelineStore, client)
	_, blockedErr := bridge.Execute(context.Background(), appendOp)
	stateAfterBlocked, _ := loadServerState(serverStatePath)
	ambiguousPass := appendErr != nil && errors.Is(blockedErr, tooljournal.ErrAmbiguousOperation) && len(stateAfterAppend.Appends) == 1 && len(stateAfterBlocked.Appends) == 1 && stateAfterBlocked.ToolCalls["ambiguous.append"] == 1
	add("ambiguous_nonrepeatable_mcp_operation_remained_blocked_after_restart", ambiguousPass, map[string]any{"first_error": errorString(appendErr), "restart_error": errorString(blockedErr), "append_count": len(stateAfterBlocked.Appends), "tool_calls": stateAfterBlocked.ToolCalls["ambiguous.append"]})
	markCheck(manager, "ambiguous_blocked", ambiguousPass, "effect occurred once; restart blocked replay")

	// Idempotent operation: first process commits value then exits before response; retry succeeds.
	putOp := mcpbridge.Operation{OperationID: "op-put-1", Tool: "idempotent.put", Arguments: map[string]any{"key": "alpha", "value": "one"}, Policy: tooljournal.ReplayIdempotent}
	_, firstPutErr := bridge.Execute(context.Background(), putOp)
	journal, _ = tooljournal.Open(journalPath)
	timelineStore, _ = timeline.Open(timelinePath)
	bridge = newBridge(journal, timelineStore, client)
	putResult, secondPutErr := bridge.Execute(context.Background(), putOp)
	stateAfterPut, _ := loadServerState(serverStatePath)
	idempotentPass := firstPutErr != nil && secondPutErr == nil && putResult.Text == "put alpha=one" && stateAfterPut.KV["alpha"] == "one" && stateAfterPut.ToolCalls["idempotent.put"] == 2
	add("idempotent_mcp_operation_retried_safely_after_restart", idempotentPass, map[string]any{"first_error": errorString(firstPutErr), "second_error": errorString(secondPutErr), "result": putResult.Text, "tool_calls": stateAfterPut.ToolCalls["idempotent.put"]})
	markCheck(manager, "idempotent_retry", idempotentPass, "same idempotent operation retried and converged to one value")

	// Timeline evidence.
	timelineStore, _ = timeline.Open(timelinePath)
	events := timelineStore.ByTask(taskID)
	kinds := map[string]int{}
	for _, e := range events {
		kinds[e.Kind]++
	}
	timelinePass := kinds["mcp_tool_started"] >= 3 && kinds["mcp_tool_completed"] >= 2 && kinds["mcp_tool_result_reused"] == 1 && kinds["mcp_tool_ambiguous"] == 1 && kinds["mcp_tool_retryable_failure"] == 1
	add("mcp_lifecycle_events_entered_universal_activity_timeline", timelinePass, kinds)
	markCheck(manager, "timeline", timelinePass, fmt.Sprintf("%d durable MCP timeline events", len(events)))

	// Proof receipt with tool timeline refs and durable server-state artifact hash.
	stateRaw, err := os.ReadFile(serverStatePath)
	if err != nil {
		return err
	}
	stateHash := sha256Hex(stateRaw)
	out.FinalServerStateSHA256 = stateHash
	artifact := proofreceipt.Artifact{Name: "proof19-mcp-server-state", Path: "proof19-mcp-server-state.json", SHA256: stateHash, Size: int64(len(stateRaw))}
	// Mark receipt check before building the receipt; the evidence is the deterministic artifact hash + tool timeline.
	preReceiptPass := len(events) > 0 && stateHash != ""
	markCheck(manager, "receipt", preReceiptPass, "MCP timeline plus server-state SHA-256 bound into receipt")
	finalTaskState := taskStore.State()
	receipt, receiptErr := proofreceipt.Build(finalTaskState, events, []proofreceipt.Artifact{artifact})
	receiptStore, err := proofreceipt.Open(receiptPath)
	if err != nil {
		return err
	}
	_, created, saveErr := receiptStore.SaveOnce(receipt)
	_, createdAgain, saveAgainErr := receiptStore.SaveOnce(receipt)
	receiptPass := receiptErr == nil && saveErr == nil && saveAgainErr == nil && created && !createdAgain && len(receipt.TimelineRefs) == len(events) && len(receipt.Artifacts) == 1 && receipt.Artifacts[0].SHA256 == stateHash && finalTaskState.Progress().Complete
	add("proof_receipt_references_mcp_tool_evidence_and_artifact_hash_exactly_once", receiptPass, map[string]any{"receipt_id": receipt.ReceiptID, "timeline_refs": len(receipt.TimelineRefs), "created_once": created && !createdAgain, "error": errorString(errors.Join(receiptErr, saveErr, saveAgainErr))})

	out.ToolJournalOperations = len(journal.Snapshot())
	out.TimelineEvents = len(events)
	out.ReceiptID = receipt.ReceiptID
	out.ReceiptTimelineRefs = len(receipt.TimelineRefs)

	allPassed := true
	for _, s := range out.Scenarios {
		allPassed = allPassed && s.Passed
	}
	out.Passed = allPassed
	if allPassed {
		out.Status = "passed"
	}
	out.NextGate = "Harden the MCP bridge with explicit permission profiles, bounded frame tests, cancellation, and a production adapter seam. Then connect one real local MCP server before broad SaaS integrations."
	emit(out, boolCode(!allPassed))
	return nil
}

func newBridge(j *tooljournal.Journal, t *timeline.Store, c *mcpbridge.Client) *mcpbridge.Bridge {
	return &mcpbridge.Bridge{
		Journal: j, Timeline: t, TaskID: taskID, SessionID: sessionID, Client: c,
		AllowedTools: map[string]bool{"counter.increment": true, "ambiguous.append": true, "idempotent.put": true},
	}
}

func markCheck(manager *tasks.Manager, id string, passed bool, evidence string) {
	status := tasks.CheckFailed
	if passed {
		status = tasks.CheckPassed
	}
	_, _ = manager.UpdateCheck(id, status, evidence)
}

func runMCPServer(statePath string) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id,omitempty"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return err
		}
		state, _ := loadServerState(statePath)
		if state.RPCCounts == nil {
			state.RPCCounts = map[string]int{}
		}
		state.RPCCounts[req.Method]++
		_ = saveServerState(statePath, state)

		if len(req.ID) == 0 {
			continue
		}
		switch req.Method {
		case "initialize":
			writeRPC(enc, req.ID, map[string]any{"protocolVersion": mcpbridge.ProtocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "keydeck-proof19-local-mcp", "version": "0.1.0"}}, nil)
		case "tools/list":
			writeRPC(enc, req.ID, map[string]any{"tools": []map[string]any{
				{"name": "counter.increment", "description": "non-repeatable counter increment", "inputSchema": objectSchema("amount")},
				{"name": "ambiguous.append", "description": "append then terminate before response", "inputSchema": objectSchema("value")},
				{"name": "idempotent.put", "description": "idempotent key/value put", "inputSchema": objectSchema("key", "value")},
			}}, nil)
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				writeRPC(enc, req.ID, nil, map[string]any{"code": -32602, "message": err.Error()})
				continue
			}
			if err := executeServerTool(statePath, params.Name, params.Arguments, enc, req.ID); err != nil {
				return err
			}
		default:
			writeRPC(enc, req.ID, nil, map[string]any{"code": -32601, "message": "method not found"})
		}
	}
	return scanner.Err()
}

func executeServerTool(statePath, name string, args map[string]any, enc *json.Encoder, id json.RawMessage) error {
	state, _ := loadServerState(statePath)
	ensureStateMaps(&state)
	state.ToolCalls[name]++
	switch name {
	case "counter.increment":
		amount := intNumber(args["amount"])
		state.Counter += amount
		if err := saveServerState(statePath, state); err != nil {
			return err
		}
		writeRPC(enc, id, textResult(fmt.Sprintf("counter=%d", state.Counter)), nil)
	case "ambiguous.append":
		state.Appends = append(state.Appends, fmt.Sprint(args["value"]))
		if err := saveServerState(statePath, state); err != nil {
			return err
		}
		os.Exit(97)
	case "idempotent.put":
		key := fmt.Sprint(args["key"])
		value := fmt.Sprint(args["value"])
		state.KV[key] = value
		first := !state.IdempotentCrashDone[key]
		if first {
			state.IdempotentCrashDone[key] = true
		}
		if err := saveServerState(statePath, state); err != nil {
			return err
		}
		if first {
			os.Exit(98)
		}
		writeRPC(enc, id, textResult(fmt.Sprintf("put %s=%s", key, value)), nil)
	default:
		writeRPC(enc, id, textResult("unknown tool"), nil)
	}
	return nil
}

func textResult(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": false}
}

func objectSchema(required ...string) map[string]any {
	properties := map[string]any{}
	for _, name := range required {
		properties[name] = map[string]any{"type": "string"}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func writeRPC(enc *json.Encoder, id json.RawMessage, result any, rpcErr any) {
	msg := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	_ = enc.Encode(msg)
}

func ensureStateMaps(s *serverState) {
	if s.KV == nil {
		s.KV = map[string]string{}
	}
	if s.IdempotentCrashDone == nil {
		s.IdempotentCrashDone = map[string]bool{}
	}
	if s.ToolCalls == nil {
		s.ToolCalls = map[string]int{}
	}
	if s.RPCCounts == nil {
		s.RPCCounts = map[string]int{}
	}
}

func loadServerState(path string) (serverState, error) {
	var state serverState
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		ensureStateMaps(&state)
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	ensureStateMaps(&state)
	return state, nil
}

func saveServerState(path string, state serverState) error {
	ensureStateMaps(&state)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func totalRPCs(state serverState) int {
	total := 0
	for _, count := range state.RPCCounts {
		total += count
	}
	return total
}

func intNumber(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func emit(out report, code int) {
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	os.Exit(code)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func boolCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}
