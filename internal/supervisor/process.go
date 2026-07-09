package supervisor

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type execLauncher struct{}

type execChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	done   chan struct{}

	mu       sync.Mutex
	exitErr  error
	stopOnce sync.Once
}

func (execLauncher) Start(spec ChildSpec) (Child, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Env = mergeEnv(os.Environ(), spec.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	var stdout io.ReadCloser
	if spec.CaptureStdout {
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			_ = stdin.Close()
			return nil, err
		}
	} else {
		cmd.Stdout = io.Discard
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		if stdout != nil {
			_ = stdout.Close()
		}
		return nil, err
	}
	c := &execChild{cmd: cmd, stdin: stdin, stdout: stdout, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		c.mu.Lock()
		c.exitErr = err
		c.mu.Unlock()
		close(c.done)
	}()
	return c, nil
}

func (c *execChild) PID() int {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}
func (c *execChild) Stdout() io.Reader {
	if c == nil {
		return nil
	}
	return c.stdout
}
func (c *execChild) Done() <-chan struct{} {
	if c == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return c.done
}
func (c *execChild) ExitError() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}
func (c *execChild) Stop(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.stopOnce.Do(func() {
		if c.stdin != nil {
			_, _ = io.WriteString(c.stdin, "shutdown\n")
			_ = c.stdin.Close()
		}
	})
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		<-c.done
		return ctx.Err()
	}
}

func mergeEnv(base, overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		if i := strings.IndexByte(entry, '='); i > 0 {
			keys[entry[:i]] = struct{}{}
		}
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		i := strings.IndexByte(entry, '=')
		if i > 0 {
			if _, replaced := keys[entry[:i]]; replaced {
				continue
			}
		}
		out = append(out, entry)
	}
	return append(out, overrides...)
}

// NewProcessLauncher returns the production child-process launcher.
// It is exposed so proof harnesses can wrap and observe launch metadata without
// replacing the real operating-system process boundary.
func NewProcessLauncher() Launcher { return execLauncher{} }
