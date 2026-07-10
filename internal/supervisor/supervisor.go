package supervisor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
)

const (
	supervisorInstanceEnvVar = "KEYDECK_SUPERVISOR_INSTANCE"
	uiReadyFrameType         = "keydeck-ui-ready-v1"
	maxReadyFrameBytes       = 8 << 10
	maxRendererHTMLBytes     = 256 << 10
)

var (
	hexDigest  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	secretPath = regexp.MustCompile(`^/app/[A-Za-z0-9_-]{43}/$`)
)

type Supervisor struct {
	lifecycle sync.Mutex
	cfg       Config
	layout    corehost.Layout
	instance  string
	lease     *corehost.Lease
	execDir   string
	coreExec  string
	uiExec    string

	mu          sync.Mutex
	core        Child
	renderer    Child
	coreRuntime corehost.RuntimeInfo
	restarts    []time.Time
	running     bool
	closed      bool
	monitorOn   bool

	monitorStop chan struct{}
	monitorDone chan struct{}
	stopOnce    sync.Once
	fatal       chan error
	fatalOnce   sync.Once
}

type uiReadyFrame struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	SupervisorInstanceID string `json:"supervisor_instance_id"`
}

func platformExecutableName(base string) string {
	if goruntime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func Open(cfg Config) (*Supervisor, error) {
	normalized, layout, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	instance, err := randomHex(normalized.Random, 16)
	if err != nil {
		return nil, err
	}
	leaseDir := filepath.Join(layout.DataDir, "desktop-supervisor", "lease")
	lease, err := corehost.AcquireLease(leaseDir, instance, normalized.PID, normalized.Now, normalized.StaleLeaseAfter)
	if err != nil {
		return nil, err
	}
	cleanup := true
	var execDir string
	defer func() {
		if cleanup {
			if execDir != "" {
				_ = os.RemoveAll(execDir)
			}
			_ = lease.Release()
		}
	}()
	if _, err := os.Stat(layout.RuntimePath); err == nil {
		return nil, ErrForeignChild
	} else if !os.IsNotExist(err) {
		return nil, ErrForeignChild
	}
	execDir = filepath.Join(layout.DataDir, "desktop-supervisor", "runtime", instance)
	if err := os.MkdirAll(execDir, 0o700); err != nil {
		return nil, err
	}
	coreExec, err := prepareVerifiedExecutable(normalized.CorePath, execDir, platformExecutableName("keydeck-core"), normalized.ExpectedCoreSHA256)
	if err != nil {
		return nil, err
	}
	uiExec, err := prepareVerifiedExecutable(normalized.RendererPath, execDir, platformExecutableName("keydeck-desktop-ui"), normalized.ExpectedRendererSHA256)
	if err != nil {
		return nil, err
	}
	s := &Supervisor{
		cfg: normalized, layout: layout, instance: instance, lease: lease, execDir: execDir,
		coreExec: coreExec, uiExec: uiExec,
		monitorStop: make(chan struct{}), monitorDone: make(chan struct{}), fatal: make(chan error, 1),
	}
	cleanup = false
	return s, nil
}

func (s *Supervisor) Start(ctx context.Context) error {
	if s == nil {
		return ErrInvalidConfig
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.closed || s.running || s.core != nil || s.renderer != nil {
		s.mu.Unlock()
		return ErrInvalidConfig
	}
	s.mu.Unlock()
	if err := verifyExecutable(s.coreExec, s.cfg.ExpectedCoreSHA256); err != nil {
		return s.abortStart(err)
	}
	core, err := s.cfg.Launcher.Start(ChildSpec{
		Path: s.coreExec,
		Args: []string{"--data-dir", s.layout.DataDir, "--listen", s.cfg.CoreListen, "--supervised"},
		Env:  []string{supervisorInstanceEnvVar + "=" + s.instance},
	})
	if err != nil {
		return s.abortStart(err)
	}
	s.mu.Lock()
	s.core = core
	s.mu.Unlock()
	runtimeInfo, err := s.waitForCore(ctx, core)
	if err != nil {
		return s.abortStart(err)
	}
	s.mu.Lock()
	s.coreRuntime = runtimeInfo
	s.mu.Unlock()
	renderer, launchURL, err := s.startRenderer(ctx)
	if err != nil {
		return s.abortStart(err)
	}
	if s.cfg.Opener != nil {
		if err := s.cfg.Opener(launchURL); err != nil {
			_ = stopChild(renderer, s.cfg.StopTimeout)
			return s.abortStart(err)
		}
	}
	s.mu.Lock()
	s.renderer = renderer
	s.running = true
	s.monitorOn = true
	s.mu.Unlock()
	go s.monitorLoop()
	return nil
}

func (s *Supervisor) Status() Status {
	if s == nil {
		return Status{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{InstanceID: s.instance, RendererRestarts: len(s.restarts), Running: s.running && !s.closed}
	if s.core != nil {
		out.CorePID = s.core.PID()
	}
	if s.renderer != nil {
		out.RendererPID = s.renderer.PID()
	}
	return out
}

func (s *Supervisor) Fatal() <-chan error {
	if s == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return s.fatal
}

func (s *Supervisor) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.running = false
	monitorOn := s.monitorOn
	core := s.core
	renderer := s.renderer
	s.mu.Unlock()
	if monitorOn {
		s.stopOnce.Do(func() { close(s.monitorStop) })
		select {
		case <-s.monitorDone:
		case <-ctx.Done():
		}
	}
	var errs []error
	if renderer != nil {
		if err := stopChildWithContext(ctx, renderer, s.cfg.StopTimeout); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			errs = append(errs, err)
		}
	}
	if core != nil {
		if err := stopChildWithContext(ctx, core, s.cfg.StopTimeout); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			errs = append(errs, err)
		}
	}
	if err := s.lease.Release(); err != nil {
		errs = append(errs, err)
	}
	if err := os.RemoveAll(s.execDir); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Supervisor) abortStart(cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.StopTimeout)
	defer cancel()
	s.mu.Lock()
	core := s.core
	renderer := s.renderer
	s.closed = true
	s.mu.Unlock()
	if renderer != nil {
		_ = renderer.Stop(ctx)
	}
	if core != nil {
		_ = core.Stop(ctx)
	}
	_ = s.lease.Release()
	_ = os.RemoveAll(s.execDir)
	return cause
}

func (s *Supervisor) waitForCore(parent context.Context, child Child) (corehost.RuntimeInfo, error) {
	ctx, cancel := context.WithTimeout(parent, s.cfg.StartTimeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return corehost.RuntimeInfo{}, ctx.Err()
		case <-child.Done():
			return corehost.RuntimeInfo{}, fmt.Errorf("%w: core: %v", ErrChildExited, child.ExitError())
		case <-ticker.C:
			info, err := corehost.ReadRuntime(s.layout.RuntimePath)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return corehost.RuntimeInfo{}, ErrIdentityDrift
			}
			if err := s.validateCoreRuntime(info, child.PID(), nil); err != nil {
				return corehost.RuntimeInfo{}, err
			}
			attemptCtx, attemptCancel := context.WithTimeout(ctx, 500*time.Millisecond)
			_, connectErr := corehost.Connect(attemptCtx, s.layout, s.cfg.ExpectedBuildID, s.cfg.ExpectedAPIVersion, s.cfg.HTTPClient)
			attemptCancel()
			if connectErr != nil {
				if errors.Is(connectErr, corehost.ErrIdentityMismatch) {
					return corehost.RuntimeInfo{}, ErrIdentityDrift
				}
				continue
			}
			after, err := corehost.ReadRuntime(s.layout.RuntimePath)
			if err != nil || after != info {
				return corehost.RuntimeInfo{}, ErrIdentityDrift
			}
			return info, nil
		}
	}
}

func (s *Supervisor) startRenderer(parent context.Context) (Child, string, error) {
	if err := verifyExecutable(s.uiExec, s.cfg.ExpectedRendererSHA256); err != nil {
		return nil, "", err
	}
	if err := s.attestCurrentCore(parent); err != nil {
		return nil, "", err
	}
	child, err := s.cfg.Launcher.Start(ChildSpec{
		Path:          s.uiExec,
		Args:          []string{"--data-dir", s.layout.DataDir, "--expected-build", s.cfg.ExpectedBuildID, "--listen", s.cfg.RendererListen, "--supervised"},
		Env:           []string{supervisorInstanceEnvVar + "=" + s.instance},
		CaptureStdout: true,
	})
	if err != nil {
		return nil, "", err
	}
	frame, err := waitReadyFrame(parent, child, s.cfg.StartTimeout)
	if err != nil {
		_ = stopChild(child, s.cfg.StopTimeout)
		return nil, "", err
	}
	if frame.Type != uiReadyFrameType || frame.SupervisorInstanceID != s.instance {
		_ = stopChild(child, s.cfg.StopTimeout)
		return nil, "", ErrForeignChild
	}
	if err := s.attestRenderer(parent, frame.URL); err != nil {
		_ = stopChild(child, s.cfg.StopTimeout)
		return nil, "", err
	}
	return child, frame.URL, nil
}

func (s *Supervisor) attestCurrentCore(ctx context.Context) error {
	s.mu.Lock()
	core := s.core
	expected := s.coreRuntime
	s.mu.Unlock()
	if core == nil || expected.InstanceID == "" {
		return ErrIdentityDrift
	}
	info, err := corehost.ReadRuntime(s.layout.RuntimePath)
	if err != nil {
		return ErrIdentityDrift
	}
	if err := s.validateCoreRuntime(info, core.PID(), &expected); err != nil {
		return err
	}
	_, err = corehost.Connect(ctx, s.layout, s.cfg.ExpectedBuildID, s.cfg.ExpectedAPIVersion, s.cfg.HTTPClient)
	if err != nil {
		return err
	}
	after, err := corehost.ReadRuntime(s.layout.RuntimePath)
	if err != nil || after != info {
		return ErrIdentityDrift
	}
	return nil
}

func (s *Supervisor) validateCoreRuntime(info corehost.RuntimeInfo, pid int, expected *corehost.RuntimeInfo) error {
	if info.SupervisorInstanceID != s.instance {
		return ErrForeignChild
	}
	if info.PID != pid || info.BuildID != s.cfg.ExpectedBuildID || info.APIVersion != s.cfg.ExpectedAPIVersion {
		return ErrIdentityDrift
	}
	if expected != nil && info != *expected {
		return ErrIdentityDrift
	}
	return nil
}

func (s *Supervisor) monitorLoop() {
	defer close(s.monitorDone)
	monitorTicker := time.NewTicker(s.cfg.MonitorEvery)
	heartbeatTicker := time.NewTicker(s.cfg.HeartbeatEvery)
	defer monitorTicker.Stop()
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-s.monitorStop:
			return
		case <-heartbeatTicker.C:
			if err := s.lease.Refresh(); err != nil {
				s.fail(fmt.Errorf("supervisor lease refresh failed: %w", err))
				return
			}
		case <-monitorTicker.C:
			if err := s.monitorOnce(); err != nil {
				s.fail(err)
				return
			}
		}
	}
}

func (s *Supervisor) monitorOnce() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	core := s.core
	renderer := s.renderer
	expected := s.coreRuntime
	s.mu.Unlock()
	if core == nil || renderer == nil {
		return ErrIdentityDrift
	}
	select {
	case <-core.Done():
		return fmt.Errorf("%w: core: %v", ErrChildExited, core.ExitError())
	default:
	}
	info, err := corehost.ReadRuntime(s.layout.RuntimePath)
	if err != nil || s.validateCoreRuntime(info, core.PID(), &expected) != nil {
		return ErrIdentityDrift
	}
	select {
	case <-renderer.Done():
		return s.restartRenderer()
	default:
		return nil
	}
}

func (s *Supervisor) restartRenderer() error {
	now := s.cfg.Now().UTC()
	s.mu.Lock()
	cutoff := now.Add(-s.cfg.RestartWindow)
	kept := s.restarts[:0]
	for _, t := range s.restarts {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	s.restarts = kept
	if len(s.restarts) >= s.cfg.MaxRendererRestarts {
		s.mu.Unlock()
		return ErrRestartLimit
	}
	s.restarts = append(s.restarts, now)
	s.renderer = nil
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), s.cfg.StartTimeout)
	defer cancel()
	renderer, launchURL, err := s.startRenderer(ctx)
	if err != nil {
		return err
	}
	if s.cfg.Opener != nil {
		if err := s.cfg.Opener(launchURL); err != nil {
			_ = stopChild(renderer, s.cfg.StopTimeout)
			return err
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = stopChild(renderer, s.cfg.StopTimeout)
		return nil
	}
	s.renderer = renderer
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) fail(err error) {
	if err == nil {
		return
	}
	s.fatalOnce.Do(func() {
		s.fatal <- err
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*s.cfg.StopTimeout)
			defer cancel()
			_ = s.Close(ctx)
		}()
	})
}

func (s *Supervisor) attestRenderer(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !secretPath.MatchString(u.EscapedPath()) {
		return ErrIdentityDrift
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || port == "" {
		return ErrIdentityDrift
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return ErrIdentityDrift
	}
	client, err := hardenedHTTPClient(s.cfg.HTTPClient)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrIdentityDrift
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/html" {
		return ErrIdentityDrift
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") || resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		return ErrIdentityDrift
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRendererHTMLBytes+1))
	if err != nil || len(body) > maxRendererHTMLBytes {
		return ErrIdentityDrift
	}
	if !bytes.Contains(body, []byte("<title>KeyDeck</title>")) || bytes.Contains(body, []byte(strings.TrimPrefix(u.EscapedPath(), "/app/"))) {
		return ErrIdentityDrift
	}
	return nil
}

func waitReadyFrame(parent context.Context, child Child, timeout time.Duration) (uiReadyFrame, error) {
	if child.Stdout() == nil {
		return uiReadyFrame{}, ErrIdentityDrift
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	type result struct {
		frame uiReadyFrame
		err   error
	}
	resultCh := make(chan result, 1)
	go func() { frame, err := readReadyFrame(child.Stdout()); resultCh <- result{frame, err} }()
	select {
	case <-ctx.Done():
		return uiReadyFrame{}, ctx.Err()
	case <-child.Done():
		return uiReadyFrame{}, fmt.Errorf("%w: renderer: %v", ErrChildExited, child.ExitError())
	case r := <-resultCh:
		return r.frame, r.err
	}
}

func readReadyFrame(r io.Reader) (uiReadyFrame, error) {
	br := bufio.NewReader(io.LimitReader(r, maxReadyFrameBytes+1))
	line, err := br.ReadString('\n')
	if err != nil || len(line) > maxReadyFrameBytes {
		return uiReadyFrame{}, ErrIdentityDrift
	}
	dec := json.NewDecoder(strings.NewReader(line))
	dec.DisallowUnknownFields()
	var frame uiReadyFrame
	if err := dec.Decode(&frame); err != nil {
		return uiReadyFrame{}, ErrIdentityDrift
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return uiReadyFrame{}, ErrIdentityDrift
	}
	return frame, nil
}

func normalizeConfig(cfg Config) (Config, corehost.Layout, error) {
	layout, err := corehost.BuildLayout(cfg.DataDir)
	if err != nil {
		return Config{}, corehost.Layout{}, err
	}
	cfg.CorePath = strings.TrimSpace(cfg.CorePath)
	cfg.RendererPath = strings.TrimSpace(cfg.RendererPath)
	cfg.ExpectedCoreSHA256 = strings.ToLower(strings.TrimSpace(cfg.ExpectedCoreSHA256))
	cfg.ExpectedRendererSHA256 = strings.ToLower(strings.TrimSpace(cfg.ExpectedRendererSHA256))
	cfg.ExpectedBuildID = strings.TrimSpace(cfg.ExpectedBuildID)
	cfg.ExpectedAPIVersion = strings.TrimSpace(cfg.ExpectedAPIVersion)
	if cfg.CorePath == "" || cfg.RendererPath == "" || !hexDigest.MatchString(cfg.ExpectedCoreSHA256) || !hexDigest.MatchString(cfg.ExpectedRendererSHA256) || cfg.ExpectedBuildID == "" || cfg.ExpectedAPIVersion == "" {
		return Config{}, corehost.Layout{}, ErrInvalidConfig
	}
	if cfg.CoreListen == "" {
		cfg.CoreListen = DefaultCoreListen
	}
	if cfg.RendererListen == "" {
		cfg.RendererListen = DefaultRendererListen
	}
	if !explicitLoopbackListen(cfg.CoreListen) || !explicitLoopbackListen(cfg.RendererListen) {
		return Config{}, corehost.Layout{}, ErrInvalidConfig
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = DefaultStartTimeout
	}
	if cfg.StopTimeout <= 0 {
		cfg.StopTimeout = DefaultStopTimeout
	}
	if cfg.MonitorEvery <= 0 {
		cfg.MonitorEvery = DefaultMonitorEvery
	}
	if cfg.StaleLeaseAfter <= 0 {
		cfg.StaleLeaseAfter = DefaultStaleLeaseAfter
	}
	if cfg.HeartbeatEvery <= 0 {
		cfg.HeartbeatEvery = DefaultHeartbeatEvery
	}
	if cfg.HeartbeatEvery >= cfg.StaleLeaseAfter {
		return Config{}, corehost.Layout{}, ErrInvalidConfig
	}
	if cfg.RestartWindow <= 0 {
		cfg.RestartWindow = DefaultRestartWindow
	}
	if cfg.MaxRendererRestarts < 0 {
		return Config{}, corehost.Layout{}, ErrInvalidConfig
	}
	if cfg.MaxRendererRestarts == 0 {
		cfg.MaxRendererRestarts = DefaultMaxRestarts
	}
	if cfg.Random == nil {
		cfg.Random = rand.Reader
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Launcher == nil {
		cfg.Launcher = execLauncher{}
	}
	if cfg.PID <= 0 {
		cfg.PID = os.Getpid()
	}
	return cfg, layout, nil
}

func prepareVerifiedExecutable(source, dir, base, expected string) (string, error) {
	abs, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", ErrBinaryIdentity
	}
	actual, err := fileSHA256(abs)
	if err != nil || actual != expected {
		return "", ErrBinaryIdentity
	}
	ext := filepath.Ext(abs)
	dest := filepath.Join(dir, base+ext)
	in, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		return "", err
	}
	copied, err := fileSHA256(dest)
	if err != nil || copied != expected {
		return "", ErrBinaryIdentity
	}
	return dest, nil
}

func verifyExecutable(path, expected string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrBinaryIdentity
	}
	actual, err := fileSHA256(path)
	if err != nil || actual != expected {
		return ErrBinaryIdentity
	}
	return nil
}
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func randomHex(r io.Reader, bytesN int) (string, error) {
	b := make([]byte, bytesN)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func explicitLoopbackListen(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func hardenedHTTPClient(base *http.Client) (*http.Client, error) {
	var out http.Client
	if base == nil {
		out.Timeout = 5 * time.Second
	} else {
		out = *base
		if out.Timeout <= 0 {
			out.Timeout = 5 * time.Second
		}
	}
	switch tr := out.Transport.(type) {
	case nil:
		clone := http.DefaultTransport.(*http.Transport).Clone()
		clone.Proxy = nil
		out.Transport = clone
	case *http.Transport:
		clone := tr.Clone()
		clone.Proxy = nil
		out.Transport = clone
	default:
		return nil, ErrInvalidConfig
	}
	out.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &out, nil
}
func stopChild(child Child, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return child.Stop(ctx)
}
func stopChildWithContext(parent context.Context, child Child, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return child.Stop(ctx)
}
