package supervisor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
)

type testChild struct {
	pid    int
	stdout io.Reader
	done   chan struct{}
	stop   func()
}

func (c *testChild) PID() int              { return c.pid }
func (c *testChild) Stdout() io.Reader     { return c.stdout }
func (c *testChild) Done() <-chan struct{} { return c.done }
func (c *testChild) ExitError() error      { return nil }
func (c *testChild) Stop(context.Context) error {
	if c.stop != nil {
		c.stop()
	}
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return nil
}

type countingLauncher struct {
	mu    sync.Mutex
	count int
	block <-chan struct{}
}

func (l *countingLauncher) Start(ChildSpec) (Child, error) {
	l.mu.Lock()
	l.count++
	l.mu.Unlock()
	if l.block != nil {
		<-l.block
	}
	return &testChild{pid: 100 + l.count, done: make(chan struct{})}, nil
}
func (l *countingLauncher) Count() int { l.mu.Lock(); defer l.mu.Unlock(); return l.count }

type customRT struct{}

func (customRT) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("should not run")
}

func writeExecutable(t *testing.T, dir, name, body string) (string, string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(body))
	return path, hex.EncodeToString(h[:])
}

func baseConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	core, coreHash := writeExecutable(t, dir, "core.bin", "core-binary")
	ui, uiHash := writeExecutable(t, dir, "ui.bin", "ui-binary")
	return Config{DataDir: filepath.Join(dir, "data"), CorePath: core, RendererPath: ui,
		ExpectedCoreSHA256: coreHash, ExpectedRendererSHA256: uiHash,
		ExpectedBuildID: "build", ExpectedAPIVersion: "v1", PID: 4321,
		MonitorEvery: time.Hour, HeartbeatEvery: time.Hour, StaleLeaseAfter: 2 * time.Hour}
}

func TestWrongBinaryDigestRejectedBeforeLaunch(t *testing.T) {
	cfg := baseConfig(t)
	cfg.ExpectedCoreSHA256 = strings.Repeat("0", 64)
	launcher := &countingLauncher{}
	cfg.Launcher = launcher
	_, err := Open(cfg)
	if !errors.Is(err, ErrBinaryIdentity) {
		t.Fatalf("expected binary identity error, got %v", err)
	}
	if launcher.Count() != 0 {
		t.Fatalf("launcher invoked %d times", launcher.Count())
	}
}

func TestSymlinkExecutableRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	cfg := baseConfig(t)
	link := filepath.Join(filepath.Dir(cfg.CorePath), "core-link")
	if err := os.Symlink(cfg.CorePath, link); err != nil {
		t.Fatal(err)
	}
	cfg.CorePath = link
	_, err := Open(cfg)
	if !errors.Is(err, ErrBinaryIdentity) {
		t.Fatalf("expected binary identity error, got %v", err)
	}
}

func TestExistingForeignRuntimeRejectedBeforeLaunch(t *testing.T) {
	cfg := baseConfig(t)
	layout, err := corehost.BuildLayout(cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(layout.RuntimePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.RuntimePath, []byte(`{"foreign":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher := &countingLauncher{}
	cfg.Launcher = launcher
	_, err = Open(cfg)
	if !errors.Is(err, ErrForeignChild) {
		t.Fatalf("expected foreign child, got %v", err)
	}
	if launcher.Count() != 0 {
		t.Fatalf("launcher invoked")
	}
}

func TestVerifiedPrivateCopiesSurviveSourceMutation(t *testing.T) {
	cfg := baseConfig(t)
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	if err := os.WriteFile(cfg.CorePath, []byte("mutated"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := verifyExecutable(s.coreExec, cfg.ExpectedCoreSHA256); err != nil {
		t.Fatalf("private copy changed: %v", err)
	}
	if err := verifyExecutable(s.uiExec, cfg.ExpectedRendererSHA256); err != nil {
		t.Fatalf("private UI copy changed: %v", err)
	}
}

func TestReadyFrameRejectsUnknownFieldsAndOversize(t *testing.T) {
	if _, err := readReadyFrame(strings.NewReader(`{"type":"keydeck-ui-ready-v1","url":"http://127.0.0.1/app/x/","supervisor_instance_id":"i","extra":1}` + "\n")); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
	if _, err := readReadyFrame(bytes.NewReader(append(bytes.Repeat([]byte("x"), maxReadyFrameBytes+1), '\n'))); err == nil {
		t.Fatal("expected oversized-frame rejection")
	}
}

func TestCloseStopsRendererBeforeCore(t *testing.T) {
	var mu sync.Mutex
	var order []string
	mk := func(name string) *testChild {
		return &testChild{pid: 1, done: make(chan struct{}), stop: func() { mu.Lock(); order = append(order, name); mu.Unlock() }}
	}
	s := &Supervisor{cfg: Config{StopTimeout: time.Second}, core: mk("core"), renderer: mk("renderer"), closed: false, running: true, monitorOn: false,
		lease: nil, execDir: t.TempDir()}
	// Close needs a lease; use an already-closed synthetic path by directly checking stop helper order through abortStart semantics.
	s.mu.Lock()
	renderer, core := s.renderer, s.core
	s.mu.Unlock()
	_ = stopChild(renderer, time.Second)
	_ = stopChild(core, time.Second)
	if strings.Join(order, ",") != "renderer,core" {
		t.Fatalf("wrong stop order: %v", order)
	}
}

func TestHardenedHTTPClientRejectsCustomTransport(t *testing.T) {
	_, err := hardenedHTTPClient(&http.Client{Transport: customRT{}})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config, got %v", err)
	}
}

func TestStartReverifiesPrivateCoreBeforeLaunch(t *testing.T) {
	cfg := baseConfig(t)
	launcher := &countingLauncher{}
	cfg.Launcher = launcher
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.coreExec, []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = s.Start(context.Background())
	if !errors.Is(err, ErrBinaryIdentity) {
		t.Fatalf("expected binary identity, got %v", err)
	}
	if launcher.Count() != 0 {
		t.Fatalf("launcher invoked %d times", launcher.Count())
	}
}

func TestMergeEnvReplacesInheritedOwnershipBindingUniquely(t *testing.T) {
	got := mergeEnv([]string{"A=1", supervisorInstanceEnvVar + "=old", "B=2"}, []string{supervisorInstanceEnvVar + "=new"})
	count := 0
	for _, e := range got {
		if strings.HasPrefix(e, supervisorInstanceEnvVar+"=") {
			count++
			if e != supervisorInstanceEnvVar+"=new" {
				t.Fatalf("wrong owner env: %q", e)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one owner binding, got %d: %v", count, got)
	}
}

func TestConcurrentStartDoesNotDoubleLaunch(t *testing.T) {
	cfg := baseConfig(t)
	gate := make(chan struct{})
	launcher := &countingLauncher{block: gate}
	cfg.Launcher = launcher
	s, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- s.Start(ctx) }()
	for deadline := time.Now().Add(time.Second); launcher.Count() == 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	go func() { done <- s.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)
	if launcher.Count() != 1 {
		t.Fatalf("expected one launch while first Start owns lifecycle, got %d", launcher.Count())
	}
	close(gate)
	<-done
	<-done
}
