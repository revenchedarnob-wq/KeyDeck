package fakeprovider

import (
	"bufio"
	"encoding/json"
	"net/http"
	"sync"

	"keydeck.local/feasibilitylab/internal/protocol"
)

type StreamTerminal string

const (
	StreamDone         StreamTerminal = "done"
	StreamKeyExhausted StreamTerminal = "key_exhausted"
	StreamProviderBusy StreamTerminal = "provider_busy"
	StreamAbruptEOF    StreamTerminal = "abrupt_eof"
)

type StreamOutcome struct {
	Chunks   []string
	Terminal StreamTerminal
	Usage    protocol.Usage
}

type StreamRequestRecord struct {
	Key  string
	Body []byte
}

type StreamPlan struct {
	mu       sync.Mutex
	ByKey    map[string][]StreamOutcome
	Default  StreamOutcome
	Requests []StreamRequestRecord
}

func NewStreamPlan() *StreamPlan {
	return &StreamPlan{
		ByKey:   map[string][]StreamOutcome{},
		Default: StreamOutcome{Chunks: []string{"OK"}, Terminal: StreamDone},
	}
}

func (p *StreamPlan) next(key string, body []byte) StreamOutcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Requests = append(p.Requests, StreamRequestRecord{Key: key, Body: append([]byte(nil), body...)})
	queue := p.ByKey[key]
	if len(queue) == 0 {
		return p.Default
	}
	out := queue[0]
	if len(queue) > 1 {
		p.ByKey[key] = queue[1:]
	}
	return out
}

func (p *StreamPlan) SnapshotRequests() []StreamRequestRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]StreamRequestRecord, len(p.Requests))
	copy(out, p.Requests)
	return out
}

func Mux(plan *Plan, streamPlan *StreamPlan) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/v1/messages", Handler(plan))
	mux.HandleFunc("/v1/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var raw json.RawMessage
		if err := json.NewDecoder(bufio.NewReader(r.Body)).Decode(&raw); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "request", err.Error())
			return
		}
		key := r.Header.Get("x-api-key")
		out := streamPlan.next(key, raw)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, _ := w.(http.Flusher)

		for _, chunk := range out.Chunks {
			writeSSE(w, map[string]any{"type": "text_delta", "text": chunk})
			if flusher != nil {
				flusher.Flush()
			}
		}

		if out.Terminal != StreamDone && out.Usage != (protocol.Usage{}) {
			writeSSE(w, map[string]any{"type": "usage", "usage": out.Usage})
		}

		switch out.Terminal {
		case StreamDone:
			writeSSE(w, map[string]any{"type": "usage", "usage": out.Usage})
			writeSSE(w, map[string]any{"type": "done"})
		case StreamKeyExhausted:
			writeSSE(w, map[string]any{"type": "error", "error": map[string]any{"code": "key_exhausted", "scope": "key", "message": "test key exhausted mid-stream"}})
		case StreamProviderBusy:
			writeSSE(w, map[string]any{"type": "error", "error": map[string]any{"code": "provider_busy", "scope": "provider", "message": "test provider busy mid-stream"}})
		case StreamAbruptEOF:
			// Deliberately end the response without a terminal event. The client must
			// treat the partial stream as ambiguous and must not rotate keys.
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	})
	return mux
}

func writeSSE(w http.ResponseWriter, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
}
