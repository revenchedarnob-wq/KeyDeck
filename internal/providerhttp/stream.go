package providerhttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"keydeck.local/feasibilitylab/internal/protocol"
)

var ErrAbruptStream = errors.New("stream ended without terminal event")

type StreamEventType string

const (
	StreamTextDelta StreamEventType = "text_delta"
	StreamUsage     StreamEventType = "usage"
	StreamError     StreamEventType = "error"
	StreamDone      StreamEventType = "done"
)

type StreamEvent struct {
	Type  StreamEventType
	Text  string
	Usage protocol.Usage
	Code  string
	Scope string
}

type StreamClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *StreamClient) Do(ctx context.Context, key string, body []byte, onEvent func(StreamEvent) error) error {
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/stream", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-api-key", key)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("stream HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64<<10)
	scanner.Buffer(buf, 2<<20)
	terminal := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var raw struct {
			Type  string         `json:"type"`
			Text  string         `json:"text,omitempty"`
			Usage protocol.Usage `json:"usage,omitempty"`
			Error *struct {
				Code  string `json:"code"`
				Scope string `json:"scope,omitempty"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &raw); err != nil {
			return fmt.Errorf("decode stream event: %w", err)
		}
		event := StreamEvent{Type: StreamEventType(raw.Type), Text: raw.Text, Usage: raw.Usage}
		if raw.Error != nil {
			event.Code, event.Scope = raw.Error.Code, raw.Error.Scope
		}
		if err := onEvent(event); err != nil {
			return err
		}
		if event.Type == StreamDone || event.Type == StreamError {
			terminal = true
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if !terminal {
		return ErrAbruptStream
	}
	return nil
}
