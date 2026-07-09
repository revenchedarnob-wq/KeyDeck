package conformance

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

var ErrInvalidProfile = errors.New("invalid provider conformance profile")

// FailureRule is an exact, evidence-backed provider behavior mapping. Safe key
// rotation classes require an explicit key scope. Provider-wide busy requires
// an explicit provider scope.
type FailureRule struct {
	StatusCode int               `json:"status_code"`
	ErrorCode  string            `json:"error_code"`
	ErrorScope string            `json:"error_scope"`
	Class      pool.FailureClass `json:"class"`
}

// ProviderProfile is versioned and dated so KeyDeck never treats provider
// behavior as timeless or universal.
type ProviderProfile struct {
	Provider   string        `json:"provider"`
	Version    string        `json:"version"`
	TestedAt   time.Time     `json:"tested_at"`
	EvidenceID string        `json:"evidence_id"`
	Rules      []FailureRule `json:"rules"`
}

func (p ProviderProfile) Validate() error {
	if strings.TrimSpace(p.Provider) == "" || strings.TrimSpace(p.Version) == "" || p.TestedAt.IsZero() || strings.TrimSpace(p.EvidenceID) == "" {
		return fmt.Errorf("%w: provider, version, test date and evidence id are required", ErrInvalidProfile)
	}
	for i, rule := range p.Rules {
		if rule.StatusCode <= 0 || strings.TrimSpace(rule.ErrorCode) == "" || strings.TrimSpace(rule.ErrorScope) == "" {
			return fmt.Errorf("%w: rule %d requires exact status, error code and scope", ErrInvalidProfile, i)
		}
		switch rule.Class {
		case pool.FailureKeyExhausted, pool.FailureInvalidKey, pool.FailureKeyRateLimited:
			if !strings.EqualFold(strings.TrimSpace(rule.ErrorScope), "key") {
				return fmt.Errorf("%w: rule %d key-scoped class requires scope=key", ErrInvalidProfile, i)
			}
		case pool.FailureProviderBusy:
			if !strings.EqualFold(strings.TrimSpace(rule.ErrorScope), "provider") {
				return fmt.Errorf("%w: rule %d provider busy requires scope=provider", ErrInvalidProfile, i)
			}
		case pool.FailureAmbiguous:
			// Exact evidence may explicitly force a conservative stop.
		default:
			return fmt.Errorf("%w: rule %d uses unsupported class %q", ErrInvalidProfile, i, rule.Class)
		}
	}
	return nil
}

// Classify implements pool.FailureClassifier. Only exact rules from a valid,
// versioned evidence profile may upgrade an unknown response into a safe key
// rotation class. Everything else falls back to KeyDeck's conservative core.
func (p ProviderProfile) Classify(resp providerhttp.Response, transportErr error) pool.FailureClass {
	if transportErr != nil {
		return pool.FailureAmbiguous
	}
	if err := p.Validate(); err == nil {
		env, decodeErr := protocol.DecodeEnvelope(resp.Body)
		if decodeErr == nil && env.Error != nil {
			code := strings.TrimSpace(env.Error.Code)
			scope := strings.TrimSpace(env.Error.Scope)
			for _, rule := range p.Rules {
				if resp.StatusCode == rule.StatusCode && strings.EqualFold(code, strings.TrimSpace(rule.ErrorCode)) && strings.EqualFold(scope, strings.TrimSpace(rule.ErrorScope)) {
					return rule.Class
				}
			}
		}
	}
	return (pool.ConservativeClassifier{}).Classify(resp, transportErr)
}
