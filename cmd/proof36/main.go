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
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
	"keydeck.local/feasibilitylab/internal/presentation"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

const buildID = "keydeck-proof36-presentation-shell"

type scenario struct {
	Name     string         `json:"name"`
	Passed   bool           `json:"passed"`
	Evidence map[string]any `json:"evidence,omitempty"`
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
		s := sha256.Sum256([]byte(fmt.Sprintf("proof36-random-%d", r.counter)))
		i += copy(p[i:], s[:])
	}
	return len(p), nil
}

type countingTransport struct {
	calls atomic.Int64
	inner http.RoundTripper
}

func (t *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	if t.inner == nil {
		return http.DefaultTransport.RoundTrip(r)
	}
	return t.inner.RoundTrip(r)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := report{
		Proof:      "0.36-authenticated-core-client-and-presentation-projection-boundary",
		Status:     "failed",
		BuildID:    buildID,
		APIVersion: corehost.DefaultAPIVersion,
		NextGate:   "Proof 0.37 — visual desktop renderer consumes only the proven presentation snapshot/command boundary",
	}
	add := func(name string, passed bool, evidence map[string]any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Evidence: evidence})
	}

	root, err := os.MkdirTemp("", "keydeck-proof36-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	dataDir := filepath.Join(root, "primary")
	layout, err := corehost.BuildLayout(dataDir)
	if err != nil {
		return err
	}
	random := &deterministicReader{}
	backend, err := corehost.OpenFileBackend(layout, buildID, corehost.DefaultAPIVersion)
	if err != nil {
		return err
	}
	counted := &countingBackend{inner: backend}
	host, err := corehost.Open(corehost.Config{DataDir: dataDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Backend: counted, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	if _, err := host.Start(); err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = host.Close(context.Background())
		}
	}()

	shell := presentation.New(layout, buildID, corehost.DefaultAPIVersion, nil)
	beforeConnectCalls := counted.calls.Load()
	connectErr := shell.Connect(context.Background())
	identity, identityErr := shell.Identity()
	add("exact_identity_attestation_precedes_projection", connectErr == nil && identityErr == nil && identity.Product == "KeyDeck" && identity.BuildID == buildID && counted.calls.Load() == beforeConnectCalls, map[string]any{"backend_calls_before_projection": counted.calls.Load() - beforeConnectCalls})

	wrong := presentation.New(layout, "wrong-build", corehost.DefaultAPIVersion, nil)
	beforeWrong := counted.calls.Load()
	wrongErr := wrong.Connect(context.Background())
	add("wrong_build_blocks_before_backend_projection", errors.Is(wrongErr, corehost.ErrIdentityMismatch) && counted.calls.Load() == beforeWrong, map[string]any{"backend_calls": counted.calls.Load() - beforeWrong})

	nonLoopDir := filepath.Join(root, "nonloop")
	nonLoopLayout, _ := corehost.BuildLayout(nonLoopDir)
	_ = os.MkdirAll(nonLoopDir, 0o700)
	writeJSON(nonLoopLayout.RuntimePath, corehost.RuntimeInfo{Version: 1, InstanceID: strings.Repeat("1", 32), InstallID: strings.Repeat("2", 32), Address: "203.0.113.1:4444", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, PID: 1})
	transport := &countingTransport{}
	nonLoop := presentation.New(nonLoopLayout, buildID, corehost.DefaultAPIVersion, &http.Client{Transport: transport})
	nonLoopErr := nonLoop.Connect(context.Background())
	add("non_loopback_runtime_rejected_before_credential_or_network", errors.Is(nonLoopErr, corehost.ErrIdentityMismatch) && transport.calls.Load() == 0, map[string]any{"network_calls": transport.calls.Load()})

	attestRacePass, attestRaceEvidence, err := attestationRaceScenario(filepath.Join(root, "attestation-race"))
	if err != nil {
		return err
	}
	add("attestation_rejects_runtime_instance_change_during_identity_roundtrip", attestRacePass, attestRaceEvidence)

	responseRacePass, responseRaceEvidence, err := responseRaceScenario(filepath.Join(root, "response-race"))
	if err != nil {
		return err
	}
	add("api_response_rejected_when_runtime_instance_changes_mid_roundtrip", responseRacePass, responseRaceEvidence)

	emptySnap, refreshErr := shell.Refresh(context.Background(), 0, 100)
	add("refresh_projects_authenticated_canonical_status_tasks_and_timeline", refreshErr == nil && emptySnap.Connected && emptySnap.Status.TaskCount == 0 && len(emptySnap.Tasks) == 0 && len(emptySnap.Timeline) == 0, map[string]any{"task_count": len(emptySnap.Tasks), "timeline_count": len(emptySnap.Timeline)})

	credential, err := corehost.ReadCredential(layout.CredentialPath)
	if err != nil {
		return err
	}
	snapshotRaw, _ := json.Marshal(emptySnap)
	add("presentation_snapshot_excludes_install_credential_token", !bytes.Contains(snapshotRaw, []byte(credential.Token)), map[string]any{"token_present": bytes.Contains(snapshotRaw, []byte(credential.Token))})

	req1 := corehost.TaskCreateRequest{TaskID: "proof36-task-one", SessionID: "proof36-session", Contract: tasks.Contract{Goal: "prove presentation command routing", Checks: []tasks.AcceptanceCheck{{ID: "visible", Description: "task appears through presentation projection"}}}}
	created, createErr := shell.CreateTask(context.Background(), "proof36-create-one", req1)
	taskStore, taskErr := tasks.Open(filepath.Join(layout.TaskDir, req1.TaskID+".jsonl"))
	state := taskStore.State()
	snap1, snap1Err := shell.Refresh(context.Background(), 0, 100)
	add("task_create_routes_through_core_into_canonical_task_and_timeline", createErr == nil && taskErr == nil && state.TaskID == req1.TaskID && snap1Err == nil && len(snap1.Tasks) == 1 && len(snap1.Timeline) == 1 && created.State.TaskID == req1.TaskID, map[string]any{"tasks": len(snap1.Tasks), "timeline": len(snap1.Timeline)})

	shell.Disconnect()
	reconnectErr := shell.Connect(context.Background())
	reused, reuseErr := shell.CreateTask(context.Background(), "proof36-create-one", req1)
	snapReuse, reuseSnapErr := shell.Refresh(context.Background(), 0, 100)
	add("duplicate_idempotent_create_reuses_after_shell_reconnect", reconnectErr == nil && reuseErr == nil && reused.Reused && reuseSnapErr == nil && len(snapReuse.Tasks) == 1 && len(snapReuse.Timeline) == 1 && snapReuse.Status.RequestRecords == 1, map[string]any{"reused": reused.Reused, "tasks": len(snapReuse.Tasks), "timeline": len(snapReuse.Timeline), "request_records": snapReuse.Status.RequestRecords})

	beforeConflict, err := canonicalStateSHA(layout)
	if err != nil {
		return err
	}
	conflictReq := req1
	conflictReq.Contract.Goal = "different canonical goal"
	_, conflictErr := shell.CreateTask(context.Background(), "proof36-create-one", conflictReq)
	afterConflict, err := canonicalStateSHA(layout)
	if err != nil {
		return err
	}
	add("conflicting_idempotency_key_rejected_without_canonical_mutation", errors.Is(conflictErr, corehost.ErrIdempotencyConflict) && beforeConflict == afterConflict, map[string]any{"state_unchanged": beforeConflict == afterConflict})

	req2 := corehost.TaskCreateRequest{TaskID: "proof36-task-two", SessionID: "proof36-session", Contract: tasks.Contract{Goal: "prove exact timeline cursor pagination", Checks: []tasks.AcceptanceCheck{{ID: "paged", Description: "event appears on next page"}}}}
	if _, err := shell.CreateTask(context.Background(), "proof36-create-two", req2); err != nil {
		return err
	}
	page1, err := shell.Refresh(context.Background(), 0, 1)
	if err != nil {
		return err
	}
	page2, err := shell.Refresh(context.Background(), page1.NextAfter, 1)
	if err != nil {
		return err
	}
	paginationPass := len(page1.Timeline) == 1 && len(page2.Timeline) == 1 && page1.NextAfter == page1.Timeline[0].Sequence && page2.Timeline[0].Sequence > page1.NextAfter
	add("timeline_cursor_paginates_without_overlap_or_skip", paginationPass, map[string]any{"page1": len(page1.Timeline), "page2": len(page2.Timeline), "monotonic": paginationPass})

	if err := host.Close(context.Background()); err != nil {
		return err
	}
	closed = true
	restartBackend, err := corehost.OpenFileBackend(layout, buildID, corehost.DefaultAPIVersion)
	if err != nil {
		return err
	}
	restartCounted := &countingBackend{inner: restartBackend}
	restarted, err := corehost.Open(corehost.Config{DataDir: dataDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Backend: restartCounted, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	if _, err := restarted.Start(); err != nil {
		return err
	}
	host = restarted
	closed = false
	staleBefore := restartCounted.calls.Load()
	_, staleErr := shell.Refresh(context.Background(), 0, 100)
	add("stale_client_rejects_new_core_instance_before_backend_request", errors.Is(staleErr, corehost.ErrIdentityMismatch) && restartCounted.calls.Load() == staleBefore, map[string]any{"backend_calls": restartCounted.calls.Load() - staleBefore})

	reconnectErr = shell.Connect(context.Background())
	recoveredSnap, recoveredErr := shell.Refresh(context.Background(), 0, 100)
	add("reconnect_attests_new_instance_and_restores_same_canonical_projection", reconnectErr == nil && recoveredErr == nil && len(recoveredSnap.Tasks) == 2 && len(recoveredSnap.Timeline) == 2 && recoveredSnap.Status.RequestRecords == 2, map[string]any{"tasks": len(recoveredSnap.Tasks), "timeline": len(recoveredSnap.Timeline), "request_records": recoveredSnap.Status.RequestRecords})

	beforeOffline, err := canonicalStateSHA(layout)
	if err != nil {
		return err
	}
	if err := host.Close(context.Background()); err != nil {
		return err
	}
	closed = true
	_, offlineErr := shell.Refresh(context.Background(), 0, 100)
	afterOffline, err := canonicalStateSHA(layout)
	if err != nil {
		return err
	}
	add("core_unreachable_has_no_direct_store_fallback_or_mutation", offlineErr != nil && beforeOffline == afterOffline, map[string]any{"refresh_failed": offlineErr != nil, "state_unchanged": beforeOffline == afterOffline})

	redirectPass, redirectEvidence, err := redirectAndProxyScenario(filepath.Join(root, "redirect"))
	if err != nil {
		return err
	}
	add("redirect_and_proxy_paths_cannot_receive_core_credential", redirectPass, redirectEvidence)

	responsePass, responseEvidence, err := malformedAndOversizedScenario(filepath.Join(root, "malformed"))
	if err != nil {
		return err
	}
	add("oversized_and_malformed_core_responses_fail_closed", responsePass, responseEvidence)

	inconsistentPass, inconsistentEvidence, err := inconsistentProjectionScenario(filepath.Join(root, "inconsistent"))
	if err != nil {
		return err
	}
	add("inconsistent_projection_payloads_are_rejected_even_when_authenticated", inconsistentPass, inconsistentEvidence)

	finalBackend, err := corehost.OpenFileBackend(layout, buildID, corehost.DefaultAPIVersion)
	if err != nil {
		return err
	}
	finalHost, err := corehost.Open(corehost.Config{DataDir: dataDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Backend: finalBackend, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	if _, err := finalHost.Start(); err != nil {
		return err
	}
	host = finalHost
	closed = false
	if err := shell.Connect(context.Background()); err != nil {
		return err
	}
	beforeConcurrent, err := canonicalStateSHA(layout)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var failures atomic.Int64
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap, err := shell.Refresh(context.Background(), 0, 100)
			if err != nil || len(snap.Tasks) != 2 || len(snap.Timeline) != 2 {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	afterConcurrent, err := canonicalStateSHA(layout)
	if err != nil {
		return err
	}
	add("concurrent_refreshes_are_read_only_and_consistent", failures.Load() == 0 && beforeConcurrent == afterConcurrent, map[string]any{"refreshes": 32, "failures": failures.Load(), "state_unchanged": beforeConcurrent == afterConcurrent})

	atomicPass, atomicEvidence, err := atomicProjectionScenario(filepath.Join(root, "atomic-projection"))
	if err != nil {
		return err
	}
	add("atomic_projection_remains_internally_consistent_during_concurrent_commands", atomicPass, atomicEvidence)

	shell.Disconnect()
	_, disconnectedErr := shell.Refresh(context.Background(), 0, 100)
	add("explicit_disconnect_forgets_client_and_requires_reattestation", errors.Is(disconnectedErr, presentation.ErrDisconnected), map[string]any{"disconnected": errors.Is(disconnectedErr, presentation.ErrDisconnected)})

	all := true
	for _, s := range out.Scenarios {
		all = all && s.Passed
	}
	out.Passed = all && len(out.Scenarios) == 20
	if out.Passed {
		out.Status = "passed"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	if !out.Passed {
		return errors.New("Proof 0.36 failed")
	}
	return nil
}

func attestationRaceScenario(dir string) (bool, map[string]any, error) {
	layout, _ := corehost.BuildLayout(dir)
	credential := corehost.Credential{Version: 1, InstallID: strings.Repeat("9", 32), Token: strings.Repeat("8", 64)}
	if err := writeJSON(layout.CredentialPath, credential); err != nil {
		return false, nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, nil, err
	}
	defer ln.Close()
	original := corehost.RuntimeInfo{Version: 1, InstanceID: strings.Repeat("7", 32), InstallID: credential.InstallID, Address: ln.Addr().String(), BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, PID: 1}
	if err := writeJSON(layout.RuntimePath, original); err != nil {
		return false, nil, err
	}
	var calls atomic.Int64
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		changed := original
		changed.InstanceID = strings.Repeat("6", 32)
		_ = writeJSON(layout.RuntimePath, changed)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(corehost.Identity{Product: "KeyDeck", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, InstallID: credential.InstallID, InstanceID: original.InstanceID})
	})}
	go server.Serve(ln)
	defer server.Close()
	_, connectErr := corehost.Connect(context.Background(), layout, buildID, corehost.DefaultAPIVersion, nil)
	pass := errors.Is(connectErr, corehost.ErrIdentityMismatch) && calls.Load() == 1
	return pass, map[string]any{"identity_calls": calls.Load(), "stale_attestation_rejected": errors.Is(connectErr, corehost.ErrIdentityMismatch)}, nil
}

func inconsistentProjectionScenario(dir string) (bool, map[string]any, error) {
	layout, _ := corehost.BuildLayout(dir)
	credential := corehost.Credential{Version: 1, InstallID: strings.Repeat("1", 32), Token: strings.Repeat("0", 64)}
	if err := writeJSON(layout.CredentialPath, credential); err != nil {
		return false, nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, nil, err
	}
	defer ln.Close()
	identity := corehost.Identity{Product: "KeyDeck", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, InstallID: credential.InstallID, InstanceID: strings.Repeat("a", 32)}
	if err := writeJSON(layout.RuntimePath, corehost.RuntimeInfo{Version: 1, InstanceID: identity.InstanceID, InstallID: credential.InstallID, Address: ln.Addr().String(), BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, PID: 1}); err != nil {
		return false, nil, err
	}
	var mode atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/identity", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(identity)
	})
	mux.HandleFunc("/v1/snapshot", func(w http.ResponseWriter, r *http.Request) {
		current := mode.Add(1)
		w.Header().Set("Content-Type", "application/json")
		snapshot := corehost.ProjectionSnapshot{Identity: identity, Status: corehost.Status{Product: "KeyDeck", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion}, After: 0, NextAfter: 0}
		switch current {
		case 1:
			snapshot.Status.TaskCount = 2
			snapshot.Tasks = []corehost.TaskSummary{{TaskID: "one"}}
		case 2:
			snapshot.Status.TaskCount = 2
			snapshot.Tasks = []corehost.TaskSummary{{TaskID: "dup"}, {TaskID: "dup"}}
		default:
			snapshot.Status.TimelineEvents = 2
			snapshot.Timeline = []corehost.TimelineEvent{{Sequence: 2}, {Sequence: 1}}
			snapshot.NextAfter = 1
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	defer server.Close()
	shell := presentation.New(layout, buildID, corehost.DefaultAPIVersion, nil)
	if err := shell.Connect(context.Background()); err != nil {
		return false, nil, err
	}
	_, countErr := shell.Refresh(context.Background(), 0, 100)
	_, duplicateErr := shell.Refresh(context.Background(), 0, 100)
	_, orderErr := shell.Refresh(context.Background(), 0, 100)
	pass := errors.Is(countErr, corehost.ErrIdentityMismatch) && errors.Is(duplicateErr, corehost.ErrIdentityMismatch) && errors.Is(orderErr, corehost.ErrIdentityMismatch)
	return pass, map[string]any{"count_mismatch_rejected": errors.Is(countErr, corehost.ErrIdentityMismatch), "duplicate_task_rejected": errors.Is(duplicateErr, corehost.ErrIdentityMismatch), "non_monotonic_timeline_rejected": errors.Is(orderErr, corehost.ErrIdentityMismatch)}, nil
}

func atomicProjectionScenario(dir string) (bool, map[string]any, error) {
	layout, err := corehost.BuildLayout(dir)
	if err != nil {
		return false, nil, err
	}
	random := &deterministicReader{}
	host, err := corehost.Open(corehost.Config{DataDir: dir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return false, nil, err
	}
	if _, err := host.Start(); err != nil {
		return false, nil, err
	}
	defer host.Close(context.Background())
	shell := presentation.New(layout, buildID, corehost.DefaultAPIVersion, nil)
	if err := shell.Connect(context.Background()); err != nil {
		return false, nil, err
	}
	var createFailures atomic.Int64
	var snapshotFailures atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			id := fmt.Sprintf("atomic-task-%02d", i)
			req := corehost.TaskCreateRequest{TaskID: id, SessionID: "atomic-session", Contract: tasks.Contract{Goal: "prove atomic presentation projection", Checks: []tasks.AcceptanceCheck{{ID: "visible", Description: "task appears atomically"}}}}
			if _, err := shell.CreateTask(context.Background(), "atomic-key-"+id, req); err != nil {
				createFailures.Add(1)
			}
		}
	}()
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap, err := shell.Refresh(context.Background(), 0, 100)
			if err != nil || snap.Status.TaskCount != len(snap.Tasks) || snap.Status.TimelineEvents != len(snap.Timeline) || snap.Status.RequestRecords != len(snap.Tasks) {
				snapshotFailures.Add(1)
			}
		}()
	}
	wg.Wait()
	final, err := shell.Refresh(context.Background(), 0, 100)
	if err != nil {
		return false, nil, err
	}
	pass := createFailures.Load() == 0 && snapshotFailures.Load() == 0 && len(final.Tasks) == 20 && len(final.Timeline) == 20 && final.Status.RequestRecords == 20
	return pass, map[string]any{"creates": 20, "create_failures": createFailures.Load(), "refreshes": 64, "snapshot_inconsistencies": snapshotFailures.Load(), "final_tasks": len(final.Tasks)}, nil
}

func responseRaceScenario(dir string) (bool, map[string]any, error) {
	layout, _ := corehost.BuildLayout(dir)
	credential := corehost.Credential{Version: 1, InstallID: strings.Repeat("5", 32), Token: strings.Repeat("4", 64)}
	if err := writeJSON(layout.CredentialPath, credential); err != nil {
		return false, nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, nil, err
	}
	defer ln.Close()
	original := corehost.RuntimeInfo{Version: 1, InstanceID: strings.Repeat("3", 32), InstallID: credential.InstallID, Address: ln.Addr().String(), BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, PID: 1}
	if err := writeJSON(layout.RuntimePath, original); err != nil {
		return false, nil, err
	}
	identity := corehost.Identity{Product: "KeyDeck", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, InstallID: credential.InstallID, InstanceID: original.InstanceID}
	var statusCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/identity", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(identity)
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		statusCalls.Add(1)
		changed := original
		changed.InstanceID = strings.Repeat("2", 32)
		_ = writeJSON(layout.RuntimePath, changed)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(corehost.Status{Product: "KeyDeck", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion})
	})
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	defer server.Close()
	client, err := corehost.Connect(context.Background(), layout, buildID, corehost.DefaultAPIVersion, nil)
	if err != nil {
		return false, nil, err
	}
	_, statusErr := client.Status(context.Background())
	pass := errors.Is(statusErr, corehost.ErrIdentityMismatch) && statusCalls.Load() == 1
	return pass, map[string]any{"status_calls": statusCalls.Load(), "stale_response_rejected": errors.Is(statusErr, corehost.ErrIdentityMismatch)}, nil
}

func redirectAndProxyScenario(dir string) (bool, map[string]any, error) {
	layout, _ := corehost.BuildLayout(dir)
	credential := corehost.Credential{Version: 1, InstallID: strings.Repeat("b", 32), Token: strings.Repeat("a", 64)}
	if err := writeJSON(layout.CredentialPath, credential); err != nil {
		return false, nil, err
	}
	var captureCalls atomic.Int64
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { captureCalls.Add(1); w.WriteHeader(http.StatusNoContent) })
	captureLn, _ := net.Listen("tcp", "127.0.0.1:0")
	captureServer := &http.Server{Handler: capture}
	go captureServer.Serve(captureLn)
	defer captureServer.Close()
	coreLn, _ := net.Listen("tcp", "127.0.0.1:0")
	identity := corehost.Identity{Product: "KeyDeck", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, InstallID: credential.InstallID, InstanceID: strings.Repeat("c", 32)}
	var coreCalls atomic.Int64
	coreMux := http.NewServeMux()
	coreMux.HandleFunc("/v1/identity", func(w http.ResponseWriter, r *http.Request) {
		coreCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identity)
	})
	coreMux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		coreCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", "http://"+captureLn.Addr().String()+"/capture")
		w.WriteHeader(http.StatusFound)
		io.WriteString(w, `{"error":"redirect"}`)
	})
	coreServer := &http.Server{Handler: coreMux}
	go coreServer.Serve(coreLn)
	defer coreServer.Close()
	if err := writeJSON(layout.RuntimePath, corehost.RuntimeInfo{Version: 1, InstanceID: identity.InstanceID, InstallID: credential.InstallID, Address: coreLn.Addr().String(), BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, PID: 1}); err != nil {
		return false, nil, err
	}
	proxyURL, _ := url.Parse("http://" + captureLn.Addr().String())
	base := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	client, err := corehost.Connect(context.Background(), layout, buildID, corehost.DefaultAPIVersion, base)
	if err != nil {
		return false, nil, err
	}
	_, statusErr := client.Status(context.Background())
	pass := statusErr != nil && captureCalls.Load() == 0 && coreCalls.Load() >= 2
	return pass, map[string]any{"capture_calls": captureCalls.Load(), "core_calls_at_least_two": coreCalls.Load() >= 2, "status_failed": statusErr != nil}, nil
}

func malformedAndOversizedScenario(dir string) (bool, map[string]any, error) {
	layout, _ := corehost.BuildLayout(dir)
	credential := corehost.Credential{Version: 1, InstallID: strings.Repeat("d", 32), Token: strings.Repeat("e", 64)}
	if err := writeJSON(layout.CredentialPath, credential); err != nil {
		return false, nil, err
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	identity := corehost.Identity{Product: "KeyDeck", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, InstallID: credential.InstallID, InstanceID: strings.Repeat("f", 32)}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/identity", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(identity)
	})
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"product":"KeyDeck","build_id":"`+buildID+`","api_version":"v1","padding":"`+strings.Repeat("x", int(corehost.DefaultMaxResponseBytes)+32)+`"}`)
	})
	mux.HandleFunc("/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"tasks":[]} {"extra":true}`)
	})
	server := &http.Server{Handler: mux}
	go server.Serve(ln)
	defer server.Close()
	if err := writeJSON(layout.RuntimePath, corehost.RuntimeInfo{Version: 1, InstanceID: identity.InstanceID, InstallID: credential.InstallID, Address: ln.Addr().String(), BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, PID: 1}); err != nil {
		return false, nil, err
	}
	client, err := corehost.Connect(context.Background(), layout, buildID, corehost.DefaultAPIVersion, nil)
	if err != nil {
		return false, nil, err
	}
	_, oversizedErr := client.Status(context.Background())
	_, malformedErr := client.ListTasks(context.Background())
	pass := errors.Is(oversizedErr, corehost.ErrRequestTooLarge) && malformedErr != nil
	return pass, map[string]any{"oversized_rejected": errors.Is(oversizedErr, corehost.ErrRequestTooLarge), "malformed_rejected": malformedErr != nil}, nil
}

func canonicalStateSHA(layout corehost.Layout) (string, error) {
	paths := []string{layout.TaskDir, layout.TimelinePath, layout.RequestJournal}
	h := sha256.New()
	for _, p := range paths {
		info, err := os.Stat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			raw, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			h.Write([]byte(filepath.Base(p)))
			h.Write(raw)
			continue
		}
		var files []string
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		sort.Strings(files)
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				return "", err
			}
			rel, _ := filepath.Rel(p, f)
			h.Write([]byte(rel))
			h.Write(raw)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}
