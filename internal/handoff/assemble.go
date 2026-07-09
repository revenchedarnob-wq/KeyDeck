package handoff

import (
	"encoding/json"
	"fmt"
	"strings"

	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/tasks"
)

func Assemble(in Input) (Package, error) {
	if in.Task.TaskID == "" || in.Task.SessionID == "" || in.Task.Contract.Goal == "" || in.ContextPacketID == "" || in.ContextPacketSHA256 == "" || in.MCPServerID == "" || in.MCPSchemaSHA256 == "" || in.ProjectSourceFingerprint == "" || in.Brain.RevisionSHA256 == "" || in.Brain.Context.InspectionSHA256 == "" || in.EngineID == "" {
		return Package{}, ErrInvalidPackage
	}
	if in.Task.SessionID != in.Passport.SessionID || in.Task.SessionID != in.Brain.SessionID || in.ProjectSourceFingerprint != in.Brain.ProjectFingerprint || in.ContextPacketID != in.Brain.Context.PacketID || in.ContextPacketSHA256 != in.Brain.Context.PacketSHA256 {
		return Package{}, ErrInvalidPackage
	}
	task := TaskSnapshot{TaskID: in.Task.TaskID, SessionID: in.Task.SessionID, Status: in.Task.Status, LastSequence: in.Task.LastSequence, Progress: in.Task.Progress(), Goal: strings.TrimSpace(in.Task.Contract.Goal), RequiredOutcomes: normalizeStrings(in.Task.Contract.RequiredOutcomes), ForbiddenScope: normalizeStrings(in.Task.Contract.ForbiddenScope)}
	for _, c := range in.Task.Contract.Checks {
		ref := CheckRef{ID: c.ID, Description: c.Description, Status: c.Status, Evidence: c.Evidence}
		if c.Status == tasks.CheckPassed {
			task.PassedChecks = append(task.PassedChecks, ref)
		} else {
			task.PendingChecks = append(task.PendingChecks, ref)
		}
	}
	caps := normalizeCaps(in.RequiredCapabilities)
	prompt := renderPrompt(task, in.Brain.RevisionSHA256, in.Brain.Context.InspectionSHA256, in.ContextPacketID, in.ContextPacketSHA256)
	req := engineruntime.Request{ExecutionID: "exec-" + digest(struct{ Task, Packet, Brain, Engine string }{in.Task.TaskID, in.ContextPacketID, in.Brain.RevisionSHA256, in.EngineID})[:20], TaskID: in.Task.TaskID, SessionID: in.Task.SessionID, EngineID: in.EngineID, Prompt: prompt, Passport: in.Passport, RequiredCapabilities: caps}
	p := Package{Version: 1, Task: task, ContextPacketID: in.ContextPacketID, ContextPacketSHA256: in.ContextPacketSHA256, MCPServerID: in.MCPServerID, MCPSchemaSHA256: in.MCPSchemaSHA256, ProjectSourceFingerprint: in.ProjectSourceFingerprint, ProjectBrainRevisionSHA256: in.Brain.RevisionSHA256, ContextInspectionSHA256: in.Brain.Context.InspectionSHA256, IncludedInspectionEvidence: append([]projectbrain.InspectionEvidence(nil), in.Brain.Context.IncludedEvidence...), OmittedInspectionEvidenceCount: in.Brain.Context.OmittedEvidenceCount, Passport: in.Passport, RequiredEngineCapabilities: caps, EngineRequest: req}
	if forbidden(p, in.ForbiddenExactValues) {
		return Package{}, ErrForbiddenContext
	}
	p.PackageSHA256 = packageDigest(p)
	p.PackageID = "handoff-" + p.PackageSHA256[:20]
	if err := Validate(p, CurrentState{TaskSequence: in.Task.LastSequence, ContextPacketID: in.ContextPacketID, ProjectBrainRevisionSHA256: in.Brain.RevisionSHA256}, in.ForbiddenExactValues); err != nil {
		return Package{}, err
	}
	return p, nil
}

func Validate(p Package, current CurrentState, forbiddenValues []string) error {
	if p.Version != 1 || p.PackageID == "" || p.PackageSHA256 == "" || p.PackageSHA256 != packageDigest(p) || p.PackageID != "handoff-"+p.PackageSHA256[:20] {
		return ErrInvalidPackage
	}
	if p.Task.LastSequence != current.TaskSequence {
		return ErrStaleTask
	}
	if p.ContextPacketID != current.ContextPacketID {
		return ErrStaleContext
	}
	if p.ProjectBrainRevisionSHA256 != current.ProjectBrainRevisionSHA256 {
		return ErrStaleBrain
	}
	r := p.EngineRequest
	if r.TaskID != p.Task.TaskID || r.SessionID != p.Task.SessionID || r.Passport.SessionID != p.Passport.SessionID || digest(r.Passport) != digest(p.Passport) || digest(r.RequiredCapabilities) != digest(p.RequiredEngineCapabilities) || !strings.Contains(r.Prompt, p.ContextPacketID) || !strings.Contains(r.Prompt, p.ProjectBrainRevisionSHA256) || !strings.Contains(r.Prompt, p.ContextInspectionSHA256) {
		return ErrInvalidPackage
	}
	if forbidden(p, forbiddenValues) {
		return ErrForbiddenContext
	}
	return nil
}
func renderPrompt(t TaskSnapshot, brainSHA, inspectionSHA, packetID, packetSHA string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "KEYDECK TASK OBJECTIVE\nGoal: %s\n", t.Goal)
	if len(t.RequiredOutcomes) > 0 {
		b.WriteString("Required outcomes:\n")
		for _, v := range t.RequiredOutcomes {
			fmt.Fprintf(&b, "- %s\n", v)
		}
	}
	if len(t.PendingChecks) > 0 {
		b.WriteString("Pending acceptance checks:\n")
		for _, c := range t.PendingChecks {
			fmt.Fprintf(&b, "- %s: %s\n", c.ID, c.Description)
		}
	}
	if len(t.ForbiddenScope) > 0 {
		b.WriteString("Forbidden scope:\n")
		for _, v := range t.ForbiddenScope {
			fmt.Fprintf(&b, "- %s\n", v)
		}
	}
	fmt.Fprintf(&b, "Task context: %d acceptance checks already passed and remain sealed as completed evidence.\nContext packet: %s @ %s\nProject Brain revision: %s\nContext inspection: %s\n", len(t.PassedChecks), packetID, packetSHA, brainSHA, inspectionSHA)
	return b.String()
}
func forbidden(v any, exact []string) bool {
	raw, _ := json.Marshal(v)
	text := string(raw)
	for _, s := range exact {
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}
