package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type state struct {
	RPCCounts        map[string]int `json:"rpc_counts"`
	ToolCalls        map[string]int `json:"tool_calls"`
	CredentialHashes []string       `json:"credential_hashes"`
	Resources        []string       `json:"resources"`
}

type config struct {
	StatePath          string
	ExpectedSecretHash string
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
		case "--expected-secret-sha256":
			if i+1 >= len(args) {
				return cfg, errors.New("--expected-secret-sha256 requires a value")
			}
			i++
			cfg.ExpectedSecretHash = args[i]
		default:
			return cfg, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if cfg.StatePath == "" || len(cfg.ExpectedSecretHash) != 64 {
		return cfg, errors.New("--state and a 64-character --expected-secret-sha256 are required")
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
			writeRPC(enc, req.ID, map[string]any{
				"protocolVersion": "2025-11-25",
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
				"serverInfo":      map[string]any{"name": "keydeck-proof21-secret-aware-local-mcp", "version": "0.1.0"},
			}, nil)
		case "tools/list":
			writeRPC(enc, req.ID, map[string]any{"tools": []map[string]any{
				{"name": "secure.fetch", "description": "fetch using an adapter-boundary credential", "inputSchema": objectSchema("resource", "credential")},
				{"name": "secure.fail", "description": "return a tool error containing the received credential to test redaction", "inputSchema": objectSchema("resource", "credential")},
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
			if err := callTool(cfg, params.Name, params.Arguments, enc, req.ID); err != nil {
				return err
			}
		default:
			writeRPC(enc, req.ID, nil, map[string]any{"code": -32601, "message": "method not found"})
		}
	}
	return scanner.Err()
}

func callTool(cfg config, name string, args map[string]any, enc *json.Encoder, id json.RawMessage) error {
	s, _ := load(cfg.StatePath)
	s.ToolCalls[name]++
	credential := fmt.Sprint(args["credential"])
	resource := fmt.Sprint(args["resource"])
	credentialHash := sha256Hex([]byte(credential))
	s.CredentialHashes = append(s.CredentialHashes, credentialHash)
	s.Resources = append(s.Resources, resource)
	if err := save(cfg.StatePath, s); err != nil {
		return err
	}
	if credentialHash != cfg.ExpectedSecretHash {
		writeRPC(enc, id, textResult("credential hash mismatch", true), nil)
		return nil
	}
	switch name {
	case "secure.fetch":
		writeRPC(enc, id, textResult("secure:ok:"+resource, false), nil)
	case "secure.fail":
		writeRPC(enc, id, textResult("upstream rejected credential "+credential, true), nil)
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

func textResult(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}
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
	return state{RPCCounts: map[string]int{}, ToolCalls: map[string]int{}}
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

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
