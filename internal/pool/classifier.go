package pool

import (
	"strings"

	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

type FailureClass string

const (
	FailureNone           FailureClass = "none"
	FailureKeyExhausted   FailureClass = "key_exhausted"
	FailureInvalidKey     FailureClass = "invalid_key"
	FailureKeyRateLimited FailureClass = "key_rate_limited"
	FailureProviderBusy   FailureClass = "provider_busy"
	FailureAmbiguous      FailureClass = "ambiguous"
	FailureNonRetryable   FailureClass = "non_retryable"
)

// FailureClassifier is the narrow provider boundary used by the key pool.
// Provider-specific profiles may classify only exact, evidenced behavior while
// unknown behavior falls back to the conservative classifier.
type FailureClassifier interface {
	Classify(resp providerhttp.Response, transportErr error) FailureClass
}

// FailureClassifierFunc adapts a function to FailureClassifier.
type FailureClassifierFunc func(providerhttp.Response, error) FailureClass

func (f FailureClassifierFunc) Classify(resp providerhttp.Response, transportErr error) FailureClass {
	return f(resp, transportErr)
}

// ConservativeClassifier is KeyDeck's safe default for unknown providers.
// Unknown 429/502/504/network outcomes remain ambiguous and never authorize
// automatic replay or backup-key spending.
type ConservativeClassifier struct{}

func (ConservativeClassifier) Classify(resp providerhttp.Response, transportErr error) FailureClass {
	return classifyConservatively(resp, transportErr)
}

func classifyConservatively(resp providerhttp.Response, transportErr error) FailureClass {
	if transportErr != nil {
		return FailureAmbiguous
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return FailureNone
	}

	env, err := protocol.DecodeEnvelope(resp.Body)
	if err == nil && env.Error != nil {
		code := strings.ToLower(strings.TrimSpace(env.Error.Code))
		scope := strings.ToLower(strings.TrimSpace(env.Error.Scope))
		switch code {
		case "key_exhausted", "insufficient_balance", "credit_exhausted":
			if scope == "" || scope == "key" {
				return FailureKeyExhausted
			}
		case "invalid_key", "invalid_api_key":
			if scope == "" || scope == "key" {
				return FailureInvalidKey
			}
		case "key_rate_limited", "key_cooldown":
			if scope == "" || scope == "key" {
				return FailureKeyRateLimited
			}
		case "provider_busy", "global_busy", "provider_overloaded":
			if scope == "" || scope == "provider" {
				return FailureProviderBusy
			}
		}
	}

	// Safety-first generic behavior: unknown 429/502/504/network outcomes are
	// ambiguous. Provider-specific profiles may classify exact evidence, but the
	// universal core never guesses that another key is safe to spend.
	switch resp.StatusCode {
	case 429, 502, 504:
		return FailureAmbiguous
	case 503, 529:
		return FailureProviderBusy
	default:
		return FailureNonRetryable
	}
}
