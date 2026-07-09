package session

import (
	"context"
	"errors"
	"time"
)

type EngineResult struct {
	Text             string
	Decisions        []string
	CompletedActions []string
	PendingTasks     []string
	RelevantFiles    []string
	Checkpoint       string
}

type Engine interface {
	Name() string
	Run(ctx context.Context, passport Passport, userPrompt string) (EngineResult, error)
}

type StatefulEngine interface {
	Engine
	RestoreBinding(EngineBinding) error
	CurrentBinding() EngineBinding
}

type Orchestrator struct {
	State State
}

// Run records one user message, runs one engine, and commits the engine result
// into the single canonical KeyDeck session.
func (o *Orchestrator) Run(ctx context.Context, engine Engine, prompt, reason string) (Passport, EngineResult, error) {
	if engine == nil {
		return Passport{}, EngineResult{}, errors.New("engine is required")
	}
	from := o.State.ActiveEngine
	if err := o.restoreBinding(engine); err != nil {
		return Passport{}, EngineResult{}, err
	}
	o.State.Transcript = append(o.State.Transcript, Message{At: time.Now().UTC(), Role: RoleUser, Text: prompt})
	passport := o.passport(from, engine.Name(), reason)
	result, err := engine.Run(ctx, passport, prompt)
	if err != nil {
		return passport, EngineResult{}, err
	}
	o.commitEngineResult(engine, result)
	return passport, result, nil
}

// RunWithFallback records the user's task once, tries the primary engine, and
// only if shouldFallback explicitly approves the observed error does it hand
// the same canonical task to the fallback engine. This prevents duplicate user
// turns during automatic API-pool -> agent handoff.

// BeginInterruptible records the user task once and runs the selected engine.
// If the engine returns a PartialResultError, KeyDeck stages the model-agnostic
// continuation state durably without committing a partial assistant message.
// A later engine can resume the same visible response through ContinueInFlight.
func (o *Orchestrator) BeginInterruptible(ctx context.Context, engine Engine, prompt, reason string) (Passport, EngineResult, error) {
	if engine == nil {
		return Passport{}, EngineResult{}, errors.New("engine is required")
	}
	if o.State.InFlightResponse != nil {
		return Passport{}, EngineResult{}, errors.New("an in-flight response already exists")
	}
	from := o.State.ActiveEngine
	if err := o.restoreBinding(engine); err != nil {
		return Passport{}, EngineResult{}, err
	}
	o.State.Transcript = append(o.State.Transcript, Message{At: time.Now().UTC(), Role: RoleUser, Text: prompt})
	passport := o.passport(from, engine.Name(), reason)
	result, err := engine.Run(ctx, passport, prompt)
	if err == nil {
		o.commitEngineResult(engine, result)
		return passport, result, nil
	}
	partial, ok := AsPartialResult(err)
	if !ok {
		return passport, EngineResult{}, err
	}
	continuation := partial.Continuation
	if continuation.OriginalPrompt == "" {
		continuation.OriginalPrompt = prompt
	}
	if continuation.SourceEngine == "" {
		continuation.SourceEngine = engine.Name()
	}
	now := time.Now().UTC()
	o.State.InFlightResponse = &InFlightResponse{
		ResponseID:   "response-" + now.Format("20060102T150405.000000000Z"),
		Status:       InFlightAwaitingHandoff,
		Continuation: continuation,
		StartedAt:    now,
		UpdatedAt:    now,
	}
	return passport, partial.Partial, err
}

// ContinueInFlight hands one already-started canonical response to another
// engine. Confirmed visible output is kept exactly once; the unstable fragment
// is context only and is never committed directly to the transcript.
func (o *Orchestrator) ContinueInFlight(ctx context.Context, engine Engine, reason string) (Passport, EngineResult, error) {
	if engine == nil {
		return Passport{}, EngineResult{}, errors.New("engine is required")
	}
	if o.State.InFlightResponse == nil {
		return Passport{}, EngineResult{}, errors.New("no in-flight response exists")
	}
	if o.State.InFlightResponse.Status != InFlightAwaitingHandoff {
		return Passport{}, EngineResult{}, errors.New("in-flight response is not awaiting handoff")
	}
	if err := o.restoreBinding(engine); err != nil {
		return Passport{}, EngineResult{}, err
	}
	from := o.State.InFlightResponse.Continuation.SourceEngine
	passport := o.passport(from, engine.Name(), reason)
	continuation := o.State.InFlightResponse.Continuation
	passport.Continuation = &continuation
	result, err := engine.Run(ctx, passport, continuation.OriginalPrompt)
	if err != nil {
		return passport, EngineResult{}, err
	}
	result.Text = mergeContinuationOutput(continuation.ConfirmedOutput, result.Text)
	o.commitEngineResult(engine, result)
	o.State.InFlightResponse = nil
	return passport, result, nil
}

func (o *Orchestrator) RunWithFallback(
	ctx context.Context,
	primary Engine,
	fallback Engine,
	prompt string,
	primaryReason string,
	fallbackReason string,
	shouldFallback func(error) bool,
) (Passport, EngineResult, error) {
	if primary == nil || fallback == nil {
		return Passport{}, EngineResult{}, errors.New("primary and fallback engines are required")
	}
	if shouldFallback == nil {
		return Passport{}, EngineResult{}, errors.New("fallback policy is required")
	}
	from := o.State.ActiveEngine
	if err := o.restoreBinding(primary); err != nil {
		return Passport{}, EngineResult{}, err
	}
	o.State.Transcript = append(o.State.Transcript, Message{At: time.Now().UTC(), Role: RoleUser, Text: prompt})

	primaryPassport := o.passport(from, primary.Name(), primaryReason)
	primaryResult, err := primary.Run(ctx, primaryPassport, prompt)
	if err == nil {
		o.commitEngineResult(primary, primaryResult)
		return primaryPassport, primaryResult, nil
	}
	if !shouldFallback(err) {
		return primaryPassport, EngineResult{}, err
	}

	if err := o.restoreBinding(fallback); err != nil {
		return Passport{}, EngineResult{}, err
	}
	fallbackPassport := o.passport(from, fallback.Name(), fallbackReason)
	fallbackResult, fallbackErr := fallback.Run(ctx, fallbackPassport, prompt)
	if fallbackErr != nil {
		return fallbackPassport, EngineResult{}, fallbackErr
	}
	o.commitEngineResult(fallback, fallbackResult)
	return fallbackPassport, fallbackResult, nil
}

func (o *Orchestrator) restoreBinding(engine Engine) error {
	if stateful, ok := engine.(StatefulEngine); ok {
		if binding, exists := o.State.EngineBindings[engine.Name()]; exists {
			if err := stateful.RestoreBinding(binding); err != nil {
				return err
			}
		}
	}
	return nil
}

func (o *Orchestrator) commitEngineResult(engine Engine, result EngineResult) {
	o.State.ActiveEngine = engine.Name()
	o.State.Transcript = append(o.State.Transcript, Message{At: time.Now().UTC(), Role: RoleAssistant, Engine: engine.Name(), Text: result.Text})
	for _, d := range result.Decisions {
		o.State.Decisions = append(o.State.Decisions, Decision{At: time.Now().UTC(), Summary: d, Source: engine.Name()})
	}
	for _, a := range result.CompletedActions {
		o.State.CompletedActions = append(o.State.CompletedActions, Action{At: time.Now().UTC(), Summary: a, Source: engine.Name()})
	}
	if result.PendingTasks != nil {
		o.State.PendingTasks = append([]string(nil), result.PendingTasks...)
	}
	if result.RelevantFiles != nil {
		o.State.RelevantFiles = mergeStrings(o.State.RelevantFiles, result.RelevantFiles)
	}
	if result.Checkpoint != "" {
		o.State.Checkpoint = result.Checkpoint
	}
	if stateful, ok := engine.(StatefulEngine); ok {
		if o.State.EngineBindings == nil {
			o.State.EngineBindings = map[string]EngineBinding{}
		}
		binding := stateful.CurrentBinding()
		binding.Engine = engine.Name()
		binding.UpdatedAt = time.Now().UTC()
		o.State.EngineBindings[engine.Name()] = binding
	}
}

func (o *Orchestrator) passport(from, to, reason string) Passport {
	return Passport{
		SessionID:        o.State.SessionID,
		ProjectRoot:      o.State.ProjectRoot,
		Goal:             o.State.Goal,
		FromEngine:       from,
		ToEngine:         to,
		HandoffReason:    reason,
		Transcript:       append([]Message(nil), o.State.Transcript...),
		Decisions:        append([]Decision(nil), o.State.Decisions...),
		CompletedActions: append([]Action(nil), o.State.CompletedActions...),
		PendingTasks:     append([]string(nil), o.State.PendingTasks...),
		RelevantFiles:    append([]string(nil), o.State.RelevantFiles...),
		Checkpoint:       o.State.Checkpoint,
	}
}

func mergeStrings(existing, incoming []string) []string {
	seen := make(map[string]bool, len(existing)+len(incoming))
	out := make([]string, 0, len(existing)+len(incoming))
	for _, value := range append(existing, incoming...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
