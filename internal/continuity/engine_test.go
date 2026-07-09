package continuity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

func newStreamingEngine(t *testing.T, streamPlan *fakeprovider.StreamPlan) (*Engine, *events.Recorder, func()) {
	t.Helper()
	plan := fakeprovider.NewPlan()
	srv := httptest.NewServer(fakeprovider.Mux(plan, streamPlan))
	recorder := &events.Recorder{}
	engine := New([]pool.Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
	}, &providerhttp.StreamClient{BaseURL: srv.URL}, recorder)
	return engine, recorder, srv.Close
}

func TestExplicitMidStreamExhaustionContinuesAtSentenceBoundary(t *testing.T) {
	streamPlan := fakeprovider.NewStreamPlan()
	streamPlan.ByKey["secret-1"] = []fakeprovider.StreamOutcome{{
		Chunks: []string{
			"I found two problems. ",
			"The first problem is in the gateway because ",
		},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	streamPlan.ByKey["secret-2"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"The first problem is in the gateway because the lock releases before state is saved."},
		Terminal: fakeprovider.StreamDone,
	}}

	engine, _, closeFn := newStreamingEngine(t, streamPlan)
	defer closeFn()
	original := []byte(`{"prompt":"inspect the gateway"}`)
	result, err := engine.Execute(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	want := "I found two problems. The first problem is in the gateway because the lock releases before state is saved."
	if result.Text != want {
		t.Fatalf("unexpected continued text:\nwant: %q\n got: %q", want, result.Text)
	}
	if result.FinalKey != "key-2" || result.Switches != 1 {
		t.Fatalf("unexpected result metadata: %#v", result)
	}

	requests := streamPlan.SnapshotRequests()
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	var second Request
	if err := json.Unmarshal(requests[1].Body, &second); err != nil {
		t.Fatal(err)
	}
	if second.Continuation == nil {
		t.Fatal("second request missing continuation package")
	}
	if second.Continuation.ConfirmedOutput != "I found two problems. " {
		t.Fatalf("wrong confirmed output: %q", second.Continuation.ConfirmedOutput)
	}
	if second.Continuation.UnstableFragment != "The first problem is in the gateway because " {
		t.Fatalf("wrong unstable fragment: %q", second.Continuation.UnstableFragment)
	}
	if !strings.Contains(string(second.Continuation.OriginalRequest), "inspect the gateway") {
		t.Fatal("continuation package lost original request")
	}
}

func TestAbruptPartialStreamDoesNotRotate(t *testing.T) {
	streamPlan := fakeprovider.NewStreamPlan()
	streamPlan.ByKey["secret-1"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"One safe sentence. Then an unfinished thought"},
		Terminal: fakeprovider.StreamAbruptEOF,
	}}
	streamPlan.ByKey["secret-2"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"should never run"},
		Terminal: fakeprovider.StreamDone,
	}}

	engine, _, closeFn := newStreamingEngine(t, streamPlan)
	defer closeFn()
	result, err := engine.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if !errors.Is(err, ErrStreamAmbiguous) {
		t.Fatalf("expected ambiguous stream error, got %v", err)
	}
	if result.Text != "One safe sentence. " {
		t.Fatalf("only confirmed sentence should be visible, got %q", result.Text)
	}
	requests := streamPlan.SnapshotRequests()
	if len(requests) != 1 {
		t.Fatalf("ambiguous stream rotated/replayed unexpectedly: requests=%d", len(requests))
	}
}

func TestProviderBusyMidStreamPreservesBackupKey(t *testing.T) {
	streamPlan := fakeprovider.NewStreamPlan()
	streamPlan.ByKey["secret-1"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"One safe sentence. Partial"},
		Terminal: fakeprovider.StreamProviderBusy,
	}}
	streamPlan.ByKey["secret-2"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"should never run"},
		Terminal: fakeprovider.StreamDone,
	}}

	engine, _, closeFn := newStreamingEngine(t, streamPlan)
	defer closeFn()
	result, err := engine.Execute(context.Background(), []byte(`{"prompt":"proof"}`))
	if !errors.Is(err, ErrStreamBusy) {
		t.Fatalf("expected provider busy, got %v", err)
	}
	if result.Text != "One safe sentence. " {
		t.Fatalf("unexpected committed text: %q", result.Text)
	}
	if len(streamPlan.SnapshotRequests()) != 1 {
		t.Fatal("provider-wide busy spent a backup key")
	}
}
