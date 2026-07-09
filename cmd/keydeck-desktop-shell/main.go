package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"keydeck.local/feasibilitylab/internal/corehost"
	"keydeck.local/feasibilitylab/internal/presentation"
)

const defaultExpectedBuildID = "keydeck-v0.35.0-reconstructed"

type output struct {
	CreateResult *corehost.TaskCreateResult `json:"create_result,omitempty"`
	Snapshot     presentation.Snapshot      `json:"snapshot"`
}

func main() {
	dataDir := flag.String("data-dir", defaultDataDir(), "KeyDeck local data directory")
	expectedBuild := flag.String("expected-build", defaultExpectedBuildID, "expected keydeck-core build identity")
	after := flag.Uint64("after", 0, "timeline sequence cursor")
	limit := flag.Int("limit", 100, "timeline page size")
	createPath := flag.String("create-task-json", "", "optional path to a TaskCreateRequest JSON file")
	idempotencyKey := flag.String("idempotency-key", "", "required with --create-task-json")
	flag.Parse()

	layout, err := corehost.BuildLayout(*dataDir)
	if err != nil {
		fatal(err)
	}
	shell := presentation.New(layout, *expectedBuild, corehost.DefaultAPIVersion, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shell.Connect(ctx); err != nil {
		fatal(err)
	}

	var created *corehost.TaskCreateResult
	if *createPath != "" {
		if *idempotencyKey == "" {
			fatal(fmt.Errorf("--idempotency-key is required with --create-task-json"))
		}
		req, err := readTaskRequest(*createPath)
		if err != nil {
			fatal(err)
		}
		result, err := shell.CreateTask(ctx, *idempotencyKey, req)
		if err != nil {
			fatal(err)
		}
		created = &result
	}

	snapshot, err := shell.Refresh(ctx, *after, *limit)
	if err != nil {
		fatal(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output{CreateResult: created, Snapshot: snapshot}); err != nil {
		fatal(err)
	}
}

func readTaskRequest(path string) (corehost.TaskCreateRequest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return corehost.TaskCreateRequest{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var req corehost.TaskCreateRequest
	if err := dec.Decode(&req); err != nil {
		return corehost.TaskCreateRequest{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return corehost.TaskCreateRequest{}, fmt.Errorf("multiple JSON values")
		}
		return corehost.TaskCreateRequest{}, err
	}
	return req, nil
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "KeyDeck")
	}
	return filepath.Join(".", ".keydeck")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
