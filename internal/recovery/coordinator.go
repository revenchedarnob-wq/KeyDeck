package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type Paths struct {
	TaskEvents     string
	SessionState   string
	ToolJournal    string
	Timeline       string
	ProofReceipts  string
	EngineLedger   string
	ArtifactLedger string
}

type Coordinator struct {
	paths     Paths
	tasks     *tasks.Store
	journal   *tooljournal.Journal
	timeline  *timeline.Store
	receipts  *proofreceipt.Store
	engines   *EngineStore
	artifacts *ArtifactStore
}

func Open(paths Paths) (*Coordinator, error) {
	if paths.TaskEvents == "" || paths.SessionState == "" || paths.ToolJournal == "" || paths.Timeline == "" || paths.ProofReceipts == "" || paths.EngineLedger == "" || paths.ArtifactLedger == "" {
		return nil, errors.New("all recovery paths are required")
	}
	taskStore, err := tasks.Open(paths.TaskEvents)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(paths.ToolJournal)
	if err != nil {
		return nil, err
	}
	activity, err := timeline.Open(paths.Timeline)
	if err != nil {
		return nil, err
	}
	receipts, err := proofreceipt.Open(paths.ProofReceipts)
	if err != nil {
		return nil, err
	}
	engines, err := OpenEngineStore(paths.EngineLedger)
	if err != nil {
		return nil, err
	}
	artifacts, err := OpenArtifactStore(paths.ArtifactLedger)
	if err != nil {
		return nil, err
	}
	return &Coordinator{paths: paths, tasks: taskStore, journal: journal, timeline: activity, receipts: receipts, engines: engines, artifacts: artifacts}, nil
}

func (c *Coordinator) EngineStore() *EngineStore     { return c.engines }
func (c *Coordinator) ArtifactStore() *ArtifactStore { return c.artifacts }

func (c *Coordinator) Recover() (Report, error) {
	taskState := c.tasks.State()
	if taskState.TaskID == "" || taskState.SessionID == "" {
		return Report{}, errors.New("recovery requires an existing task")
	}
	sessionState, err := session.Load(c.paths.SessionState)
	if err != nil {
		return Report{}, err
	}
	if sessionState.SessionID != taskState.SessionID {
		return Report{}, errors.New("task/session identity mismatch")
	}
	report := Report{TaskID: taskState.TaskID, SessionID: taskState.SessionID, RecoveredAt: time.Now().UTC()}

	if err := c.reconcileTools(&report); err != nil {
		return report, err
	}
	if err := c.reconcileResults(&report, &sessionState); err != nil {
		return report, err
	}
	if err := c.reconcileInterruptedExecutions(&report); err != nil {
		return report, err
	}
	if err := c.ensureReceipt(&report); err != nil {
		return report, err
	}
	return report, nil
}

func (c *Coordinator) reconcileTools(report *Report) error {
	state := c.tasks.State()
	journal := c.journal.Snapshot()
	stepIDs := make([]string, 0, len(state.Steps))
	for stepID := range state.Steps {
		stepIDs = append(stepIDs, stepID)
	}
	sort.Strings(stepIDs)
	for _, stepID := range stepIDs {
		step := c.tasks.State().Steps[stepID]
		if step.OperationID == "" {
			continue
		}
		record, exists := journal[step.OperationID]
		if !exists {
			continue
		}
		switch record.State {
		case tooljournal.StateCompleted:
			if step.Status != "completed" {
				if _, err := c.tasks.Append(tasks.EventStepCompleted, tasks.Step{ID: step.ID, Result: record.Result}); err != nil {
					return err
				}
				_, _ = c.tasks.Append(tasks.EventRecoveryReconciled, map[string]any{"step_id": step.ID, "resolution": string(DispositionReusedCompletedToolResult)})
			}
			if err := c.recordDecision(report, timeline.DomainTool, "tool_recovery", step.OperationID, DispositionReusedCompletedToolResult, "Completed tool result reused; operation was not replayed", record); err != nil {
				return err
			}
		case tooljournal.StateStarted, tooljournal.StateFailed:
			if record.Policy == tooljournal.ReplayForbidden {
				if step.Status != "ambiguous" {
					if _, err := c.tasks.Append(tasks.EventStepAmbiguous, tasks.Step{ID: step.ID, Error: "tool operation outcome is ambiguous; replay is forbidden"}); err != nil {
						return err
					}
					_, _ = c.tasks.Append(tasks.EventRecoveryReconciled, map[string]any{"step_id": step.ID, "resolution": string(DispositionBlockedAmbiguous)})
				}
				if err := c.recordDecision(report, timeline.DomainTool, "tool_recovery", step.OperationID, DispositionBlockedAmbiguous, "Ambiguous non-repeatable tool operation remains blocked", record); err != nil {
					return err
				}
			} else {
				if err := c.recordDecision(report, timeline.DomainTool, "tool_recovery", step.OperationID, DispositionRetryIdempotent, "Interrupted idempotent tool operation is eligible for safe retry", record); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Coordinator) reconcileResults(report *Report, sessionState *session.State) error {
	results := c.engines.Results()
	sort.Slice(results, func(i, j int) bool { return results[i].ResultID < results[j].ResultID })
	for _, result := range results {
		if result.TaskID != report.TaskID || result.SessionID != report.SessionID || result.CanonicalCommitted {
			continue
		}
		alreadyCanonical := HasCanonicalResult(*sessionState, result.ResultID)
		if !alreadyCanonical {
			updated, applied := ApplyResultToSession(*sessionState, result)
			if !applied {
				return errors.New("result was expected to be newly applied")
			}
			if err := session.Save(c.paths.SessionState, updated); err != nil {
				return err
			}
			*sessionState = updated
			if _, _, err := c.engines.MarkCommittedOnce(result.ResultID); err != nil {
				return err
			}
			if err := c.recordDecision(report, timeline.DomainEngine, "canonical_result_recovery", result.ResultID, DispositionCanonicalCommitApplied, "Completed engine result committed to canonical session exactly once", result); err != nil {
				return err
			}
			continue
		}
		if _, _, err := c.engines.MarkCommittedOnce(result.ResultID); err != nil {
			return err
		}
		if err := c.recordDecision(report, timeline.DomainEngine, "canonical_result_recovery", result.ResultID, DispositionCanonicalCommitReconciled, "Canonical result already existed after crash; ledger commit was reconciled without duplication", result); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) reconcileInterruptedExecutions(report *Report) error {
	executions := c.engines.Executions()
	results := c.engines.Results()
	completed := map[string]bool{}
	for _, result := range results {
		completed[result.ExecutionID] = true
	}
	sort.Slice(executions, func(i, j int) bool { return executions[i].ExecutionID < executions[j].ExecutionID })
	for _, execution := range executions {
		if execution.TaskID != report.TaskID || execution.SessionID != report.SessionID || completed[execution.ExecutionID] {
			continue
		}
		if execution.Resumable && execution.ExternalThreadID != "" {
			if err := c.recordDecision(report, timeline.DomainEngine, "engine_recovery", execution.ExecutionID, DispositionResumeRequired, "Durable external engine thread is available and requires resume", execution); err != nil {
				return err
			}
			continue
		}
		reason := fmt.Sprintf("engine execution %s cannot be resumed safely", execution.ExecutionID)
		state := c.tasks.State()
		terminal := state.Status == tasks.StatusCompleted || state.Status == tasks.StatusFailed || state.Status == tasks.StatusCancelled
		if !terminal && (state.Status != tasks.StatusInputRequired || state.NeedsInput != reason) {
			if _, err := c.tasks.Append(tasks.EventInputRequested, map[string]string{"reason": reason}); err != nil {
				return err
			}
		}
		if err := c.recordDecision(report, timeline.DomainEngine, "engine_recovery", execution.ExecutionID, DispositionInputRequired, "Interrupted non-resumable engine requires user input", execution); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) ensureReceipt(report *Report) error {
	state := c.tasks.State()
	if state.Status != tasks.StatusCompleted || !state.Progress().Complete {
		return nil
	}
	artifacts := c.artifacts.ForTask(state.TaskID, state.SessionID)
	proofArtifacts := make([]proofreceipt.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		proofArtifacts = append(proofArtifacts, proofreceipt.Artifact{Name: artifact.Name, Path: artifact.Path, SHA256: artifact.SHA256, Size: artifact.Size})
	}
	receipt, err := proofreceipt.Build(state, c.timeline.ByTask(state.TaskID), proofArtifacts)
	if err != nil {
		return err
	}
	saved, _, err := c.receipts.SaveOnce(receipt)
	if err != nil {
		return err
	}
	proofInput := timeline.Input{
		EventID: "recovery-proof-" + saved.ReceiptID, TaskID: state.TaskID, SessionID: state.SessionID,
		Domain: timeline.DomainProof, Kind: "proof_receipt_generated", SourceRef: saved.ReceiptID,
		Summary: "Integrated recovery Proof Receipt generated", DataHash: saved.InputDigest,
	}
	_, appended, err := c.timeline.AppendOnce(proofInput)
	if err != nil {
		return err
	}
	report.ReceiptID = saved.ReceiptID
	report.Decisions = append(report.Decisions, Decision{Domain: "proof", Reference: saved.ReceiptID, Disposition: DispositionProofReceiptReady, EventID: proofInput.EventID, Appended: appended})
	return nil
}

func (c *Coordinator) recordDecision(report *Report, domain timeline.Domain, kind, reference string, disposition Disposition, summary string, data any) error {
	hash := digestJSON(data)
	eventID := "recovery-" + shortDigest(report.TaskID+"|"+string(domain)+"|"+kind+"|"+reference+"|"+string(disposition))
	_, appended, err := c.timeline.AppendOnce(timeline.Input{
		EventID: eventID, TaskID: report.TaskID, SessionID: report.SessionID, Domain: domain,
		Kind: kind, SourceRef: reference, Summary: summary, DataHash: hash,
	})
	if err != nil {
		return err
	}
	report.Decisions = append(report.Decisions, Decision{Domain: string(domain), Reference: reference, Disposition: disposition, EventID: eventID, Appended: appended})
	return nil
}

func digestJSON(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func shortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}
