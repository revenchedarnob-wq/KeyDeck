package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
)

const (
	buildID                  = "keydeck-v0.35.0-reconstructed"
	supervisorInstanceEnvVar = "KEYDECK_SUPERVISOR_INSTANCE"
)

func main() {
	dataDir := flag.String("data-dir", defaultDataDir(), "KeyDeck local data directory")
	listen := flag.String("listen", "127.0.0.1:0", "explicit loopback listen address")
	supervised := flag.Bool("supervised", false, "run as a desktop-supervisor owned child process")
	flag.Parse()

	owner := ""
	if *supervised {
		owner = strings.TrimSpace(os.Getenv(supervisorInstanceEnvVar))
		if owner == "" {
			fmt.Fprintln(os.Stderr, "supervised core requires supervisor ownership identity")
			os.Exit(1)
		}
	}

	host, err := corehost.Open(corehost.Config{
		DataDir:              *dataDir,
		ListenAddress:        *listen,
		BuildID:              buildID,
		SupervisorInstanceID: owner,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runtime, err := host.Start()
	if err != nil {
		_ = host.Close(context.Background())
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !*supervised {
		fmt.Printf("KeyDeck core ready at %s (build %s)\n", runtime.Address, runtime.BuildID)
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()
	if *supervised {
		go cancelOnSupervisorPipeClose(cancel)
	}

	exitCode := 0
	select {
	case <-ctx.Done():
	case err := <-host.Fatal():
		fmt.Fprintln(os.Stderr, err)
		exitCode = 1
	}
	shutdown, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := host.Close(shutdown); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitCode = 1
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func cancelOnSupervisorPipeClose(cancel context.CancelFunc) {
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	cancel()
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "KeyDeck")
	}
	return filepath.Join(".", ".keydeck")
}
