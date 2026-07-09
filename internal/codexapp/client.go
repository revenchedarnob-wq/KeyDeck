package codexapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcResponse struct {
	result json.RawMessage
	err    error
}

type Client struct {
	transport Transport
	info      ClientInfo
	nextID    atomic.Int64

	mu          sync.Mutex
	pending     map[int64]chan rpcResponse
	notes       chan Notification
	diagnostics chan error
	done        chan struct{}
	closeOnce   sync.Once
}

func NewClient(transport Transport, info ClientInfo) *Client {
	if info.Name == "" {
		info.Name = "keydeck_lab"
	}
	if info.Title == "" {
		info.Title = "KeyDeck Feasibility Lab"
	}
	if info.Version == "" {
		info.Version = "0.2.0"
	}
	c := &Client{
		transport:   transport,
		info:        info,
		pending:     make(map[int64]chan rpcResponse),
		notes:       make(chan Notification, 512),
		diagnostics: make(chan error, 64),
		done:        make(chan struct{}),
	}
	c.nextID.Store(0)
	go c.readLoop()
	return c
}

func (c *Client) Initialize(ctx context.Context) error {
	var result map[string]any
	if err := c.Call(ctx, "initialize", map[string]any{"clientInfo": c.info}, &result); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return c.Notify("initialized", map[string]any{})
}

func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := c.nextID.Add(1)
	payload := map[string]any{"method": method, "id": id}
	if params != nil {
		payload["params"] = params
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ch := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.transport.Send(b); err != nil {
		c.removePending(id)
		return err
	}
	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case <-c.done:
		c.removePending(id)
		return errors.New("codex app-server connection closed")
	case resp := <-ch:
		if resp.err != nil {
			return resp.err
		}
		if out == nil || len(resp.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.result, out); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (c *Client) Notify(method string, params any) error {
	payload := map[string]any{"method": method}
	if params != nil {
		payload["params"] = params
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.transport.Send(b)
}

func (c *Client) Notifications() <-chan Notification { return c.notes }
func (c *Client) Diagnostics() <-chan error          { return c.diagnostics }
func (c *Client) Done() <-chan struct{}              { return c.done }
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		_ = c.transport.Close()
		close(c.done)
	})
	return nil
}

func (c *Client) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) readLoop() {
	for {
		select {
		case line, ok := <-c.transport.Lines():
			if !ok {
				c.failAll(errors.New("codex app-server stdout closed"))
				return
			}
			var msg rpcMessage
			if err := json.Unmarshal(line, &msg); err != nil {
				continue
			}
			if msg.ID != nil && msg.Method == "" {
				c.mu.Lock()
				ch := c.pending[*msg.ID]
				delete(c.pending, *msg.ID)
				c.mu.Unlock()
				if ch != nil {
					if msg.Error != nil {
						ch <- rpcResponse{err: fmt.Errorf("rpc %d: %s", msg.Error.Code, msg.Error.Message)}
					} else {
						ch <- rpcResponse{result: msg.Result}
					}
				}
				continue
			}
			if msg.ID != nil && msg.Method != "" {
				// Server-initiated requests require explicit handling. The lab
				// refuses unknown requests instead of silently approving actions.
				_ = c.respondUnsupported(*msg.ID, msg.Method)
				continue
			}
			if msg.Method != "" {
				select {
				case c.notes <- Notification{Method: msg.Method, Params: msg.Params}:
				default:
					// Do not block the transport reader; production KeyDeck will
					// persist events to its canonical event store.
				}
			}
		case err := <-c.transport.Errors():
			// Diagnostic errors do not necessarily terminate stdio. Preserve them
			// for proof output and future canonical tracing without terminating the
			// JSON-RPC connection.
			if err != nil {
				select {
				case c.diagnostics <- err:
				default:
				}
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) respondUnsupported(id int64, method string) error {
	payload := map[string]any{
		"id":    id,
		"error": map[string]any{"code": -32601, "message": "KeyDeck Lab does not auto-approve server request: " + method},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.transport.Send(b)
}

func (c *Client) failAll(err error) {
	c.closeOnce.Do(func() { close(c.done) })
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- rpcResponse{err: err}
		delete(c.pending, id)
	}
}

// WaitForNotification waits for a specific notification. It is intended for
// proof code and narrow lifecycle events, not as the final event-bus design.
func (c *Client) WaitForNotification(ctx context.Context, method string) (Notification, error) {
	for {
		select {
		case <-ctx.Done():
			return Notification{}, ctx.Err()
		case <-c.done:
			return Notification{}, errors.New("codex app-server closed")
		case note := <-c.notes:
			if note.Method == method {
				return note, nil
			}
		}
	}
}

func defaultTimeout() time.Duration { return 30 * time.Second }
