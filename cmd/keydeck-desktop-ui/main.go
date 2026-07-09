package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
	"keydeck.local/feasibilitylab/internal/presentation"
	"keydeck.local/feasibilitylab/internal/visualshell"
)

const (
	defaultBuildID           = "keydeck-v0.35.0-reconstructed"
	supervisorInstanceEnvVar = "KEYDECK_SUPERVISOR_INSTANCE"
	supervisedReadyFrameType = "keydeck-ui-ready-v1"
)

type supervisedReadyFrame struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	SupervisorInstanceID string `json:"supervisor_instance_id"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var dataDir, expectedBuild, listen string
	var noOpen, supervised bool
	flag.StringVar(&dataDir, "data-dir", "", "KeyDeck core data directory")
	flag.StringVar(&expectedBuild, "expected-build", defaultBuildID, "exact keydeck-core build ID to attest")
	flag.StringVar(&listen, "listen", visualshell.DefaultListenAddress, "explicit loopback renderer listen address")
	flag.BoolVar(&noOpen, "no-open", false, "print the visual shell URL without opening the default browser")
	flag.BoolVar(&supervised, "supervised", false, "run as a desktop-supervisor owned child process")
	flag.Parse()
	if dataDir == "" {
		return errors.New("--data-dir is required")
	}
	owner := ""
	if supervised {
		owner = strings.TrimSpace(os.Getenv(supervisorInstanceEnvVar))
		if owner == "" {
			return errors.New("supervised visual shell requires supervisor ownership identity")
		}
		noOpen = true
	}

	layout, err := corehost.BuildLayout(dataDir)
	if err != nil {
		return err
	}
	shell := presentation.New(layout, expectedBuild, corehost.DefaultAPIVersion, nil)
	renderer, err := visualshell.Open(visualshell.Config{ListenAddress: listen, Shell: shell})
	if err != nil {
		return err
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	if supervised {
		go cancelOnSupervisorPipeClose(cancel)
	}

	launchURL, err := renderer.Start(ctx)
	if err != nil {
		return err
	}
	if supervised {
		if err := json.NewEncoder(os.Stdout).Encode(supervisedReadyFrame{Type: supervisedReadyFrameType, URL: launchURL, SupervisorInstanceID: owner}); err != nil {
			_ = renderer.Close(context.Background())
			return err
		}
	} else {
		fmt.Println(launchURL)
	}
	if !noOpen {
		if err := openURL(launchURL); err != nil {
			_ = renderer.Close(context.Background())
			return err
		}
	}
	select {
	case <-ctx.Done():
	case err := <-renderer.Fatal():
		if err != nil {
			_ = renderer.Close(context.Background())
			return fmt.Errorf("visual shell failed: %w", err)
		}
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	return renderer.Close(closeCtx)
}

func cancelOnSupervisorPipeClose(cancel context.CancelFunc) {
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	cancel()
}

func openURL(raw string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	case "darwin":
		cmd = exec.Command("open", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	return cmd.Start()
}
