package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/contextscout"
	"keydeck.local/feasibilitylab/internal/projectbrain"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

const (
	taskID         = "proof27-task"
	sessionID      = "proof27-session"
	secretSentinel = "PROOF27_SECRET_MUST_NEVER_PERSIST_123456"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}
type report struct {
	Proof               string     `json:"proof"`
	Status              string     `json:"status"`
	Passed              bool       `json:"passed"`
	Scenarios           []scenario `json:"scenarios"`
	PacketID            string     `json:"packet_id"`
	InspectionSHA256    string     `json:"inspection_sha256"`
	BrainRevisionSHA256 string     `json:"brain_revision_sha256"`
	ReceiptID           string     `json:"receipt_id"`
	NextGate            string     `json:"next_gate"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	stateRoot := filepath.Join(os.TempDir(), "keydeck-proof27-reconstructed")
	_ = os.RemoveAll(stateRoot)
	defer os.RemoveAll(stateRoot)
	project := filepath.Join(stateRoot, "project")
	if err := os.MkdirAll(filepath.Join(project, "internal"), 0o700); err != nil {
		return err
	}
	sourcePath := filepath.Join(project, "internal", "router.go")
	if err := os.WriteFile(sourcePath, []byte("package internal\nfunc RouteRequest(){ /* exact project brain target */ }\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(project, "internal", "noise.go"), []byte("package internal\n// lower ranked evidence\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(project, ".env"), []byte("TOKEN="+secretSentinel+"\n"), 0o600); err != nil {
		return err
	}
	fp, err := contextscout.FingerprintProject(project)
	if err != nil {
		return err
	}
	packet := contextcompiler.Packet{Version: 1, CreatedAt: time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), Objective: "preserve canonical project brain and inspect exact context", ProjectRoot: project, ProjectID: "proof27-project", Keywords: []string{"canonical", "brain", "context"}, StructuralProvider: "proof27-fixture", StructuralVersion: "1.0.0", StructuralEvidence: []contextcompiler.StructuralEvidence{{Tool: "search_graph", Arguments: `{"project":"proof27-project","name_pattern":"ProjectBrain"}`, Output: `{"symbols":[{"name":"RouteRequest","path":"internal/router.go"}]}`, Successful: true}}, StructuralIndexSucceeded: true, StructuralSearchSucceeded: true, SourceSnippets: []contextcompiler.SourceSnippet{{Path: "internal/router.go", StartLine: 1, EndLine: 2, Score: 100, Content: "package internal\nfunc RouteRequest(){ /* exact project brain target */ }\n"}}, OmittedEvidenceCount: 1}
	packet.RenderedChars = len(packet.Render())
	contextPath := filepath.Join(stateRoot, "context.jsonl")
	cstore, err := contextscout.OpenStore(contextPath)
	if err != nil {
		return err
	}
	record, _, err := cstore.Save(contextscout.SaveInput{CacheKey: contextscout.CacheKeyInput{ProviderServerID: "mcp-proof27", ProviderSchemaSHA256: strings.Repeat("a", 64), ProjectRoot: project, Objective: packet.Objective, MaxChars: 12000, MaxFiles: 1}.Hash(), ProjectFingerprint: fp, ProviderServerID: "mcp-proof27", ProviderSchemaSHA256: strings.Repeat("a", 64), ProjectRoot: project, Objective: packet.Objective, MaxChars: 12000, MaxFiles: 1, Packet: packet, ProviderEvidence: contextscout.ProviderEvidence{ProviderServerID: "mcp-proof27", ProviderSchemaSHA256: strings.Repeat("a", 64), ProviderTools: []string{"search_graph"}, CacheKey: contextscout.CacheKeyInput{ProviderServerID: "mcp-proof27", ProviderSchemaSHA256: strings.Repeat("a", 64), ProjectRoot: project, Objective: packet.Objective, MaxChars: 12000, MaxFiles: 1}.Hash()}})
	if err != nil {
		return err
	}
	inspection, err := projectbrain.BuildInspection(packet, record, fp)
	if err != nil {
		return err
	}
	brainPath := filepath.Join(stateRoot, "brain.jsonl")
	brain, err := projectbrain.Open(brainPath)
	if err != nil {
		return err
	}
	in := projectbrain.RevisionInput{ProjectID: "proof27-project", SessionID: sessionID, ProjectRoot: project, ProjectFingerprint: fp, Goal: "keep one canonical project brain bound to verified inspected context", Decisions: []string{"KeyDeck owns canonical project truth", "engines are replaceable workers"}, KnownFailures: []string{"do not resend broad context blindly"}, CompletedWork: []string{"Proof 0.26 production Context Scout/Compiler"}, PendingWork: []string{"Proof 0.28 handoff package assembly"}, RelevantFiles: []string{"internal/router.go"}, Context: inspection}
	rev, created, err := brain.Append(in, []string{secretSentinel})
	if err != nil {
		return err
	}
	out := report{Proof: "0.27-canonical-project-brain-context-inspection-reconstructed", Status: "failed", PacketID: record.PacketID, InspectionSHA256: inspection.InspectionSHA256, BrainRevisionSHA256: rev.RevisionSHA256, NextGate: "Proof 0.28 — Production Handoff Package Assembly"}
	add := func(name string, pass bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: pass, Detail: detail})
	}

	add("canonical_revision_binds_exact_project_session_source_fingerprint_and_verified_packet", created && rev.ProjectID == "proof27-project" && rev.SessionID == sessionID && rev.ProjectFingerprint == fp && rev.Context.PacketID == record.PacketID && rev.Context.PacketSHA256 == record.PacketSHA256, map[string]any{"revision": rev.RevisionSHA256, "packet": record.PacketID})
	add("context_inspection_accounts_for_included_structural_source_and_omitted_evidence", len(inspection.IncludedEvidence) == 2 && inspection.OmittedEvidenceCount == 1, map[string]any{"included": len(inspection.IncludedEvidence), "omitted": inspection.OmittedEvidenceCount})
	expectedInspection, _ := projectbrain.BuildInspection(packet, record, fp)
	add("inspection_identity_is_deterministic_for_exact_packet_and_fingerprint", expectedInspection.InspectionSHA256 == inspection.InspectionSHA256, inspection.InspectionSHA256)
	same, createdAgain, err := brain.Append(in, []string{secretSentinel})
	add("identical_project_brain_revision_reuses_without_duplicate_append", err == nil && !createdAgain && same.RevisionSHA256 == rev.RevisionSHA256 && brain.Count() == 1, map[string]any{"count": brain.Count()})
	restarted, err := projectbrain.Open(brainPath)
	if err != nil {
		return err
	}
	latest, ok := restarted.Latest()
	add("restart_replays_exact_revision_and_hash_chain", ok && latest.RevisionSHA256 == rev.RevisionSHA256 && latest.Context.InspectionSHA256 == inspection.InspectionSHA256, latest.RevisionSHA256)

	if err := os.WriteFile(sourcePath, []byte("package internal\nfunc RouteRequest(){ /* changed after inspection */ }\n"), 0o600); err != nil {
		return err
	}
	changedFP, _ := contextscout.FingerprintProject(project)
	_, staleErr := projectbrain.BuildInspection(packet, record, changedFP)
	before := restarted.Count()
	staleIn := in
	staleIn.ProjectFingerprint = changedFP
	staleIn.Context.ProjectFingerprint = changedFP
	_, _, appendStaleErr := restarted.Append(staleIn, []string{secretSentinel})
	add("source_change_makes_old_context_stale_before_new_revision_persistence", errors.Is(staleErr, projectbrain.ErrStaleContext) && appendStaleErr != nil && restarted.Count() == before, map[string]any{"old": fp, "new": changedFP, "error": fmt.Sprint(staleErr)})
	if err := os.WriteFile(sourcePath, []byte("package internal\nfunc RouteRequest(){ /* exact project brain target */ }\n"), 0o600); err != nil {
		return err
	}

	secretIn := in
	secretIn.Decisions = []string{"persist " + secretSentinel}
	_, _, secretErr := restarted.Append(secretIn, []string{secretSentinel})
	rawBrain, _ := os.ReadFile(brainPath)
	add("forbidden_exact_secret_is_rejected_before_durable_brain_write", errors.Is(secretErr, projectbrain.ErrSecret) && !strings.Contains(string(rawBrain), secretSentinel) && restarted.Count() == before, fmt.Sprint(secretErr))

	forged := rev
	forged.Context.IncludedEvidence[0].Reference = "forged/path.go" // self-consistent rehash requires package internals, emulate by marshal/store not accepted against source via current revision validation using original plus changed inspection identity probe.
	forged.Context.InspectionSHA256 = "forged-self-consistent-inspection"
	contextErr := projectbrain.ValidateRevisionContext(forged, packet, record, fp)
	add("context_validation_rejects_brain_revision_not_matching_exact_packet_inspection", errors.Is(contextErr, projectbrain.ErrTampered), fmt.Sprint(contextErr))

	// Context artifact tampering is rejected by the upstream verified packet store before Project Brain use.
	artifactDir := strings.TrimSuffix(contextPath, filepath.Ext(contextPath)) + "-artifacts"
	packetFile := filepath.Join(artifactDir, record.PacketID+".json")
	originalPacket, _ := os.ReadFile(packetFile)
	_ = os.WriteFile(packetFile, append(originalPacket, []byte(" ")...), 0o600)
	_, _, _, artifactErr := cstore.FindFresh(record.CacheKey, fp)
	_ = os.WriteFile(packetFile, originalPacket, 0o600)
	add("tampered_context_artifact_is_blocked_before_project_brain_use", errors.Is(artifactErr, contextscout.ErrArtifactTampered), fmt.Sprint(artifactErr))

	timelinePath := filepath.Join(stateRoot, "timeline.jsonl")
	tl, err := timeline.Open(timelinePath)
	if err != nil {
		return err
	}
	_, _, err = tl.AppendOnce(timeline.Input{EventID: "project-brain:" + rev.RevisionSHA256[:20], TaskID: taskID, SessionID: sessionID, Domain: timeline.DomainArtifact, Kind: "project_brain_revision", SourceRef: record.PacketID + "@" + inspection.InspectionSHA256, Summary: "stored canonical project brain revision from exact verified context inspection", DataHash: rev.RevisionSHA256})
	if err != nil {
		return err
	}
	taskPath := filepath.Join(stateRoot, "task.jsonl")
	ts, err := tasks.Open(taskPath)
	if err != nil {
		return err
	}
	tm := &tasks.Manager{Store: ts}
	checks := make([]tasks.AcceptanceCheck, 10)
	for i := range checks {
		checks[i] = tasks.AcceptanceCheck{ID: fmt.Sprintf("c%02d", i+1), Description: "Proof 0.27 acceptance scenario"}
	}
	if _, err = tm.Create(taskID, sessionID, tasks.Contract{Goal: "Prove canonical Project Brain revisions are durable, source-bound and inspected before handoff.", ForbiddenScope: []string{"no secret persistence", "no stale context reuse", "no model-owned canonical truth"}, Checks: checks}); err != nil {
		return err
	}
	for i, s := range out.Scenarios {
		status := tasks.CheckFailed
		if s.Passed {
			status = tasks.CheckPassed
		}
		if _, err = tm.UpdateCheck(fmt.Sprintf("c%02d", i+1), status, s.Name); err != nil {
			return err
		}
	}
	brainArtifact, err := restarted.ReceiptArtifact()
	if err != nil {
		return err
	}
	contextArtifacts, err := cstore.ReceiptArtifacts(record)
	if err != nil {
		return err
	}
	artifacts := append(contextArtifacts, brainArtifact)
	receipt, err := proofreceipt.BuildRedacted(ts.State(), tl.Snapshot(), artifacts, []string{secretSentinel})
	if err != nil {
		return err
	}
	rr, _ := json.Marshal(receipt)
	brainBound, contextBound := false, false
	for _, a := range receipt.Artifacts {
		if a.Name == "project brain revision store" {
			brainBound = true
		}
		if a.Name == "context packet JSON" {
			contextBound = true
		}
	}
	pass10 := brainBound && contextBound && !strings.Contains(string(rr), secretSentinel)
	add("proof_receipt_binds_brain_context_inspection_and_packet_artifacts_without_secret", pass10, map[string]any{"receipt": receipt.ReceiptID, "brain_bound": brainBound, "context_bound": contextBound})
	if _, err = tm.UpdateCheck("c10", map[bool]tasks.CheckStatus{true: tasks.CheckPassed, false: tasks.CheckFailed}[pass10], "exact brain/context/inspection provenance"); err != nil {
		return err
	}
	receipt, err = proofreceipt.BuildRedacted(ts.State(), tl.Snapshot(), artifacts, []string{secretSentinel})
	if err != nil {
		return err
	}
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
	if err := enc.Encode(out); err != nil {
		return err
	}
	if !out.Passed {
		return errors.New("Proof 0.27 acceptance gate failed")
	}
	_ = context.Background()
	return nil
}
