package apiengine

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"keydeck.local/feasibilitylab/internal/costguard"
	"keydeck.local/feasibilitylab/internal/events"
	"keydeck.local/feasibilitylab/internal/fakeprovider"
	"keydeck.local/feasibilitylab/internal/pool"
	"keydeck.local/feasibilitylab/internal/providerhttp"
	"keydeck.local/feasibilitylab/internal/session"
)

func TestAPIEngineCompilesProjectEvidence(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "proof.txt"), []byte("PHASE_1\nPHASE_2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := fakeprovider.NewPlan()
	plan.ByKey["secret"] = []fakeprovider.Outcome{{Behavior: fakeprovider.Success, Output: "reviewed"}}
	srv := httptest.NewServer(fakeprovider.Handler(plan))
	defer srv.Close()
	guard, _ := costguard.New(costguard.Config{LargeWriteMin: 100000, LowReadMax: 5000, ConsecutiveLimit: 2, ExtremeWriteMin: 180000})
	p := pool.New([]pool.Key{{ID: "key", Secret: "secret"}}, &providerhttp.Client{BaseURL: srv.URL}, &events.Recorder{}, guard)
	engine := &Engine{Pool: p, EvidenceFiles: []string{"proof.txt"}}
	passport := session.Passport{SessionID: "s1", ProjectRoot: project, Goal: "test", ToEngine: "api-pool"}

	result, err := engine.Run(context.Background(), passport, "Review")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "reviewed" {
		t.Fatalf("unexpected result: %q", result.Text)
	}
	body := string(plan.LastBody("secret"))
	if !strings.Contains(body, "PHASE_1") || !strings.Contains(body, "PHASE_2") {
		t.Fatalf("request lost project evidence: %s", body)
	}
}
