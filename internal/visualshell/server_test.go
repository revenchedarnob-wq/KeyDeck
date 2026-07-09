package visualshell

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"keydeck.local/feasibilitylab/internal/presentation"
)

type deterministicReader struct{ b byte }

func (r *deterministicReader) Read(p []byte) (int, error) {
	for i := range p {
		r.b++
		p[i] = r.b
	}
	return len(p), nil
}

type fakeShell struct {
	mu                                        sync.Mutex
	connects, disconnects, refreshes, creates int
	snapshot                                  presentation.Snapshot
	create                                    presentation.TaskCreateResult
	connectErr, refreshErr, createErr         error
}

func (f *fakeShell) Connect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connects++
	return f.connectErr
}
func (f *fakeShell) Disconnect() { f.mu.Lock(); f.disconnects++; f.mu.Unlock() }
func (f *fakeShell) Refresh(context.Context, uint64, int) (presentation.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	return f.snapshot, f.refreshErr
}
func (f *fakeShell) CreateTask(context.Context, string, presentation.TaskCreateRequest) (presentation.TaskCreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	return f.create, f.createErr
}

func TestOpenRejectsNonLoopback(t *testing.T) {
	if _, err := Open(Config{ListenAddress: "0.0.0.0:0", Shell: &fakeShell{}}); err == nil {
		t.Fatal("expected non-loopback rejection")
	}
}

func TestSecretPathHostAndOriginGuards(t *testing.T) {
	fake := &fakeShell{snapshot: presentation.Snapshot{Connected: true}}
	s, err := Open(Config{Shell: fake, Random: &deterministicReader{}})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := s.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	u, _ := url.Parse(launch)
	client := &http.Client{}

	root, err := client.Get(u.Scheme + "://" + u.Host + "/")
	if err != nil {
		t.Fatal(err)
	}
	root.Body.Close()
	if root.StatusCode != http.StatusNotFound {
		t.Fatalf("root status=%d", root.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, launch+"api/snapshot", nil)
	req.Host = "evil.example"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong host status=%d", resp.StatusCode)
	}

	body := `{"idempotency_key":"key","task":{"task_id":"t","session_id":"s","contract":{"goal":"g","checks":[{"id":"c","description":"d","status":"pending"}]}}}`
	req, _ = http.NewRequest(http.MethodPost, launch+"api/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d", resp.StatusCode)
	}
	fake.mu.Lock()
	refreshes, creates := fake.refreshes, fake.creates
	fake.mu.Unlock()
	if refreshes != 0 || creates != 0 {
		t.Fatalf("guarded request reached shell refresh=%d create=%d", refreshes, creates)
	}
}

func TestProjectionSanitizesIdentitySecrets(t *testing.T) {
	fake := &fakeShell{snapshot: presentation.Snapshot{Connected: true, Identity: presentation.Identity{Product: "KeyDeck", BuildID: "build", APIVersion: "v1", InstallID: "install-secret", InstanceID: "instance-secret"}}}
	s, _ := Open(Config{Shell: fake, Random: &deterministicReader{}})
	launch, err := s.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	resp, err := http.Get(launch + "api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("install-secret")) || bytes.Contains(raw, []byte("instance-secret")) {
		t.Fatalf("identity secrets leaked: %s", raw)
	}
	var view ViewSnapshot
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.BuildID != "build" || view.APIVersion != "v1" {
		t.Fatalf("bad view: %+v", view)
	}
}

func TestRendererSessionTokenRotates(t *testing.T) {
	fake := &fakeShell{}
	first, _ := Open(Config{Shell: fake, Random: &deterministicReader{}})
	u1, err := first.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first.Close(context.Background())
	second, _ := Open(Config{Shell: fake, Random: &deterministicReader{b: 99}})
	u2, err := second.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	if u1 == u2 {
		t.Fatal("renderer session URL did not rotate")
	}
	parsed, _ := url.Parse(u1)
	parsed.Host = strings.TrimPrefix(strings.TrimPrefix(u2, "http://"), "https://")
	_ = parsed
}
