package corehost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/tasks"
)

func TestReadCredentialDoesNotCreateMissingSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if _, err := ReadCredential(path); !os.IsNotExist(err) {
		t.Fatalf("expected not-exist, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read unexpectedly created credential file: %v", err)
	}
}

func TestCredentialCreatedOnceAndReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	first, created, err := LoadOrCreateCredential(path, &testReader{})
	if err != nil || !created {
		t.Fatalf("first create: created=%v err=%v", created, err)
	}
	second, created, err := LoadOrCreateCredential(path, &testReader{})
	if err != nil || created || first != second {
		t.Fatalf("reuse mismatch: created=%v err=%v", created, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("credential stat: %v", err)
	}
	// FileMode permission bits do not represent Windows ACLs.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("credential permissions: mode=%v", info.Mode().Perm())
	}
}

func TestLeaseBlocksActiveOwnerAndReclaimsStaleOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "core.lock")
	now := time.Now().UTC()
	clock := func() time.Time { return now }
	first, err := AcquireLease(dir, "one", 1, clock, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLease(dir, "two", 2, clock, time.Minute); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("expected active owner block, got %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(LeaseRecord{Version: 1, InstanceID: "dead", PID: 9, HeartbeatAt: now.Add(-2 * time.Minute)})
	if err := os.WriteFile(filepath.Join(dir, "owner.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	reclaimed, err := AcquireLease(dir, "three", 3, clock, time.Minute)
	if err != nil {
		t.Fatalf("stale lease not reclaimed: %v", err)
	}
	_ = reclaimed.Release()
}

func TestLeaseReleaseFailsClosedWhenOwnershipCannotBeVerified(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "core.lock")
	lease, err := AcquireLease(dir, "owner", 1, time.Now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "owner.json")); err != nil {
		t.Fatal(err)
	}
	if err := lease.Release(); err == nil || !strings.Contains(err.Error(), "ownership cannot be verified") {
		t.Fatalf("expected fail-closed release error, got %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("lease directory was removed without verified ownership: %v", err)
	}
	_ = os.RemoveAll(dir)
}

func TestReadRuntimeRejectsNonLoopbackAddress(t *testing.T) {
	layout, err := BuildLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info := RuntimeInfo{Version: 1, InstanceID: "instance", InstallID: "install", Address: "192.0.2.10:8080", BuildID: "build", APIVersion: DefaultAPIVersion, PID: 1}
	if err := atomicWriteJSON(layout.RuntimePath, info, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRuntime(layout.RuntimePath); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("expected non-loopback runtime rejection, got %v", err)
	}
}

func TestAttestDoesNotFollowRedirectWithCredential(t *testing.T) {
	var redirectedAuth string
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	redirectSource := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer redirectSource.Close()

	layout, err := BuildLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	credential := Credential{Version: 1, InstallID: strings.Repeat("a", 32), Token: strings.Repeat("b", 64)}
	if err := atomicWriteJSON(layout.CredentialPath, credential, 0o600); err != nil {
		t.Fatal(err)
	}
	address := strings.TrimPrefix(redirectSource.URL, "http://")
	info := RuntimeInfo{Version: 1, InstanceID: "instance", InstallID: credential.InstallID, Address: address, BuildID: "build", APIVersion: DefaultAPIVersion, PID: 1}
	if err := atomicWriteJSON(layout.RuntimePath, info, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Attest(context.Background(), layout, "build", DefaultAPIVersion, nil); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("expected redirect attestation failure, got %v", err)
	}
	if redirectedAuth != "" {
		t.Fatal("credential-bearing Authorization header followed redirect")
	}
}

func TestUnexpectedHTTPServerFailureStopsHeartbeat(t *testing.T) {
	h, err := Open(Config{DataDir: t.TempDir(), ListenAddress: "127.0.0.1:0", BuildID: "build", HeartbeatEvery: 10 * time.Millisecond, StaleLeaseAfter: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Start(); err != nil {
		t.Fatal(err)
	}
	if err := h.listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case fatalErr := <-h.Fatal():
		if fatalErr == nil || !strings.Contains(fatalErr.Error(), "HTTP server failed") {
			t.Fatalf("unexpected fatal server error: %v", fatalErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unexpected HTTP server failure did not become fatal")
	}
	select {
	case <-h.hbDone:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat did not stop after HTTP server failure")
	}
	ownerBefore, err := readLeaseRecord(filepath.Join(h.Layout().LeaseDir, "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	ownerAfter, err := readLeaseRecord(filepath.Join(h.Layout().LeaseDir, "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !ownerBefore.HeartbeatAt.Equal(ownerAfter.HeartbeatAt) {
		t.Fatalf("heartbeat continued after fatal server failure: before=%s after=%s", ownerBefore.HeartbeatAt, ownerAfter.HeartbeatAt)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDuplicateCreateConverges(t *testing.T) {
	layout, _ := BuildLayout(t.TempDir())
	backend, err := OpenFileBackend(layout, "build", DefaultAPIVersion)
	if err != nil {
		t.Fatal(err)
	}
	req := TaskCreateRequest{TaskID: "task", SessionID: "session", Contract: tasks.Contract{Goal: "one task", Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "created"}}}}
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := backend.CreateTask(req, "same-key")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("duplicate create failed: %v", err)
		}
	}
	status, err := backend.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.TaskCount != 1 || status.TimelineEvents != 1 || status.RequestRecords != 1 {
		t.Fatalf("did not converge: %+v", status)
	}
}

func TestRehashedForgedJournalResponseRejectedAgainstCanonicalTask(t *testing.T) {
	layout, _ := BuildLayout(t.TempDir())
	backend, err := OpenFileBackend(layout, "build", DefaultAPIVersion)
	if err != nil {
		t.Fatal(err)
	}
	req := TaskCreateRequest{TaskID: "task", SessionID: "session", Contract: tasks.Contract{Goal: "canonical", Checks: []tasks.AcceptanceCheck{{ID: "done", Description: "created"}}}}
	if _, err := backend.CreateTask(req, "key"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(layout.RequestJournal)
	if err != nil {
		t.Fatal(err)
	}
	var record RequestRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	var result TaskCreateResult
	if err := json.Unmarshal(record.Response, &result); err != nil {
		t.Fatal(err)
	}
	result.State.TaskID = "forged-task"
	record.Response, _ = json.Marshal(result)
	record.RecordSHA256 = requestRecordDigest(record)
	forged, _ := json.Marshal(record)
	if err := os.WriteFile(layout.RequestJournal, append(forged, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFileBackend(layout, "build", DefaultAPIVersion)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.CreateTask(req, "key"); err == nil {
		t.Fatal("forged journal response was accepted")
	}
}

func TestLeaseOwnershipLossForcesHostFailClosed(t *testing.T) {
	h, err := Open(Config{
		DataDir:         t.TempDir(),
		ListenAddress:   "127.0.0.1:0",
		BuildID:         "build",
		HeartbeatEvery:  10 * time.Millisecond,
		StaleLeaseAfter: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := h.Start()
	if err != nil {
		t.Fatal(err)
	}

	ownerPath := filepath.Join(h.Layout().LeaseDir, "owner.json")
	forged := LeaseRecord{
		Version:     1,
		InstanceID:  "different-owner",
		PID:         999999,
		HeartbeatAt: time.Now().UTC(),
	}
	if err := atomicWriteJSON(ownerPath, forged, 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case fatalErr := <-h.Fatal():
		if fatalErr == nil || !strings.Contains(fatalErr.Error(), "lease ownership changed") {
			t.Fatalf("unexpected fatal error: %v", fatalErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host did not fail closed after lease ownership loss")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		resp, requestErr := http.Get("http://" + runtime.Address + "/healthz")
		if requestErr != nil {
			break
		}
		_ = resp.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("HTTP server remained reachable after fatal lease ownership loss")
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if closeErr := h.Close(ctx); closeErr != nil && !strings.Contains(closeErr.Error(), "lease ownership changed") {
		t.Fatalf("unexpected close error after ownership loss: %v", closeErr)
	}
}

func TestCloseBeforeStartDoesNotDeadlock(t *testing.T) {
	h, err := Open(Config{DataDir: t.TempDir(), ListenAddress: "127.0.0.1:0", BuildID: "build"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type testReader struct{ n byte }

func (r *testReader) Read(p []byte) (int, error) {
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}
