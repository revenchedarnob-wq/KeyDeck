package pool

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/protocol"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

func testGuard(t *testing.T) *costguard.Guard {
	t.Helper()
	// LAB FIXTURE ONLY. These values prove control flow; they are not claimed as
	// production-safe thresholds. Production profiles require provider evidence.
	g, err := costguard.New(costguard.Config{
		LargeWriteMin:    100_000,
		LowReadMax:       5_000,
		ConsecutiveLimit: 2,
		ExtremeWriteMin:  180_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func newPool(t *testing.T, plan *fakeprovider.Plan) (*Pool, *events.Recorder, func()) {
	t.Helper()
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	recorder := &events.Recorder{}
	p := New([]Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
		{ID: "key-3", Secret: "secret-3"},
	}, &providerhttp.Client{BaseURL: srv.URL}, recorder, testGuard(t))
	return p, recorder, srv.Close
}

func TestExplicitExhaustionFallsThroughKeys(t *testing.T) {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-3"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "done"}}

	p, _, closeFn := newPool(t, plan)
	defer closeFn()
	result, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.KeyID != "key-3" {
		t.Fatalf("expected key-3, got %s", result.KeyID)
	}
	states := p.Snapshot()
	if states[0].State != KeyExhausted || states[1].State != KeyExhausted || states[2].State != KeyActive {
		t.Fatalf("unexpected states: %#v", states)
	}
}

func TestProviderBusyPreservesBackupKeys(t *testing.T) {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.ProviderBusy}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}

	p, _, closeFn := newPool(t, plan)
	defer closeFn()
	_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if !errors.Is(err, ErrProviderBusy) {
		t.Fatalf("expected provider busy, got %v", err)
	}
	if got := plan.Calls("secret-2"); got != 0 {
		t.Fatalf("backup key was spent during provider-wide busy: calls=%d", got)
	}
}

func TestAmbiguousFailureDoesNotReplayOrRotate(t *testing.T) {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Ambiguous502}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}

	p, _, closeFn := newPool(t, plan)
	defer closeFn()
	_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if !errors.Is(err, ErrAmbiguousFailure) {
		t.Fatalf("expected ambiguous failure, got %v", err)
	}
	if got := plan.Calls("secret-1"); got != 1 {
		t.Fatalf("ambiguous request replayed: calls=%d", got)
	}
	if got := plan.Calls("secret-2"); got != 0 {
		t.Fatalf("rotated to backup after ambiguous failure: calls=%d", got)
	}
}

func TestUnclassified429DoesNotRotate(t *testing.T) {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Ambiguous429}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success}}

	p, _, closeFn := newPool(t, plan)
	defer closeFn()
	_, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if !errors.Is(err, ErrAmbiguousFailure) {
		t.Fatalf("expected ambiguous failure, got %v", err)
	}
	if got := plan.Calls("secret-2"); got != 0 {
		t.Fatalf("unknown 429 burned backup key: calls=%d", got)
	}
}

func TestCostGuardBlocksBeforeBackupKeyCanBurn(t *testing.T) {
	plan := fakeprovider.NewPlan()
	miss := protocol.Usage{CacheCreationInputTokens: 120_000, CacheReadInputTokens: 0}
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{
		{Behavior: fakeprovider.Success, Output: "first", Usage: miss},
		{Behavior: fakeprovider.Success, Output: "second", Usage: miss},
	}

	p, _, closeFn := newPool(t, plan)
	defer closeFn()
	if _, err := p.Execute(context.Background(), []byte(`{"prompt":"one"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Execute(context.Background(), []byte(`{"prompt":"two"}`)); err != nil {
		t.Fatal(err)
	}
	_, err := p.Execute(context.Background(), []byte(`{"prompt":"three"}`))
	if !errors.Is(err, ErrCostSafetyBlocked) {
		t.Fatalf("expected cost safety block, got %v", err)
	}
	if got := plan.Calls("secret-2"); got != 0 {
		t.Fatalf("cost anomaly burned backup key: calls=%d", got)
	}
}

func TestExplicitInvalidKeyMayFailOver(t *testing.T) {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.InvalidKey}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "done"}}

	p, _, closeFn := newPool(t, plan)
	defer closeFn()
	result, err := p.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result.KeyID != "key-2" {
		t.Fatalf("expected key-2, got %s", result.KeyID)
	}
}
