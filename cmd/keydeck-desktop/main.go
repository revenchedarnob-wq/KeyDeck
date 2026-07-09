package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
	"keydeck.local/feasibilitylab/internal/supervisor"
)

const buildID = "keydeck-v0.35.0-reconstructed"

// Set at deterministic release build time from the exact sibling binaries.
var (
	expectedCoreSHA256     string
	expectedRendererSHA256 string
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var dataDir string
	flag.StringVar(&dataDir, "data-dir", defaultDataDir(), "KeyDeck local data directory")
	flag.Parse()

	coreHash := strings.ToLower(strings.TrimSpace(expectedCoreSHA256))
	rendererHash := strings.ToLower(strings.TrimSpace(expectedRendererSHA256))
	if len(coreHash) != 64 || len(rendererHash) != 64 {
		return errors.New("production desktop binary identities were not injected at build time")
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	binDir := filepath.Dir(self)
	corePath := filepath.Join(binDir, executableName("keydeck-core"))
	rendererPath := filepath.Join(binDir, executableName("keydeck-desktop-ui"))

	s, err := supervisor.Open(supervisor.Config{
		DataDir:                dataDir,
		CorePath:               corePath,
		RendererPath:           rendererPath,
		ExpectedCoreSHA256:     coreHash,
		ExpectedRendererSHA256: rendererHash,
		ExpectedBuildID:        buildID,
		ExpectedAPIVersion:     corehost.DefaultAPIVersion,
	})
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	}()

	startCtx, startCancel := context.WithTimeout(context.Background(), 15*time.Second)
	err = s.Start(startCtx)
	startCancel()
	if err != nil {
		return err
	}

	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signalCtx.Done():
		return nil
	case err := <-s.Fatal():
		if err == nil {
			return errors.New("desktop supervisor stopped unexpectedly")
		}
		return err
	}
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "KeyDeck")
	}
	return filepath.Join(".", ".keydeck")
}
