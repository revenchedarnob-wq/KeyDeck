package continuity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

var (
	ErrStreamAmbiguous = errors.New("stream failed ambiguously; automatic continuation is forbidden")
	ErrStreamBusy      = errors.New("provider is globally busy during stream")
)

type ContinuationPackage struct {
	OriginalRequest  json.RawMessage `json:"original_request"`
	ConfirmedOutput  string          `json:"confirmed_output"`
	UnstableFragment string          `json:"unstable_fragment"`
	Instruction      string          `json:"instruction"`
}

type Request struct {
	Prompt       string               `json:"prompt,omitempty"`
	Continuation *ContinuationPackage `json:"continuation,omitempty"`
}

type Result struct {
	Text             string
	ConfirmedOutput  string
	UnstableFragment string
	FinalKey         string
	Switches         int
	Usage            protocol.Usage
}

type Engine struct {
	keys   []pool.Key
	client *providerhttp.StreamClient
	events *events.Recorder
}

func New(keys []pool.Key, client *providerhttp.StreamClient, recorder *events.Recorder) *Engine {
	copyKeys := make([]pool.Key, len(keys))
	copy(copyKeys, keys)
	return &Engine{keys: copyKeys, client: client, events: recorder}
}

func (e *Engine) Execute(ctx context.Context, original []byte) (Result, error) {
	var visible strings.Builder
	buffer := &SentenceBuffer{}
	switches := 0

	for i := range e.keys {
		if e.keys[i].State == pool.KeyExhausted || e.keys[i].State == pool.KeyInvalid || e.keys[i].State == pool.KeyCoolingDown {
			continue
		}
		requestBody := original
		if i > 0 {
			pkg := ContinuationPackage{
				OriginalRequest:  append([]byte(nil), original...),
				ConfirmedOutput:  visible.String(),
				UnstableFragment: buffer.Pending(),
				Instruction:      "Continue the same answer from the last confirmed boundary. Use the unstable fragment only as directional context. Do not repeat confirmed output.",
			}
			requestBody, _ = json.Marshal(Request{Continuation: &pkg})
			switches++
			e.events.Add(events.Event{Kind: events.Kind("continuation_started"), KeyID: e.keys[i].ID, Detail: fmt.Sprintf("confirmed=%d unstable=%d", len(pkg.ConfirmedOutput), len(pkg.UnstableFragment))})
			buffer = &SentenceBuffer{}
		}

		e.events.Add(events.Event{Kind: events.RequestAttempt, KeyID: e.keys[i].ID, Attempt: i + 1})
		terminalClass := pool.FailureNone
		usage := protocol.Usage{}
		err := e.client.Do(ctx, e.keys[i].Secret, requestBody, func(event providerhttp.StreamEvent) error {
			switch event.Type {
			case providerhttp.StreamTextDelta:
				if committed := buffer.Push(event.Text); committed != "" {
					visible.WriteString(committed)
					e.events.Add(events.Event{Kind: events.Kind("output_committed"), KeyID: e.keys[i].ID, Detail: committed})
				}
			case providerhttp.StreamUsage:
				usage = event.Usage
			case providerhttp.StreamError:
				terminalClass = classifyStreamError(event.Code, event.Scope)
			case providerhttp.StreamDone:
				terminalClass = pool.FailureNone
			}
			return nil
		})

		if errors.Is(err, providerhttp.ErrAbruptStream) || (err != nil && terminalClass == pool.FailureNone) {
			e.events.Add(events.Event{Kind: events.AmbiguousFailure, KeyID: e.keys[i].ID, FailureClass: string(pool.FailureAmbiguous), Detail: "partial stream ended without explicit key-scoped evidence"})
			return Result{Text: visible.String(), ConfirmedOutput: visible.String(), UnstableFragment: buffer.Pending(), FinalKey: e.keys[i].ID, Switches: switches}, ErrStreamAmbiguous
		}
		if err != nil {
			return Result{Text: visible.String(), ConfirmedOutput: visible.String(), UnstableFragment: buffer.Pending(), FinalKey: e.keys[i].ID, Switches: switches}, err
		}

		switch terminalClass {
		case pool.FailureNone:
			visible.WriteString(buffer.Flush())
			e.keys[i].State = pool.KeyActive
			e.events.Add(events.Event{Kind: events.RequestSucceeded, KeyID: e.keys[i].ID})
			return Result{Text: visible.String(), ConfirmedOutput: visible.String(), FinalKey: e.keys[i].ID, Switches: switches, Usage: usage}, nil
		case pool.FailureKeyExhausted:
			e.keys[i].State = pool.KeyExhausted
			e.events.Add(events.Event{Kind: events.KeyStateChanged, KeyID: e.keys[i].ID, FailureClass: string(terminalClass), Detail: "explicit mid-stream exhaustion; semantic continuation allowed"})
			continue
		case pool.FailureProviderBusy:
			e.events.Add(events.Event{Kind: events.ProviderBusy, KeyID: e.keys[i].ID, FailureClass: string(terminalClass), Detail: "partial stream preserved; backup keys untouched"})
			return Result{Text: visible.String(), ConfirmedOutput: visible.String(), UnstableFragment: buffer.Pending(), FinalKey: e.keys[i].ID, Switches: switches}, ErrStreamBusy
		default:
			e.events.Add(events.Event{Kind: events.AmbiguousFailure, KeyID: e.keys[i].ID, FailureClass: string(terminalClass), Detail: "mid-stream failure not proven safe for continuation"})
			return Result{Text: visible.String(), ConfirmedOutput: visible.String(), UnstableFragment: buffer.Pending(), FinalKey: e.keys[i].ID, Switches: switches}, ErrStreamAmbiguous
		}
	}
	return Result{Text: visible.String(), ConfirmedOutput: visible.String(), UnstableFragment: buffer.Pending(), Switches: switches}, pool.ErrAllKeysUnavailable
}

func classifyStreamError(code, scope string) pool.FailureClass {
	code = strings.ToLower(strings.TrimSpace(code))
	scope = strings.ToLower(strings.TrimSpace(scope))
	if (code == "key_exhausted" || code == "insufficient_balance" || code == "credit_exhausted") && (scope == "" || scope == "key") {
		return pool.FailureKeyExhausted
	}
	if (code == "provider_busy" || code == "global_busy" || code == "provider_overloaded") && (scope == "" || scope == "provider") {
		return pool.FailureProviderBusy
	}
	return pool.FailureAmbiguous
}
