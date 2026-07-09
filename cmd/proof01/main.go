package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"

	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
)

func main() {
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret-1"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-2"] = []fakeprovider.Outcome{{Behavior: fakeprovider.KeyExhausted}}
	plan.ByKey["secret-3"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "Proof 0.1 completed"}}

	srv := httptest.NewServer(fakeprovider.Handler(plan))
	defer srv.Close()
	guard, err := costguard.New(costguard.Config{LargeWriteMin: 100_000, LowReadMax: 5_000, ConsecutiveLimit: 2, ExtremeWriteMin: 180_000})
	if err != nil {
		panic(err)
	}
	recorder := &events.Recorder{}
	p := pool.New([]pool.Key{
		{ID: "key-1", Secret: "secret-1"},
		{ID: "key-2", Secret: "secret-2"},
		{ID: "key-3", Secret: "secret-3"},
	}, &providerhttp.Client{BaseURL: srv.URL}, recorder, guard)

	result, runErr := p.Execute(context.Background(), []byte(`{"prompt":"run proof 0.1"}`))
	report := map[string]any{
		"proof":       "0.1-elastic-api-pool",
		"passed":      runErr == nil && result.KeyID == "key-3",
		"finalKey":    result.KeyID,
		"error":       errorString(runErr),
		"keyStates":   p.Snapshot(),
		"eventStream": recorder.Snapshot(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if runErr != nil || result.KeyID != "key-3" {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "PASS: Proof 0.1 elastic pool")
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
