package engineruntime

import (
	"context"
	"errors"

	"keydeck.local/feasibilitylab/internal/session"
)

// SessionEngineAdapter is a thin compatibility wrapper for existing KeyDeck
// engines. Official deep integrations remain intact; this wrapper only exposes
// them through the engine-neutral lifecycle boundary.
type SessionEngineAdapter struct {
	Engine               session.Engine
	DeclaredCapabilities []Capability
	HealthCheck          func(context.Context) (Health, error)
	CancelFunc           func(context.Context, Binding) error
}

func (a *SessionEngineAdapter) ID() string {
	if a.Engine == nil {
		return ""
	}
	return a.Engine.Name()
}

func (a *SessionEngineAdapter) Capabilities(context.Context) ([]Capability, error) {
	if a.Engine == nil {
		return nil, errors.New("session engine is required")
	}
	return normalizedCapabilities(a.DeclaredCapabilities), nil
}

func (a *SessionEngineAdapter) Health(ctx context.Context) (Health, error) {
	if a.Engine == nil {
		return Health{State: HealthUnhealthy, Detail: "session engine is missing"}, nil
	}
	if a.HealthCheck != nil {
		return a.HealthCheck(ctx)
	}
	return Health{State: HealthHealthy}, nil
}

func (a *SessionEngineAdapter) Start(ctx context.Context, req Request) (Outcome, error) {
	return a.run(ctx, req)
}

func (a *SessionEngineAdapter) Continue(ctx context.Context, req Request) (Outcome, error) {
	return a.run(ctx, req)
}

func (a *SessionEngineAdapter) Resume(ctx context.Context, req Request) (Outcome, error) {
	if req.Binding == nil || req.Binding.ExternalHandle == "" {
		return Outcome{}, errors.New("resume requires a durable external binding")
	}
	stateful, ok := a.Engine.(session.StatefulEngine)
	if !ok {
		return Outcome{Disposition: DispositionInputRequired, Detail: "wrapped engine does not support durable binding restore"}, nil
	}
	if err := stateful.RestoreBinding(session.EngineBinding{Engine: a.Engine.Name(), ExternalThreadID: req.Binding.ExternalHandle}); err != nil {
		return Outcome{}, err
	}
	return a.run(ctx, req)
}

func (a *SessionEngineAdapter) Cancel(ctx context.Context, binding Binding) error {
	if a.CancelFunc == nil {
		return errors.New("wrapped engine does not implement cancellation")
	}
	return a.CancelFunc(ctx, binding)
}

func (a *SessionEngineAdapter) run(ctx context.Context, req Request) (Outcome, error) {
	if a.Engine == nil {
		return Outcome{}, errors.New("session engine is required")
	}
	result, err := a.Engine.Run(ctx, req.Passport, req.Prompt)
	if err != nil {
		return Outcome{}, err
	}
	outcome := Outcome{Disposition: DispositionCompleted, Result: result}
	if stateful, ok := a.Engine.(session.StatefulEngine); ok {
		binding := stateful.CurrentBinding()
		if binding.ExternalThreadID != "" {
			outcome.ExternalHandle = binding.ExternalThreadID
			outcome.Resumable = hasCapability(a.DeclaredCapabilities, CapabilityResume)
		}
	}
	return outcome, nil
}
