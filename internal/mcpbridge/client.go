package mcpbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

const ProtocolVersion = "2025-11-25"

var ErrFrameTooLarge = errors.New("MCP frame exceeds configured limit")

type CommandConfig struct {
	Path          string
	Args          []string
	Env           []string
	Dir           string
	MaxFrameBytes int
}

type Client struct {
	cfg CommandConfig
}

func NewClient(cfg CommandConfig) *Client { return &Client{cfg: cfg} }

type Session struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	reader        *bufio.Reader
	maxFrameBytes int
	nextID        int64
	mu            sync.Mutex
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type CallToolResult struct {
	Content []Content      `json:"content"`
	IsError bool           `json:"isError,omitempty"`
	Meta    map[string]any `json:"_meta,omitempty"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func (c *Client) Connect(ctx context.Context) (*Session, error) {
	if strings.TrimSpace(c.cfg.Path) == "" {
		return nil, errors.New("MCP command path is required")
	}
	cmd := exec.CommandContext(ctx, c.cfg.Path, c.cfg.Args...)
	if c.cfg.Env != nil {
		cmd.Env = c.cfg.Env
	}
	if c.cfg.Dir != "" {
		cmd.Dir = c.cfg.Dir
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	max := c.cfg.MaxFrameBytes
	if max <= 0 {
		max = 1 << 20
	}
	return &Session{
		cmd:           cmd,
		stdin:         stdin,
		reader:        bufio.NewReaderSize(stdout, min(max, 64<<10)),
		maxFrameBytes: max,
	}, nil
}

func (s *Session) Initialize() error {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "keydeck", "version": "0.1.0"},
	}
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := s.request("initialize", params, &result); err != nil {
		return err
	}
	if result.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unexpected MCP protocol version %q", result.ProtocolVersion)
	}
	return s.notify("notifications/initialized", map[string]any{})
}

func (s *Session) ListTools() ([]Tool, error) {
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := s.request("tools/list", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func (s *Session) CallTool(name string, args map[string]any) (CallToolResult, error) {
	var result CallToolResult
	err := s.request("tools/call", map[string]any{"name": name, "arguments": args}, &result)
	return result, err
}

func (s *Session) Close() error {
	_ = s.stdin.Close()
	return s.cmd.Wait()
}

func (s *Session) request(method string, params any, out any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	msg := map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method, "params": params}
	if err := writeLine(s.stdin, msg); err != nil {
		return err
	}
	frame, err := readFrame(s.reader, s.maxFrameBytes)
	if err != nil {
		return fmt.Errorf("read MCP response: %w", err)
	}
	var resp rpcResponse
	if err := json.Unmarshal(frame, &resp); err != nil {
		return fmt.Errorf("decode MCP response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("decode MCP result: %w", err)
		}
	}
	return nil
}

func (s *Session) notify(method string, params any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeLine(s.stdin, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	if max <= 0 {
		return nil, errors.New("MCP frame limit must be positive")
	}
	buf := make([]byte, 0, min(max, 64<<10))
	for {
		fragment, err := r.ReadSlice('\n')
		if len(buf)+len(fragment) > max {
			return nil, ErrFrameTooLarge
		}
		buf = append(buf, fragment...)
		switch {
		case err == nil:
			return bytes.TrimSuffix(buf, []byte{'\n'}), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}

func writeLine(w io.Writer, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
