package presentation

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"keydeck.local/feasibilitylab/internal/corehost"
)

var ErrDisconnected = errors.New("presentation shell is disconnected")

type CoreClient interface {
	Identity() corehost.Identity
	Projection(context.Context, uint64, int) (corehost.ProjectionSnapshot, error)
	CreateTask(context.Context, string, corehost.TaskCreateRequest) (corehost.TaskCreateResult, error)
}

type Connector func(context.Context) (CoreClient, error)

type Snapshot struct {
	Connected bool                     `json:"connected"`
	Identity  corehost.Identity        `json:"identity"`
	Status    corehost.Status          `json:"status"`
	Tasks     []corehost.TaskSummary   `json:"tasks"`
	Timeline  []corehost.TimelineEvent `json:"timeline"`
	After     uint64                   `json:"after"`
	NextAfter uint64                   `json:"next_after"`
}

type Shell struct {
	mu        sync.Mutex
	connector Connector
	client    CoreClient
}

func New(layout corehost.Layout, expectedBuildID, expectedAPIVersion string, httpClient *http.Client) *Shell {
	return NewWithConnector(func(ctx context.Context) (CoreClient, error) {
		return corehost.Connect(ctx, layout, expectedBuildID, expectedAPIVersion, httpClient)
	})
}

func NewWithConnector(connector Connector) *Shell {
	return &Shell{connector: connector}
}

func (s *Shell) Connect(ctx context.Context) error {
	if s == nil || s.connector == nil {
		return ErrDisconnected
	}
	s.mu.Lock()
	s.client = nil
	s.mu.Unlock()
	client, err := s.connector(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.client = client
	s.mu.Unlock()
	return nil
}

func (s *Shell) Disconnect() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.client = nil
	s.mu.Unlock()
}

func (s *Shell) Identity() (corehost.Identity, error) {
	client, err := s.current()
	if err != nil {
		return corehost.Identity{}, err
	}
	return client.Identity(), nil
}

func (s *Shell) Refresh(ctx context.Context, after uint64, limit int) (Snapshot, error) {
	client, err := s.current()
	if err != nil {
		return Snapshot{}, err
	}
	projection, err := client.Projection(ctx, after, limit)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Connected: true,
		Identity:  projection.Identity,
		Status:    projection.Status,
		Tasks:     projection.Tasks,
		Timeline:  projection.Timeline,
		After:     projection.After,
		NextAfter: projection.NextAfter,
	}, nil
}

func (s *Shell) CreateTask(ctx context.Context, idempotencyKey string, req corehost.TaskCreateRequest) (corehost.TaskCreateResult, error) {
	client, err := s.current()
	if err != nil {
		return corehost.TaskCreateResult{}, err
	}
	return client.CreateTask(ctx, idempotencyKey, req)
}

func (s *Shell) current() (CoreClient, error) {
	if s == nil {
		return nil, ErrDisconnected
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil, ErrDisconnected
	}
	return s.client, nil
}
