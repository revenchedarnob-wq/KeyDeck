package supervisor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

var (
	ErrInvalidConfig  = errors.New("invalid desktop supervisor configuration")
	ErrBinaryIdentity = errors.New("child binary identity mismatch")
	ErrForeignChild   = errors.New("foreign or stale child process detected")
	ErrIdentityDrift  = errors.New("supervised child identity drift")
	ErrChildExited    = errors.New("supervised child exited unexpectedly")
	ErrRestartLimit   = errors.New("renderer restart limit exceeded")
)

const (
	DefaultCoreListen      = "127.0.0.1:0"
	DefaultRendererListen  = "127.0.0.1:0"
	DefaultStartTimeout    = 8 * time.Second
	DefaultStopTimeout     = 5 * time.Second
	DefaultMonitorEvery    = 250 * time.Millisecond
	DefaultStaleLeaseAfter = 5 * time.Second
	DefaultHeartbeatEvery  = time.Second
	DefaultRestartWindow   = 30 * time.Second
	DefaultMaxRestarts     = 2
)

type Config struct {
	DataDir string

	CorePath               string
	RendererPath           string
	ExpectedCoreSHA256     string
	ExpectedRendererSHA256 string
	ExpectedBuildID        string
	ExpectedAPIVersion     string

	CoreListen     string
	RendererListen string

	StartTimeout        time.Duration
	StopTimeout         time.Duration
	MonitorEvery        time.Duration
	StaleLeaseAfter     time.Duration
	HeartbeatEvery      time.Duration
	RestartWindow       time.Duration
	MaxRendererRestarts int

	Random     io.Reader
	Now        func() time.Time
	HTTPClient *http.Client
	Launcher   Launcher
	Opener     func(string) error
	PID        int
}

type ChildSpec struct {
	Path          string
	Args          []string
	Env           []string
	CaptureStdout bool
}

type Child interface {
	PID() int
	Stdout() io.Reader
	Done() <-chan struct{}
	ExitError() error
	Stop(context.Context) error
}

type Launcher interface {
	Start(ChildSpec) (Child, error)
}

type Status struct {
	InstanceID       string
	CorePID          int
	RendererPID      int
	RendererRestarts int
	Running          bool
}
