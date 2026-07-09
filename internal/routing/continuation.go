package routing

import "strings"

func PlanContinuation(responseID string, failure FailureClass, from, to Plan) (ContinuationPlan, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" || from.RouteID == "" || to.RouteID == "" || from.TaskID != to.TaskID || from.SessionID != to.SessionID {
		return ContinuationPlan{}, ErrContinuationDenied
	}
	switch failure {
	case FailureKeyExhausted, FailureProviderBusy, FailureAmbiguousTransport, FailureEngineInterrupted:
	default:
		return ContinuationPlan{}, ErrContinuationDenied
	}
	if from.RouteID == to.RouteID || from.SelectedEngineID == to.SelectedEngineID {
		return ContinuationPlan{}, ErrContinuationDenied
	}
	if failure == FailureProviderBusy && from.SelectedProviderID == to.SelectedProviderID {
		return ContinuationPlan{}, ErrProviderBusySameProvider
	}
	p := ContinuationPlan{
		Version:         1,
		ResponseID:      responseID,
		TaskID:          from.TaskID,
		SessionID:       from.SessionID,
		FailureClass:    failure,
		FromRouteID:     from.RouteID,
		FromRouteSHA256: from.RouteSHA256,
		ToRouteID:       to.RouteID,
		ToRouteSHA256:   to.RouteSHA256,
		FromEngineID:    from.SelectedEngineID,
		FromProviderID:  from.SelectedProviderID,
		ToEngineID:      to.SelectedEngineID,
		ToProviderID:    to.SelectedProviderID,
	}
	p.SHA256 = continuationDigest(p)
	p.ContinuationID = "continuation-" + p.SHA256[:20]
	return p, nil
}

func ValidateContinuation(p ContinuationPlan, from, to Plan) error {
	if p.Version != 1 || p.ContinuationID == "" || p.SHA256 == "" || p.SHA256 != continuationDigest(p) || p.ContinuationID != "continuation-"+p.SHA256[:20] {
		return ErrContinuationDenied
	}
	expected, err := PlanContinuation(p.ResponseID, p.FailureClass, from, to)
	if err != nil {
		return err
	}
	if expected.SHA256 != p.SHA256 {
		return ErrContinuationDenied
	}
	return nil
}
