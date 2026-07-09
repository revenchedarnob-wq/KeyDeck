package corehost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type FileBackend struct {
	mu       sync.Mutex
	layout   Layout
	buildID  string
	api      string
	timeline *timeline.Store
	journal  *RequestJournal
}

func OpenFileBackend(layout Layout, buildID, apiVersion string) (*FileBackend, error) {
	if err := os.MkdirAll(layout.TaskDir, 0o700); err != nil {
		return nil, err
	}
	tl, err := timeline.Open(layout.TimelinePath)
	if err != nil {
		return nil, err
	}
	j, err := OpenRequestJournal(layout.RequestJournal)
	if err != nil {
		return nil, err
	}
	return &FileBackend{layout: layout, buildID: buildID, api: apiVersion, timeline: tl, journal: j}, nil
}

func (b *FileBackend) CreateTask(req TaskCreateRequest, key string) (TaskCreateResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := validateTaskCreateRequest(req, key); err != nil {
		return TaskCreateResult{}, err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return TaskCreateResult{}, err
	}
	requestSHA := sha256Hex(raw)
	if record, ok := b.journal.Lookup(key); ok {
		if record.RequestSHA256 != requestSHA {
			return TaskCreateResult{}, ErrIdempotencyConflict
		}
		var result TaskCreateResult
		if err := json.Unmarshal(record.Response, &result); err != nil {
			return TaskCreateResult{}, err
		}
		if !sameTaskRequest(result.State, req) {
			return TaskCreateResult{}, errors.New("request journal response does not match canonical request")
		}
		path, err := b.taskPath(req.TaskID)
		if err != nil {
			return TaskCreateResult{}, err
		}
		store, err := tasks.Open(path)
		if err != nil || !sameTaskRequest(store.State(), req) {
			return TaskCreateResult{}, errors.New("request journal does not match current canonical task")
		}
		result.Reused = true
		return result, nil
	}

	path, err := b.taskPath(req.TaskID)
	if err != nil {
		return TaskCreateResult{}, err
	}
	var state tasks.State
	reconciled := false
	if _, err := os.Stat(path); err == nil {
		store, err := tasks.Open(path)
		if err != nil {
			return TaskCreateResult{}, err
		}
		state = store.State()
		if !sameTaskRequest(state, req) {
			return TaskCreateResult{}, ErrTaskConflict
		}
		reconciled = true
	} else if !os.IsNotExist(err) {
		return TaskCreateResult{}, err
	} else {
		store, err := tasks.Open(path)
		if err != nil {
			return TaskCreateResult{}, err
		}
		manager := &tasks.Manager{Store: store}
		state, err = manager.Create(req.TaskID, req.SessionID, req.Contract)
		if err != nil {
			return TaskCreateResult{}, err
		}
	}

	eventID := "corehost-task-created-" + requestSHA[:20]
	if _, _, err := b.timeline.AppendOnce(timeline.Input{EventID: eventID, TaskID: req.TaskID, SessionID: req.SessionID, Domain: timeline.DomainTask, Kind: "task_created", SourceRef: "corehost:" + key, Summary: "Task created through authenticated KeyDeck core host", DataHash: requestSHA}); err != nil {
		return TaskCreateResult{}, err
	}
	result := TaskCreateResult{State: state, Reconciled: reconciled}
	if _, err := b.journal.Append(key, requestSHA, 201, result); err != nil {
		return TaskCreateResult{}, err
	}
	return result, nil
}

func (b *FileBackend) GetTask(taskID string) (tasks.State, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	path, err := b.taskPath(taskID)
	if err != nil {
		return tasks.State{}, err
	}
	store, err := tasks.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return tasks.State{}, ErrNotFound
		}
		return tasks.State{}, err
	}
	state := store.State()
	if state.TaskID == "" {
		return tasks.State{}, ErrNotFound
	}
	return state, nil
}

func (b *FileBackend) ListTasks() ([]TaskSummary, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entries, err := os.ReadDir(b.layout.TaskDir)
	if err != nil {
		return nil, err
	}
	out := make([]TaskSummary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		store, err := tasks.Open(filepath.Join(b.layout.TaskDir, e.Name()))
		if err != nil {
			return nil, err
		}
		s := store.State()
		if s.TaskID == "" {
			continue
		}
		out = append(out, TaskSummary{TaskID: s.TaskID, SessionID: s.SessionID, Status: s.Status, LastSequence: s.LastSequence, Progress: s.Progress()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out, nil
}

func (b *FileBackend) Timeline(after uint64, limit int) ([]timeline.Event, error) {
	events := b.timeline.Snapshot()
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	out := make([]timeline.Event, 0, limit)
	for _, e := range events {
		if e.Sequence <= after {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (b *FileBackend) Status() (Status, error) {
	tasksList, err := b.ListTasks()
	if err != nil {
		return Status{}, err
	}
	return Status{Product: "KeyDeck", BuildID: b.buildID, APIVersion: b.api, TaskCount: len(tasksList), TimelineEvents: len(b.timeline.Snapshot()), RequestRecords: b.journal.Count()}, nil
}

func (b *FileBackend) taskPath(taskID string) (string, error) {
	if !safeID.MatchString(taskID) {
		return "", ErrInvalidConfig
	}
	return filepath.Join(b.layout.TaskDir, taskID+".jsonl"), nil
}

func validateTaskCreateRequest(req TaskCreateRequest, key string) error {
	if !safeID.MatchString(req.TaskID) || !safeID.MatchString(req.SessionID) || !safeID.MatchString(strings.TrimSpace(key)) {
		return ErrInvalidConfig
	}
	if strings.TrimSpace(req.Contract.Goal) == "" || len(req.Contract.Checks) == 0 {
		return ErrInvalidConfig
	}
	seen := map[string]bool{}
	for _, check := range req.Contract.Checks {
		id := strings.TrimSpace(check.ID)
		if !safeID.MatchString(id) || strings.TrimSpace(check.Description) == "" || seen[id] {
			return ErrInvalidConfig
		}
		if check.Status != "" && check.Status != tasks.CheckPending {
			return ErrInvalidConfig
		}
		if strings.TrimSpace(check.Evidence) != "" || !check.UpdatedAt.IsZero() {
			return ErrInvalidConfig
		}
		seen[id] = true
	}
	return nil
}

func sameTaskRequest(state tasks.State, req TaskCreateRequest) bool {
	if state.TaskID != req.TaskID || state.SessionID != req.SessionID {
		return false
	}
	return canonicalContract(state.Contract) == canonicalContract(req.Contract)
}

func canonicalContract(c tasks.Contract) string {
	clone := c
	clone.Goal = strings.TrimSpace(clone.Goal)
	clone.RequiredOutcomes = normalizeStrings(clone.RequiredOutcomes)
	clone.ForbiddenScope = normalizeStrings(clone.ForbiddenScope)
	for i := range clone.Checks {
		clone.Checks[i].ID = strings.TrimSpace(clone.Checks[i].ID)
		clone.Checks[i].Description = strings.TrimSpace(clone.Checks[i].Description)
		clone.Checks[i].Status = ""
		clone.Checks[i].Evidence = ""
		clone.Checks[i].UpdatedAt = time.Time{}
	}
	raw, _ := json.Marshal(clone)
	return string(raw)
}

func normalizeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func (b *FileBackend) EvidencePaths() []string {
	return []string{b.layout.TimelinePath, b.layout.RequestJournal, b.layout.TaskDir}
}

var _ Backend = (*FileBackend)(nil)
