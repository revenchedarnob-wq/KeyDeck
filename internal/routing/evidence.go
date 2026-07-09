package routing

import (
	"encoding/json"

	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/timeline"
)

func Artifact(plan Plan) proofreceipt.Artifact {
	raw, _ := json.Marshal(plan)
	return proofreceipt.Artifact{Name: "route plan", Path: "keydeck://routing/" + plan.RouteID, SHA256: plan.RouteSHA256, Size: int64(len(raw))}
}

func RecordPlan(store *timeline.Store, plan Plan) (timeline.Event, bool, error) {
	return store.AppendOnce(timeline.Input{
		EventID:   "routing-" + plan.RouteID,
		TaskID:    plan.TaskID,
		SessionID: plan.SessionID,
		Domain:    timeline.DomainEngine,
		Kind:      "route_selected",
		SourceRef: plan.RouteID,
		Summary:   "deterministic qualified route selected",
		DataHash:  plan.RouteSHA256,
	})
}
