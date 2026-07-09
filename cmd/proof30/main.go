package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/handoff"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/routing"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}
type report struct {
	Proof          string     `json:"proof"`
	Status         string     `json:"status"`
	Passed         bool       `json:"passed"`
	Scenarios      []scenario `json:"scenarios"`
	RouteID        string     `json:"route_id"`
	RouteSHA256    string     `json:"route_sha256"`
	ContinuationID string     `json:"continuation_id"`
	ReceiptID      string     `json:"receipt_id"`
	NextGate       string     `json:"next_gate"`
}

func req(task, sessionID string) routing.Requirements {
	return routing.Requirements{TaskID: task, SessionID: sessionID, RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
}
func candidates() []routing.Candidate {
	return []routing.Candidate{
		{EngineID: "engine-b", ProviderID: "provider-b", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 80, EvidenceRefs: []string{"proof-b"}},
		{EngineID: "engine-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText, engineruntime.CapabilityResume}, EvidenceScore: 90, EvidenceRefs: []string{"proof-a"}},
	}
}

type adapter struct {
	id     string
	starts int
}

func (a *adapter) ID() string { return a.id }
func (a *adapter) Capabilities(context.Context) ([]engineruntime.Capability, error) {
	return []engineruntime.Capability{engineruntime.CapabilityText}, nil
}
func (a *adapter) Health(context.Context) (engineruntime.Health, error) {
	return engineruntime.Health{State: engineruntime.HealthHealthy}, nil
}
func (a *adapter) Start(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	a.starts++
	return engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "route-bound result"}}, nil
}
func (a *adapter) Continue(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	return engineruntime.Outcome{}, errors.New("unused")
}
func (a *adapter) Resume(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	return engineruntime.Outcome{}, errors.New("unused")
}
func (a *adapter) Cancel(context.Context, engineruntime.Binding) error { return nil }

type execFixture struct {
	rx              *routing.Executor
	pkg             handoff.Package
	plan            routing.Plan
	ad              *adapter
	routeReq        routing.Requirements
	routeCandidates []routing.Candidate
}

func newExecFixture(root string) (*execFixture, error) {
	state := tasks.State{TaskID: "proof30-task-exec", SessionID: "proof30-session-exec", Status: tasks.StatusWorking, Contract: tasks.Contract{Goal: "execute only the current qualified route", Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "route proof"}}}, LastSequence: 1}
	brain := projectbrain.Revision{ProjectID: "proof30-project", SessionID: state.SessionID, ProjectFingerprint: "proof30-fp", RevisionSHA256: strings.Repeat("b", 64), Context: projectbrain.ContextInspection{PacketID: "proof30-packet", PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "proof30-fp", InspectionSHA256: strings.Repeat("c", 64)}}
	passport := session.Passport{SessionID: state.SessionID, ProjectRoot: root, Goal: state.Contract.Goal, FromEngine: "api", ToEngine: "engine-a", HandoffReason: "proof30-route"}
	pkg, err := handoff.Assemble(handoff.Input{Task: state, ContextPacketID: brain.Context.PacketID, ContextPacketSHA256: brain.Context.PacketSHA256, MCPServerID: "mcp-proof30", MCPSchemaSHA256: strings.Repeat("d", 64), ProjectSourceFingerprint: brain.ProjectFingerprint, Brain: brain, Passport: passport, EngineID: "engine-a", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}})
	if err != nil {
		return nil, err
	}
	r := req(state.TaskID, state.SessionID)
	r.HandoffPackageID = pkg.PackageID
	r.HandoffPackageSHA256 = pkg.PackageSHA256
	cs := []routing.Candidate{{EngineID: "engine-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 100, EvidenceRefs: []string{"proof-route"}}}
	plan, err := routing.Select(r, cs)
	if err != nil {
		return nil, err
	}
	hs, err := handoff.OpenStore(filepath.Join(root, "handoff.jsonl"))
	if err != nil {
		return nil, err
	}
	es, err := recovery.OpenEngineStore(filepath.Join(root, "engine.jsonl"))
	if err != nil {
		return nil, err
	}
	rs, err := engineruntime.Open(filepath.Join(root, "runtime.jsonl"))
	if err != nil {
		return nil, err
	}
	tl, err := timeline.Open(filepath.Join(root, "timeline.jsonl"))
	if err != nil {
		return nil, err
	}
	rt, err := engineruntime.New(rs, es, tl)
	if err != nil {
		return nil, err
	}
	current := handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: pkg.ContextPacketID, ProjectBrainRevisionSHA256: pkg.ProjectBrainRevisionSHA256}
	hx := &handoff.Executor{Store: hs, Runtime: rt, Current: func() handoff.CurrentState { return current }}
	f := &execFixture{pkg: pkg, plan: plan, routeReq: r, routeCandidates: cs, ad: &adapter{id: "engine-a"}}
	f.rx = &routing.Executor{Handoff: hx, Current: func() (routing.Requirements, []routing.Candidate) { return f.routeReq, f.routeCandidates }}
	return f, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	root := filepath.Join(os.TempDir(), "keydeck-proof30-reconstructed")
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	out := report{Proof: "0.30-deterministic-route-selection-and-route-bound-continuation-reconstructed", Status: "failed", NextGate: "Proof 0.31 — Evidence-Bound Candidate Collection and Reconciliation Assessment (reconstructed)"}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	r := req("proof30-task", "proof30-session")
	cs := candidates()
	p1, err := routing.Select(r, cs)
	if err != nil {
		return err
	}
	rev := []routing.Candidate{cs[1], cs[0]}
	p2, err := routing.Select(r, rev)
	if err != nil {
		return err
	}
	add("candidate_order_does_not_change_deterministic_route_identity", p1.RouteSHA256 == p2.RouteSHA256 && p1.SelectedEngineID == "engine-a", p1.RouteID)

	capReq := r
	capReq.RequiredCapabilities = []engineruntime.Capability{engineruntime.CapabilityResume}
	capPlan, err := routing.Select(capReq, cs)
	if err != nil {
		return err
	}
	add("only_capability_qualified_engines_enter_route_selection", capPlan.SelectedEngineID == "engine-a", capPlan.SelectedEngineID)

	gated := append([]routing.Candidate(nil), cs...)
	gated = append(gated,
		routing.Candidate{EngineID: "unhealthy-high", ProviderID: "provider-x", Available: true, Health: engineruntime.HealthUnhealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999},
		routing.Candidate{EngineID: "unavailable-high", ProviderID: "provider-y", Available: false, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999},
	)
	gatedReq := r
	gatedReq.ExcludedEngineIDs = []string{"engine-a"}
	gatedPlan, err := routing.Select(gatedReq, gated)
	if err != nil {
		return err
	}
	add("unhealthy_unavailable_and_explicitly_excluded_routes_are_filtered_before_ranking", gatedPlan.SelectedEngineID == "engine-b", gatedPlan.SelectedEngineID)

	_, noRouteErr := routing.Select(r, []routing.Candidate{{EngineID: "none", ProviderID: "none", Available: false, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999}})
	add("no_qualified_route_fails_closed", errors.Is(noRouteErr, routing.ErrNoQualifiedRoute), fmt.Sprint(noRouteErr))

	fx, err := newExecFixture(filepath.Join(root, "exec"))
	if err != nil {
		return err
	}
	packageBound := routing.Validate(fx.plan, fx.routeReq, fx.routeCandidates) == nil && routing.ValidatePackage(fx.plan, fx.pkg) == nil && fx.plan.HandoffPackageID == fx.pkg.PackageID && fx.plan.HandoffPackageSHA256 == fx.pkg.PackageSHA256
	add("route_identity_binds_exact_task_session_handoff_candidate_set_and_selected_engine", packageBound, map[string]any{"route": fx.plan.RouteID, "package": fx.pkg.PackageID})

	tampered := fx.plan
	tampered.SelectedEngineID = "tampered-engine"
	add("tampered_route_plan_is_rejected", errors.Is(routing.Validate(tampered, fx.routeReq, fx.routeCandidates), routing.ErrInvalidPlan), tampered.SelectedEngineID)

	fx.routeCandidates[0].EvidenceScore = 1
	fx.routeCandidates = append(fx.routeCandidates, routing.Candidate{EngineID: "engine-z", ProviderID: "provider-z", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999})
	_, staleErr := fx.rx.Execute(context.Background(), fx.ad, fx.pkg, fx.plan)
	add("stale_route_is_blocked_before_adapter_invocation", errors.Is(staleErr, routing.ErrInvalidPlan) && fx.ad.starts == 0, map[string]any{"error": fmt.Sprint(staleErr), "starts": fx.ad.starts})

	from, _ := routing.Select(r, []routing.Candidate{{EngineID: "api-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	to, _ := routing.Select(r, []routing.Candidate{{EngineID: "agent-b", ProviderID: "provider-b", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	cont, contErr := routing.PlanContinuation("response-proof30", routing.FailureKeyExhausted, from, to)
	add("key_exhaustion_may_continue_the_same_response_to_an_eligible_different_route", contErr == nil && routing.ValidateContinuation(cont, from, to) == nil && cont.ToEngineID == "agent-b", cont.ContinuationID)

	sameProvider, _ := routing.Select(r, []routing.Candidate{{EngineID: "api-c", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	_, busyErr := routing.PlanContinuation("response-busy", routing.FailureProviderBusy, from, sameProvider)
	add("provider_wide_busy_denies_same_provider_continuation", errors.Is(busyErr, routing.ErrProviderBusySameProvider), fmt.Sprint(busyErr))

	tl, err := timeline.Open(filepath.Join(root, "timeline.jsonl"))
	if err != nil {
		return err
	}
	ev, appended, err := routing.RecordPlan(tl, p1)
	if err != nil {
		return err
	}
	state := tasks.State{TaskID: r.TaskID, SessionID: r.SessionID, Status: tasks.StatusCompleted, Contract: tasks.Contract{Goal: "prove deterministic routing", Checks: []tasks.AcceptanceCheck{{ID: "route", Description: "route selected", Status: tasks.CheckPassed, Evidence: p1.RouteID}}}}
	receipt, err := proofreceipt.Build(state, tl.ByTask(r.TaskID), []proofreceipt.Artifact{routing.Artifact(p1)})
	if err != nil {
		return err
	}
	receiptBound := appended && ev.DataHash == p1.RouteSHA256 && len(receipt.Artifacts) == 1 && receipt.Artifacts[0].SHA256 == p1.RouteSHA256
	add("timeline_and_proof_receipt_bind_exact_route_evidence", receiptBound, receipt.ReceiptID)

	out.RouteID, out.RouteSHA256, out.ContinuationID, out.ReceiptID = p1.RouteID, p1.RouteSHA256, cont.ContinuationID, receipt.ReceiptID
	out.Passed = len(out.Scenarios) == 10
	for _, s := range out.Scenarios {
		if !s.Passed {
			out.Passed = false
		}
	}
	if out.Passed {
		out.Status = "passed"
	}
	raw, _ := jsonMarshalIndent(out)
	fmt.Println(string(raw))
	if !out.Passed {
		return errors.New("proof 0.30 reconstructed acceptance gate failed")
	}
	return nil
}
func jsonMarshalIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }
