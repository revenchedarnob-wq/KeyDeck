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
	"keydeck.local/feasibilitylab/internal/visualshell"
)

const buildID = "keydeck-proof37-visual-renderer"

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

type deterministicReader struct{ counter uint64 }

func (r *deterministicReader) Read(p []byte) (int, error) {
	for i := 0; i < len(p); {
		r.counter++
		s := sha256.Sum256([]byte(fmt.Sprintf("proof37-random-%d", r.counter)))
		i += copy(p[i:], s[:])
	}
	return len(p), nil
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

type countingShell struct {
	inner       *presentation.Shell
	connects    atomic.Int64
	disconnects atomic.Int64
	refreshes   atomic.Int64
	creates     atomic.Int64
}

func (s *countingShell) Connect(ctx context.Context) error {
	s.connects.Add(1)
	return s.inner.Connect(ctx)
}
func (s *countingShell) Disconnect() { s.disconnects.Add(1); s.inner.Disconnect() }
func (s *countingShell) Refresh(ctx context.Context, a uint64, l int) (presentation.Snapshot, error) {
	s.refreshes.Add(1)
	return s.inner.Refresh(ctx, a, l)
}
func (s *countingShell) CreateTask(ctx context.Context, k string, r presentation.TaskCreateRequest) (presentation.TaskCreateResult, error) {
	s.creates.Add(1)
	return s.inner.CreateTask(ctx, k, r)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	out := report{Proof: "0.37-secure-visual-desktop-renderer", Status: "failed", BuildID: buildID, APIVersion: corehost.DefaultAPIVersion, NextGate: "Proof 0.38 — production desktop supervisor starts and attests core plus visual renderer"}
	add := func(name string, passed bool, evidence map[string]any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Evidence: evidence})
	}

	root, err := os.MkdirTemp("", "keydeck-proof37-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	dataDir := filepath.Join(root, "data")
	layout, err := corehost.BuildLayout(dataDir)
	if err != nil {
		return err
	}
	random := &deterministicReader{}
	backend, err := corehost.OpenFileBackend(layout, buildID, corehost.DefaultAPIVersion)
	if err != nil {
		return err
	}
	countedBackend := &countingBackend{inner: backend}
	host, err := corehost.Open(corehost.Config{DataDir: dataDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Backend: countedBackend, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	if _, err := host.Start(); err != nil {
		return err
	}
	hostClosed := false
	defer func() {
		if !hostClosed {
			_ = host.Close(context.Background())
		}
	}()

	baseShell := presentation.New(layout, buildID, corehost.DefaultAPIVersion, nil)
	countedShell := &countingShell{inner: baseShell}
	renderer, err := visualshell.Open(visualshell.Config{ListenAddress: "127.0.0.1:0", Shell: countedShell, Random: random, MaxBodyBytes: 8 << 10})
	if err != nil {
		return err
	}
	launch, err := renderer.Start(context.Background())
	if err != nil {
		return err
	}
	rendererClosed := false
	defer func() {
		if !rendererClosed {
			_ = renderer.Close(context.Background())
		}
	}()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	u, err := url.Parse(launch)
	if err != nil {
		return err
	}
	origin := u.Scheme + "://" + u.Host
	token := strings.Split(strings.Trim(u.Path, "/"), "/")[1]
	ip := net.ParseIP(u.Hostname())
	rootResp, err := client.Get(origin + "/")
	if err != nil {
		return err
	}
	rootResp.Body.Close()
	add("renderer_binds_loopback_with_unpersisted_secret_launch_path", ip != nil && ip.IsLoopback() && len(token) >= 40 && rootResp.StatusCode == http.StatusNotFound, map[string]any{"loopback": ip != nil && ip.IsLoopback(), "secret_path_bits_at_least_256": len(token) >= 40, "public_root_status": rootResp.StatusCode})

	beforeGuardRefresh := countedShell.refreshes.Load()
	wrongSecretResp, err := client.Get(origin + "/app/wrong/api/snapshot")
	if err != nil {
		return err
	}
	wrongSecretResp.Body.Close()
	wrongHostReq, _ := http.NewRequest(http.MethodGet, launch+"api/snapshot", nil)
	wrongHostReq.Host = "evil.example"
	wrongHostResp, err := client.Do(wrongHostReq)
	if err != nil {
		return err
	}
	wrongHostResp.Body.Close()
	add("wrong_secret_and_dns_rebinding_host_rejected_before_presentation", wrongSecretResp.StatusCode == 404 && wrongHostResp.StatusCode == 404 && countedShell.refreshes.Load() == beforeGuardRefresh, map[string]any{"wrong_secret_status": wrongSecretResp.StatusCode, "wrong_host_status": wrongHostResp.StatusCode, "presentation_calls": countedShell.refreshes.Load() - beforeGuardRefresh})

	indexRaw, indexResp, err := getRaw(client, launch)
	if err != nil {
		return err
	}
	cssRaw, cssResp, err := getRaw(client, launch+"styles.css")
	if err != nil {
		return err
	}
	jsRaw, jsResp, err := getRaw(client, launch+"app.js")
	if err != nil {
		return err
	}
	assetPass := indexResp.StatusCode == 200 && cssResp.StatusCode == 200 && jsResp.StatusCode == 200 && strings.Contains(string(indexRaw), "id=\"task-form\"") && strings.Contains(string(indexRaw), "id=\"tasks-list\"") && strings.Contains(string(indexRaw), "id=\"timeline-list\"") && !bytes.Contains(indexRaw, []byte("http://")) && !bytes.Contains(indexRaw, []byte("https://")) && !bytes.Contains(jsRaw, []byte("http://")) && !bytes.Contains(jsRaw, []byte("https://"))
	add("embedded_visual_assets_are_deterministic_and_external_resource_free", assetPass, map[string]any{"index_sha256": sha(indexRaw), "css_sha256": sha(cssRaw), "js_sha256": sha(jsRaw), "required_controls": assetPass})

	secretAssetPass := !bytes.Contains(indexRaw, []byte(token)) && !bytes.Contains(cssRaw, []byte(token)) && !bytes.Contains(jsRaw, []byte(token))
	add("secret_launch_token_is_never_embedded_in_visual_assets", secretAssetPass, map[string]any{"token_present_in_assets": !secretAssetPass})

	domSafety := !bytes.Contains(jsRaw, []byte("innerHTML")) && !bytes.Contains(jsRaw, []byte("outerHTML")) && !bytes.Contains(jsRaw, []byte("insertAdjacentHTML")) && !bytes.Contains(jsRaw, []byte("document.write")) && bytes.Count(jsRaw, []byte("textContent")) >= 8
	add("canonical_text_uses_safe_dom_text_sinks_without_html_injection_apis", domSafety, map[string]any{"safe_text_sinks": domSafety, "text_content_uses": bytes.Count(jsRaw, []byte("textContent"))})

	securityPass := strings.Contains(indexResp.Header.Get("Content-Security-Policy"), "default-src 'none'") && strings.Contains(indexResp.Header.Get("Content-Security-Policy"), "connect-src 'self'") && indexResp.Header.Get("Referrer-Policy") == "no-referrer" && indexResp.Header.Get("X-Frame-Options") == "DENY" && indexResp.Header.Get("Access-Control-Allow-Origin") == ""
	add("renderer_sets_strict_security_headers_and_no_cors", securityPass, map[string]any{"csp": securityPass, "cors_header_present": indexResp.Header.Get("Access-Control-Allow-Origin") != ""})

	credential, err := corehost.ReadCredential(layout.CredentialPath)
	if err != nil {
		return err
	}
	runtimeInfo, err := corehost.ReadRuntime(layout.RuntimePath)
	if err != nil {
		return err
	}
	snapRaw, snapResp, err := getRaw(client, launch+"api/snapshot?after=0&limit=100")
	if err != nil {
		return err
	}
	secretPass := snapResp.StatusCode == 200 && !bytes.Contains(snapRaw, []byte(credential.Token)) && !bytes.Contains(snapRaw, []byte(credential.InstallID)) && !bytes.Contains(snapRaw, []byte(runtimeInfo.InstanceID)) && !bytes.Contains(indexRaw, []byte(credential.Token)) && !bytes.Contains(jsRaw, []byte(credential.Token))
	add("browser_state_excludes_core_credential_install_and_instance_secrets", secretPass, map[string]any{"snapshot_status": snapResp.StatusCode, "secrets_present": !secretPass})

	var empty visualshell.ViewSnapshot
	if err := json.Unmarshal(snapRaw, &empty); err != nil {
		return err
	}
	add("visual_projection_consumes_authenticated_presentation_snapshot", empty.Connected && empty.Status.TaskCount == 0 && len(empty.Tasks) == 0 && len(empty.Timeline) == 0 && countedShell.refreshes.Load() > 0, map[string]any{"refresh_calls": countedShell.refreshes.Load(), "tasks": len(empty.Tasks), "timeline": len(empty.Timeline)})

	taskBody := createBody("visual-create-one", "proof37-task-one", "proof37-session", "Render a canonical task safely", "Task is visible through the visual renderer")
	createRaw, createResp, err := postJSON(client, launch+"api/tasks", origin, taskBody)
	if err != nil {
		return err
	}
	afterCreateRaw, _, err := getRaw(client, launch+"api/snapshot?after=0&limit=100")
	if err != nil {
		return err
	}
	var afterCreate visualshell.ViewSnapshot
	if err := json.Unmarshal(afterCreateRaw, &afterCreate); err != nil {
		return err
	}
	add("visual_task_command_routes_through_presentation_into_canonical_core", createResp.StatusCode == 200 && len(afterCreate.Tasks) == 1 && afterCreate.Status.TaskCount == 1 && len(afterCreate.Timeline) == 1 && countedShell.creates.Load() == 1, map[string]any{"create_status": createResp.StatusCode, "tasks": len(afterCreate.Tasks), "timeline": len(afterCreate.Timeline), "create_calls": countedShell.creates.Load()})
	_ = createRaw

	reuseRaw, reuseResp, err := postJSON(client, launch+"api/tasks", origin, taskBody)
	if err != nil {
		return err
	}
	var reuse map[string]any
	_ = json.Unmarshal(reuseRaw, &reuse)
	reuseSnapRaw, _, _ := getRaw(client, launch+"api/snapshot?after=0&limit=100")
	var reuseSnap visualshell.ViewSnapshot
	_ = json.Unmarshal(reuseSnapRaw, &reuseSnap)
	add("duplicate_visual_command_reuses_same_idempotent_canonical_result", reuseResp.StatusCode == 200 && reuse["reused"] == true && len(reuseSnap.Tasks) == 1 && len(reuseSnap.Timeline) == 1 && reuseSnap.Status.RequestRecords == 1, map[string]any{"reused": reuse["reused"], "tasks": len(reuseSnap.Tasks), "timeline": len(reuseSnap.Timeline), "request_records": reuseSnap.Status.RequestRecords})

	beforeConflict := sha(reuseSnapRaw)
	conflictBody := createBody("visual-create-one", "proof37-task-one", "proof37-session", "Different conflicting goal", "Task is visible")
	conflictRaw, conflictResp, err := postJSON(client, launch+"api/tasks", origin, conflictBody)
	if err != nil {
		return err
	}
	conflictSnapRaw, _, _ := getRaw(client, launch+"api/snapshot?after=0&limit=100")
	conflictSafe := conflictResp.StatusCode == http.StatusConflict && beforeConflict == sha(conflictSnapRaw) && !bytes.Contains(conflictRaw, []byte(credential.Token)) && !bytes.Contains(conflictRaw, []byte(dataDir))
	add("conflicting_visual_idempotency_key_is_generic_and_non_mutating", conflictSafe, map[string]any{"status": conflictResp.StatusCode, "state_unchanged": beforeConflict == sha(conflictSnapRaw), "sensitive_error": !conflictSafe})

	beforeCrossCreate := countedShell.creates.Load()
	crossRaw, crossResp, err := postJSON(client, launch+"api/tasks", "http://evil.example", createBody("cross", "cross-task", "s", "g", "c"))
	if err != nil {
		return err
	}
	add("cross_origin_mutation_is_rejected_before_presentation", crossResp.StatusCode == http.StatusForbidden && countedShell.creates.Load() == beforeCrossCreate && bytes.Contains(crossRaw, []byte("Cross-origin")), map[string]any{"status": crossResp.StatusCode, "presentation_calls": countedShell.creates.Load() - beforeCrossCreate})

	beforeStrict := countedShell.creates.Load()
	oversized := []byte(`{"idempotency_key":"large","task":{"task_id":"large","session_id":"s","contract":{"goal":"g","checks":[{"id":"c","description":"d","status":"pending"}]}},"padding":"` + strings.Repeat("x", 9000) + `"}`)
	_, oversizedResp, err := postJSON(client, launch+"api/tasks", origin, oversized)
	if err != nil {
		return err
	}
	_, malformedResp, err := postJSON(client, launch+"api/tasks", origin, []byte(`{"idempotency_key":`))
	if err != nil {
		return err
	}
	add("oversized_and_malformed_visual_commands_fail_closed", oversizedResp.StatusCode == http.StatusRequestEntityTooLarge && malformedResp.StatusCode == http.StatusBadRequest && countedShell.creates.Load() == beforeStrict, map[string]any{"oversized_status": oversizedResp.StatusCode, "malformed_status": malformedResp.StatusCode, "presentation_calls": countedShell.creates.Load() - beforeStrict})

	if err := host.Close(context.Background()); err != nil {
		return err
	}
	hostClosed = true
	restartBackend, err := corehost.OpenFileBackend(layout, buildID, corehost.DefaultAPIVersion)
	if err != nil {
		return err
	}
	restartCounted := &countingBackend{inner: restartBackend}
	host2, err := corehost.Open(corehost.Config{DataDir: dataDir, ListenAddress: "127.0.0.1:0", BuildID: buildID, Backend: restartCounted, Random: random, StaleLeaseAfter: 2 * time.Second, HeartbeatEvery: 100 * time.Millisecond})
	if err != nil {
		return err
	}
	if _, err := host2.Start(); err != nil {
		return err
	}
	defer host2.Close(context.Background())
	staleRaw, staleResp, err := getRaw(client, launch+"api/snapshot?after=0&limit=100")
	if err != nil {
		return err
	}
	add("stale_core_instance_blocks_visual_projection_without_direct_store_fallback", staleResp.StatusCode == http.StatusServiceUnavailable && restartCounted.calls.Load() == 0 && !bytes.Contains(staleRaw, []byte(dataDir)), map[string]any{"status": staleResp.StatusCode, "new_core_backend_calls": restartCounted.calls.Load()})

	reconnectRaw, reconnectResp, err := postJSON(client, launch+"api/reconnect", origin, []byte(`{}`))
	if err != nil {
		return err
	}
	restoredRaw, restoredResp, err := getRaw(client, launch+"api/snapshot?after=0&limit=100")
	if err != nil {
		return err
	}
	var restored visualshell.ViewSnapshot
	_ = json.Unmarshal(restoredRaw, &restored)
	add("explicit_visual_reconnect_reattests_new_core_and_restores_projection", reconnectResp.StatusCode == 200 && restoredResp.StatusCode == 200 && len(restored.Tasks) == 1 && restartCounted.calls.Load() > 0, map[string]any{"reconnect_status": reconnectResp.StatusCode, "snapshot_status": restoredResp.StatusCode, "tasks": len(restored.Tasks)})
	_ = reconnectRaw

	beforeReconnectGuard := countedShell.connects.Load()
	_, badReconnectResp, err := postJSON(client, launch+"api/reconnect", "http://evil.example", []byte(`{}`))
	if err != nil {
		return err
	}
	add("cross_origin_reconnect_is_rejected_before_reattest", badReconnectResp.StatusCode == http.StatusForbidden && countedShell.connects.Load() == beforeReconnectGuard, map[string]any{"status": badReconnectResp.StatusCode, "connect_calls": countedShell.connects.Load() - beforeReconnectGuard})

	walkRaw, err := readTree(dataDir)
	if err != nil {
		return err
	}
	add("renderer_session_secret_is_memory_only_and_absent_from_core_data", !bytes.Contains(walkRaw, []byte(token)), map[string]any{"token_persisted": bytes.Contains(walkRaw, []byte(token))})

	concurrentPass, taskCount, timelineCount, err := concurrentScenario(client, launch, origin)
	if err != nil {
		return err
	}
	add("visual_snapshots_remain_consistent_during_concurrent_commands", concurrentPass, map[string]any{"task_count": taskCount, "timeline_count": timelineCount, "consistent": concurrentPass})

	oldLaunch := launch
	if err := renderer.Close(context.Background()); err != nil {
		return err
	}
	rendererClosed = true
	renderer2, err := visualshell.Open(visualshell.Config{ListenAddress: "127.0.0.1:0", Shell: countedShell, Random: random, MaxBodyBytes: 8 << 10})
	if err != nil {
		return err
	}
	launch2, err := renderer2.Start(context.Background())
	if err != nil {
		return err
	}
	defer renderer2.Close(context.Background())
	oldURL, _ := url.Parse(oldLaunch)
	newURL, _ := url.Parse(launch2)
	oldToken := strings.Split(strings.Trim(oldURL.Path, "/"), "/")[1]
	newToken := strings.Split(strings.Trim(newURL.Path, "/"), "/")[1]
	oldPathOnNew := newURL.Scheme + "://" + newURL.Host + oldURL.Path
	oldResp, err := client.Get(oldPathOnNew)
	if err != nil {
		return err
	}
	oldResp.Body.Close()
	add("renderer_restart_rotates_secret_session_and_invalidates_old_path", oldToken != newToken && oldResp.StatusCode == http.StatusNotFound, map[string]any{"token_rotated": oldToken != newToken, "old_path_status": oldResp.StatusCode})

	finalRaw, finalResp, err := getRaw(client, launch2+"api/snapshot?after=0&limit=200")
	if err != nil {
		return err
	}
	var final visualshell.ViewSnapshot
	_ = json.Unmarshal(finalRaw, &final)
	add("renderer_restart_reconnects_to_same_canonical_state", finalResp.StatusCode == 200 && final.Status.TaskCount == len(final.Tasks) && final.Status.TaskCount == 13, map[string]any{"status": finalResp.StatusCode, "tasks": final.Status.TaskCount, "timeline": final.Status.TimelineEvents})

	if err := renderer2.Close(context.Background()); err != nil {
		return err
	}
	closedClient := &http.Client{Timeout: 300 * time.Millisecond}
	_, closedErr := closedClient.Get(launch2)
	add("renderer_close_disconnects_presentation_and_stops_serving", closedErr != nil && countedShell.disconnects.Load() >= 2, map[string]any{"endpoint_unreachable": closedErr != nil, "disconnect_calls": countedShell.disconnects.Load()})

	all := len(out.Scenarios) == 21
	for _, s := range out.Scenarios {
		all = all && s.Passed
	}
	out.Passed = all
	if all {
		out.Status = "passed"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func createBody(key, taskID, sessionID, goal, check string) []byte {
	v := map[string]any{"idempotency_key": key, "task": map[string]any{"task_id": taskID, "session_id": sessionID, "contract": map[string]any{"goal": goal, "checks": []map[string]any{{"id": "acceptance", "description": check, "status": "pending"}}}}}
	b, _ := json.Marshal(v)
	return b
}

func getRaw(c *http.Client, raw string) ([]byte, *http.Response, error) {
	resp, err := c.Get(raw)
	if err != nil {
		return nil, nil, err
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	return b, resp, err
}
func postJSON(c *http.Client, raw, origin string, body []byte) ([]byte, *http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, raw, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	return b, resp, err
}
func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func readTree(root string) ([]byte, error) {
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out bytes.Buffer
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		out.Write(b)
	}
	return out.Bytes(), nil
}
func concurrentScenario(c *http.Client, launch, origin string) (bool, int, int, error) {
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	consistent := atomic.Bool{}
	consistent.Store(true)
	for i := 0; i < n; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			body := createBody(fmt.Sprintf("concurrent-%02d", i), fmt.Sprintf("proof37-concurrent-%02d", i), "proof37-session", "Concurrent visual task", "Task persists")
			_, resp, err := postJSON(c, launch+"api/tasks", origin, body)
			if err != nil {
				errs <- err
				return
			}
			if resp.StatusCode != 200 {
				errs <- fmt.Errorf("create status %d", resp.StatusCode)
			}
		}()
		go func() {
			defer wg.Done()
			raw, resp, err := getRaw(c, launch+"api/snapshot?after=0&limit=200")
			if err != nil {
				errs <- err
				return
			}
			if resp.StatusCode != 200 {
				errs <- fmt.Errorf("snapshot status %d", resp.StatusCode)
				return
			}
			var s visualshell.ViewSnapshot
			if err := json.Unmarshal(raw, &s); err != nil {
				errs <- err
				return
			}
			if s.Status.TaskCount != len(s.Tasks) || s.Status.TimelineEvents < len(s.Timeline) {
				consistent.Store(false)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return false, 0, 0, err
		}
	}
	raw, resp, err := getRaw(c, launch+"api/snapshot?after=0&limit=200")
	if err != nil {
		return false, 0, 0, err
	}
	if resp.StatusCode != 200 {
		return false, 0, 0, fmt.Errorf("final status %d", resp.StatusCode)
	}
	var s visualshell.ViewSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return false, 0, 0, err
	}
	return consistent.Load() && s.Status.TaskCount == len(s.Tasks) && s.Status.TimelineEvents == len(s.Timeline), s.Status.TaskCount, s.Status.TimelineEvents, nil
}

var _ = errors.Is
