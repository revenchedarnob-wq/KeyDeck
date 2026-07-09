package codexapp

import (
	"bufio"
	"context"
	"io"
	"os/exec"
)

type Transport interface {
	Send([]byte) error
	Lines() <-chan []byte
	Errors() <-chan error
	Close() error
}

type processTransport struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan []byte
	errs  chan error
}

func NewProcessTransport(ctx context.Context, command string, args ...string) (Transport, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	t := &processTransport{cmd: cmd, stdin: stdin, lines: make(chan []byte, 256), errs: make(chan error, 8)}
	go t.scan(stdout)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			// Stderr is diagnostic-only. Surface it as an error channel message
			// without terminating the JSON-RPC stream.
			t.errs <- &serverDiagnostic{message: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			t.errs <- err
		}
	}()
	go func() {
		err := cmd.Wait()
		if err != nil {
			t.errs <- err
		}
		close(t.lines)
	}()
	return t, nil
}

func (t *processTransport) scan(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		t.lines <- line
	}
	if err := scanner.Err(); err != nil {
		t.errs <- err
	}
}

func (t *processTransport) Send(b []byte) error {
	if _, err := t.stdin.Write(b); err != nil {
		return err
	}
	_, err := t.stdin.Write([]byte("\n"))
	return err
}
func (t *processTransport) Lines() <-chan []byte { return t.lines }
func (t *processTransport) Errors() <-chan error { return t.errs }
func (t *processTransport) Close() error {
	_ = t.stdin.Close()
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	return nil
}

type serverDiagnostic struct{ message string }

func (e *serverDiagnostic) Error() string { return "codex app-server: " + e.message }

type streamTransport struct {
	rw    io.ReadWriteCloser
	lines chan []byte
	errs  chan error
}

func NewStreamTransport(rw io.ReadWriteCloser) Transport {
	t := &streamTransport{rw: rw, lines: make(chan []byte, 256), errs: make(chan error, 8)}
	go func() {
		defer close(t.lines)
		scanner := bufio.NewScanner(rw)
		scanner.Buffer(make([]byte, 64<<10), 16<<20)
		for scanner.Scan() {
			t.lines <- append([]byte(nil), scanner.Bytes()...)
		}
		if err := scanner.Err(); err != nil {
			t.errs <- err
		}
	}()
	return t
}
func (t *streamTransport) Send(b []byte) error {
	if _, err := t.rw.Write(b); err != nil {
		return err
	}
	_, err := t.rw.Write([]byte("\n"))
	return err
}
func (t *streamTransport) Lines() <-chan []byte { return t.lines }
func (t *streamTransport) Errors() <-chan error { return t.errs }
func (t *streamTransport) Close() error         { return t.rw.Close() }
