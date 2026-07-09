package engineruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/timeline"
)

type Runtime struct {
	store    *Store
	engines  *recovery.EngineStore
	timeline *timeline.Store
}

func New(store *Store, engines *recovery.EngineStore, activity *timeline.Store) (*Runtime, error) {
	if store == nil || engines == nil || activity == nil {
		return nil, errors.New("runtime store, engine ledger and timeline are required")
	}
	return &Runtime{store: store, engines: engines, timeline: activity}, nil
}

func (r *Runtime) Store() *Store { return r.store }

func (r *Runtime) Invoke(ctx context.Context, adapter Adapter, operation Operation, req Request) (Result, error) {
	if adapter == nil {
		return Result{}, errors.New("engine adapter is required")
	}
	if req.ExecutionID == "" || req.TaskID == "" || req.SessionID == "" || req.EngineID == "" {
		return Result{}, errors.New("execution, task, session and engine ids are required")
	}
	if req.EngineID != adapter.ID() {
		return Result{}, fmt.Errorf("request engine %q does not match adapter %q", req.EngineID, adapter.ID())
	}
	if operation != OperationStart && operation != OperationContinue && operation != OperationResume {
		return Result{}, fmt.Errorf("unsupported runtime operation %q", operation)
	}

	if existing := r.store.Execution(req.ExecutionID); existing.ExecutionID != "" {
		reconciled, handled, err := r.handleExisting(existing, operation)
		if err != nil {
			return Result{}, err
		}
		if handled {
			return reconciled, nil
		}
	} else {
		if operation == OperationResume {
			return Result{}, errors.New("resume requires an existing runtime execution")
		}
		if _, _, err := r.store.BeginOnce(Execution{
			ExecutionID: req.ExecutionID,
			TaskID:      req.TaskID,
			SessionID:   req.SessionID,
			EngineID:    req.EngineID,
			Operation:   operation,
		}); err != nil {
			return Result{}, err
		}
	}

	capabilities, err := adapter.Capabilities(ctx)
	if err != nil {
		return r.failWithoutInvocation(req.ExecutionID, "capability probe failed: "+err.Error())
	}
	capabilities = normalizedCapabilities(capabilities)
	if missing := missingCapabilities(capabilities, req.RequiredCapabilities); len(missing) > 0 {
		return r.failWithoutInvocation(req.ExecutionID, "missing required capabilities: "+strings.Join(missing, ", "))
	}
	health, err := adapter.Health(ctx)
	if err != nil {
		return r.failWithoutInvocation(req.ExecutionID, "health probe failed: "+err.Error())
	}
	if health.State == HealthUnhealthy || health.State == "" {
		detail := strings.TrimSpace(health.Detail)
		if detail == "" {
			detail = "engine is unhealthy"
		}
		return r.failWithoutInvocation(req.ExecutionID, detail)
	}

	if operation == OperationResume {
		binding, ok := r.store.BindingForExecution(req.ExecutionID)
		if !ok || !binding.ResumeSupported || binding.ExternalHandle == "" {
			return r.transition(req.ExecutionID, DispositionInputRequired, "", "", "durable resume binding is unavailable", false)
		}
		req.Binding = &binding
	}

	var outcome Outcome
	switch operation {
	case OperationStart:
		outcome, err = adapter.Start(ctx, req)
	case OperationContinue:
		outcome, err = adapter.Continue(ctx, req)
	case OperationResume:
		outcome, err = adapter.Resume(ctx, req)
	}
	if err != nil {
		result, transitionErr := r.transition(req.ExecutionID, DispositionFailed, "", "", err.Error(), true)
		if transitionErr != nil {
			return Result{}, transitionErr
		}
		return result, nil
	}
	return r.persistOutcome(req, capabilities, outcome)
}

func (r *Runtime) Cancel(ctx context.Context, adapter Adapter, executionID string) (Result, error) {
	if adapter == nil {
		return Result{}, errors.New("engine adapter is required")
	}
	execution := r.store.Execution(executionID)
	if execution.ExecutionID == "" {
		return Result{}, errors.New("runtime execution not found")
	}
	if execution.EngineID != adapter.ID() {
		return Result{}, errors.New("adapter does not own runtime execution")
	}
	if terminal(execution.Disposition) {
		return r.snapshot(execution, false), nil
	}
	capabilities, err := adapter.Capabilities(ctx)
	if err != nil {
		return Result{}, err
	}
	if !hasCapability(capabilities, CapabilityCancel) {
		return r.transition(executionID, DispositionInputRequired, execution.BindingID, execution.ResultID, "engine does not support cancellation", false)
	}
	binding, _ := r.store.BindingForExecution(executionID)
	if err := adapter.Cancel(ctx, binding); err != nil {
		return r.transition(executionID, DispositionFailed, execution.BindingID, execution.ResultID, "cancel failed: "+err.Error(), true)
	}
	return r.transition(executionID, DispositionCancelled, execution.BindingID, execution.ResultID, "engine execution cancelled", true)
}

func (r *Runtime) handleExisting(existing Execution, operation Operation) (Result, bool, error) {
	if terminal(existing.Disposition) || existing.Disposition == DispositionInputRequired {
		return r.snapshot(existing, false), true, nil
	}
	if existing.Disposition == DispositionResumeRequired {
		if operation == OperationResume {
			return Result{}, false, nil
		}
		return r.snapshot(existing, false), true, nil
	}
	if existing.Disposition != DispositionRunning {
		return Result{}, false, fmt.Errorf("unknown runtime disposition %q", existing.Disposition)
	}

	resultID := deterministicResultID(existing.ExecutionID)
	if durable := r.engines.Result(resultID); durable.ResultID != "" {
		updated, _, err := r.store.SetDispositionOnce(existing.ExecutionID, DispositionCompleted, existing.BindingID, resultID, "durable result already exists; runtime state reconciled without replay")
		if err != nil {
			return Result{}, true, err
		}
		if err := r.record(updated, "runtime_reconciled", "Durable engine result found after restart; adapter was not replayed"); err != nil {
			return Result{}, true, err
		}
		return r.snapshot(updated, false), true, nil
	}
	if binding, ok := r.store.BindingForExecution(existing.ExecutionID); ok && binding.ResumeSupported && binding.ExternalHandle != "" {
		updated, _, err := r.store.SetDispositionOnce(existing.ExecutionID, DispositionResumeRequired, binding.BindingID, "", "interrupted runtime execution has a durable resume binding")
		if err != nil {
			return Result{}, true, err
		}
		if err := r.record(updated, "runtime_reconciled", "Interrupted execution recovered as resume_required without replay"); err != nil {
			return Result{}, true, err
		}
		return r.snapshot(updated, false), true, nil
	}
	updated, _, err := r.store.SetDispositionOnce(existing.ExecutionID, DispositionInputRequired, "", "", "interrupted runtime execution has no durable resume path; replay is unsafe")
	if err != nil {
		return Result{}, true, err
	}
	if err := r.record(updated, "runtime_reconciled", "Interrupted non-resumable execution recovered as input_required without replay"); err != nil {
		return Result{}, true, err
	}
	return r.snapshot(updated, false), true, nil
}

func (r *Runtime) persistOutcome(req Request, capabilities []Capability, outcome Outcome) (Result, error) {
	if outcome.Disposition == "" {
		return r.transition(req.ExecutionID, DispositionFailed, "", "", "adapter returned no disposition", true)
	}
	bindingID := ""
	if outcome.ExternalHandle != "" {
		binding := Binding{
			BindingID:       "runtime-binding-" + req.ExecutionID,
			TaskID:          req.TaskID,
			SessionID:       req.SessionID,
			ExecutionID:     req.ExecutionID,
			EngineID:        req.EngineID,
			ExternalHandle:  outcome.ExternalHandle,
			ResumeSupported: outcome.Resumable,
			Capabilities:    capabilities,
		}
		saved, _, err := r.store.SaveBindingOnce(binding)
		if err != nil {
			return Result{}, err
		}
		bindingID = saved.BindingID
	}

	switch outcome.Disposition {
	case DispositionCompleted:
		execution := recovery.Execution{
			ExecutionID:      req.ExecutionID,
			TaskID:           req.TaskID,
			SessionID:        req.SessionID,
			Engine:           req.EngineID,
			Resumable:        outcome.Resumable,
			ExternalThreadID: outcome.ExternalHandle,
		}
		if err := r.ensureRecoveryExecution(execution); err != nil {
			return Result{}, err
		}
		resultID := deterministicResultID(req.ExecutionID)
		if _, _, err := r.engines.CompleteResultOnce(recovery.Result{
			ResultID:         resultID,
			ExecutionID:      req.ExecutionID,
			TaskID:           req.TaskID,
			SessionID:        req.SessionID,
			Engine:           req.EngineID,
			ExternalThreadID: outcome.ExternalHandle,
			Output:           outcome.Result,
		}); err != nil {
			return Result{}, err
		}
		return r.transition(req.ExecutionID, DispositionCompleted, bindingID, resultID, outcome.Detail, true)
	case DispositionResumeRequired:
		if !outcome.Resumable || outcome.ExternalHandle == "" || bindingID == "" {
			return r.transition(req.ExecutionID, DispositionFailed, bindingID, "", "resume_required outcome must provide a durable resumable handle", true)
		}
		if err := r.ensureRecoveryExecution(recovery.Execution{
			ExecutionID:      req.ExecutionID,
			TaskID:           req.TaskID,
			SessionID:        req.SessionID,
			Engine:           req.EngineID,
			Resumable:        true,
			ExternalThreadID: outcome.ExternalHandle,
		}); err != nil {
			return Result{}, err
		}
		return r.transition(req.ExecutionID, DispositionResumeRequired, bindingID, "", outcome.Detail, true)
	case DispositionInputRequired:
		return r.transition(req.ExecutionID, DispositionInputRequired, bindingID, "", outcome.Detail, true)
	case DispositionFailed:
		return r.transition(req.ExecutionID, DispositionFailed, bindingID, "", outcome.Detail, true)
	case DispositionCancelled:
		return r.transition(req.ExecutionID, DispositionCancelled, bindingID, "", outcome.Detail, true)
	default:
		return r.transition(req.ExecutionID, DispositionFailed, bindingID, "", "adapter returned unsupported disposition "+string(outcome.Disposition), true)
	}
}

func (r *Runtime) ensureRecoveryExecution(wanted recovery.Execution) error {
	existing := r.engines.Execution(wanted.ExecutionID)
	if existing.ExecutionID == "" {
		_, _, err := r.engines.StartOnce(wanted)
		return err
	}
	if existing.TaskID != wanted.TaskID || existing.SessionID != wanted.SessionID || existing.Engine != wanted.Engine {
		return errors.New("runtime execution conflicts with durable recovery execution")
	}
	if wanted.Resumable && (!existing.Resumable || existing.ExternalThreadID != wanted.ExternalThreadID) {
		return errors.New("runtime resume binding conflicts with durable recovery execution")
	}
	return nil
}

func (r *Runtime) failWithoutInvocation(executionID, detail string) (Result, error) {
	return r.transition(executionID, DispositionFailed, "", "", detail, false)
}

func (r *Runtime) transition(executionID string, disposition Disposition, bindingID, resultID, detail string, invoked bool) (Result, error) {
	updated, _, err := r.store.SetDispositionOnce(executionID, disposition, bindingID, resultID, detail)
	if err != nil {
		return Result{}, err
	}
	if err := r.record(updated, "runtime_disposition", detail); err != nil {
		return Result{}, err
	}
	result := r.snapshot(updated, invoked)
	return result, nil
}

func (r *Runtime) snapshot(execution Execution, invoked bool) Result {
	result := Result{Execution: execution, AdapterInvoked: invoked}
	if binding, ok := r.store.BindingForExecution(execution.ExecutionID); ok {
		copy := binding
		result.Binding = &copy
	}
	return result
}

func (r *Runtime) record(execution Execution, kind, summary string) error {
	data := strings.Join([]string{execution.ExecutionID, execution.EngineID, string(execution.Operation), string(execution.Disposition), execution.BindingID, execution.ResultID, execution.Detail}, "|")
	sum := sha256.Sum256([]byte(data))
	dataHash := hex.EncodeToString(sum[:])
	eventID := "runtime-" + execution.ExecutionID + "-" + kind + "-" + string(execution.Disposition)
	_, _, err := r.timeline.AppendOnce(timeline.Input{
		EventID:   eventID,
		TaskID:    execution.TaskID,
		SessionID: execution.SessionID,
		Domain:    timeline.DomainEngine,
		Kind:      kind,
		SourceRef: execution.ExecutionID,
		Summary:   summary,
		DataHash:  dataHash,
	})
	return err
}

func deterministicResultID(executionID string) string {
	return "runtime-result-" + executionID
}

func terminal(disposition Disposition) bool {
	return disposition == DispositionCompleted || disposition == DispositionFailed || disposition == DispositionCancelled
}

func normalizedCapabilities(values []Capability) []Capability {
	seen := map[Capability]bool{}
	out := make([]Capability, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func hasCapability(values []Capability, wanted Capability) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func missingCapabilities(have, required []Capability) []string {
	missing := []string{}
	for _, wanted := range normalizedCapabilities(required) {
		if !hasCapability(have, wanted) {
			missing = append(missing, string(wanted))
		}
	}
	return missing
}
