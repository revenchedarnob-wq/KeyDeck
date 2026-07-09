package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/contextscout"
	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpmanager"
	"keydeck.local/feasibilitylab/internal/mcpregistry"
	"keydeck.local/feasibilitylab/internal/proofreceipt"
	"keydeck.local/feasibilitylab/internal/tasks"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

const (
	taskID         = "proof26-task"
	sessionID      = "proof26-session"
	secretSentinel = "PROOF26_SECRET_MUST_NEVER_PERSIST_123456"
)

type scenario struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail any    `json:"detail,omitempty"`
}
type report struct {
	Proof              string     `json:"proof"`
	Status             string     `json:"status"`
	Passed             bool       `json:"passed"`
	Scenarios          []scenario `json:"scenarios"`
	ServerID           string     `json:"server_id"`
	IdentitySHA256     string     `json:"identity_sha256"`
	RuntimeSHA256      string     `json:"runtime_sha256"`
	SchemaSHA256       string     `json:"schema_sha256"`
	FirstPacketID      string     `json:"first_packet_id"`
	RebuiltPacketID    string     `json:"rebuilt_packet_id"`
	FirstFingerprint   string     `json:"first_fingerprint"`
	RebuiltFingerprint string     `json:"rebuilt_fingerprint"`
	CacheKey           string     `json:"cache_key"`
	ContextStoreSHA256 string     `json:"context_store_sha256"`
	TimelineSHA256     string     `json:"timeline_sha256"`
	ReceiptID          string     `json:"receipt_id"`
	NextGate           string     `json:"next_gate"`
}

type discoverer struct {
	identity mcpbridge.ServerIdentity
	contract mcpregistry.RuntimeContract
	tools    []mcpbridge.Tool
}

func (d *discoverer) BoundServerIdentity() *mcpbridge.ServerIdentity    { x := d.identity; return &x }
func (d *discoverer) BoundRuntimeContract() mcpregistry.RuntimeContract { return d.contract }
func (d *discoverer) DiscoverTools(context.Context) ([]mcpbridge.Tool, error) {
	return append([]mcpbridge.Tool(nil), d.tools...), nil
}

type staticHealth struct{}

func (staticHealth) Check(_ context.Context, b mcpmanager.LocalBinding) (mcpmanager.HealthObservation, error) {
	return mcpmanager.HealthObservation{ServerID: b.ServerID, BindingSHA256: b.BindingSHA256, Status: mcpmanager.HealthHealthy, DetailCode: "in_process_fixture_ready", ToolCount: 4}, nil
}

type providerAdapter struct {
	identity mcpbridge.ServerIdentity
	owner    *providerFactory
}

func (a *providerAdapter) BoundServerIdentity() *mcpbridge.ServerIdentity { x := a.identity; return &x }
func (a *providerAdapter) Invoke(_ context.Context, tool string, _ map[string]any) (mcpbridge.CallToolResult, error) {
	a.owner.mu.Lock()
	a.owner.calls++
	secret := a.owner.injectSecret
	a.owner.mu.Unlock()
	var text string
	switch tool {
	case "index_repository":
		text = `{"project_id":"proof26-project"}`
	case "get_architecture":
		text = `{"files":["internal/router.go","internal/cache.go"]}`
	case "search_graph":
		text = `{"symbols":[{"name":"RouteRequest","path":"internal/router.go"},{"name":"CacheLookup","path":"internal/cache.go"}]}`
	case "trace_path":
		text = `{"path":"internal/router.go"}`
	default:
		return mcpbridge.CallToolResult{}, fmt.Errorf("unknown context fixture tool %q", tool)
	}
	if secret != "" {
		text += secret
	}
	return mcpbridge.CallToolResult{Content: []mcpbridge.Content{{Type: "text", Text: text}}}, nil
}

type providerFactory struct {
	identity      mcpbridge.ServerIdentity
	mu            sync.Mutex
	builds, calls int
	injectSecret  string
}

func (f *providerFactory) Build(context.Context, mcpmanager.ExecutionPlan) (mcpbridge.Adapter, error) {
	f.mu.Lock()
	f.builds++
	f.mu.Unlock()
	return &providerAdapter{identity: f.identity, owner: f}, nil
}
func (f *providerFactory) Counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.builds, f.calls
}

type fixture struct {
	root, stateRoot, registryPath, managerPath, journalPath, timelinePath, storePath string
	identity                                                                         mcpbridge.ServerIdentity
	reg                                                                              mcpregistry.Registration
	snap                                                                             mcpregistry.DiscoverySnapshot
	registry                                                                         *mcpregistry.Registry
	manager                                                                          *mcpmanager.Manager
	journal                                                                          *tooljournal.Journal
	timeline                                                                         *timeline.Store
	store                                                                            *contextscout.Store
	factory                                                                          *providerFactory
	router                                                                           *mcpmanager.ExecutionRouter
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	f, err := newFixture()
	if err != nil {
		return err
	}
	defer os.RemoveAll(f.stateRoot)
	identityHash, _ := f.identity.Hash()
	out := report{Proof: "0.26-production-context-scout-compiler-reconstructed", Status: "failed", ServerID: f.reg.ServerID, IdentitySHA256: identityHash, RuntimeSHA256: f.reg.RuntimeSHA256, SchemaSHA256: f.snap.SchemaSHA256, NextGate: "Proof 0.27 — Canonical Project Brain + Context Inspection Contract"}
	add := func(name string, pass bool, detail any) {
		out.Scenarios = append(out.Scenarios, scenario{Name: name, Passed: pass, Detail: detail})
	}
	options := contextscout.BuildOptions{ProjectRoot: f.root, Objective: "route provider cache invalidation target", MaxChars: 12000, MaxFiles: 2, ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snap.SchemaSHA256, ForbiddenExactValues: []string{secretSentinel}}
	coord := f.coordinator()

	// 1. Local fingerprint ignores env but tracks source.
	fp1, err := contextscout.FingerprintProject(f.root)
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(f.root, ".env"), []byte("TOKEN=changed\n"), 0600); err != nil {
		return err
	}
	fpEnv, _ := contextscout.FingerprintProject(f.root)
	sourcePath := filepath.Join(f.root, "internal/router.go")
	original, _ := os.ReadFile(sourcePath)
	if err = os.WriteFile(sourcePath, append(original, []byte("// fingerprint change probe\n")...), 0600); err != nil {
		return err
	}
	fpSource, _ := contextscout.FingerprintProject(f.root)
	if err = os.WriteFile(sourcePath, original, 0600); err != nil {
		return err
	}
	pass1 := fp1 == fpEnv && fpSource != fp1
	add("local_source_fingerprint_ignores_env_and_tracks_relevant_source", pass1, map[string]any{"initial": fp1, "after_env": fpEnv, "after_source": fpSource})

	// First production build.
	first, err := coord.Build(context.Background(), options)
	if err != nil {
		return err
	}
	out.FirstPacketID = first.Record.PacketID
	out.FirstFingerprint = first.ProjectFingerprint
	out.CacheKey = first.CacheKey
	firstOps := operationIDs(f.journal)

	// 2. Exact cache and packet identity binding.
	expectedCache := contextscout.CacheKeyInput{ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snap.SchemaSHA256, ProjectRoot: filepath.Clean(f.root), Objective: options.Objective, MaxChars: options.MaxChars, MaxFiles: options.MaxFiles}.Hash()
	expectedPacket := contextscout.DerivePacketID(first.CacheKey, first.ProjectFingerprint, first.Record.PacketSHA256)
	variants := []contextscout.CacheKeyInput{
		{ProviderServerID: f.reg.ServerID + "x", ProviderSchemaSHA256: f.snap.SchemaSHA256, ProjectRoot: filepath.Clean(f.root), Objective: options.Objective, MaxChars: options.MaxChars, MaxFiles: options.MaxFiles},
		{ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snap.SchemaSHA256 + "x", ProjectRoot: filepath.Clean(f.root), Objective: options.Objective, MaxChars: options.MaxChars, MaxFiles: options.MaxFiles},
		{ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snap.SchemaSHA256, ProjectRoot: filepath.Clean(f.root) + "x", Objective: options.Objective, MaxChars: options.MaxChars, MaxFiles: options.MaxFiles},
		{ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snap.SchemaSHA256, ProjectRoot: filepath.Clean(f.root), Objective: options.Objective + "x", MaxChars: options.MaxChars, MaxFiles: options.MaxFiles},
		{ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snap.SchemaSHA256, ProjectRoot: filepath.Clean(f.root), Objective: options.Objective, MaxChars: options.MaxChars + 1, MaxFiles: options.MaxFiles},
		{ProviderServerID: f.reg.ServerID, ProviderSchemaSHA256: f.snap.SchemaSHA256, ProjectRoot: filepath.Clean(f.root), Objective: options.Objective, MaxChars: options.MaxChars, MaxFiles: options.MaxFiles + 1},
	}
	unique := true
	for _, v := range variants {
		if v.Hash() == first.CacheKey {
			unique = false
		}
	}
	pass2 := first.CacheKey == expectedCache && first.Record.PacketID == expectedPacket && first.Record.ProviderServerID == f.reg.ServerID && first.Record.ProviderSchemaSHA256 == f.snap.SchemaSHA256 && unique
	add("cache_and_packet_identity_bind_exact_provider_schema_project_objective_budgets_and_fingerprint", pass2, map[string]any{"cache_key": first.CacheKey, "packet_id": first.Record.PacketID, "variant_keys_distinct": unique})

	// 3. Restart fresh reuse with zero provider calls.
	beforeBuilds, beforeCalls := f.factory.Counts()
	restarted, err := f.restart()
	if err != nil {
		return err
	}
	reuse, err := restarted.coordinator().Build(context.Background(), options)
	if err != nil {
		return err
	}
	afterBuilds, afterCalls := f.factory.Counts()
	pass3 := reuse.Reused && reuse.ProviderCallCount == 0 && reuse.Record.PacketID == first.Record.PacketID && beforeBuilds == afterBuilds && beforeCalls == afterCalls
	add("fresh_verified_packet_reuses_after_restart_with_zero_provider_calls", pass3, map[string]any{"reused": reuse.Reused, "provider_calls": reuse.ProviderCallCount, "factory_builds_before": beforeBuilds, "factory_builds_after": afterBuilds})

	// 4. Fresh reuse remains available while provider disabled.
	if _, err = restarted.manager.SetEnabled(f.reg.ServerID, false, "prove offline packet reuse"); err != nil {
		return err
	}
	disabledReuse, err := restarted.coordinator().Build(context.Background(), options)
	pass4 := err == nil && disabledReuse.Reused && disabledReuse.ProviderCallCount == 0
	add("fresh_verified_packet_reuses_while_provider_disabled", pass4, map[string]any{"reused": disabledReuse.Reused, "error": fmt.Sprint(err)})

	// 5. Relevant source change invalidates old packet.
	changed := []byte("package internal\nfunc RouteRequest(){ /* changed route provider cache invalidation target */ }\n")
	if err = os.WriteFile(sourcePath, changed, 0600); err != nil {
		return err
	}
	changedFP, err := contextscout.FingerprintProject(f.root)
	if err != nil {
		return err
	}
	_, _, freshOld, err := restarted.store.FindFresh(first.CacheKey, changedFP)
	if err != nil {
		return err
	}
	pass5 := changedFP != first.ProjectFingerprint && !freshOld
	add("relevant_source_change_invalidates_old_packet_fingerprint", pass5, map[string]any{"old": first.ProjectFingerprint, "new": changedFP, "old_packet_fresh": freshOld})

	// 6. Disabled provider blocks stale rebuild before execution and persistence.
	countBefore := restarted.store.Count()
	buildsBefore, callsBefore := f.factory.Counts()
	_, blockedErr := restarted.coordinator().Build(context.Background(), options)
	buildsAfter, callsAfter := f.factory.Counts()
	pass6 := errors.Is(blockedErr, mcpmanager.ErrExecutionDisabled) && restarted.store.Count() == countBefore && buildsBefore == buildsAfter && callsBefore == callsAfter
	add("disabled_provider_blocks_stale_rebuild_before_execution_and_persistence", pass6, map[string]any{"error": fmt.Sprint(blockedErr), "store_before": countBefore, "store_after": restarted.store.Count(), "builds_before": buildsBefore, "builds_after": buildsAfter, "calls_before": callsBefore, "calls_after": callsAfter})

	// 7. Re-enable rebuilds fresh packet with fresh operation identities.
	if _, err = restarted.manager.SetEnabled(f.reg.ServerID, true, "rebuild stale packet"); err != nil {
		return err
	}
	rebuilt, err := restarted.coordinator().Build(context.Background(), options)
	if err != nil {
		return err
	}
	out.RebuiltPacketID = rebuilt.Record.PacketID
	out.RebuiltFingerprint = rebuilt.ProjectFingerprint
	rebuiltOps := operationIDs(restarted.journal)
	newOps := difference(rebuiltOps, firstOps)
	pass7 := !rebuilt.Reused && rebuilt.Record.PacketID != first.Record.PacketID && rebuilt.ProjectFingerprint != first.ProjectFingerprint && len(newOps) > 0
	add("reenabled_provider_rebuilds_fresh_packet_with_fresh_operation_identities", pass7, map[string]any{"first_packet": first.Record.PacketID, "rebuilt_packet": rebuilt.Record.PacketID, "fresh_operation_ids": newOps})

	// 8. Hygiene enforces all required boundaries.
	h := rebuilt.Hygiene
	pathsContained := true
	seen := map[string]bool{}
	noDup := true
	for _, s := range rebuilt.Packet.SourceSnippets {
		full := filepath.Join(f.root, filepath.FromSlash(s.Path))
		rel, e := filepath.Rel(f.root, full)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			pathsContained = false
		}
		raw, _ := json.Marshal(s)
		sum := sha256.Sum256(raw)
		k := hex.EncodeToString(sum[:])
		if seen[k] {
			noDup = false
		}
		seen[k] = true
	}
	packetRaw, _ := json.Marshal(rebuilt.Packet)
	secretFree := !strings.Contains(string(packetRaw), secretSentinel) && !strings.Contains(rebuilt.Packet.Render(), secretSentinel)
	dup := rebuilt.Packet
	if len(dup.SourceSnippets) > 0 {
		dup.SourceSnippets = append(dup.SourceSnippets, dup.SourceSnippets[0])
		dup.RenderedChars = len(dup.Render())
	}
	_, dupErr := contextscout.ValidateHygiene(dup, f.root, 12000, 3, []string{secretSentinel})
	pass8 := h.RenderedChars <= options.MaxChars && h.SourceSnippetCount <= options.MaxFiles && h.StructuralReceipts > 0 && pathsContained && noDup && secretFree && errors.Is(dupErr, contextscout.ErrHygiene)
	add("context_hygiene_enforces_budgets_containment_uniqueness_secret_exclusion_and_receipts", pass8, map[string]any{"hygiene": h, "paths_contained": pathsContained, "unique": noDup, "secret_free": secretFree, "duplicate_probe": fmt.Sprint(dupErr)})

	// 9. Exact target focus and explicit omitted evidence.
	snippetPaths := []string{}
	for _, s := range rebuilt.Packet.SourceSnippets {
		snippetPaths = append(snippetPaths, filepath.ToSlash(s.Path))
	}
	sort.Strings(snippetPaths)
	hasRouter, hasCache := contains(snippetPaths, "internal/router.go"), contains(snippetPaths, "internal/cache.go")
	pass9 := hasRouter && hasCache && rebuilt.Packet.OmittedEvidenceCount > 0
	add("packet_focuses_exact_target_files_and_records_omitted_lower_ranked_evidence", pass9, map[string]any{"snippet_paths": snippetPaths, "omitted": rebuilt.Packet.OmittedEvidenceCount})

	// 10. Proof Receipt binds packet/store and exact provider identity, with no raw secret.
	taskPath := filepath.Join(f.stateRoot, "task.jsonl")
	taskStore, err := tasks.Open(taskPath)
	if err != nil {
		return err
	}
	taskManager := &tasks.Manager{Store: taskStore, Journal: restarted.journal}
	names := []string{"fingerprint", "identity", "restart reuse", "disabled reuse", "invalidation", "manager gate", "fresh rebuild", "hygiene", "focus and omission", "receipt provenance"}
	checks := make([]tasks.AcceptanceCheck, 10)
	for i, n := range names {
		checks[i] = tasks.AcceptanceCheck{ID: fmt.Sprintf("c%02d", i+1), Description: n}
	}
	if _, err = taskManager.Create(taskID, sessionID, tasks.Contract{Goal: "Prove a production Context Scout/Compiler path that reuses only fresh verified packets and applies manager safety before stale rebuilds.", ForbiddenScope: []string{"no secret persistence", "no silent local fallback when configured provider is blocked", "no unbounded context"}, Checks: checks}); err != nil {
		return err
	}
	for i, s := range out.Scenarios {
		status := tasks.CheckFailed
		if s.Passed {
			status = tasks.CheckPassed
		}
		if _, err = taskManager.UpdateCheck(fmt.Sprintf("c%02d", i+1), status, s.Name); err != nil {
			return err
		}
	}
	artifacts, err := rebuilt.ReceiptArtifacts(restarted.store)
	if err != nil {
		return err
	}
	provisional, err := proofreceipt.BuildRedacted(taskStore.State(), restarted.timeline.Snapshot(), artifacts, []string{secretSentinel})
	if err != nil {
		return err
	}
	provRaw, _ := json.Marshal(provisional)
	sourceBound := false
	for _, ref := range provisional.TimelineRefs {
		if strings.Contains(ref.SourceRef, f.reg.ServerID+"@"+f.snap.SchemaSHA256) {
			sourceBound = true
		}
	}
	artifactBound := false
	for _, a := range provisional.Artifacts {
		if a.Name == "context provider identity evidence" {
			artifactBound = true
		}
	}
	durableSecret := scanContains([]string{restarted.registryPath, restarted.managerPath, restarted.journalPath, restarted.timelinePath, restarted.storePath}, secretSentinel)
	pass10 := sourceBound && artifactBound && !strings.Contains(string(provRaw), secretSentinel) && !durableSecret
	add("proof_receipt_binds_packet_store_and_exact_provider_identity_with_no_raw_secret", pass10, map[string]any{"provisional_receipt": provisional.ReceiptID, "provider_source_bound": sourceBound, "provider_artifact_bound": artifactBound, "secret_present": durableSecret})
	finalStatus := tasks.CheckFailed
	if pass10 {
		finalStatus = tasks.CheckPassed
	}
	if _, err = taskManager.UpdateCheck("c10", finalStatus, "exact packet/store/provider provenance"); err != nil {
		return err
	}
	finalReceipt, err := proofreceipt.BuildRedacted(taskStore.State(), restarted.timeline.Snapshot(), artifacts, []string{secretSentinel})
	if err != nil {
		return err
	}
	out.ReceiptID = finalReceipt.ReceiptID

	if out.ContextStoreSHA256, _, err = fileSHA256(restarted.storePath); err != nil {
		return err
	}
	if out.TimelineSHA256, _, err = fileSHA256(restarted.timelinePath); err != nil {
		return err
	}
	out.Passed = len(out.Scenarios) == 10
	for _, s := range out.Scenarios {
		out.Passed = out.Passed && s.Passed
	}
	if out.Passed {
		out.Status = "passed"
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err = enc.Encode(out); err != nil {
		return err
	}
	if !out.Passed {
		return errors.New("Proof 0.26 acceptance contract failed")
	}
	return nil
}

func newFixture() (*fixture, error) {
	stateRoot := filepath.Join(os.TempDir(), "keydeck-proof26-state")
	_ = os.RemoveAll(stateRoot)
	if err := os.MkdirAll(stateRoot, 0700); err != nil {
		return nil, err
	}
	project := filepath.Join(stateRoot, "project")
	files := map[string]string{"internal/router.go": "package internal\nfunc RouteRequest(){ /* route provider cache invalidation target */ }\n", "internal/cache.go": "package internal\nfunc CacheLookup(){ /* cache provider route target */ }\n", "internal/noise-a.go": "package internal\n// route provider cache invalidation lower ranked evidence\n", "internal/noise-b.go": "package internal\n// route provider cache invalidation lower ranked evidence\n", "internal/noise-c.go": "package internal\n// route provider cache invalidation lower ranked evidence\n", ".env": "TOKEN=" + secretSentinel + "\n"}
	for p, c := range files {
		if err := writeFile(filepath.Join(project, p), c); err != nil {
			return nil, err
		}
	}
	packageHash := sha256.Sum256([]byte("keydeck-proof26-deterministic-context-provider"))
	identity := mcpbridge.ServerIdentity{Name: "KeyDeck deterministic context provider fixture", Version: "1.0.0", Registry: "fixture", Package: "keydeck/proof26-context-provider", PackageIntegrity: "sha512-proof26-deterministic", PackageSHA256: hex.EncodeToString(packageHash[:]), EntryPoint: "provider"}
	runtime := mcpregistry.RuntimeContract{Transport: mcpregistry.TransportStdio, Runtime: "in-process-fixture", Entrypoint: "provider", ProtocolVersion: mcpbridge.ProtocolVersion, MaxFrameBytes: 1 << 20}
	reg, err := mcpregistry.NewRegistration(identity, runtime)
	if err != nil {
		return nil, err
	}
	registryPath := filepath.Join(stateRoot, "registry.jsonl")
	registry, err := mcpregistry.Open(registryPath)
	if err != nil {
		return nil, err
	}
	if _, _, err = registry.Register(reg); err != nil {
		return nil, err
	}
	tools := []mcpbridge.Tool{}
	for _, name := range contextscout.DefaultProviderTools {
		tools = append(tools, mcpbridge.Tool{Name: name, InputSchema: map[string]any{"type": "object"}})
	}
	snap, _, err := registry.Discover(context.Background(), reg.ServerID, &discoverer{identity: identity, contract: runtime, tools: tools})
	if err != nil {
		return nil, err
	}
	managerPath := filepath.Join(stateRoot, "manager.jsonl")
	manager, err := mcpmanager.Open(managerPath, registry)
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	entry := filepath.Join(stateRoot, "provider-entry")
	if err = writeFile(entry, "fixture"); err != nil {
		return nil, err
	}
	binding, err := mcpmanager.NewLocalBinding(reg, exe, entry, nil)
	if err != nil {
		return nil, err
	}
	if _, err = manager.Bind(binding); err != nil {
		return nil, err
	}
	if _, err = manager.SetEnabled(reg.ServerID, true, "production context fixture"); err != nil {
		return nil, err
	}
	if _, err = manager.ApproveTools(reg.ServerID, snap.SchemaSHA256, contextscout.DefaultProviderTools); err != nil {
		return nil, err
	}
	if _, err = manager.CheckHealth(context.Background(), reg.ServerID, staticHealth{}); err != nil {
		return nil, err
	}
	journalPath := filepath.Join(stateRoot, "journal.jsonl")
	journal, err := tooljournal.Open(journalPath)
	if err != nil {
		return nil, err
	}
	timelinePath := filepath.Join(stateRoot, "timeline.jsonl")
	tl, err := timeline.Open(timelinePath)
	if err != nil {
		return nil, err
	}
	factory := &providerFactory{identity: identity}
	schemas := &mcpbridge.SchemaPolicy{Tools: map[string]mcpbridge.ArgumentSchema{}}
	for _, name := range contextscout.DefaultProviderTools {
		schemas.Tools[name] = mcpbridge.ArgumentSchema{AllowUnknown: true}
	}
	router := &mcpmanager.ExecutionRouter{Manager: manager, Factory: factory, Journal: journal, Timeline: tl, TaskID: taskID, SessionID: sessionID, ActiveProfile: mcpbridge.ProfileFullControl, Schemas: schemas}
	storePath := filepath.Join(stateRoot, "context.jsonl")
	store, err := contextscout.OpenStore(storePath)
	if err != nil {
		return nil, err
	}
	return &fixture{root: project, stateRoot: stateRoot, registryPath: registryPath, managerPath: managerPath, journalPath: journalPath, timelinePath: timelinePath, storePath: storePath, identity: identity, reg: reg, snap: snap, registry: registry, manager: manager, journal: journal, timeline: tl, store: store, factory: factory, router: router}, nil
}
func (f *fixture) coordinator() *contextscout.Coordinator {
	return &contextscout.Coordinator{Router: f.router, Store: f.store, Timeline: f.timeline, TaskID: taskID, SessionID: sessionID}
}
func (f *fixture) restart() (*fixture, error) {
	registry, err := mcpregistry.Open(f.registryPath)
	if err != nil {
		return nil, err
	}
	manager, err := mcpmanager.Open(f.managerPath, registry)
	if err != nil {
		return nil, err
	}
	journal, err := tooljournal.Open(f.journalPath)
	if err != nil {
		return nil, err
	}
	tl, err := timeline.Open(f.timelinePath)
	if err != nil {
		return nil, err
	}
	store, err := contextscout.OpenStore(f.storePath)
	if err != nil {
		return nil, err
	}
	schemas := &mcpbridge.SchemaPolicy{Tools: map[string]mcpbridge.ArgumentSchema{}}
	for _, name := range contextscout.DefaultProviderTools {
		schemas.Tools[name] = mcpbridge.ArgumentSchema{AllowUnknown: true}
	}
	router := &mcpmanager.ExecutionRouter{Manager: manager, Factory: f.factory, Journal: journal, Timeline: tl, TaskID: taskID, SessionID: sessionID, ActiveProfile: mcpbridge.ProfileFullControl, Schemas: schemas}
	copy := *f
	copy.registry = registry
	copy.manager = manager
	copy.journal = journal
	copy.timeline = tl
	copy.store = store
	copy.router = router
	return &copy, nil
}
func operationIDs(j *tooljournal.Journal) []string {
	m := j.Snapshot()
	out := make([]string, 0, len(m))
	for id := range m {
		if strings.HasPrefix(id, "context:") {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
func difference(all, old []string) []string {
	seen := map[string]bool{}
	for _, x := range old {
		seen[x] = true
	}
	out := []string{}
	for _, x := range all {
		if !seen[x] {
			out = append(out, x)
		}
	}
	return out
}
func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0600)
}
func fileSHA256(path string) (string, int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), st.Size(), nil
}
func scanContains(paths []string, needle string) bool {
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}

var _ = contextcompiler.Packet{}
