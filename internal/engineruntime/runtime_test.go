package engineruntime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"keydeck.local/feasibilitylab/internal/recovery"
	"keydeck.local/feasibilitylab/internal/session"
	"keydeck.local/feasibilitylab/internal/timeline"
)

type fakeAdapter struct {
	id            string
	capabilities  []Capability
	health        Health
	starts        int
	continues     int
	resumes       int
	cancels       int
	startOutcome  Outcome
	resumeOutcome Outcome
	startErr      error
}

func (f *fakeAdapter) ID() string { return f.id }
func (f *fakeAdapter) Capabilities(context.Context) ([]Capability, error) {
	return append([]Capability(nil), f.capabilities...), nil
}
func (f *fakeAdapter) Health(context.Context) (Health, error) { return f.health, nil }
func (f *fakeAdapter) Start(context.Context, Request) (Outcome, error) {
	f.starts++
	return f.startOutcome, f.startErr
}
func (f *fakeAdapter) Continue(context.Context, Request) (Outcome, error) {
	f.continues++
	return f.startOutcome, f.startErr
}
func (f *fakeAdapter) Resume(_ context.Context, req Request) (Outcome, error) {
	f.resumes++
	if req.Binding == nil || req.Binding.ExternalHandle == "" {
		return Outcome{}, errors.New("missing binding")
	}
	return f.resumeOutcome, nil
}
func (f *fakeAdapter) Cancel(context.Context, Binding) error {
	f.cancels++
	return nil
}

func openRuntime(t *testing.T, dir string) (*Runtime, *Store, *recovery.EngineStore) {
	t.Helper()
	store, err := Open(filepath.Join(dir, "runtime.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	engines, err := recovery.OpenEngineStore(filepath.Join(dir, "engines.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	activity, err := timeline.Open(filepath.Join(dir, "timeline.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(store, engines, activity)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, store, engines
}

func request(id, engine string, required ...Capability) Request {
	return Request{
		ExecutionID:          id,
		TaskID:               "task-1",
		SessionID:            "session-1",
		EngineID:             engine,
		Prompt:               "continue",
		Passport:             session.Passport{SessionID: "session-1", ToEngine: engine},
		RequiredCapabilities: required,
	}
}

func TestCapabilityAndHealthChecksBlockBeforeInvocation(t *testing.T) {
	t.Run("missing capability", func(t *testing.T) {
		runtime, _, _ := openRuntime(t, t.TempDir())
		adapter := &fakeAdapter{id: "engine-a", capabilities: []Capability{CapabilityText}, health: Health{State: HealthHealthy}}
		result, err := runtime.Invoke(context.Background(), adapter, OperationStart, request("exec-cap", adapter.id, CapabilityResume))
		if err != nil {
			t.Fatal(err)
		}
		if result.Execution.Disposition != DispositionFailed || result.AdapterInvoked || adapter.starts != 0 {
			t.Fatalf("expected pre-invocation capability block, got %+v starts=%d", result, adapter.starts)
		}
	})

	t.Run("unhealthy", func(t *testing.T) {
		runtime, _, _ := openRuntime(t, t.TempDir())
		adapter := &fakeAdapter{id: "engine-b", capabilities: []Capability{CapabilityText}, health: Health{State: HealthUnhealthy, Detail: "offline"}}
		result, err := runtime.Invoke(context.Background(), adapter, OperationStart, request("exec-health", adapter.id, CapabilityText))
		if err != nil {
			t.Fatal(err)
		}
		if result.Execution.Disposition != DispositionFailed || result.AdapterInvoked || adapter.starts != 0 {
			t.Fatalf("expected pre-invocation health block, got %+v starts=%d", result, adapter.starts)
		}
	})
}

func TestFailedExecutionSurvivesRestartWithoutReplay(t *testing.T) {
	dir := t.TempDir()
	runtime, _, _ := openRuntime(t, dir)
	first := &fakeAdapter{id: "engine-fail", capabilities: []Capability{CapabilityText}, health: Health{State: HealthHealthy}, startErr: errors.New("provider failure")}
	result, err := runtime.Invoke(context.Background(), first, OperationStart, request("exec-fail", first.id, CapabilityText))
	if err != nil {
		t.Fatal(err)
	}
	if result.Execution.Disposition != DispositionFailed || first.starts != 1 {
		t.Fatalf("unexpected first result: %+v", result)
	}

	reopened, _, _ := openRuntime(t, dir)
	second := &fakeAdapter{id: first.id, capabilities: first.capabilities, health: first.health}
	replay, err := reopened.Invoke(context.Background(), second, OperationStart, request("exec-fail", second.id, CapabilityText))
	if err != nil {
		t.Fatal(err)
	}
	if replay.Execution.Disposition != DispositionFailed || replay.AdapterInvoked || second.starts != 0 {
		t.Fatalf("failed execution replayed after restart: %+v starts=%d", replay, second.starts)
	}
}

func TestDurableBindingResumeAndCancellationSurviveRestart(t *testing.T) {
	t.Run("resume", func(t *testing.T) {
		dir := t.TempDir()
		runtime, _, _ := openRuntime(t, dir)
		adapter := &fakeAdapter{
			id:            "engine-resume",
			capabilities:  []Capability{CapabilityText, CapabilityPersistentSession, CapabilityResume, CapabilityCancel},
			health:        Health{State: HealthHealthy},
			startOutcome:  Outcome{Disposition: DispositionResumeRequired, ExternalHandle: "thread-42", Resumable: true, Detail: "thread persisted"},
			resumeOutcome: Outcome{Disposition: DispositionCompleted, ExternalHandle: "thread-42", Resumable: true, Result: session.EngineResult{Text: "done"}},
		}
		first, err := runtime.Invoke(context.Background(), adapter, OperationStart, request("exec-resume", adapter.id, CapabilityResume))
		if err != nil {
			t.Fatal(err)
		}
		if first.Execution.Disposition != DispositionResumeRequired || first.Binding == nil || first.Binding.ExternalHandle != "thread-42" {
			t.Fatalf("unexpected start: %+v", first)
		}

		reopened, store, _ := openRuntime(t, dir)
		if binding, ok := store.BindingForExecution("exec-resume"); !ok || binding.ExternalHandle != "thread-42" {
			t.Fatalf("binding did not survive restart: %+v %v", binding, ok)
		}
		resumed, err := reopened.Invoke(context.Background(), adapter, OperationResume, request("exec-resume", adapter.id, CapabilityResume))
		if err != nil {
			t.Fatal(err)
		}
		if resumed.Execution.Disposition != DispositionCompleted || adapter.resumes != 1 {
			t.Fatalf("resume failed: %+v resumes=%d", resumed, adapter.resumes)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		dir := t.TempDir()
		runtime, _, _ := openRuntime(t, dir)
		adapter := &fakeAdapter{
			id:           "engine-cancel",
			capabilities: []Capability{CapabilityText, CapabilityResume, CapabilityCancel},
			health:       Health{State: HealthHealthy},
			startOutcome: Outcome{Disposition: DispositionResumeRequired, ExternalHandle: "job-9", Resumable: true},
		}
		if _, err := runtime.Invoke(context.Background(), adapter, OperationStart, request("exec-cancel", adapter.id, CapabilityResume)); err != nil {
			t.Fatal(err)
		}
		cancelled, err := runtime.Cancel(context.Background(), adapter, "exec-cancel")
		if err != nil {
			t.Fatal(err)
		}
		if cancelled.Execution.Disposition != DispositionCancelled || adapter.cancels != 1 {
			t.Fatalf("cancel failed: %+v", cancelled)
		}

		reopened, _, _ := openRuntime(t, dir)
		after, err := reopened.Invoke(context.Background(), adapter, OperationResume, request("exec-cancel", adapter.id, CapabilityResume))
		if err != nil {
			t.Fatal(err)
		}
		if after.Execution.Disposition != DispositionCancelled || after.AdapterInvoked || adapter.resumes != 0 {
			t.Fatalf("cancelled execution did not survive restart: %+v", after)
		}
	})
}

func TestRunningExecutionRestartsConservatively(t *testing.T) {
	t.Run("durable result reconciles without replay", func(t *testing.T) {
		dir := t.TempDir()
		runtime, store, engines := openRuntime(t, dir)
		_, _, err := store.BeginOnce(Execution{ExecutionID: "exec-result", TaskID: "task-1", SessionID: "session-1", EngineID: "engine-r", Operation: OperationStart})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := engines.StartOnce(recovery.Execution{ExecutionID: "exec-result", TaskID: "task-1", SessionID: "session-1", Engine: "engine-r"}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := engines.CompleteResultOnce(recovery.Result{ResultID: deterministicResultID("exec-result"), ExecutionID: "exec-result", TaskID: "task-1", SessionID: "session-1", Engine: "engine-r", Output: session.EngineResult{Text: "persisted"}}); err != nil {
			t.Fatal(err)
		}

		reopened, _, _ := openRuntime(t, dir)
		adapter := &fakeAdapter{id: "engine-r", capabilities: []Capability{CapabilityText}, health: Health{State: HealthHealthy}}
		result, err := reopened.Invoke(context.Background(), adapter, OperationStart, request("exec-result", adapter.id, CapabilityText))
		if err != nil {
			t.Fatal(err)
		}
		if result.Execution.Disposition != DispositionCompleted || result.AdapterInvoked || adapter.starts != 0 {
			t.Fatalf("durable result was replayed: %+v starts=%d", result, adapter.starts)
		}
		_ = runtime
	})

	t.Run("no durable path becomes input required", func(t *testing.T) {
		dir := t.TempDir()
		_, store, _ := openRuntime(t, dir)
		if _, _, err := store.BeginOnce(Execution{ExecutionID: "exec-unknown", TaskID: "task-1", SessionID: "session-1", EngineID: "engine-u", Operation: OperationStart}); err != nil {
			t.Fatal(err)
		}
		reopened, _, _ := openRuntime(t, dir)
		adapter := &fakeAdapter{id: "engine-u", capabilities: []Capability{CapabilityText}, health: Health{State: HealthHealthy}}
		result, err := reopened.Invoke(context.Background(), adapter, OperationStart, request("exec-unknown", adapter.id, CapabilityText))
		if err != nil {
			t.Fatal(err)
		}
		if result.Execution.Disposition != DispositionInputRequired || result.AdapterInvoked || adapter.starts != 0 {
			t.Fatalf("interrupted execution was replayed: %+v starts=%d", result, adapter.starts)
		}
	})
}

type fakeStatefulSessionEngine struct {
	name    string
	binding session.EngineBinding
	runs    int
}

func (f *fakeStatefulSessionEngine) Name() string { return f.name }
func (f *fakeStatefulSessionEngine) Run(context.Context, session.Passport, string) (session.EngineResult, error) {
	f.runs++
	if f.binding.ExternalThreadID == "" {
		f.binding = session.EngineBinding{Engine: f.name, ExternalThreadID: "thread-session-adapter"}
	}
	return session.EngineResult{Text: "wrapped result"}, nil
}
func (f *fakeStatefulSessionEngine) RestoreBinding(binding session.EngineBinding) error {
	f.binding = binding
	return nil
}
func (f *fakeStatefulSessionEngine) CurrentBinding() session.EngineBinding { return f.binding }

func TestSessionEngineAdapterPreservesOfficialEngineBinding(t *testing.T) {
	engine := &fakeStatefulSessionEngine{name: "codex-like"}
	adapter := &SessionEngineAdapter{
		Engine:               engine,
		DeclaredCapabilities: []Capability{CapabilityText, CapabilityPersistentSession, CapabilityResume},
	}
	start, err := adapter.Start(context.Background(), request("exec-wrap", engine.name, CapabilityText))
	if err != nil {
		t.Fatal(err)
	}
	if start.Disposition != DispositionCompleted || start.ExternalHandle != "thread-session-adapter" || !start.Resumable {
		t.Fatalf("unexpected wrapped start: %+v", start)
	}

	resumeReq := request("exec-wrap", engine.name, CapabilityResume)
	resumeReq.Binding = &Binding{ExternalHandle: "thread-restored"}
	resumed, err := adapter.Resume(context.Background(), resumeReq)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Disposition != DispositionCompleted || engine.binding.ExternalThreadID != "thread-restored" || engine.runs != 2 {
		t.Fatalf("wrapped resume did not preserve binding: outcome=%+v binding=%+v runs=%d", resumed, engine.binding, engine.runs)
	}
}
