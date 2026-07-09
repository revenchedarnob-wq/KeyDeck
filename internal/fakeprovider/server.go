package fakeprovider

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"keydeck.local/feasibilitylab/internal/protocol"
)

type Behavior string

const (
	Success        Behavior = "success"
	KeyExhausted   Behavior = "key_exhausted"
	InvalidKey     Behavior = "invalid_key"
	KeyRateLimited Behavior = "key_rate_limited"
	ProviderBusy   Behavior = "provider_busy"
	Ambiguous502   Behavior = "ambiguous_502"
	Ambiguous429   Behavior = "ambiguous_429"
	CustomError    Behavior = "custom_error"
)

type Outcome struct {
	Behavior   Behavior
	Output     string
	Usage      protocol.Usage
	StatusCode int
	ErrorCode  string
	ErrorScope string
	Message    string
}

type Plan struct {
	mu        sync.Mutex
	Default   Outcome
	ByKey     map[string][]Outcome
	CallCount map[string]int
	Bodies    map[string][][]byte
}

func NewPlan() *Plan {
	return &Plan{
		Default:   Outcome{Behavior: Success, Output: "OK"},
		ByKey:     map[string][]Outcome{},
		CallCount: map[string]int{},
		Bodies:    map[string][][]byte{},
	}
}

func (p *Plan) next(key string, body []byte) Outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CallCount[key]++
	p.Bodies[key] = append(p.Bodies[key], append([]byte(nil), body...))
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

func (p *Plan) Calls(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.CallCount[key]
}

func (p *Plan) LastBody(key string) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	bodies := p.Bodies[key]
	if len(bodies) == 0 {
		return nil
	}
	return append([]byte(nil), bodies[len(bodies)-1]...)
}

func Handler(plan *Plan) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		key := r.Header.Get("x-api-key")
		out := plan.next(key, body)
		w.Header().Set("Content-Type", "application/json")

		switch out.Behavior {
		case Success:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(protocol.Envelope{Output: out.Output, Usage: out.Usage})
		case KeyExhausted:
			writeError(w, http.StatusPaymentRequired, "key_exhausted", "key", "test key has no remaining credit")
		case InvalidKey:
			writeError(w, http.StatusUnauthorized, "invalid_key", "key", "test key is invalid")
		case KeyRateLimited:
			writeError(w, http.StatusTooManyRequests, "key_rate_limited", "key", "test key is rate limited")
		case ProviderBusy:
			writeError(w, http.StatusServiceUnavailable, "provider_busy", "provider", "test provider is globally busy")
		case Ambiguous502:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"upstream connection failed after unknown processing state"}}`))
		case Ambiguous429:
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
		case CustomError:
			status := out.StatusCode
			if status == 0 {
				status = http.StatusBadRequest
			}
			message := out.Message
			if message == "" {
				message = "custom test error"
			}
			if out.Usage != (protocol.Usage{}) {
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": out.ErrorCode, "scope": out.ErrorScope, "message": message},
					"usage": out.Usage,
				})
			} else {
				writeError(w, status, out.ErrorCode, out.ErrorScope, message)
			}
		default:
			writeError(w, http.StatusInternalServerError, "unknown_test_behavior", "provider", "unknown fake-provider behavior")
		}
	})
}

func writeError(w http.ResponseWriter, status int, code, scope, message string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","scope":"` + scope + `","message":"` + message + `"}}`))
}
