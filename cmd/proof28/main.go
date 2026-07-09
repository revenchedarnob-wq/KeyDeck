package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/contextscout"
	"keydeck.local/feasibilitylab/internal/engineruntime"
	"keydeck.local/feasibilitylab/internal/handoff"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

const (
	taskID         = "proof28-task"
	sessionID      = "proof28-session"
	secretSentinel = "PROOF28_SECRET_MUST_NEVER_PERSIST_123456"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}
type report struct {
	Proof                      string     `json:"proof"`
	Status                     string     `json:"status"`
	Passed                     bool       `json:"passed"`
	Scenarios                  []scenario `json:"scenarios"`
	PackageID                  string     `json:"package_id"`
	PackageSHA256              string     `json:"package_sha256"`
	PacketID                   string     `json:"packet_id"`
	PacketSHA256               string     `json:"packet_sha256"`
	TaskSequence               uint64     `json:"task_sequence"`
	ProjectBrainRevisionSHA256 string     `json:"project_brain_revision_sha256"`
	EngineRequestSHA256        string     `json:"engine_request_sha256"`
	ReceiptID                  string     `json:"receipt_id"`
	NextGate                   string     `json:"next_gate"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	root := filepath.Join(os.TempDir(), "keydeck-proof28-reconstructed")
	_ = os.RemoveAll(root)
	defer os.RemoveAll(root)
	project := filepath.Join(root, "project")
	_ = os.MkdirAll(filepath.Join(project, "internal"), 0o700)
	_ = os.WriteFile(filepath.Join(project, "internal", "handoff.go"), []byte("package internal\nfunc AssembleHandoff(){}\n"), 0o600)
	fp, err := contextscout.FingerprintProject(project)
	if err != nil {
		return err
	}
	packet := contextcompiler.Packet{Version: 1, CreatedAt: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), Objective: "assemble exact production handoff package", ProjectRoot: project, ProjectID: "proof28-project", StructuralProvider: "proof28-fixture", StructuralEvidence: []contextcompiler.StructuralEvidence{{Tool: "search_graph", Arguments: "{}", Output: "handoff.go", Successful: true}}, SourceSnippets: []contextcompiler.SourceSnippet{{Path: "internal/handoff.go", StartLine: 1, EndLine: 2, Score: 100, Content: "package internal\nfunc AssembleHandoff(){}\n"}}, OmittedEvidenceCount: 2}
	packet.RenderedChars = len(packet.Render())
	cpath := filepath.Join(root, "context.jsonl")
	cs, _ := contextscout.OpenStore(cpath)
	cache := contextscout.CacheKeyInput{ProviderServerID: "mcp-proof28", ProviderSchemaSHA256: strings.Repeat("a", 64), ProjectRoot: project, Objective: packet.Objective, MaxChars: 12000, MaxFiles: 1}.Hash()
	rec, _, err := cs.Save(contextscout.SaveInput{CacheKey: cache, ProjectFingerprint: fp, ProviderServerID: "mcp-proof28", ProviderSchemaSHA256: strings.Repeat("a", 64), ProjectRoot: project, Objective: packet.Objective, MaxChars: 12000, MaxFiles: 1, Packet: packet, ProviderEvidence: contextscout.ProviderEvidence{ProviderServerID: "mcp-proof28", ProviderSchemaSHA256: strings.Repeat("a", 64), ProviderTools: []string{"search_graph"}, CacheKey: cache}})
	if err != nil {
		return err
	}
	inspection, err := projectbrain.BuildInspection(packet, rec, fp)
	if err != nil {
		return err
	}
	bs, _ := projectbrain.Open(filepath.Join(root, "brain.jsonl"))
	brain, _, err := bs.Append(projectbrain.RevisionInput{ProjectID: "proof28-project", SessionID: sessionID, ProjectRoot: project, ProjectFingerprint: fp, Goal: "assemble exact production handoff package", Decisions: []string{"canonical state stays in KeyDeck"}, CompletedWork: []string{"Proof 0.27"}, PendingWork: []string{"execute handoff safely"}, RelevantFiles: []string{"internal/handoff.go"}, Context: inspection}, []string{secretSentinel})
	if err != nil {
		return err
	}
	tpath := filepath.Join(root, "task.jsonl")
	ts, _ := tasks.Open(tpath)
	tm := &tasks.Manager{Store: ts}
	checks := []tasks.AcceptanceCheck{{ID: "passed-a", Description: "context already verified"}, {ID: "passed-b", Description: "brain already verified"}, {ID: "pending-a", Description: "execute handoff"}, {ID: "pending-b", Description: "verify receipt"}}
	_, err = tm.Create(taskID, sessionID, tasks.Contract{Goal: "continue the canonical task through a safe engine handoff", RequiredOutcomes: []string{"preserve canonical state", "bind exact context", "preserve canonical state"}, ForbiddenScope: []string{"no secrets", "no stale context"}, Checks: checks})
	if err != nil {
		return err
	}
	_, _ = tm.UpdateCheck("passed-a", tasks.CheckPassed, "context packet proof")
	_, _ = tm.UpdateCheck("passed-b", tasks.CheckPassed, "project brain proof")
	state := ts.State()
	passport := session.Passport{SessionID: sessionID, ProjectRoot: project, Goal: state.Contract.Goal, FromEngine: "api", ToEngine: "codex", HandoffReason: "bounded production continuation", PendingTasks: []string{"execute handoff"}, RelevantFiles: []string{"internal/handoff.go"}, Checkpoint: "proof28-checkpoint"}
	in := handoff.Input{Task: state, ContextPacketID: rec.PacketID, ContextPacketSHA256: rec.PacketSHA256, MCPServerID: "mcp-proof28", MCPSchemaSHA256: strings.Repeat("a", 64), ProjectSourceFingerprint: fp, Brain: brain, Passport: passport, EngineID: "codex", RequiredCapabilities: []engineruntime.Capability{engineruntime.CapabilityResume, engineruntime.CapabilityText}, ForbiddenExactValues: []string{secretSentinel}}
	pkg, err := handoff.Assemble(in)
	if err != nil {
		return err
	}
	out := report{Proof: "0.28-production-handoff-package-assembly-reconstructed", Status: "failed", PackageID: pkg.PackageID, PackageSHA256: pkg.PackageSHA256, PacketID: rec.PacketID, PacketSHA256: rec.PacketSHA256, TaskSequence: state.LastSequence, ProjectBrainRevisionSHA256: brain.RevisionSHA256, EngineRequestSHA256: hash(pkg.EngineRequest), NextGate: "Proof 0.29 — Handoff Package Persistence, Replay-Safe Engine Execution, and Restart Reconciliation"}
	add := func(n string, p bool, d any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: n, Passed: p, Detail: d})
	}
	prompt := pkg.EngineRequest.Prompt
	sorted := len(pkg.Task.RequiredOutcomes) == 2 && pkg.Task.RequiredOutcomes[0] == "bind exact context" && pkg.Task.RequiredOutcomes[1] == "preserve canonical state"
	pendingOnly := strings.Contains(prompt, "pending-a") && strings.Contains(prompt, "pending-b") && !strings.Contains(prompt, "passed-a") && !strings.Contains(prompt, "passed-b")
	add("task_aware_objective_uses_goal_sorted_outcomes_only_pending_checks_and_forbidden_scope", sorted && pendingOnly && strings.Contains(prompt, state.Contract.Goal) && strings.Contains(prompt, "no secrets"), map[string]any{"outcomes": pkg.Task.RequiredOutcomes, "pending": len(pkg.Task.PendingChecks)})
	add("passed_checks_remain_sealed_as_task_context_without_becoming_pending_work", len(pkg.Task.PassedChecks) == 2 && len(pkg.Task.PendingChecks) == 2 && strings.Contains(prompt, "2 acceptance checks already passed"), map[string]any{"passed": len(pkg.Task.PassedChecks), "pending": len(pkg.Task.PendingChecks)})
	pass3 := pkg.Task.TaskID == taskID && pkg.Task.SessionID == sessionID && pkg.Task.LastSequence == state.LastSequence && pkg.ContextPacketID == rec.PacketID && pkg.ContextPacketSHA256 == rec.PacketSHA256 && pkg.MCPServerID == "mcp-proof28" && pkg.MCPSchemaSHA256 == strings.Repeat("a", 64) && pkg.ProjectSourceFingerprint == fp && pkg.ProjectBrainRevisionSHA256 == brain.RevisionSHA256 && pkg.ContextInspectionSHA256 == inspection.InspectionSHA256 && len(pkg.IncludedInspectionEvidence) == len(inspection.IncludedEvidence) && pkg.OmittedInspectionEvidenceCount == inspection.OmittedEvidenceCount
	add("one_package_seals_task_context_mcp_brain_inspection_passport_capabilities_and_engine_request", pass3, map[string]any{"inspection": pkg.ContextInspectionSHA256, "omitted": pkg.OmittedInspectionEvidenceCount})
	tampered := pkg
	tampered.ContextPacketID = "packet-forged"
	add("package_identity_is_canonical_and_tamper_evident", errors.Is(handoff.Validate(tampered, handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: "packet-forged", ProjectBrainRevisionSHA256: brain.RevisionSHA256}, nil), handoff.ErrInvalidPackage), pkg.PackageSHA256)
	add("stale_task_sequence_is_rejected_before_engine_use", errors.Is(handoff.Validate(pkg, handoff.CurrentState{TaskSequence: state.LastSequence + 1, ContextPacketID: rec.PacketID, ProjectBrainRevisionSHA256: brain.RevisionSHA256}, nil), handoff.ErrStaleTask), nil)
	add("stale_context_packet_id_is_rejected_before_engine_use", errors.Is(handoff.Validate(pkg, handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: "packet-new", ProjectBrainRevisionSHA256: brain.RevisionSHA256}, nil), handoff.ErrStaleContext), nil)
	add("stale_project_brain_revision_is_rejected_before_engine_use", errors.Is(handoff.Validate(pkg, handoff.CurrentState{TaskSequence: state.LastSequence, ContextPacketID: rec.PacketID, ProjectBrainRevisionSHA256: "brain-new"}, nil), handoff.ErrStaleBrain), nil)
	secretIn := in
	secretIn.Passport.Decisions = []session.Decision{{Summary: "use " + secretSentinel, Source: "probe"}}
	_, secretErr := handoff.Assemble(secretIn)
	add("forbidden_exact_secret_like_context_is_rejected_before_engine_use", errors.Is(secretErr, handoff.ErrForbiddenContext), fmt.Sprint(secretErr))
	req := pkg.EngineRequest
	pass9 := req.TaskID == pkg.Task.TaskID && req.SessionID == pkg.Task.SessionID && hash(req.Passport) == hash(pkg.Passport) && hash(req.RequiredCapabilities) == hash(pkg.RequiredEngineCapabilities) && strings.Contains(req.Prompt, pkg.ContextPacketID) && strings.Contains(req.Prompt, pkg.ProjectBrainRevisionSHA256) && strings.Contains(req.Prompt, pkg.ContextInspectionSHA256)
	add("engine_runtime_request_is_bound_to_same_task_session_passport_capabilities_and_context_prompt", pass9, out.EngineRequestSHA256)
	pfile := filepath.Join(root, pkg.PackageID+".json")
	raw, _ := json.MarshalIndent(pkg, "", "  ")
	raw = append(raw, '\n')
	if err := os.WriteFile(pfile, raw, 0o600); err != nil {
		return err
	}
	tl, _ := timeline.Open(filepath.Join(root, "timeline.jsonl"))
	_, _, _ = tl.AppendOnce(timeline.Input{EventID: "handoff:" + pkg.PackageID, TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainArtifact, Kind: "handoff_package_assembled", SourceRef: pkg.ContextPacketID + "@" + pkg.ProjectBrainRevisionSHA256, Summary: "assembled exact production handoff package", DataHash: pkg.PackageSHA256})
	art := proofreceipt.Artifact{Name: "handoff package", Path: pfile, SHA256: fileHash(raw), Size: int64(len(raw))}
	receipt, err := proofreceipt.BuildRedacted(ts.State(), tl.Snapshot(), []proofreceipt.Artifact{art}, []string{secretSentinel})
	if err != nil {
		return err
	}
	bound := false
	for _, a := range receipt.Artifacts {
		if a.Name == "handoff package" && a.SHA256 == fileHash(raw) {
			bound = true
		}
	}
	add("proof_receipt_binds_exact_handoff_package_artifact", bound, receipt.ReceiptID)
	out.ReceiptID = receipt.ReceiptID
	out.Passed = len(out.Scenarios) == 10
	for _, s := range out.Scenarios {
		out.Passed = out.Passed && s.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if !out.Passed {
		return errors.New("Proof 0.28 acceptance gate failed")
	}
	return nil
}
func hash(v any) string {
	raw, _ := json.Marshal(v)
	s := sha256.Sum256(raw)
	return hex.EncodeToString(s[:])
}
func fileHash(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
