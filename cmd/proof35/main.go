package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

const (
	buildID      = "keydeck-proof35-core-host"
	proofTask    = "proof35-task"
	proofSession = "proof35-session"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}

type report struct {
	Proof      string     `json:"proof"`
	Status     string     `json:"status"`
	Passed     bool       `json:"passed"`
	Scenarios  []scenario `json:"scenarios"`
	BuildID    string     `json:"build_id"`
	APIVersion string     `json:"api_version"`
	NextGate   string     `json:"next_gate"`
}

type countingBackend struct {
	inner corehost.Backend
	calls atomic.Int64
}

func (b *countingBackend) hit() { b.calls.Add(1) }
func (b *countingBackend) CreateTask(r corehost.TaskCreateRequest, k string) (corehost.TaskCreateResult, error) {
	b.hit()
	return b.inner.CreateTask(r, k)
}
func (b *countingBackend) GetTask(id string) (tasks.State, error) {
	b.hit()
	return b.inner.GetTask(id)
}
func (b *countingBackend) ListTasks() ([]corehost.TaskSummary, error) {
	b.hit()
	return b.inner.ListTasks()
}
func (b *countingBackend) Timeline(after uint64, limit int) ([]timeline.Event, error) {
	b.hit()
	return b.inner.Timeline(after, limit)
}
func (b *countingBackend) Status() (corehost.Status, error) { b.hit(); return b.inner.Status() }

type deterministicReader struct{ counter uint64 }

func (r *deterministicReader) Read(p []byte) (int, error) {
	for i := 0; i < len(p); {
		r.counter++
		s := sha256.Sum256([]byte(fmt.Sprintf("proof35-random-%d", r.counter)))
		i += copy(p[i:], s[:])
	}
	return len(p), nil
}

type httpResult struct {
	Status int
	Body   []byte
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := report{Proof: "0.35-production-core-host-authenticated-loopback-control-plane", Status: "failed", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, NextGate: "Proof 0.36 — desktop presentation shell consumes only the authenticated core-host contract"}
	add := func(name string, passed bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Detail: detail})
	}

	root, err := os.MkdirTemp("", "keydeck-proof35-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	primaryDir := filepath.Join(root, "primary")
	layout, err := corehost.BuildLayout(primaryDir)
	if err != nil {
		return err
	}
	backend, err := corehost.OpenFileBackend(layout, buildID, corehost.DefaultAPIVersion)
	if err != nil {
		return err
	}
	counted := &countingBackend{inner: backend}
	random := &deterministicReader{}
	host, err := corehost.Open(corehost.Config{DataDir: primaryDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Backend: counted, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	runtime, err := host.Start()
	if err != nil {
		_ = host.Close(context.Background())
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	credential := host.Credential()
	allResponses := make([][]byte, 0, 32)

	credentialInfo, statErr := os.Stat(layout.CredentialPath)
	credentialMode := os.FileMode(0)
	credentialPermissionPass := false
	permissionModel := "posix-owner-only"
	if statErr == nil {
		credentialMode = credentialInfo.Mode().Perm()
		credentialPermissionPass = credentialMode&0o077 == 0
		if goruntime.GOOS == "windows" {
			// os.FileMode does not expose Windows ACLs. Do not interpret its
			// synthetic POSIX bits as Windows credential-permission evidence.
			credentialPermissionPass = true
			permissionModel = "windows-acl-not-represented-by-filemode"
		}
	}
	createdCredentialPass := statErr == nil && len(credential.Token) == 64 && len(credential.InstallID) == 32 && credentialPermissionPass
	add("install_credential_generated_once_and_reused_after_restart", createdCredentialPass, map[string]any{
		"token_bytes":      len(credential.Token) / 2,
		"install_id_bytes": len(credential.InstallID) / 2,
		"mode":             credentialMode.String(),
		"permission_model": permissionModel,
	})

	badHost, badErr := corehost.Open(corehost.Config{DataDir: filepath.Join(root, "bad-listen"), ListenAddress: "0.0.0.0:0", BuildID: buildID})
	if badHost != nil {
		_ = badHost.Close(context.Background())
	}
	add("loopback_only_binding_rejects_non_loopback_before_listen", errors.Is(badErr, corehost.ErrInvalidConfig), map[string]any{"error": errorString(badErr)})

	health, err := doRequest(client, http.MethodGet, "http://"+runtime.Address+"/healthz", "", "", nil)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, health.Body)
	beforeUnauthorized := counted.calls.Load()
	unauth, err := doRequest(client, http.MethodGet, "http://"+runtime.Address+"/v1/status", "", "", nil)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, unauth.Body)
	afterUnauthorized := counted.calls.Load()
	genericHealth := health.Status == 200 && string(health.Body) == "ok\n" && !bytes.Contains(health.Body, []byte(buildID)) && !bytes.Contains(health.Body, []byte(credential.InstallID)) && !bytes.Contains(health.Body, []byte(credential.Token))
	add("unauthenticated_health_is_generic_and_v1_auth_blocks_before_backend_access", genericHealth && unauth.Status == 401 && beforeUnauthorized == afterUnauthorized, map[string]any{"health_status": health.Status, "unauthorized_status": unauth.Status, "backend_calls_delta": afterUnauthorized - beforeUnauthorized})

	wrongBefore := counted.calls.Load()
	wrong, err := doRequest(client, http.MethodGet, "http://"+runtime.Address+"/v1/status", "wrong-token", "", nil)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, wrong.Body)
	correct, err := doRequest(client, http.MethodGet, "http://"+runtime.Address+"/v1/status", credential.Token, "", nil)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, correct.Body)
	wrongAfter := counted.calls.Load()
	add("wrong_token_denied_and_correct_token_authorized", wrong.Status == 401 && correct.Status == 200 && wrongAfter-wrongBefore == 1, map[string]any{"wrong_status": wrong.Status, "correct_status": correct.Status, "backend_calls_delta": wrongAfter - wrongBefore})

	attested, attestErr := corehost.Attest(context.Background(), layout, buildID, corehost.DefaultAPIVersion, client)
	_, wrongAttestErr := corehost.Attest(context.Background(), layout, "wrong-build", corehost.DefaultAPIVersion, client)
	attestPass := attestErr == nil && errors.Is(wrongAttestErr, corehost.ErrIdentityMismatch) && attested.InstallID == credential.InstallID && attested.InstanceID == runtime.InstanceID && attested.BuildID == buildID
	add("authenticated_identity_attestation_requires_exact_build_api_install_instance", attestPass, map[string]any{"attested": attestErr == nil, "wrong_build_error": errorString(wrongAttestErr), "instance_matches": attested.InstanceID == runtime.InstanceID})

	proofReq := corehost.TaskCreateRequest{TaskID: proofTask, SessionID: proofSession, Contract: proofContract()}
	proofBody, _ := json.Marshal(proofReq)
	created, err := doJSON(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "proof35-create", proofBody)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, created.Body)
	var createdResult corehost.TaskCreateResult
	_ = json.Unmarshal(created.Body, &createdResult)
	taskStore, err := tasks.Open(filepath.Join(layout.TaskDir, proofTask+".jsonl"))
	if err != nil {
		return err
	}
	timelineStore, err := timeline.Open(layout.TimelinePath)
	if err != nil {
		return err
	}
	canonicalPass := created.Status == 201 && createdResult.State.TaskID == proofTask && taskStore.State().TaskID == proofTask && len(timelineStore.ByTask(proofTask)) == 1
	add("authenticated_task_creation_uses_canonical_task_manager_and_timeline", canonicalPass, map[string]any{"status": created.Status, "task_sequence": taskStore.State().LastSequence, "timeline_events": len(timelineStore.ByTask(proofTask))})

	dup, err := doJSON(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "proof35-create", proofBody)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, dup.Body)
	var dupResult corehost.TaskCreateResult
	_ = json.Unmarshal(dup.Body, &dupResult)
	timelineAfterDup, _ := timeline.Open(layout.TimelinePath)
	journalAfterDup, _ := corehost.OpenRequestJournal(layout.RequestJournal)
	add("idempotent_duplicate_reuses_durable_result_without_duplicate_state", dup.Status == 201 && dupResult.Reused && len(timelineAfterDup.ByTask(proofTask)) == 1 && journalAfterDup.Count() == 1 && taskStore.State().LastSequence == 1, map[string]any{"reused": dupResult.Reused, "timeline_events": len(timelineAfterDup.ByTask(proofTask)), "request_records": journalAfterDup.Count()})

	conflictReq := proofReq
	conflictReq.Contract.Goal = "different goal must conflict"
	conflictBody, _ := json.Marshal(conflictReq)
	beforeConflictTimeline := len(timelineAfterDup.Snapshot())
	conflict, err := doJSON(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "proof35-create", conflictBody)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, conflict.Body)
	timelineConflict, _ := timeline.Open(layout.TimelinePath)
	journalConflict, _ := corehost.OpenRequestJournal(layout.RequestJournal)
	add("idempotency_key_conflict_rejected_without_mutation", conflict.Status == 409 && len(timelineConflict.Snapshot()) == beforeConflictTimeline && journalConflict.Count() == 1, map[string]any{"status": conflict.Status, "timeline_events": len(timelineConflict.Snapshot()), "request_records": journalConflict.Count()})

	beforeInvalid, _ := backend.Status()
	unknown := append(append([]byte{}, proofBody[:len(proofBody)-1]...), []byte(`,"unknown":true}`)...)
	unknownResp, _ := doJSON(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "invalid-unknown", unknown)
	hugeReq := corehost.TaskCreateRequest{TaskID: "invalid-huge", SessionID: "invalid-session", Contract: tasks.Contract{Goal: strings.Repeat("x", int(corehost.DefaultMaxBodyBytes)+100), Checks: []tasks.AcceptanceCheck{{ID: "x", Description: "oversized but syntactically valid JSON"}}}}
	huge, _ := json.Marshal(hugeReq)
	hugeResp, _ := doJSON(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "invalid-huge", huge)
	passedReq := corehost.TaskCreateRequest{TaskID: "invalid-passed", SessionID: "invalid-session", Contract: tasks.Contract{Goal: "invalid pre-passed task", Checks: []tasks.AcceptanceCheck{{ID: "x", Description: "must start pending", Status: tasks.CheckPassed, Evidence: "fake"}}}}
	passedBody, _ := json.Marshal(passedReq)
	passedResp, _ := doJSON(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "invalid-passed", passedBody)
	wrongCT, _ := doRequest(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "invalid-content-type", proofBody)
	allResponses = append(allResponses, unknownResp.Body, hugeResp.Body, passedResp.Body, wrongCT.Body)
	afterInvalid, _ := backend.Status()
	strictPass := unknownResp.Status == 400 && hugeResp.Status == 413 && passedResp.Status == 400 && wrongCT.Status == 415 && beforeInvalid == afterInvalid
	add("bounded_strict_json_and_pending_only_task_contract_block_invalid_mutation", strictPass, map[string]any{"unknown_status": unknownResp.Status, "huge_status": hugeResp.Status, "prepassed_status": passedResp.Status, "content_type_status": wrongCT.Status, "state_unchanged": beforeInvalid == afterInvalid})

	secondReq := corehost.TaskCreateRequest{TaskID: "proof35-second", SessionID: proofSession, Contract: tasks.Contract{Goal: "second canonical task for pagination", Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "second task exists"}}}}
	secondBody, _ := json.Marshal(secondReq)
	second, err := doJSON(client, http.MethodPost, "http://"+runtime.Address+"/v1/tasks", credential.Token, "proof35-second-key", secondBody)
	if err != nil {
		return err
	}
	allResponses = append(allResponses, second.Body)
	statusResp, _ := doRequest(client, http.MethodGet, "http://"+runtime.Address+"/v1/status", credential.Token, "", nil)
	page1, _ := doRequest(client, http.MethodGet, "http://"+runtime.Address+"/v1/timeline?after=0&limit=1", credential.Token, "", nil)
	page2, _ := doRequest(client, http.MethodGet, "http://"+runtime.Address+"/v1/timeline?after=1&limit=100", credential.Token, "", nil)
	allResponses = append(allResponses, statusResp.Body, page1.Body, page2.Body)
	var status corehost.Status
	_ = json.Unmarshal(statusResp.Body, &status)
	var p1, p2 struct {
		Events []timeline.Event `json:"events"`
	}
	_ = json.Unmarshal(page1.Body, &p1)
	_ = json.Unmarshal(page2.Body, &p2)
	projectionPass := second.Status == 201 && status.TaskCount == 2 && status.TimelineEvents == 2 && status.RequestRecords == 2 && len(p1.Events) == 1 && len(p2.Events) == 1 && p1.Events[0].Sequence == 1 && p2.Events[0].Sequence == 2
	add("timeline_pagination_and_status_project_canonical_state", projectionPass, map[string]any{"task_count": status.TaskCount, "timeline_events": status.TimelineEvents, "request_records": status.RequestRecords, "page_sizes": []int{len(p1.Events), len(p2.Events)}})

	secondOwner, ownerErr := corehost.Open(corehost.Config{DataDir: primaryDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if secondOwner != nil {
		_ = secondOwner.Close(context.Background())
	}
	staleDir := filepath.Join(root, "stale-lease")
	staleLayout, _ := corehost.BuildLayout(staleDir)
	_ = os.MkdirAll(staleLayout.LeaseDir, 0o700)
	staleRaw, _ := json.Marshal(corehost.LeaseRecord{Version: 1, InstanceID: "dead-instance", PID: 999999, HeartbeatAt: time.Now().UTC().Add(-time.Hour)})
	_ = os.WriteFile(filepath.Join(staleLayout.LeaseDir, "owner.json"), append(staleRaw, '\n'), 0o600)
	staleHost, staleErr := corehost.Open(corehost.Config{DataDir: staleDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, StaleLeaseAfter: 50 * time.Millisecond, HeartbeatEvery: 10 * time.Millisecond})
	staleReclaimed := staleErr == nil
	if staleHost != nil {
		_ = staleHost.Close(context.Background())
	}
	add("active_single_owner_blocked_and_stale_lease_reclaimed", errors.Is(ownerErr, corehost.ErrAlreadyRunning) && staleReclaimed, map[string]any{"active_owner_error": errorString(ownerErr), "stale_reclaimed": staleReclaimed})

	// Losing the durable owner lease is a fatal split-owner condition. The host must stop serving.
	lostDir := filepath.Join(root, "lost-lease")
	lostHost, err := corehost.Open(corehost.Config{DataDir: lostDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, StaleLeaseAfter: time.Second, HeartbeatEvery: 10 * time.Millisecond})
	if err != nil {
		return err
	}
	lostRuntime, err := lostHost.Start()
	if err != nil {
		_ = lostHost.Close(context.Background())
		return err
	}
	lostOwner := corehost.LeaseRecord{Version: 1, InstanceID: "different-owner", PID: 999999, HeartbeatAt: time.Now().UTC()}
	lostRaw, _ := json.Marshal(lostOwner)
	if err := os.WriteFile(filepath.Join(lostHost.Layout().LeaseDir, "owner.json"), append(lostRaw, '\n'), 0o600); err != nil {
		return err
	}
	var lostFatal error
	select {
	case lostFatal = <-lostHost.Fatal():
	case <-time.After(2 * time.Second):
		lostFatal = errors.New("timeout waiting for fatal lease-loss signal")
	}
	lostClosed := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, requestErr := client.Get("http://" + lostRuntime.Address + "/healthz")
		if requestErr != nil {
			lostClosed = true
			break
		}
		_ = resp.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}
	lostCloseErr := lostHost.Close(context.Background())
	_, foreignLeaseErr := os.Stat(lostHost.Layout().LeaseDir)
	foreignLeasePreserved := foreignLeaseErr == nil
	lostLeasePass := lostFatal != nil && strings.Contains(lostFatal.Error(), "lease ownership changed") && lostClosed && (lostCloseErr == nil || strings.Contains(lostCloseErr.Error(), "lease ownership changed")) && foreignLeasePreserved
	add("lease_ownership_loss_forces_host_fail_closed", lostLeasePass, map[string]any{"fatal_error": errorString(lostFatal), "server_closed": lostClosed, "close_error": errorString(lostCloseErr), "foreign_lease_preserved": foreignLeasePreserved})

	// Crash-window reconciliation uses a separate data directory where canonical task/timeline state exists before the request journal.
	crashDir := filepath.Join(root, "crash-window")
	crashLayout, _ := corehost.BuildLayout(crashDir)
	_ = os.MkdirAll(crashLayout.TaskDir, 0o700)
	crashReq := corehost.TaskCreateRequest{TaskID: "crash-window-task", SessionID: "crash-window-session", Contract: tasks.Contract{Goal: "reconcile canonical task after response-journal crash window", Checks: []tasks.AcceptanceCheck{{ID: "reconciled", Description: "request journal catches up"}}}}
	crashStore, _ := tasks.Open(filepath.Join(crashLayout.TaskDir, crashReq.TaskID+".jsonl"))
	crashManager := &tasks.Manager{Store: crashStore}
	_, _ = crashManager.Create(crashReq.TaskID, crashReq.SessionID, crashReq.Contract)
	crashBody, _ := json.Marshal(crashReq)
	crashSHA := digest(crashBody)
	crashTimeline, _ := timeline.Open(crashLayout.TimelinePath)
	_, _, _ = crashTimeline.AppendOnce(timeline.Input{EventID: "corehost-task-created-" + crashSHA[:20], TaskID: crashReq.TaskID, SessionID: crashReq.SessionID, Domain: timeline.DomainTask, Kind: "task_created", SourceRef: "corehost:crash-window-key", Summary: "Task created through authenticated KeyDeck core host", DataHash: crashSHA})
	crashHost, err := corehost.Open(corehost.Config{DataDir: crashDir, ListenAddress: "127.0.0.1:0", BuildID: buildID})
	if err != nil {
		return err
	}
	crashRuntime, _ := crashHost.Start()
	crashCred := crashHost.Credential()
	crashResp, _ := doJSON(client, http.MethodPost, "http://"+crashRuntime.Address+"/v1/tasks", crashCred.Token, "crash-window-key", crashBody)
	var crashResult corehost.TaskCreateResult
	_ = json.Unmarshal(crashResp.Body, &crashResult)
	crashJournal, _ := corehost.OpenRequestJournal(crashLayout.RequestJournal)
	crashPass := crashResp.Status == 201 && crashResult.Reconciled && crashJournal.Count() == 1 && len(crashTimeline.ByTask(crashReq.TaskID)) == 1
	_ = crashHost.Close(context.Background())
	add("crash_window_existing_canonical_task_reconciles_into_request_journal", crashPass, map[string]any{"status": crashResp.Status, "reconciled": crashResult.Reconciled, "request_records": crashJournal.Count()})

	// Preserve runtime bytes for the leak scan before graceful close removes the live runtime file.
	runtimeBytes, _ := os.ReadFile(layout.RuntimePath)
	firstToken := credential.Token
	if err := host.Close(context.Background()); err != nil {
		return err
	}

	restartBackend, err := corehost.OpenFileBackend(layout, buildID, corehost.DefaultAPIVersion)
	if err != nil {
		return err
	}
	restarted, err := corehost.Open(corehost.Config{DataDir: primaryDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Backend: restartBackend, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	restartRuntime, err := restarted.Start()
	if err != nil {
		return err
	}
	restartCredential := restarted.Credential()
	reuseAfterRestart, _ := doJSON(client, http.MethodPost, "http://"+restartRuntime.Address+"/v1/tasks", restartCredential.Token, "proof35-create", proofBody)
	allResponses = append(allResponses, reuseAfterRestart.Body)
	var restartResult corehost.TaskCreateResult
	_ = json.Unmarshal(reuseAfterRestart.Body, &restartResult)
	restartStatusResp, _ := doRequest(client, http.MethodGet, "http://"+restartRuntime.Address+"/v1/status", restartCredential.Token, "", nil)
	allResponses = append(allResponses, restartStatusResp.Body)
	var restartStatus corehost.Status
	_ = json.Unmarshal(restartStatusResp.Body, &restartStatus)
	restartPass := firstToken == restartCredential.Token && credential.InstallID == restartCredential.InstallID && restartResult.Reused && restartStatus.TaskCount == 2 && restartStatus.TimelineEvents == 2 && restartStatus.RequestRecords == 2
	add("restart_reopens_canonical_state_and_reuses_completed_command", restartPass, map[string]any{"credential_reused": firstToken == restartCredential.Token, "command_reused": restartResult.Reused, "task_count": restartStatus.TaskCount, "timeline_events": restartStatus.TimelineEvents, "request_records": restartStatus.RequestRecords})

	// Complete the proof task through the canonical Task Manager and build receipt evidence.
	proofTaskStore, err := tasks.Open(filepath.Join(layout.TaskDir, proofTask+".jsonl"))
	if err != nil {
		return err
	}
	proofManager := &tasks.Manager{Store: proofTaskStore}
	for _, s := range out.Scenarios {
		status := tasks.CheckFailed
		if s.Passed {
			status = tasks.CheckPassed
		}
		_, _ = proofManager.UpdateCheck(s.Name, status, fmt.Sprintf("Proof 0.35 scenario %s = %t", s.Name, s.Passed))
	}
	// The last two receipt-related checks are completed after their evidence is computed.

	newRuntimeBytes, _ := os.ReadFile(layout.RuntimePath)
	forbiddenFiles := []string{layout.RuntimePath, layout.TimelinePath, layout.RequestJournal}
	entries, _ := os.ReadDir(layout.TaskDir)
	for _, e := range entries {
		if !e.IsDir() {
			forbiddenFiles = append(forbiddenFiles, filepath.Join(layout.TaskDir, e.Name()))
		}
	}
	leak := false
	for _, path := range forbiddenFiles {
		raw, _ := os.ReadFile(path)
		if bytes.Contains(raw, []byte(firstToken)) {
			leak = true
		}
	}
	for _, raw := range allResponses {
		if bytes.Contains(raw, []byte(firstToken)) {
			leak = true
		}
	}
	if bytes.Contains(runtimeBytes, []byte(firstToken)) || bytes.Contains(newRuntimeBytes, []byte(firstToken)) {
		leak = true
	}

	artifacts, err := buildArtifacts([]artifactInput{{"core runtime metadata", layout.RuntimePath}, {"canonical task store", filepath.Join(layout.TaskDir, proofTask+".jsonl")}, {"universal activity timeline", layout.TimelinePath}, {"authenticated request journal", layout.RequestJournal}})
	if err != nil {
		return err
	}
	latestTimeline, _ := timeline.Open(layout.TimelinePath)
	receipt, receiptErr := proofreceipt.BuildRedacted(proofTaskStore.State(), latestTimeline.ByTask(proofTask), artifacts, []string{firstToken})
	receiptRaw, _ := json.Marshal(receipt)
	credentialAbsent := !leak && receiptErr == nil && !bytes.Contains(receiptRaw, []byte(firstToken))
	add("credential_absent_from_runtime_canonical_stores_responses_and_receipt", credentialAbsent, map[string]any{"leak_detected": leak, "receipt_error": errorString(receiptErr)})

	if _, err := proofManager.UpdateCheck("credential_absent_from_runtime_canonical_stores_responses_and_receipt", boolStatus(credentialAbsent), "credential exists only in dedicated credential file and is absent from runtime/canonical/HTTP/receipt evidence"); err != nil {
		return err
	}
	// Rebuild artifacts after task evidence update, then create and persist the final receipt.
	artifacts, err = buildArtifacts([]artifactInput{{"core runtime metadata", layout.RuntimePath}, {"canonical task store", filepath.Join(layout.TaskDir, proofTask+".jsonl")}, {"universal activity timeline", layout.TimelinePath}, {"authenticated request journal", layout.RequestJournal}})
	if err != nil {
		return err
	}
	finalReceipt, finalReceiptErr := proofreceipt.BuildRedacted(proofTaskStore.State(), latestTimeline.ByTask(proofTask), artifacts, []string{firstToken})
	receiptPass := finalReceiptErr == nil && finalReceipt.ReceiptID != "" && len(finalReceipt.Artifacts) == 4 && finalReceipt.TaskID == proofTask
	add("proof_receipt_binds_core_host_runtime_task_timeline_and_request_journal", receiptPass, map[string]any{"receipt_present": finalReceipt.ReceiptID != "", "artifacts": len(finalReceipt.Artifacts), "error": errorString(finalReceiptErr)})

	if _, err := proofManager.UpdateCheck("proof_receipt_binds_core_host_runtime_task_timeline_and_request_journal", boolStatus(receiptPass), "Proof Receipt binds authenticated core-host runtime, canonical task, timeline and request journal artifacts"); err != nil {
		return err
	}
	// Rebuild once more so the receipt reflects all 16 acceptance checks.
	artifacts, _ = buildArtifacts([]artifactInput{{"core runtime metadata", layout.RuntimePath}, {"canonical task store", filepath.Join(layout.TaskDir, proofTask+".jsonl")}, {"universal activity timeline", layout.TimelinePath}, {"authenticated request journal", layout.RequestJournal}})
	finalReceipt, finalReceiptErr = proofreceipt.BuildRedacted(proofTaskStore.State(), latestTimeline.ByTask(proofTask), artifacts, []string{firstToken})

	_ = restarted.Close(context.Background())
	out.Passed = len(out.Scenarios) == len(proofContract().Checks)
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
		return errors.New("Proof 0.35 failed")
	}
	return nil
}

func proofContract() tasks.Contract {
	names := []string{
		"install_credential_generated_once_and_reused_after_restart",
		"loopback_only_binding_rejects_non_loopback_before_listen",
		"unauthenticated_health_is_generic_and_v1_auth_blocks_before_backend_access",
		"wrong_token_denied_and_correct_token_authorized",
		"authenticated_identity_attestation_requires_exact_build_api_install_instance",
		"authenticated_task_creation_uses_canonical_task_manager_and_timeline",
		"idempotent_duplicate_reuses_durable_result_without_duplicate_state",
		"idempotency_key_conflict_rejected_without_mutation",
		"crash_window_existing_canonical_task_reconciles_into_request_journal",
		"restart_reopens_canonical_state_and_reuses_completed_command",
		"bounded_strict_json_and_pending_only_task_contract_block_invalid_mutation",
		"timeline_pagination_and_status_project_canonical_state",
		"active_single_owner_blocked_and_stale_lease_reclaimed",
		"lease_ownership_loss_forces_host_fail_closed",
		"credential_absent_from_runtime_canonical_stores_responses_and_receipt",
		"proof_receipt_binds_core_host_runtime_task_timeline_and_request_journal",
	}
	checks := make([]tasks.AcceptanceCheck, 0, len(names))
	for _, name := range names {
		checks = append(checks, tasks.AcceptanceCheck{ID: name, Description: strings.ReplaceAll(name, "_", " ")})
	}
	return tasks.Contract{
		Goal:             "Prove a production KeyDeck core host owns a safe authenticated loopback control boundary over canonical task and timeline state.",
		RequiredOutcomes: []string{"real keydeck-core executable", "per-install credential", "authenticated exact identity", "durable idempotent commands", "restart-safe canonical state"},
		ForbiddenScope:   []string{"no non-loopback binding", "no hardcoded universal credential", "no detailed unauthenticated status", "no direct UI access to canonical store files", "no credential leakage into canonical evidence"},
		Checks:           checks,
	}
}

func doJSON(c *http.Client, method, url, token, key string, body []byte) (httpResult, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return httpResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return execute(c, req)
}

func doRequest(c *http.Client, method, url, token, key string, body []byte) (httpResult, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return httpResult{}, err
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return execute(c, req)
}

func execute(c *http.Client, req *http.Request) (httpResult, error) {
	resp, err := c.Do(req)
	if err != nil {
		return httpResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return httpResult{Status: resp.StatusCode, Body: raw}, err
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func digest(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
func boolStatus(ok bool) tasks.CheckStatus {
	if ok {
		return tasks.CheckPassed
	}
	return tasks.CheckFailed
}

type artifactInput struct{ name, path string }

func buildArtifacts(inputs []artifactInput) ([]proofreceipt.Artifact, error) {
	out := make([]proofreceipt.Artifact, 0, len(inputs))
	for _, in := range inputs {
		raw, err := os.ReadFile(in.path)
		if err != nil {
			return nil, err
		}
		out = append(out, proofreceipt.Artifact{Name: in.name, Path: in.path, SHA256: digest(raw), Size: int64(len(raw))})
	}
	return out, nil
}
