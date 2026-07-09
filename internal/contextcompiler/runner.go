package contextcompiler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Runner interface {
	Call(ctx context.Context, tool string, args map[string]any) ([]byte, error)
	Name() string
	Version(ctx context.Context) string
}

type CLIRunner struct {
	Binary   string
	CacheDir string
}

func (r *CLIRunner) Name() string { return "codebase-memory-mcp" }

func (r *CLIRunner) Version(ctx context.Context) string {
	if strings.TrimSpace(r.Binary) == "" {
		return ""
	}
	for _, args := range [][]string{{"--version"}, {"version"}} {
		cmd := exec.CommandContext(ctx, r.Binary, args...)
		cmd.Env = r.env()
		out, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out))
		}
	}
	return "unknown"
}

func (r *CLIRunner) Call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	if strings.TrimSpace(r.Binary) == "" {
		return nil, fmt.Errorf("codebase-memory binary is required")
	}
	payload := "{}"
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		payload = string(b)
	}
	cmd := exec.CommandContext(ctx, r.Binary, "cli", tool, payload)
	cmd.Env = r.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", tool, err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("%s returned empty stdout", tool)
	}
	return stdout.Bytes(), nil
}

func (r *CLIRunner) env() []string {
	env := append([]string(nil), os.Environ()...)
	env = append(env, "CBM_LOG_LEVEL=error", "CBM_WORKERS=2")
	if r.CacheDir != "" {
		env = append(env, "CBM_CACHE_DIR="+r.CacheDir)
	}
	return env
}
