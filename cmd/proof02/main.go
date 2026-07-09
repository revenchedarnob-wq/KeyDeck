package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"

	"keydeck.local/feasibilitylab/internal/continuity"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

func main() {
	streamPlan := fakeprovider.NewStreamPlan()
	streamPlan.ByKey["secret-1"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"I found two problems. ", "The first problem is in the gateway because "},
		Terminal: fakeprovider.StreamKeyExhausted,
	}}
	streamPlan.ByKey["secret-2"] = []fakeprovider.StreamOutcome{{
		Chunks:   []string{"The first problem is in the gateway because the lock releases before state is saved."},
		Terminal: fakeprovider.StreamDone,
	}}

	srv := httptest.NewServer(fakeprovider.Mux(fakeprovider.NewPlan(), streamPlan))
	defer srv.Close()
	recorder := &events.Recorder{}
	engine := continuity.New([]pool.Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
	}, &providerhttp.StreamClient{BaseURL: srv.URL}, recorder)

	result, err := engine.Execute(context.Background(), []byte(`{"prompt":"inspect the gateway"}`))
	report := map[string]any{
		"proof":       "0.2-mid-answer-same-model-key-continuation",
		"passed":      err == nil && result.FinalKey == "key-2" && result.Switches == 1,
		"result":      result,
		"error":       errorString(err),
		"eventStream": recorder.Snapshot(),
		"requests":    streamPlan.SnapshotRequests(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if err != nil {
		os.Exit(1)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
