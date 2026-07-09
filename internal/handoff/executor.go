package handoff

import (
	"context"
	"errors"

	"keydeck.local/feasibilitylab/internal/engineruntime"
)

var ErrPackageCancelled = errors.New("handoff package is cancelled")

type StateResolver func() CurrentState
type Executor struct {
	Store                *Store
	Runtime              *engineruntime.Runtime
	Current              StateResolver
	ForbiddenExactValues []string
}

func (e *Executor) Execute(ctx context.Context, adapter engineruntime.Adapter, p Package) (engineruntime.Result, error) {
	if e == nil || e.Store == nil || e.Runtime == nil || e.Current == nil {
		return engineruntime.Result{}, errors.New("handoff executor not configured")
	}
	if err := Validate(p, e.Current(), e.ForbiddenExactValues); err != nil {
		return engineruntime.Result{}, err
	}
	saved, _, err := e.Store.SaveOnce(p)
	if err != nil {
		return engineruntime.Result{}, err
	}
	if e.Store.Cancelled(saved.PackageID) {
		return engineruntime.Result{}, ErrPackageCancelled
	}
	if _, _, err = e.Store.BindExecutionOnce(saved.PackageID, saved.EngineRequest.ExecutionID); err != nil {
		return engineruntime.Result{}, err
	}
	existing := e.Runtime.Store().Execution(saved.EngineRequest.ExecutionID)
	op := engineruntime.OperationStart
	if existing.Disposition == engineruntime.DispositionResumeRequired {
		op = engineruntime.OperationResume
	}
	return e.Runtime.Invoke(ctx, adapter, op, saved.EngineRequest)
}
func (e *Executor) Cancel(ctx context.Context, adapter engineruntime.Adapter, p Package) (engineruntime.Result, error) {
	if err := Validate(p, e.Current(), e.ForbiddenExactValues); err != nil {
		return engineruntime.Result{}, err
	}
	if _, _, err := e.Store.SaveOnce(p); err != nil {
		return engineruntime.Result{}, err
	}
	if _, _, err := e.Store.BindExecutionOnce(p.PackageID, p.EngineRequest.ExecutionID); err != nil {
		return engineruntime.Result{}, err
	}
	if _, err := e.Store.CancelOnce(p.PackageID); err != nil {
		return engineruntime.Result{}, err
	}
	existing := e.Runtime.Store().Execution(p.EngineRequest.ExecutionID)
	if existing.ExecutionID == "" {
		return engineruntime.Result{}, nil
	}
	return e.Runtime.Cancel(ctx, adapter, p.EngineRequest.ExecutionID)
}
