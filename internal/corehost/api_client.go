package corehost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"keydeck.local/feasibilitylab/internal/tasks"
)

const DefaultMaxResponseBytes = int64(1 << 20)

type APIClient struct {
	layout        Layout
	expectedBuild string
	expectedAPI   string
	runtime       RuntimeInfo
	identity      Identity
	token         string
	httpClient    *http.Client
	maxResponse   int64
}

func Connect(ctx context.Context, layout Layout, expectedBuildID, expectedAPIVersion string, base *http.Client) (*APIClient, error) {
	expectedBuildID = strings.TrimSpace(expectedBuildID)
	expectedAPIVersion = strings.TrimSpace(expectedAPIVersion)
	if expectedBuildID == "" || expectedAPIVersion == "" {
		return nil, fmt.Errorf("%w: expected build and API identity required", ErrInvalidConfig)
	}
	info, err := ReadRuntime(layout.RuntimePath)
	if err != nil {
		return nil, err
	}
	if info.BuildID != expectedBuildID || info.APIVersion != expectedAPIVersion {
		return nil, ErrIdentityMismatch
	}
	credential, err := ReadCredential(layout.CredentialPath)
	if err != nil {
		return nil, err
	}
	if credential.InstallID != info.InstallID || strings.TrimSpace(credential.Token) == "" {
		return nil, ErrIdentityMismatch
	}
	httpClient, err := hardenedLoopbackClient(base)
	if err != nil {
		return nil, err
	}
	client := &APIClient{
		layout:        layout,
		expectedBuild: expectedBuildID,
		expectedAPI:   expectedAPIVersion,
		runtime:       info,
		token:         credential.Token,
		httpClient:    httpClient,
		maxResponse:   DefaultMaxResponseBytes,
	}
	var identity Identity
	if err := client.doJSON(ctx, http.MethodGet, "/v1/identity", nil, "", &identity); err != nil {
		return nil, err
	}
	if identity.Product != "KeyDeck" || identity.BuildID != expectedBuildID || identity.APIVersion != expectedAPIVersion || identity.InstallID != credential.InstallID || identity.InstanceID != info.InstanceID {
		return nil, ErrIdentityMismatch
	}
	if err := client.verifyRuntimeStillCurrent(); err != nil {
		return nil, err
	}
	client.identity = identity
	return client, nil
}

func (c *APIClient) Identity() Identity {
	return c.identity
}

func (c *APIClient) Projection(ctx context.Context, after uint64, limit int) (ProjectionSnapshot, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	path := "/v1/snapshot?after=" + strconv.FormatUint(after, 10) + "&limit=" + strconv.Itoa(limit)
	var out ProjectionSnapshot
	if err := c.doJSON(ctx, http.MethodGet, path, nil, "", &out); err != nil {
		return ProjectionSnapshot{}, err
	}
	if out.Identity != c.identity || out.Status.Product != "KeyDeck" || out.Status.BuildID != c.expectedBuild || out.Status.APIVersion != c.expectedAPI || out.After != after {
		return ProjectionSnapshot{}, ErrIdentityMismatch
	}
	if out.Status.TaskCount != len(out.Tasks) || out.Status.TimelineEvents < len(out.Timeline) || len(out.Timeline) > limit {
		return ProjectionSnapshot{}, ErrIdentityMismatch
	}
	seenTasks := make(map[string]bool, len(out.Tasks))
	for _, task := range out.Tasks {
		if task.TaskID == "" || seenTasks[task.TaskID] {
			return ProjectionSnapshot{}, ErrIdentityMismatch
		}
		seenTasks[task.TaskID] = true
	}
	last := after
	for _, event := range out.Timeline {
		if event.Sequence <= last {
			return ProjectionSnapshot{}, ErrIdentityMismatch
		}
		last = event.Sequence
	}
	if out.NextAfter != last {
		return ProjectionSnapshot{}, ErrIdentityMismatch
	}
	return out, nil
}

func (c *APIClient) Status(ctx context.Context) (Status, error) {
	var out Status
	err := c.doJSON(ctx, http.MethodGet, "/v1/status", nil, "", &out)
	if err == nil && (out.Product != "KeyDeck" || out.BuildID != c.expectedBuild || out.APIVersion != c.expectedAPI) {
		return Status{}, ErrIdentityMismatch
	}
	return out, err
}

func (c *APIClient) ListTasks(ctx context.Context) ([]TaskSummary, error) {
	var out struct {
		Tasks []TaskSummary `json:"tasks"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tasks", nil, "", &out); err != nil {
		return nil, err
	}
	return out.Tasks, nil
}

func (c *APIClient) GetTask(ctx context.Context, taskID string) (tasks.State, error) {
	if !safeID.MatchString(taskID) {
		return tasks.State{}, ErrInvalidConfig
	}
	var out tasks.State
	if err := c.doJSON(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(taskID), nil, "", &out); err != nil {
		return tasks.State{}, err
	}
	return out, nil
}

func (c *APIClient) Timeline(ctx context.Context, after uint64, limit int) ([]TimelineEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	path := "/v1/timeline?after=" + strconv.FormatUint(after, 10) + "&limit=" + strconv.Itoa(limit)
	var out struct {
		Events []TimelineEvent `json:"events"`
	}
	if err := c.doJSON(ctx, http.MethodGet, path, nil, "", &out); err != nil {
		return nil, err
	}
	return out.Events, nil
}

func (c *APIClient) CreateTask(ctx context.Context, key string, req TaskCreateRequest) (TaskCreateResult, error) {
	key = strings.TrimSpace(key)
	if !safeID.MatchString(key) {
		return TaskCreateResult{}, ErrInvalidConfig
	}
	var out TaskCreateResult
	if err := c.doJSON(ctx, http.MethodPost, "/v1/tasks", req, key, &out); err != nil {
		return TaskCreateResult{}, err
	}
	return out, nil
}

func (c *APIClient) doJSON(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	if c == nil || c.httpClient == nil || c.token == "" {
		return ErrIdentityMismatch
	}
	if err := c.verifyRuntimeStillCurrent(); err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.runtime.Address+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := validateJSONContentType(resp.Header.Get("Content-Type")); err != nil {
		return err
	}
	limited := io.LimitReader(resp.Body, c.maxResponse+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(raw)) > c.maxResponse {
		return ErrRequestTooLarge
	}
	if err := c.verifyRuntimeStillCurrent(); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mapAPIError(resp.StatusCode, raw)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := ensureJSONEOF(dec); err != nil {
		return err
	}
	return nil
}

func (c *APIClient) verifyRuntimeStillCurrent() error {
	info, err := ReadRuntime(c.layout.RuntimePath)
	if err != nil {
		return err
	}
	if info.InstanceID != c.runtime.InstanceID || info.InstallID != c.runtime.InstallID || info.Address != c.runtime.Address || info.BuildID != c.expectedBuild || info.APIVersion != c.expectedAPI {
		return ErrIdentityMismatch
	}
	return nil
}

func validateJSONContentType(value string) error {
	media, _, err := mime.ParseMediaType(value)
	if err != nil || media != "application/json" {
		return fmt.Errorf("%w: JSON response required", ErrIdentityMismatch)
	}
	return nil
}

func mapAPIError(status int, raw []byte) error {
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &payload)
	switch status {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		if strings.Contains(payload.Error, "idempotency") {
			return ErrIdempotencyConflict
		}
		return ErrTaskConflict
	case http.StatusRequestEntityTooLarge:
		return ErrRequestTooLarge
	default:
		if payload.Error != "" {
			return errors.New(payload.Error)
		}
		return fmt.Errorf("core API returned HTTP %d", status)
	}
}
