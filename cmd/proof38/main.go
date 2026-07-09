package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
	"keydeck.local/feasibilitylab/internal/presentation"
	"keydeck.local/feasibilitylab/internal/supervisor"
	"keydeck.local/feasibilitylab/internal/visualshell"
)

const (
	buildID      = "keydeck-v0.35.0-reconstructed"
	proofBuildID = "keydeck-proof38-production-desktop-supervisor"
	ownerEnv     = "KEYDECK_SUPERVISOR_INSTANCE"
)

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
type readyFrame struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	SupervisorInstanceID string `json:"supervisor_instance_id"`
}

type recordingLauncher struct {
	inner       supervisor.Launcher
	mu          sync.Mutex
	specs       []supervisor.ChildSpec
	children    []*recordingChild
	launchOrder []string
	stopOrder   []string
	uiHook      func(supervisor.ChildSpec) error
}
type recordingChild struct {
	inner    supervisor.Child
	owner    *recordingLauncher
	kind     string
	stopOnce sync.Once
}

func (c *recordingChild) PID() int              { return c.inner.PID() }
func (c *recordingChild) Stdout() io.Reader     { return c.inner.Stdout() }
func (c *recordingChild) Done() <-chan struct{} { return c.inner.Done() }
func (c *recordingChild) ExitError() error      { return c.inner.ExitError() }
func (c *recordingChild) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() { c.owner.mu.Lock(); c.owner.stopOrder = append(c.owner.stopOrder, c.kind); c.owner.mu.Unlock() })
	return c.inner.Stop(ctx)
}
func childKind(path string) string {
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, "keydeck-core") {
		return "core"
	}
	if strings.HasPrefix(base, "keydeck-desktop-ui") {
		return "renderer"
	}
	return "other"
}
func (l *recordingLauncher) Start(spec supervisor.ChildSpec) (supervisor.Child, error) {
	kind := childKind(spec.Path)
	if kind == "renderer" && l.uiHook != nil {
		if err := l.uiHook(spec); err != nil {
			return nil, err
		}
	}
	child, err := l.inner.Start(spec)
	if err != nil {
		return nil, err
	}
	rc := &recordingChild{inner: child, owner: l, kind: kind}
	l.mu.Lock()
	l.specs = append(l.specs, spec)
	l.children = append(l.children, rc)
	l.launchOrder = append(l.launchOrder, kind)
	l.mu.Unlock()
	return rc, nil
}
func (l *recordingLauncher) snapshot() ([]supervisor.ChildSpec, []*recordingChild, []string, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]supervisor.ChildSpec(nil), l.specs...), append([]*recordingChild(nil), l.children...), append([]string(nil), l.launchOrder...), append([]string(nil), l.stopOrder...)
}
func (l *recordingLauncher) count(kind string) int {
	_, _, order, _ := l.snapshot()
	n := 0
	for _, k := range order {
		if k == kind {
			n++
		}
	}
	return n
}
func (l *recordingLauncher) latest(kind string) *recordingChild {
	_, children, _, _ := l.snapshot()
	for i := len(children) - 1; i >= 0; i-- {
		if children[i].kind == kind {
			return children[i]
		}
	}
	return nil
}
func (l *recordingLauncher) latestSpec(kind string) (supervisor.ChildSpec, bool) {
	specs, _, _, _ := l.snapshot()
	for i := len(specs) - 1; i >= 0; i-- {
		if childKind(specs[i].Path) == kind {
			return specs[i], true
		}
	}
	return supervisor.ChildSpec{}, false
}

type deterministicReader struct{ n uint64 }

func (r *deterministicReader) Read(p []byte) (int, error) {
	for i := 0; i < len(p); {
		r.n++
		h := sha256.Sum256([]byte(fmt.Sprintf("proof38-random-%d", r.n)))
		i += copy(p[i:], h[:])
	}
	return len(p), nil
}

func main() {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	if strings.HasPrefix(base, "keydeck-core") {
		if err := runHelperCore(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if strings.HasPrefix(base, "keydeck-desktop-ui") {
		if err := runHelperUI(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := runProof(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runHelperCore() error {
	dataDir, listen, supervised := argValue("--data-dir"), argValue("--listen"), hasArg("--supervised")
	owner := strings.TrimSpace(os.Getenv(ownerEnv))
	if dataDir == "" || listen == "" || !supervised || owner == "" {
		return errors.New("invalid supervised core helper invocation")
	}
	h, err := corehost.Open(corehost.Config{DataDir: dataDir, ListenAddress: listen, BuildID: buildID, SupervisorInstanceID: owner})
	if err != nil {
		return err
	}
	if _, err = h.Start(); err != nil {
		_ = h.Close(context.Background())
		return err
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return h.Close(ctx)
}
func runHelperUI() error {
	dataDir, expectedBuild, listen := argValue("--data-dir"), argValue("--expected-build"), argValue("--listen")
	owner := strings.TrimSpace(os.Getenv(ownerEnv))
	if dataDir == "" || expectedBuild == "" || listen == "" || !hasArg("--supervised") || owner == "" {
		return errors.New("invalid supervised UI helper invocation")
	}
	layout, err := corehost.BuildLayout(dataDir)
	if err != nil {
		return err
	}
	shell := presentation.New(layout, expectedBuild, corehost.DefaultAPIVersion, nil)
	server, err := visualshell.Open(visualshell.Config{ListenAddress: listen, Shell: shell})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	launchURL, err := server.Start(ctx)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(readyFrame{Type: "keydeck-ui-ready-v1", URL: launchURL, SupervisorInstanceID: owner}); err != nil {
		return err
	}
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer closeCancel()
	return server.Close(closeCtx)
}
func argValue(name string) string {
	for i := 1; i+1 < len(os.Args); i++ {
		if os.Args[i] == name {
			return os.Args[i+1]
		}
	}
	return ""
}
func hasArg(name string) bool {
	for _, a := range os.Args[1:] {
		if a == name {
			return true
		}
	}
	return false
}

func runProof() error {
	out := report{Proof: "0.38-production-desktop-supervisor", Status: "failed", BuildID: proofBuildID, APIVersion: corehost.DefaultAPIVersion, NextGate: "Proof 0.39 — signed production bundle, first-run bootstrap and repair-safe desktop launch"}
	add := func(name string, passed bool, evidence map[string]any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: passed, Evidence: evidence})
	}

	// 1. Wrong digest.
	{
		root, _ := os.MkdirTemp("", "keydeck-proof38-digest-")
		defer os.RemoveAll(root)
		core, ui, coreHash, uiHash, _ := makeHelperSources(root)
		l := &recordingLauncher{inner: supervisor.NewProcessLauncher()}
		_, err := supervisor.Open(baseConfig(filepath.Join(root, "data"), core, ui, strings.Repeat("0", 64), uiHash, l, nil))
		add("wrong_child_binary_digest_is_rejected_before_launch", errors.Is(err, supervisor.ErrBinaryIdentity) && l.count("core") == 0, map[string]any{"blocked_before_launch": true})
		_ = coreHash
	}
	// 2. Existing runtime.
	{
		root, _ := os.MkdirTemp("", "keydeck-proof38-foreign-")
		defer os.RemoveAll(root)
		core, ui, ch, uh, _ := makeHelperSources(root)
		data := filepath.Join(root, "data")
		layout, _ := corehost.BuildLayout(data)
		_ = os.MkdirAll(filepath.Dir(layout.RuntimePath), 0o700)
		_ = os.WriteFile(layout.RuntimePath, []byte(`{"foreign":true}`), 0o600)
		l := &recordingLauncher{inner: supervisor.NewProcessLauncher()}
		_, err := supervisor.Open(baseConfig(data, core, ui, ch, uh, l, nil))
		add("foreign_or_stale_existing_core_runtime_blocks_all_child_launch", errors.Is(err, supervisor.ErrForeignChild) && l.count("core") == 0, map[string]any{"child_launches": 0})
	}

	// 3-14 integrated lifecycle.
	{
		root, _ := os.MkdirTemp("", "keydeck-proof38-main-")
		defer os.RemoveAll(root)
		core, ui, ch, uh, _ := makeHelperSources(root)
		data := filepath.Join(root, "data")
		layout, _ := corehost.BuildLayout(data)
		launcher := &recordingLauncher{inner: supervisor.NewProcessLauncher()}
		uiAttest := 0
		launcher.uiHook = func(spec supervisor.ChildSpec) error {
			uiAttest++
			info, err := corehost.ReadRuntime(layout.RuntimePath)
			if err != nil {
				return err
			}
			if info.SupervisorInstanceID == "" || info.BuildID != buildID || info.APIVersion != corehost.DefaultAPIVersion {
				return errors.New("core not supervisor-attested")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err = corehost.Connect(ctx, layout, buildID, corehost.DefaultAPIVersion, nil)
			return err
		}
		var openedMu sync.Mutex
		var opened []string
		opener := func(raw string) error {
			req, err := http.NewRequest(http.MethodGet, raw, nil)
			if err != nil {
				return err
			}
			client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if resp.StatusCode != 200 || !bytes.Contains(body, []byte("<title>KeyDeck</title>")) {
				return errors.New("renderer attestation failed")
			}
			openedMu.Lock()
			opened = append(opened, raw)
			openedMu.Unlock()
			return nil
		}
		cfg := baseConfig(data, core, ui, ch, uh, launcher, opener)
		cfg.Random = &deterministicReader{}
		s, err := supervisor.Open(cfg)
		if err != nil {
			return err
		}
		_ = os.WriteFile(core, []byte("mutated-core-source"), 0o700)
		_ = os.WriteFile(ui, []byte("mutated-ui-source"), 0o700)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		err = s.Start(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("main supervisor start: %w", err)
		}
		status := s.Status()
		specs, _, launchOrder, _ := launcher.snapshot()
		add("verified_private_child_copies_survive_source_mutation", status.Running, map[string]any{"running_after_source_mutation": status.Running})
		privateOK := len(specs) >= 2 && strings.Contains(specs[0].Path, filepath.Join("desktop-supervisor", "runtime", status.InstanceID)) && strings.Contains(specs[1].Path, filepath.Join("desktop-supervisor", "runtime", status.InstanceID)) && digestFile(specs[0].Path) == ch && digestFile(specs[1].Path) == uh
		add("private_execution_copies_keep_exact_verified_identity_inside_supervisor_runtime", privateOK, map[string]any{"exact_private_identity": privateOK})
		orderOK := len(launchOrder) >= 2 && launchOrder[0] == "core" && launchOrder[1] == "renderer" && uiAttest >= 1
		add("core_launches_before_renderer_and_renderer_waits_for_authenticated_core", orderOK, map[string]any{"launch_order": "core_then_renderer", "renderer_prelaunch_core_attest": uiAttest >= 1})
		info, _ := corehost.ReadRuntime(layout.RuntimePath)
		runtimeOK := info.SupervisorInstanceID == status.InstanceID && info.PID == status.CorePID && info.BuildID == buildID && info.APIVersion == corehost.DefaultAPIVersion
		add("core_runtime_is_bound_to_exact_supervisor_owner_pid_build_and_api", runtimeOK, map[string]any{"owner_pid_build_api_bound": runtimeOK})
		openedMu.Lock()
		firstURL := ""
		if len(opened) > 0 {
			firstURL = opened[0]
		}
		openedMu.Unlock()
		attested := firstURL != "" && strings.HasPrefix(firstURL, "http://127.0.0.1:")
		add("renderer_is_http_attested_before_in_memory_open", attested, map[string]any{"attested_before_open": attested})
		cred, _ := corehost.ReadCredential(layout.CredentialPath)
		firstToken := rendererToken(firstURL)
		secretFree := true
		for _, sp := range specs {
			joined := strings.Join(append(append([]string{}, sp.Args...), sp.Env...), "\n")
			if cred.Token != "" && strings.Contains(joined, cred.Token) {
				secretFree = false
			}
			if firstToken != "" && strings.Contains(joined, firstToken) {
				secretFree = false
			}
		}
		add("child_command_lines_and_environment_exclude_core_and_renderer_secrets", secretFree, map[string]any{"secret_free_process_metadata": secretFree})
		ownerOK := len(specs) >= 2
		for _, sp := range specs[:min(2, len(specs))] {
			if len(sp.Env) != 1 || sp.Env[0] != ownerEnv+"="+status.InstanceID {
				ownerOK = false
			}
		}
		add("both_children_receive_only_the_same_non_secret_supervisor_owner_binding", ownerOK, map[string]any{"single_shared_owner_binding": ownerOK})
		durableSecretFree := !treeContains(filepath.Join(data, "desktop-supervisor"), cred.Token) && !treeContains(filepath.Join(data, "desktop-supervisor"), firstToken)
		add("supervisor_durable_state_excludes_core_credential_and_renderer_launch_secret", durableSecretFree, map[string]any{"durable_secret_free": durableSecretFree})
		secondLauncher := &recordingLauncher{inner: supervisor.NewProcessLauncher()}
		_, secondErr := supervisor.Open(baseConfig(data, core, ui, ch, uh, secondLauncher, nil))
		add("second_active_supervisor_is_blocked_before_child_launch", errors.Is(secondErr, corehost.ErrAlreadyRunning) && secondLauncher.count("core") == 0, map[string]any{"second_launches": 0})

		corePID := status.CorePID
		oldUI := launcher.latest("renderer")
		oldUIPID := status.RendererPID
		killCtx, kc := context.WithTimeout(context.Background(), 2*time.Second)
		_ = oldUI.Stop(killCtx)
		kc()
		restarted := waitUntil(7*time.Second, func() bool { st := s.Status(); return st.Running && st.RendererPID > 0 && st.RendererPID != oldUIPID })
		newStatus := s.Status()
		openedMu.Lock()
		secondURL := ""
		if len(opened) > 1 {
			secondURL = opened[len(opened)-1]
		}
		openedMu.Unlock()
		rotated := restarted && newStatus.CorePID == corePID && rendererToken(firstURL) != "" && rendererToken(firstURL) != rendererToken(secondURL)
		add("renderer_crash_restarts_only_renderer_and_rotates_launch_secret", rotated, map[string]any{"core_reused": newStatus.CorePID == corePID, "secret_rotated": rendererToken(firstURL) != rendererToken(secondURL)})
		reAttested := launcher.count("core") == 1 && launcher.count("renderer") == 2 && uiAttest >= 2
		add("renderer_restart_reattests_same_exact_core_before_relaunch", reAttested, map[string]any{"core_launches": launcher.count("core"), "renderer_launches": launcher.count("renderer"), "core_attestations": uiAttest})
		_, _, _, beforeStops := launcher.snapshot()
		beforeN := len(beforeStops)
		closeCtx, cc := context.WithTimeout(context.Background(), 8*time.Second)
		closeErr := s.Close(closeCtx)
		cc()
		_, _, _, stops := launcher.snapshot()
		suffix := stops
		if beforeN < len(stops) {
			suffix = stops[beforeN:]
		}
		shutdownOK := closeErr == nil && len(suffix) >= 2 && suffix[len(suffix)-2] == "renderer" && suffix[len(suffix)-1] == "core" && waitUntil(2*time.Second, func() bool { _, e := os.Stat(layout.RuntimePath); return os.IsNotExist(e) })
		add("clean_shutdown_stops_renderer_before_core_and_removes_runtime", shutdownOK, map[string]any{"shutdown_order": "renderer_then_core", "runtime_removed": shutdownOK})
	}

	// 15. stale supervisor lease reclaim.
	{
		root, _ := os.MkdirTemp("", "keydeck-proof38-stale-")
		defer os.RemoveAll(root)
		core, ui, ch, uh, _ := makeHelperSources(root)
		data := filepath.Join(root, "data")
		old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		leaseDir := filepath.Join(data, "desktop-supervisor", "lease")
		lease, err := corehost.AcquireLease(leaseDir, "old-owner", 111, func() time.Time { return old }, time.Second)
		if err != nil {
			return err
		}
		_ = lease.Refresh()
		cfg := baseConfig(data, core, ui, ch, uh, &recordingLauncher{inner: supervisor.NewProcessLauncher()}, nil)
		cfg.Now = func() time.Time { return old.Add(time.Hour) }
		cfg.StaleLeaseAfter = time.Minute
		s, err := supervisor.Open(cfg)
		reclaimed := err == nil
		if s != nil {
			_ = s.Close(context.Background())
		}
		add("expired_supervisor_lease_is_reclaimed_without_reusing_foreign_children", reclaimed, map[string]any{"stale_lease_reclaimed": reclaimed})
	}

	// 16. bounded renderer restart storm.
	{
		s, l, cleanup, err := startScenarioSupervisor("restart-limit", 2, nil)
		if err != nil {
			return err
		}
		defer cleanup()
		bounded := true
		for i := 0; i < 3; i++ {
			child := l.latest("renderer")
			if child == nil {
				bounded = false
				break
			}
			ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
			_ = child.Stop(ctx)
			c()
			if i < 2 {
				prev := child.PID()
				if !waitUntil(6*time.Second, func() bool { st := s.Status(); return st.RendererPID > 0 && st.RendererPID != prev }) {
					bounded = false
					break
				}
			}
		}
		var fatal error
		select {
		case fatal = <-s.Fatal():
		case <-time.After(7 * time.Second):
			bounded = false
		}
		bounded = bounded && errors.Is(fatal, supervisor.ErrRestartLimit) && l.count("core") == 1 && l.count("renderer") == 3
		add("repeated_renderer_crashes_hit_bounded_restart_limit_without_storm", bounded, map[string]any{"core_launches": l.count("core"), "renderer_launches": l.count("renderer"), "bounded": bounded})
	}

	// 17. runtime identity drift fatal.
	{
		s, l, cleanup, err := startScenarioSupervisor("drift", 2, nil)
		if err != nil {
			return err
		}
		defer cleanup()
		data := launcherDataDir(l)
		layout, _ := corehost.BuildLayout(data)
		info, _ := corehost.ReadRuntime(layout.RuntimePath)
		info.SupervisorInstanceID = "foreign-owner"
		raw, _ := json.Marshal(info)
		_ = os.WriteFile(layout.RuntimePath, append(raw, '\n'), 0o600)
		var fatal error
		select {
		case fatal = <-s.Fatal():
		case <-time.After(6 * time.Second):
		}
		fatalOK := errors.Is(fatal, supervisor.ErrIdentityDrift) && waitUntil(5*time.Second, func() bool { _, _, _, st := l.snapshot(); return len(st) >= 2 }) && waitUntil(2*time.Second, func() bool { _, e := os.Stat(layout.RuntimePath); return os.IsNotExist(e) })
		add("core_runtime_identity_drift_is_fatal_and_shuts_owned_children", fatalOK, map[string]any{"fatal_identity_drift": fatalOK})
	}

	// 18. unexpected core exit fatal, no blind restart.
	{
		s, l, cleanup, err := startScenarioSupervisor("core-exit", 2, nil)
		if err != nil {
			return err
		}
		defer cleanup()
		core := l.latest("core")
		ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		_ = core.Stop(ctx)
		c()
		var fatal error
		select {
		case fatal = <-s.Fatal():
		case <-time.After(6 * time.Second):
		}
		fatalOK := errors.Is(fatal, supervisor.ErrChildExited) && waitUntil(5*time.Second, func() bool {
			_, _, _, st := l.snapshot()
			for _, k := range st {
				if k == "renderer" {
					return true
				}
			}
			return false
		}) && l.count("core") == 1
		add("unexpected_core_exit_is_fatal_and_never_blindly_restarted", fatalOK, map[string]any{"core_launches": l.count("core"), "fatal_child_exit": fatalOK})
	}

	// 19. supervisor lease ownership loss fatal and foreign lease preserved.
	{
		s, l, cleanup, err := startScenarioSupervisor("lease-loss", 2, nil)
		if err != nil {
			return err
		}
		defer cleanup()
		data := launcherDataDir(l)
		leasePath := filepath.Join(data, "desktop-supervisor", "lease", "owner.json")
		foreign := corehost.LeaseRecord{Version: 1, InstanceID: "foreign-supervisor", PID: 999, HeartbeatAt: time.Now().UTC()}
		raw, _ := json.Marshal(foreign)
		_ = os.WriteFile(leasePath, append(raw, '\n'), 0o600)
		var fatal error
		select {
		case fatal = <-s.Fatal():
		case <-time.After(6 * time.Second):
		}
		preserved := waitUntil(5*time.Second, func() bool { _, _, _, st := l.snapshot(); return len(st) >= 2 }) && treeContains(filepath.Dir(leasePath), "foreign-supervisor")
		ok := fatal != nil && strings.Contains(fatal.Error(), "supervisor lease refresh failed") && preserved
		add("supervisor_lease_ownership_loss_is_fatal_and_preserves_foreign_lease", ok, map[string]any{"fatal_refresh_failure": fatal != nil, "foreign_lease_preserved": preserved})
	}

	// 20. private renderer tamper before restart launch.
	{
		s, l, cleanup, err := startScenarioSupervisor("ui-tamper", 2, nil)
		if err != nil {
			return err
		}
		defer cleanup()
		spec, _ := l.latestSpec("renderer")
		_ = os.WriteFile(spec.Path, []byte("tampered-private-renderer"), 0o700)
		ui := l.latest("renderer")
		ctx, c := context.WithTimeout(context.Background(), 2*time.Second)
		_ = ui.Stop(ctx)
		c()
		var fatal error
		select {
		case fatal = <-s.Fatal():
		case <-time.After(6 * time.Second):
		}
		ok := errors.Is(fatal, supervisor.ErrBinaryIdentity) && l.count("renderer") == 1 && waitUntil(5*time.Second, func() bool {
			_, _, _, st := l.snapshot()
			for _, k := range st {
				if k == "core" {
					return true
				}
			}
			return false
		})
		add("tampered_private_renderer_binary_is_blocked_before_restart_launch", ok, map[string]any{"renderer_launches": l.count("renderer"), "tamper_blocked": errors.Is(fatal, supervisor.ErrBinaryIdentity)})
	}

	// 21. direct supervisor pipe closure shuts helper core and removes runtime.
	{
		root, _ := os.MkdirTemp("", "keydeck-proof38-pipe-")
		defer os.RemoveAll(root)
		self, _ := os.Executable()
		core := filepath.Join(root, "keydeck-core")
		_ = copyFile(self, core)
		data := filepath.Join(root, "data")
		layout, _ := corehost.BuildLayout(data)
		launcher := supervisor.NewProcessLauncher()
		child, err := launcher.Start(supervisor.ChildSpec{Path: core, Args: []string{"--data-dir", data, "--listen", "127.0.0.1:0", "--supervised"}, Env: []string{ownerEnv + "=pipe-owner"}})
		ok := err == nil && waitUntil(6*time.Second, func() bool {
			info, e := corehost.ReadRuntime(layout.RuntimePath)
			return e == nil && info.SupervisorInstanceID == "pipe-owner"
		})
		if ok {
			ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
			_ = child.Stop(ctx)
			c()
			ok = waitUntil(3*time.Second, func() bool { _, e := os.Stat(layout.RuntimePath); return os.IsNotExist(e) })
		}
		add("supervisor_pipe_closure_causes_graceful_core_shutdown_and_runtime_cleanup", ok, map[string]any{"runtime_removed_after_pipe_close": ok})
	}

	out.Passed = len(out.Scenarios) == 21
	for _, s := range out.Scenarios {
		if !s.Passed {
			out.Passed = false
		}
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
		return errors.New("Proof 0.38 failed")
	}
	return nil
}

func baseConfig(data, core, ui, ch, uh string, l supervisor.Launcher, opener func(string) error) supervisor.Config {
	return supervisor.Config{DataDir: data, CorePath: core, RendererPath: ui, ExpectedCoreSHA256: ch, ExpectedRendererSHA256: uh, ExpectedBuildID: buildID, ExpectedAPIVersion: corehost.DefaultAPIVersion, CoreListen: "127.0.0.1:0", RendererListen: "127.0.0.1:0", StartTimeout: 6 * time.Second, StopTimeout: 3 * time.Second, MonitorEvery: 25 * time.Millisecond, StaleLeaseAfter: 500 * time.Millisecond, HeartbeatEvery: 50 * time.Millisecond, RestartWindow: 3 * time.Second, MaxRendererRestarts: 2, Launcher: l, Opener: opener}
}
func makeHelperSources(root string) (string, string, string, string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", "", "", "", err
	}
	core := filepath.Join(root, "core-source")
	ui := filepath.Join(root, "ui-source")
	if err = copyFile(self, core); err != nil {
		return "", "", "", "", err
	}
	if err = copyFile(self, ui); err != nil {
		return "", "", "", "", err
	}
	return core, ui, digestFile(core), digestFile(ui), nil
}
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, cErr := io.Copy(out, in)
	sErr := out.Sync()
	clErr := out.Close()
	return errors.Join(cErr, sErr, clErr)
}
func digestFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))
}
func rendererToken(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "app" {
		return parts[1]
	}
	return ""
}
func treeContains(root, needle string) bool {
	if needle == "" {
		return false
	}
	found := false
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e == nil && bytes.Contains(raw, []byte(needle)) {
			found = true
		}
		return nil
	})
	return found
}
func waitUntil(timeout time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fn()
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func startScenarioSupervisor(name string, maxRestarts int, opener func(string) error) (*supervisor.Supervisor, *recordingLauncher, func(), error) {
	root, err := os.MkdirTemp("", "keydeck-proof38-"+name+"-")
	if err != nil {
		return nil, nil, nil, err
	}
	core, ui, ch, uh, err := makeHelperSources(root)
	if err != nil {
		os.RemoveAll(root)
		return nil, nil, nil, err
	}
	data := filepath.Join(root, "data")
	layout, _ := corehost.BuildLayout(data)
	l := &recordingLauncher{inner: supervisor.NewProcessLauncher()}
	l.uiHook = func(supervisor.ChildSpec) error {
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_, err := corehost.Connect(ctx, layout, buildID, corehost.DefaultAPIVersion, nil)
		return err
	}
	cfg := baseConfig(data, core, ui, ch, uh, l, opener)
	cfg.MaxRendererRestarts = maxRestarts
	s, err := supervisor.Open(cfg)
	if err != nil {
		os.RemoveAll(root)
		return nil, nil, nil, err
	}
	ctx, c := context.WithTimeout(context.Background(), 8*time.Second)
	err = s.Start(ctx)
	c()
	if err != nil {
		_ = s.Close(context.Background())
		os.RemoveAll(root)
		return nil, nil, nil, err
	}
	cleanup := func() {
		ctx, c := context.WithTimeout(context.Background(), 6*time.Second)
		_ = s.Close(ctx)
		c()
		os.RemoveAll(root)
	}
	return s, l, cleanup, nil
}
func launcherDataDir(l *recordingLauncher) string {
	spec, ok := l.latestSpec("core")
	if !ok {
		return ""
	}
	for i := 0; i+1 < len(spec.Args); i++ {
		if spec.Args[i] == "--data-dir" {
			return spec.Args[i+1]
		}
	}
	return ""
}
