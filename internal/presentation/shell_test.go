package presentation

import (
	"context"
	"errors"
	"sync"
	"testing"

	"keydeck.local/feasibilitylab/internal/corehost"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
)

type fakeClient struct {
	mu       sync.Mutex
	identity corehost.Identity
	status   corehost.Status
	tasks    []corehost.TaskSummary
	events   []timeline.Event
	create   corehost.TaskCreateResult
	err      error
}

func (f *fakeClient) Identity() corehost.Identity { return f.identity }
func (f *fakeClient) Projection(_ context.Context, after uint64, _ int) (corehost.ProjectionSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := after
	if len(f.events) > 0 {
		next = f.events[len(f.events)-1].Sequence
	}
	return corehost.ProjectionSnapshot{Identity: f.identity, Status: f.status, Tasks: append([]corehost.TaskSummary(nil), f.tasks...), Timeline: append([]corehost.TimelineEvent(nil), f.events...), After: after, NextAfter: next}, f.err
}
func (f *fakeClient) CreateTask(context.Context, string, corehost.TaskCreateRequest) (corehost.TaskCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.create, f.err
}

func TestShellRequiresConnection(t *testing.T) {
	s := NewWithConnector(nil)
	if _, err := s.Refresh(context.Background(), 0, 10); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("expected disconnected error, got %v", err)
	}
}

func TestShellRefreshProjectsOnlyClientData(t *testing.T) {
	client := &fakeClient{
		identity: corehost.Identity{Product: "KeyDeck", BuildID: "b", APIVersion: "v1", InstallID: "i", InstanceID: "n"},
		status:   corehost.Status{Product: "KeyDeck", BuildID: "b", APIVersion: "v1", TaskCount: 1, TimelineEvents: 1},
		tasks:    []corehost.TaskSummary{{TaskID: "t", SessionID: "s", Status: tasks.StatusWorking}},
		events:   []timeline.Event{{Sequence: 7, EventID: "e"}},
	}
	s := NewWithConnector(func(context.Context) (CoreClient, error) { return client, nil })
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Refresh(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Connected || snap.NextAfter != 7 || len(snap.Tasks) != 1 || len(snap.Timeline) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestConcurrentRefreshesAreReadOnly(t *testing.T) {
	client := &fakeClient{identity: corehost.Identity{Product: "KeyDeck"}, status: corehost.Status{Product: "KeyDeck"}}
	s := NewWithConnector(func(context.Context) (CoreClient, error) { return client, nil })
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Refresh(context.Background(), 0, 100); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestFailedReconnectClearsOldClient(t *testing.T) {
	client := &fakeClient{identity: corehost.Identity{Product: "KeyDeck"}, status: corehost.Status{Product: "KeyDeck"}}
	calls := 0
	s := NewWithConnector(func(context.Context) (CoreClient, error) {
		calls++
		if calls == 1 {
			return client, nil
		}
		return nil, corehost.ErrIdentityMismatch
	})
	if err := s.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Connect(context.Background()); !errors.Is(err, corehost.ErrIdentityMismatch) {
		t.Fatalf("expected reconnect failure, got %v", err)
	}
	if _, err := s.Identity(); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("expected old client cleared, got %v", err)
	}
}
