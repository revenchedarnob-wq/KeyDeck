package routing

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/handoff"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

func baseReq() Requirements {
	return Requirements{TaskID: "task-routing", SessionID: "session-routing", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}}
}
func baseCandidates() []Candidate {
	return []Candidate{
		{EngineID: "engine-b", ProviderID: "provider-b", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 80, EvidenceRefs: []string{"proof-b"}},
		{EngineID: "engine-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText, engineruntime.CapabilityResume}, EvidenceScore: 90, EvidenceRefs: []string{"proof-a"}},
	}
}

func TestSelectIsDeterministicAcrossCandidateOrder(t *testing.T) {
	a := baseCandidates()
	p1, err := Select(baseReq(), a)
	if err != nil {
		t.Fatal(err)
	}
	a[0], a[1] = a[1], a[0]
	p2, err := Select(baseReq(), a)
	if err != nil {
		t.Fatal(err)
	}
	if p1.RouteSHA256 != p2.RouteSHA256 || p1.SelectedEngineID != "engine-a" {
		t.Fatalf("non-deterministic plan: %+v %+v", p1, p2)
	}
}

func TestSelectFiltersCapabilityHealthAvailabilityAndExclusions(t *testing.T) {
	candidates := baseCandidates()
	candidates = append(candidates,
		Candidate{EngineID: "high-no-cap", ProviderID: "p", Available: true, Health: engineruntime.HealthHealthy, EvidenceScore: 999},
		Candidate{EngineID: "high-unhealthy", ProviderID: "p", Available: true, Health: engineruntime.HealthUnhealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999},
		Candidate{EngineID: "high-unavailable", ProviderID: "p", Available: false, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999},
	)
	req := baseReq()
	req.ExcludedEngineIDs = []string{"engine-a"}
	p, err := Select(req, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if p.SelectedEngineID != "engine-b" {
		t.Fatalf("selected %s", p.SelectedEngineID)
	}
}

func TestNoQualifiedRouteFailsClosed(t *testing.T) {
	_, err := Select(baseReq(), []Candidate{{EngineID: "e", ProviderID: "p", Available: false, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}}})
	if !errors.Is(err, ErrNoQualifiedRoute) {
		t.Fatalf("got %v", err)
	}
}

func TestPlanTamperRejected(t *testing.T) {
	p, _ := Select(baseReq(), baseCandidates())
	p.SelectedEngineID = "tampered"
	if !errors.Is(Validate(p, baseReq(), baseCandidates()), ErrInvalidPlan) {
		t.Fatal("tampered plan accepted")
	}
}

func TestContinuationPolicy(t *testing.T) {
	from, _ := Select(baseReq(), []Candidate{{EngineID: "api-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	to, _ := Select(baseReq(), []Candidate{{EngineID: "agent-b", ProviderID: "provider-b", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	c, err := PlanContinuation("response-1", FailureKeyExhausted, from, to)
	if err != nil || ValidateContinuation(c, from, to) != nil {
		t.Fatalf("continuation: %v", err)
	}
	sameProvider, _ := Select(baseReq(), []Candidate{{EngineID: "api-c", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 10}})
	if _, err := PlanContinuation("response-2", FailureProviderBusy, from, sameProvider); !errors.Is(err, ErrProviderBusySameProvider) {
		t.Fatalf("got %v", err)
	}
}

type countingAdapter struct {
	starts int
	id     string
}

func (a *countingAdapter) ID() string { return a.id }
func (a *countingAdapter) Capabilities(context.Context) ([]engineruntime.Capability, error) {
	return []engineruntime.Capability{engineruntime.CapabilityText}, nil
}
func (a *countingAdapter) Health(context.Context) (engineruntime.Health, error) {
	return engineruntime.Health{State: engineruntime.HealthHealthy}, nil
}
func (a *countingAdapter) Start(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	a.starts++
	return engineruntime.Outcome{Disposition: engineruntime.DispositionCompleted, Result: session.EngineResult{Text: "ok"}}, nil
}
func (a *countingAdapter) Continue(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	return engineruntime.Outcome{}, errors.New("unused")
}
func (a *countingAdapter) Resume(context.Context, engineruntime.Request) (engineruntime.Outcome, error) {
	return engineruntime.Outcome{}, errors.New("unused")
}
func (a *countingAdapter) Cancel(context.Context, engineruntime.Binding) error { return nil }

func buildExecutorFixture(t *testing.T) (*Executor, handoff.Package, Plan, *countingAdapter, *Requirements, *[]Candidate) {
	t.Helper()
	root := t.TempDir()
	state := tasks.State{TaskID: "task-route-exec", SessionID: "session-route-exec", Status: tasks.StatusWorking, Contract: tasks.Contract{Goal: "route safely", Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "done"}}}, LastSequence: 1}
	brain := projectbrain.Revision{ProjectID: "project", SessionID: state.SessionID, ProjectFingerprint: "fp", RevisionSHA256: strings.Repeat("b", 64), Context: projectbrain.ContextInspection{PacketID: "packet", PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "fp", InspectionSHA256: strings.Repeat("c", 64)}}
	passport := session.Passport{SessionID: state.SessionID, ProjectRoot: root, Goal: state.Contract.Goal, ToEngine: "engine-a", HandoffReason: "route"}
	pkg, err := handoff.Assemble(handoff.Input{Task: state, ContextPacketID: brain.Context.PacketID, ContextPacketSHA256: brain.Context.PacketSHA256, MCPServerID: "mcp", MCPSchemaSHA256: strings.Repeat("d", 64), ProjectSourceFingerprint: "fp", Brain: brain, Passport: passport, EngineID: "engine-a", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityText}})
	if err != nil {
		t.Fatal(err)
	}
	req := baseReq()
	req.TaskID = state.TaskID
	req.SessionID = state.SessionID
	req.HandoffPackageID = pkg.PackageID
	req.HandoffPackageSHA256 = pkg.PackageSHA256
	candidates := []Candidate{{EngineID: "engine-a", ProviderID: "provider-a", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 100}}
	plan, err := Select(req, candidates)
	if err != nil {
		t.Fatal(err)
	}
	hs, _ := handoff.OpenStore(filepath.Join(root, "handoff.jsonl"))
	es, _ := recovery.OpenEngineStore(filepath.Join(root, "engine.jsonl"))
	rs, _ := engineruntime.Open(filepath.Join(root, "runtime.jsonl"))
	tl, _ := timeline.Open(filepath.Join(root, "timeline.jsonl"))
	rt, _ := engineruntime.New(rs, es, tl)
	current := handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: pkg.ContextPacketID, ProjectBrainRevisionSHA256: pkg.ProjectBrainRevisionSHA256}
	hx := &handoff.Executor{Store: hs, Runtime: rt, Current: func() handoff.CurrentState { return current }}
	rx := &Executor{Handoff: hx, Current: func() (Requirements, []Candidate) { return req, candidates }}
	ad := &countingAdapter{id: "engine-a"}
	return rx, pkg, plan, ad, &req, &candidates
}

func TestExecutorRejectsStaleRouteBeforeAdapterInvocation(t *testing.T) {
	rx, pkg, plan, ad, _, candidates := buildExecutorFixture(t)
	(*candidates)[0].EvidenceScore = 1
	*candidates = append(*candidates, Candidate{EngineID: "engine-z", ProviderID: "provider-z", Available: true, Health: engineruntime.HealthHealthy, Capabilities: []engineruntime.Capability{engineruntime.CapabilityText}, EvidenceScore: 999})
	_, err := rx.Execute(context.Background(), ad, pkg, plan)
	if !errors.Is(err, ErrInvalidPlan) || ad.starts != 0 {
		t.Fatalf("err=%v starts=%d", err, ad.starts)
	}
}
