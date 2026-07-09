package contextbench

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ProofMarker = "KEYDECK_PROOF_08_COMPLETE"
	ResultFile  = "KEYDECK_PROOF_08_RESULT.json"
)

type Result struct {
	Proof      string   `json:"proof"`
	RootCause  string   `json:"root_cause"`
	CallPath   []string `json:"call_path"`
	FilesToFix []string `json:"files_to_fix"`
}

type Acceptance struct {
	Passed            bool     `json:"passed"`
	ProofMarkerOK     bool     `json:"proof_marker_ok"`
	TenantIsolationOK bool     `json:"tenant_isolation_ok"`
	CacheKeyCauseOK   bool     `json:"cache_key_cause_ok"`
	CallPathOK        bool     `json:"call_path_ok"`
	RoutingFileOK     bool     `json:"routing_file_ok"`
	CacheFileOK       bool     `json:"cache_file_ok"`
	MinimalFixSetOK   bool     `json:"minimal_fix_set_ok"`
	SourceUnchangedOK bool     `json:"source_unchanged_ok"`
	UnexpectedChanges []string `json:"unexpected_changes,omitempty"`
	ParsedResult      Result   `json:"parsed_result"`
	ResultReadError   string   `json:"result_read_error,omitempty"`
}

func Objective() string {
	return "A request from tenant B can receive tenant A's fallback policy after the active tenant changes, especially when both requests use the same route. Diagnose the exact root cause, identify the call path that makes the leak possible, and identify the two source files that must change to fix the isolation bug."
}

func Prompt(contextPacket string) string {
	base := fmt.Sprintf(`Analyze this repository and diagnose the reported multi-tenant isolation bug:

%s

Rules:
- This is a static diagnosis benchmark. Do not run builds, tests, package managers, compilers, or linters. The benchmark intentionally does not require a language toolchain.
- Do not modify source files.
- Inspect source with filesystem/search tools only.
- Write exactly one result artifact at %s.
- The artifact must be valid JSON with this schema:
  {"proof":"%s","root_cause":"...","call_path":["..."],"files_to_fix":["..."]}
- root_cause must explain the actual state/keying defect, not just repeat the symptom.
- call_path must name the important functions in execution order.
- files_to_fix must contain the minimal two source files whose code must change.
- Do not add extra files.
`, Objective(), ResultFile, ProofMarker)
	if strings.TrimSpace(contextPacket) == "" {
		return base + "\nNo precompiled context is available. Investigate the repository normally.\n"
	}
	return base + `
KeyDeck has already compiled a focused evidence packet below. Use it first. Inspect additional source only when the packet is insufficient or needs verification. Do not re-scan the whole repository merely to repeat evidence already present.

--- KEYDECK CONTEXT PACKET ---
` + contextPacket + "\n--- END KEYDECK CONTEXT PACKET ---\n"
}

func CreateRepository(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"go.mod":    "module example.local/tenantleak\n\ngo 1.23\n",
		"README.md": "# Multi-tenant routing service\n\nA production report says fallback behavior can cross tenant boundaries after tenant switching. The repository intentionally contains many cache/routing decoys.\n",
		"internal/model/request.go": `package model

type Request struct {
    TenantID string
    Route string
}

type Policy struct {
    TenantID string
    Route string
    Source string
}
`,
		"internal/tenant/active.go": `package tenant

import "sync"

type Active struct {
    mu sync.RWMutex
    id string
}

func (a *Active) Set(id string) {
    a.mu.Lock()
    defer a.mu.Unlock()
    a.id = id
}

func (a *Active) CurrentID() string {
    a.mu.RLock()
    defer a.mu.RUnlock()
    return a.id
}
`,
		"internal/policy/store.go": `package policy

import "example.local/tenantleak/internal/model"

type Store struct{}

func (s *Store) LoadFallback(tenantID, route string) (model.Policy, error) {
    return model.Policy{TenantID: tenantID, Route: route, Source: "fallback"}, nil
}
`,
		"internal/policy/cache.go": `package policy

import (
    "sync"
    "example.local/tenantleak/internal/model"
)

type Cache struct {
    mu sync.RWMutex
    byRoute map[string]model.Policy
}

func NewCache() *Cache {
    return &Cache{byRoute: map[string]model.Policy{}}
}

func (c *Cache) GetOrLoad(route string, loader func() (model.Policy, error)) (model.Policy, error) {
    c.mu.RLock()
    cached, ok := c.byRoute[route]
    c.mu.RUnlock()
    if ok {
        return cached, nil
    }
    loaded, err := loader()
    if err != nil {
        return model.Policy{}, err
    }
    c.mu.Lock()
    c.byRoute[route] = loaded
    c.mu.Unlock()
    return loaded, nil
}
`,
		"internal/routing/engine.go": `package routing

import (
    "example.local/tenantleak/internal/model"
    "example.local/tenantleak/internal/policy"
    "example.local/tenantleak/internal/tenant"
)

type Engine struct {
    active *tenant.Active
    cache *policy.Cache
    store *policy.Store
}

func NewEngine(active *tenant.Active, cache *policy.Cache, store *policy.Store) *Engine {
    return &Engine{active: active, cache: cache, store: store}
}

func (e *Engine) Resolve(req model.Request) (model.Policy, error) {
    tenantID := e.active.CurrentID()
    return e.cache.GetOrLoad(req.Route, func() (model.Policy, error) {
        return e.store.LoadFallback(tenantID, req.Route)
    })
}
`,
		"internal/app/server.go": `package app

import (
    "example.local/tenantleak/internal/model"
    "example.local/tenantleak/internal/routing"
)

type Server struct { router *routing.Engine }
func NewServer(router *routing.Engine) *Server { return &Server{router: router} }
func (s *Server) HandleRequest(req model.Request) (model.Policy, error) {
    return s.router.Resolve(req)
}
`,
		"cmd/server/main.go": `package main

import (
    "example.local/tenantleak/internal/app"
    "example.local/tenantleak/internal/policy"
    "example.local/tenantleak/internal/routing"
    "example.local/tenantleak/internal/tenant"
)

func main() {
    active := &tenant.Active{}
    router := routing.NewEngine(active, policy.NewCache(), &policy.Store{})
    _ = app.NewServer(router)
}
`,
	}
	for rel, body := range files {
		if err := write(root, rel, body); err != nil {
			return err
		}
	}
	for i := 0; i < 180; i++ {
		pkg := fmt.Sprintf("decoy%03d", i)
		rel := filepath.Join("internal", "decoys", pkg, "service.go")
		body := fmt.Sprintf(`package %s

// Service%03d is unrelated infrastructure noise. It uses tenant, route, cache,
// policy and fallback vocabulary so text-only repository discovery has to work.
type Service%03d struct { values map[string]string }

func (s *Service%03d) RefreshTenantRoutePolicy(tenantID, route string) string {
    key := tenantID + ":" + route
    if v, ok := s.values[key]; ok { return v }
    return "fallback-observer-%03d"
}
`, pkg, i, i, i, i)
		if err := write(root, rel, body); err != nil {
			return err
		}
	}
	for i := 0; i < 60; i++ {
		rel := filepath.Join("internal", "telemetry", fmt.Sprintf("metric_%03d.go", i))
		body := fmt.Sprintf(`package telemetry

func Metric%03d(activeTenant, route string) string {
    return activeTenant + route + "cache-policy-fallback-metric-%03d"
}
`, i, i)
		if err := write(root, rel, body); err != nil {
			return err
		}
	}
	return initCleanGit(root)
}

func CopyRepository(src, dst string) error {
	if err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git"+string(os.PathSeparator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	}); err != nil {
		return err
	}
	return initCleanGit(dst)
}

func Evaluate(project string) Acceptance {
	var a Acceptance
	b, err := os.ReadFile(filepath.Join(project, ResultFile))
	if err != nil {
		a.ResultReadError = err.Error()
		return a
	}
	if err := json.Unmarshal(b, &a.ParsedResult); err != nil {
		a.ResultReadError = err.Error()
		return a
	}
	a.ProofMarkerOK = a.ParsedResult.Proof == ProofMarker
	root := strings.ToLower(a.ParsedResult.RootCause)
	a.TenantIsolationOK = strings.Contains(root, "tenant") && (strings.Contains(root, "cross") || strings.Contains(root, "isolation") || strings.Contains(root, "tenant a") || strings.Contains(root, "tenant b"))
	a.CacheKeyCauseOK = strings.Contains(root, "cache") && strings.Contains(root, "route") && (strings.Contains(root, "key") || strings.Contains(root, "keyed")) && (strings.Contains(root, "omit") || strings.Contains(root, "missing") || strings.Contains(root, "not include") || strings.Contains(root, "only"))
	joinedPath := strings.ToLower(strings.Join(a.ParsedResult.CallPath, " "))
	a.CallPathOK = containsAll(joinedPath, "handlerequest", "resolve", "getorload")
	files := normalizeFiles(a.ParsedResult.FilesToFix)
	a.RoutingFileOK = files["internal/routing/engine.go"]
	a.CacheFileOK = files["internal/policy/cache.go"]
	a.MinimalFixSetOK = len(files) == 2
	a.SourceUnchangedOK, a.UnexpectedChanges = sourceUnchanged(project)
	a.Passed = a.ProofMarkerOK && a.TenantIsolationOK && a.CacheKeyCauseOK && a.CallPathOK && a.RoutingFileOK && a.CacheFileOK && a.MinimalFixSetOK && a.SourceUnchangedOK
	return a
}

func initCleanGit(root string) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil
	}
	commands := [][]string{
		{"-C", root, "init", "-q"},
		{"-C", root, "add", "."},
		{"-C", root, "-c", "user.name=KeyDeck Lab", "-c", "user.email=keydeck-lab@localhost", "commit", "-q", "-m", "proof08 benchmark baseline"},
	}
	for _, args := range commands {
		if out, err := exec.Command(git, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func sourceUnchanged(project string) (bool, []string) {
	git, err := exec.LookPath("git")
	if err != nil {
		return true, nil
	}
	out, err := exec.Command(git, "-C", project, "status", "--porcelain=v1", "--untracked-files=all").Output()
	if err != nil {
		return false, []string{"git status failed: " + err.Error()}
	}
	var unexpected []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "?? "+ResultFile {
			continue
		}
		unexpected = append(unexpected, line)
	}
	return len(unexpected) == 0, unexpected
}

func write(root, rel, body string) error {
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(body), 0o644)
}
func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
func normalizeFiles(in []string) map[string]bool {
	out := map[string]bool{}
	for _, f := range in {
		n := strings.ToLower(filepath.ToSlash(filepath.Clean(f)))
		n = strings.TrimPrefix(n, "./")
		out[n] = true
	}
	return out
}

func SortedFiles(m map[string]bool) []string {
	var out []string
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
