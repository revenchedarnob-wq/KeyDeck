package routing

import (
	"context"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/handoff"
)

func ValidatePackage(plan Plan, p handoff.Package) error {
	if plan.TaskID != p.Task.TaskID || plan.SessionID != p.Task.SessionID || plan.SelectedEngineID != p.EngineRequest.EngineID {
		return ErrRoutePackageMismatch
	}
	if plan.HandoffPackageID != "" && (plan.HandoffPackageID != p.PackageID || plan.HandoffPackageSHA256 != p.PackageSHA256) {
		return ErrRoutePackageMismatch
	}
	return nil
}

type CurrentResolver func() (Requirements, []Candidate)

type Executor struct {
	Handoff *handoff.Executor
	Current CurrentResolver
}

func (e *Executor) Execute(ctx context.Context, adapter engineruntime.Adapter, p handoff.Package, plan Plan) (engineruntime.Result, error) {
	if e == nil || e.Handoff == nil || e.Current == nil {
		return engineruntime.Result{}, ErrInvalidPlan
	}
	req, candidates := e.Current()
	if err := Validate(plan, req, candidates); err != nil {
		return engineruntime.Result{}, err
	}
	if err := ValidatePackage(plan, p); err != nil {
		return engineruntime.Result{}, err
	}
	return e.Handoff.Execute(ctx, adapter, p)
}
