package pool

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

var (
	ErrAllKeysUnavailable = errors.New("all keys unavailable")
	ErrProviderBusy       = errors.New("provider is globally busy")
	ErrAmbiguousFailure   = errors.New("ambiguous provider failure; automatic replay is forbidden")
	ErrCostSafetyBlocked  = errors.New("cost safety blocked further API spending")
)

type KeyState string

const (
	KeyReady       KeyState = "ready"
	KeyActive      KeyState = "active"
	KeyExhausted   KeyState = "exhausted"
	KeyInvalid     KeyState = "invalid"
	KeyCoolingDown KeyState = "cooling_down"
)

type Key struct {
	ID     string   `json:"id"`
	Secret string   `json:"-"`
	State  KeyState `json:"state"`
}

type Result struct {
	Output []byte
	Usage  protocol.Usage
	KeyID  string
}

type Pool struct {
	mu         sync.Mutex
	keys       []Key
	client     *providerhttp.Client
	events     *events.Recorder
	guard      *costguard.Guard
	classifier FailureClassifier
	activeAt   int
}

func New(keys []Key, client *providerhttp.Client, recorder *events.Recorder, guard *costguard.Guard) *Pool {
	return NewWithClassifier(keys, client, recorder, guard, ConservativeClassifier{})
}

func NewWithClassifier(keys []Key, client *providerhttp.Client, recorder *events.Recorder, guard *costguard.Guard, classifier FailureClassifier) *Pool {
	copyKeys := make([]Key, len(keys))
	copy(copyKeys, keys)
	for i := range copyKeys {
		if copyKeys[i].State == "" {
			copyKeys[i].State = KeyReady
		}
	}
	if classifier == nil {
		classifier = ConservativeClassifier{}
	}
	return &Pool{keys: copyKeys, client: client, events: recorder, guard: guard, classifier: classifier, activeAt: -1}
}

func (p *Pool) Execute(ctx context.Context, request []byte) (Result, error) {
	if blocked, reason := p.guard.Blocked(); blocked {
		p.events.Add(events.Event{Kind: events.RequestBlocked, Detail: reason})
		return Result{}, fmt.Errorf("%w: %s", ErrCostSafetyBlocked, reason)
	}

	for idx := 0; idx < len(p.keys); idx++ {
		key, ok := p.selectUsableFrom(idx)
		if !ok {
			break
		}
		idx = key.index
		p.events.Add(events.Event{Kind: events.RequestAttempt, KeyID: key.value.ID, Attempt: idx + 1})

		resp, err := p.client.Do(ctx, key.value.Secret, request)
		class := p.classifier.Classify(resp, err)
		switch class {
		case FailureNone:
			env, decodeErr := protocol.DecodeEnvelope(resp.Body)
			if decodeErr != nil {
				p.events.Add(events.Event{Kind: events.AmbiguousFailure, KeyID: key.value.ID, FailureClass: string(FailureAmbiguous), Detail: "successful response could not be decoded"})
				return Result{}, fmt.Errorf("%w: decode successful response: %v", ErrAmbiguousFailure, decodeErr)
			}
			p.setState(key.index, KeyActive)
			p.events.Add(events.Event{Kind: events.RequestSucceeded, KeyID: key.value.ID})
			decision := p.guard.Observe(env.Usage)
			if decision.Triggered {
				p.events.Add(events.Event{Kind: events.CostSafetyTriggered, KeyID: key.value.ID, Detail: decision.Reason})
			}
			return Result{Output: resp.Body, Usage: env.Usage, KeyID: key.value.ID}, nil

		case FailureKeyExhausted:
			p.setState(key.index, KeyExhausted)
			p.events.Add(events.Event{Kind: events.KeyStateChanged, KeyID: key.value.ID, FailureClass: string(class), Detail: "safe failover allowed by explicit key-scoped evidence"})
			continue
		case FailureInvalidKey:
			p.setState(key.index, KeyInvalid)
			p.events.Add(events.Event{Kind: events.KeyStateChanged, KeyID: key.value.ID, FailureClass: string(class), Detail: "safe failover allowed by explicit key-scoped evidence"})
			continue
		case FailureKeyRateLimited:
			p.setState(key.index, KeyCoolingDown)
			p.events.Add(events.Event{Kind: events.KeyStateChanged, KeyID: key.value.ID, FailureClass: string(class), Detail: "safe failover allowed by explicit key-scoped evidence"})
			continue
		case FailureProviderBusy:
			p.events.Add(events.Event{Kind: events.ProviderBusy, KeyID: key.value.ID, FailureClass: string(class), Detail: "backup keys preserved"})
			return Result{}, ErrProviderBusy
		case FailureAmbiguous:
			p.events.Add(events.Event{Kind: events.AmbiguousFailure, KeyID: key.value.ID, FailureClass: string(class), Detail: "no retry and no key rotation"})
			return Result{}, ErrAmbiguousFailure
		default:
			return Result{}, fmt.Errorf("provider returned non-retryable response: HTTP %d", resp.StatusCode)
		}
	}
	return Result{}, ErrAllKeysUnavailable
}

type indexedKey struct {
	index int
	value Key
}

func (p *Pool) selectUsableFrom(start int) (indexedKey, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := start; i < len(p.keys); i++ {
		switch p.keys[i].State {
		case KeyReady, KeyActive:
			return indexedKey{index: i, value: p.keys[i]}, true
		}
	}
	return indexedKey{}, false
}

func (p *Pool) setState(index int, state KeyState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.keys) {
		return
	}
	for i := range p.keys {
		if i != index && p.keys[i].State == KeyActive {
			p.keys[i].State = KeyReady
		}
	}
	p.keys[index].State = state
	if state == KeyActive {
		p.activeAt = index
	}
}

func (p *Pool) Snapshot() []Key {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Key, len(p.keys))
	copy(out, p.keys)
	return out
}
