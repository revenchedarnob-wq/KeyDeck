package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type state struct {
	RPCCounts   map[string]int    `json:"rpc_counts"`
	ToolCalls   map[string]int    `json:"tool_calls"`
	KV          map[string]string `json:"kv"`
	DeleteCount int               `json:"delete_count"`
	SlowEffects []string          `json:"slow_effects"`
}

type config struct {
	StatePath    string
	OversizeInit bool
}

func main() {
	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (config, error) {
	var cfg config
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--state":
			if i+1 >= len(args) {
				return cfg, errors.New("--state requires a path")
			}
			i++
			cfg.StatePath = args[i]
		case "--oversize-init":
			cfg.OversizeInit = true
		default:
			return cfg, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if cfg.StatePath == "" {
		return cfg, errors.New("--state is required")
	}
	return cfg, nil
}

func run(cfg config) error {
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
		s, _ := load(cfg.StatePath)
		s.RPCCounts[req.Method]++
		if err := save(cfg.StatePath, s); err != nil {
			return err
		}
		if len(req.ID) == 0 {
			continue
		}
		switch req.Method {
		case "initialize":
			result := map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
				"serverInfo":      map[string]any{"name": "keydeck-proof20-independent-local-mcp", "version": "0.1.0"},
			}
			if cfg.OversizeInit {
				result["oversizedProofPadding"] = strings.Repeat("x", 4096)
			}
			writeRPC(enc, req.ID, result, nil)
		case "tools/list":
			writeRPC(enc, req.ID, map[string]any{"tools": []map[string]any{
				{"name": "readonly.echo", "description": "read-only echo", "inputSchema": objectSchema("value")},
				{"name": "safe.write", "description": "safe deterministic write", "inputSchema": objectSchema("key", "value")},
				{"name": "admin.delete", "description": "full-control destructive operation", "inputSchema": objectSchema("target")},
				{"name": "slow.commit", "description": "commit side effect then delay response", "inputSchema": objectSchema("value")},
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
			if err := callTool(cfg.StatePath, params.Name, params.Arguments, enc, req.ID); err != nil {
				return err
			}
		default:
			writeRPC(enc, req.ID, nil, map[string]any{"code": -32601, "message": "method not found"})
		}
	}
	return scanner.Err()
}

func callTool(path, name string, args map[string]any, enc *json.Encoder, id json.RawMessage) error {
	s, _ := load(path)
	s.ToolCalls[name]++
	switch name {
	case "readonly.echo":
		if err := save(path, s); err != nil {
			return err
		}
		writeRPC(enc, id, textResult("echo:"+fmt.Sprint(args["value"])), nil)
	case "safe.write":
		s.KV[fmt.Sprint(args["key"])] = fmt.Sprint(args["value"])
		if err := save(path, s); err != nil {
			return err
		}
		writeRPC(enc, id, textResult("write:ok"), nil)
	case "admin.delete":
		s.DeleteCount++
		if err := save(path, s); err != nil {
			return err
		}
		writeRPC(enc, id, textResult("delete:ok"), nil)
	case "slow.commit":
		s.SlowEffects = append(s.SlowEffects, fmt.Sprint(args["value"]))
		if err := save(path, s); err != nil {
			return err
		}
		time.Sleep(5 * time.Second)
		writeRPC(enc, id, textResult("slow:ok"), nil)
	default:
		writeRPC(enc, id, nil, map[string]any{"code": -32602, "message": "unknown tool"})
	}
	return nil
}

func objectSchema(required ...string) map[string]any {
	props := map[string]any{}
	for _, key := range required {
		props[key] = map[string]any{"type": "string"}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func textResult(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": false}
}

func writeRPC(enc *json.Encoder, id json.RawMessage, result, rpcErr any) {
	msg := map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id)}
	if rpcErr != nil {
		msg["error"] = rpcErr
	} else {
		msg["result"] = result
	}
	_ = enc.Encode(msg)
}

func emptyState() state {
	return state{RPCCounts: map[string]int{}, ToolCalls: map[string]int{}, KV: map[string]string{}}
}

func load(path string) (state, error) {
	s := emptyState()
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, err
	}
	if s.RPCCounts == nil {
		s.RPCCounts = map[string]int{}
	}
	if s.ToolCalls == nil {
		s.ToolCalls = map[string]int{}
	}
	if s.KV == nil {
		s.KV = map[string]string{}
	}
	return s, nil
}

func save(path string, s state) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
