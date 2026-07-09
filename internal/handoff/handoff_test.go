package handoff

import (
	"errors"
	"strings"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
)

func fixture() Input {
	checks := []tasks.AcceptanceCheck{{ID: "passed", Description: "already done", Status: tasks.CheckPassed, Evidence: "proof"}, {ID: "pending", Description: "still needed", Status: tasks.CheckPending}}
	st := tasks.State{TaskID: "task", SessionID: "session", Status: tasks.StatusWorking, Contract: tasks.Contract{Goal: "finish safely", RequiredOutcomes: []string{"z outcome", "a outcome"}, ForbiddenScope: []string{"no secrets"}, Checks: checks}, LastSequence: 8}
	ins := projectbrain.ContextInspection{PacketID: "packet", PacketSHA256: strings.Repeat("a", 64), ProjectFingerprint: "fp", IncludedEvidence: []projectbrain.InspectionEvidence{{Kind: "source", Reference: "a.go", SHA256: strings.Repeat("b", 64), Successful: true}}, OmittedEvidenceCount: 2, InspectionSHA256: "inspection"}
	brain := projectbrain.Revision{ProjectID: "project", SessionID: "session", ProjectFingerprint: "fp", RevisionSHA256: "brain", Context: ins}
	passport := session.Passport{SessionID: "session", ProjectRoot: "/project", Goal: "finish safely", ToEngine: "engine"}
	return Input{Task: st, ContextPacketID: "packet", ContextPacketSHA256: strings.Repeat("a", 64), MCPServerID: "mcp", MCPSchemaSHA256: strings.Repeat("c", 64), ProjectSourceFingerprint: "fp", Brain: brain, Passport: passport, EngineID: "engine", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityResume, engineruntime.CapabilityText}}
}
func TestAssembleValidateAndTamper(t *testing.T) {
	in := fixture()
	p, err := Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Task.PendingChecks) != 1 || len(p.Task.PassedChecks) != 1 || p.Task.RequiredOutcomes[0] != "a outcome" {
		t.Fatal("task assembly wrong")
	}
	if err := Validate(p, CurrentState{8, "packet", "brain"}, nil); err != nil {
		t.Fatal(err)
	}
	p.EngineRequest.Prompt += "x"
	if !errors.Is(Validate(p, CurrentState{8, "packet", "brain"}, nil), ErrInvalidPackage) {
		t.Fatal("tamper accepted")
	}
}
func TestStaleAndForbidden(t *testing.T) {
	in := fixture()
	p, _ := Assemble(in)
	if !errors.Is(Validate(p, CurrentState{9, "packet", "brain"}, nil), ErrStaleTask) {
		t.Fatal()
	}
	if !errors.Is(Validate(p, CurrentState{8, "other", "brain"}, nil), ErrStaleContext) {
		t.Fatal()
	}
	if !errors.Is(Validate(p, CurrentState{8, "packet", "other"}, nil), ErrStaleBrain) {
		t.Fatal()
	}
	in.Passport.Decisions = []session.Decision{{At: time.Now(), Summary: "SECRET", Source: "test"}}
	in.ForbiddenExactValues = []string{"SECRET"}
	if _, err := Assemble(in); !errors.Is(err, ErrForbiddenContext) {
		t.Fatalf("got %v", err)
	}
}
