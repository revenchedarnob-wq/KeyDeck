package fakecodexapp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type State struct {
	mu          sync.Mutex
	ThreadID    string
	StartCalls  int
	ResumeCalls int
	TurnCalls   int
	LastPrompt  string
}

type Snapshot struct {
	ThreadID    string `json:"thread_id"`
	StartCalls  int    `json:"start_calls"`
	ResumeCalls int    `json:"resume_calls"`
	TurnCalls   int    `json:"turn_calls"`
	LastPrompt  string `json:"last_prompt"`
}

func (s *State) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{ThreadID: s.ThreadID, StartCalls: s.StartCalls, ResumeCalls: s.ResumeCalls, TurnCalls: s.TurnCalls, LastPrompt: s.LastPrompt}
}

func Serve(rw io.ReadWriteCloser, state *State) error {
	defer rw.Close()
	scanner := bufio.NewScanner(rw)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	enc := json.NewEncoder(rw)
	initialized := false
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		method, _ := msg["method"].(string)
		id := msg["id"]
		params, _ := msg["params"].(map[string]any)
		switch method {
		case "initialize":
			initialized = true
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"userAgent": "fake-codex-app-server", "platformFamily": "windows", "platformOs": "windows"}})
		case "initialized":
			if !initialized {
				return io.ErrUnexpectedEOF
			}
		case "account/read":
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"account": map[string]any{"type": "chatgpt", "email": "proof@example.invalid", "planType": "plus"}, "requiresOpenaiAuth": true}})
		case "thread/start":
			state.mu.Lock()
			state.StartCalls++
			if state.ThreadID == "" {
				state.ThreadID = "thr_keydeck_proof05"
			}
			tid := state.ThreadID
			state.mu.Unlock()
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": tid, "sessionId": tid}}})
			_ = enc.Encode(map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": tid}}})
		case "thread/resume":
			state.mu.Lock()
			state.ResumeCalls++
			tid := state.ThreadID
			state.mu.Unlock()
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"thread": map[string]any{"id": tid, "sessionId": tid}}})
		case "turn/start":
			state.mu.Lock()
			state.TurnCalls++
			prompt := extractPrompt(params)
			state.LastPrompt = prompt
			turnNum := state.TurnCalls
			state.mu.Unlock()
			cwd, _ := params["cwd"].(string)
			if cwd != "" {
				_ = os.WriteFile(filepath.Join(cwd, "codex-proof.txt"), []byte("updated by fake Codex turn\n"), 0o600)
			}
			turnID := "turn_proof05_1"
			if turnNum > 1 {
				turnID = "turn_proof05_2"
			}
			_ = enc.Encode(map[string]any{"id": id, "result": map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress"}}})
			_ = enc.Encode(map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": state.ThreadID, "turnId": turnID, "delta": "Codex continued from the KeyDeck passport and updated the disposable project."}})
			_ = enc.Encode(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": state.ThreadID, "turnId": turnID, "item": map[string]any{"type": "fileChange", "id": "item_file", "changes": []any{map[string]any{"path": "codex-proof.txt", "kind": "updated"}}}}})
			_ = enc.Encode(map[string]any{"method": "turn/completed", "params": map[string]any{"turn": map[string]any{"id": turnID, "status": "completed"}}})
		}
	}
	return scanner.Err()
}

func extractPrompt(params map[string]any) string {
	input, _ := params["input"].([]any)
	for _, raw := range input {
		item, _ := raw.(map[string]any)
		if item["type"] == "text" {
			text, _ := item["text"].(string)
			return text
		}
	}
	return ""
}
