package contextscout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"keydeck.local/feasibilitylab/internal/contextcompiler"
	"keydeck.local/feasibilitylab/internal/mcpmanager"
	"keydeck.local/feasibilitylab/internal/timeline"
)

type Coordinator struct {
	Router    *mcpmanager.ExecutionRouter
	Store     *Store
	Timeline  *timeline.Store
	TaskID    string
	SessionID string
}

func (c *Coordinator) Build(ctx context.Context, options BuildOptions) (Output, error) {
	if c == nil || c.Router == nil || c.Store == nil || c.Timeline == nil || c.TaskID == "" || c.SessionID == "" {
		return Output{}, errors.New("production context coordinator is not configured")
	}
	opt, err := normalizeBuildOptions(options)
	if err != nil {
		return Output{}, err
	}
	fingerprint, err := FingerprintProject(opt.ProjectRoot)
	if err != nil {
		return Output{}, err
	}
	cacheKey := CacheKeyInput{ProviderServerID: opt.ProviderServerID, ProviderSchemaSHA256: opt.ProviderSchemaSHA256, ProjectRoot: opt.ProjectRoot, Objective: opt.Objective, MaxChars: opt.MaxChars, MaxFiles: opt.MaxFiles}.Hash()
	packet, record, fresh, err := c.Store.FindFresh(cacheKey, fingerprint)
	if err != nil {
		return Output{}, err
	}
	if fresh {
		hygiene, err := ValidateHygiene(packet, opt.ProjectRoot, opt.MaxChars, opt.MaxFiles, opt.ForbiddenExactValues)
		if err != nil {
			return Output{}, err
		}
		_, _, err = c.Timeline.AppendOnce(timeline.Input{EventID: "context:" + record.PacketID + ":reused", TaskID: c.TaskID, SessionID: c.SessionID, Domain: timeline.DomainArtifact, Kind: "context_packet_reused", SourceRef: opt.ProviderServerID + "@" + opt.ProviderSchemaSHA256, Summary: "reused fresh verified context packet without provider execution", DataHash: record.PacketSHA256})
		if err != nil {
			return Output{}, err
		}
		return Output{Packet: packet, Record: record, Hygiene: hygiene, Reused: true, ProviderCallCount: 0, ProjectFingerprint: fingerprint, CacheKey: cacheKey}, nil
	}
	var firstPlan mcpmanager.ExecutionPlan
	for i, tool := range opt.ProviderTools {
		plan, err := c.Router.Preflight(opt.ProviderServerID, tool)
		if err != nil {
			return Output{}, err
		}
		if plan.ServerID != opt.ProviderServerID || plan.Discovery.SchemaSHA256 != opt.ProviderSchemaSHA256 {
			return Output{}, ErrProviderMismatch
		}
		if i == 0 {
			firstPlan = plan
		}
	}
	attempt := buildAttemptIdentity(cacheKey, fingerprint, c.Store.NextSequence())
	runner := &RouterRunner{Router: c.Router, ServerID: opt.ProviderServerID, OperationPrefix: attempt, ProviderName: firstPlan.Registration.Identity.CanonicalRef(), ProviderVersion: firstPlan.Registration.Identity.Version}
	compiler := &contextcompiler.Compiler{Runner: runner}
	packet, err = compiler.Compile(ctx, contextcompiler.CompileOptions{ProjectRoot: opt.ProjectRoot, Objective: opt.Objective, MaxChars: opt.MaxChars, MaxFiles: opt.MaxFiles})
	if err != nil {
		return Output{}, err
	}
	hygiene, err := ValidateHygiene(packet, opt.ProjectRoot, opt.MaxChars, opt.MaxFiles, opt.ForbiddenExactValues)
	if err != nil {
		return Output{}, err
	}
	evidence := ProviderEvidence{ProviderServerID: opt.ProviderServerID, ProviderSchemaSHA256: opt.ProviderSchemaSHA256, ProviderTools: append([]string(nil), opt.ProviderTools...), CacheKey: cacheKey}
	record, _, err = c.Store.Save(SaveInput{CacheKey: cacheKey, ProjectFingerprint: fingerprint, ProviderServerID: opt.ProviderServerID, ProviderSchemaSHA256: opt.ProviderSchemaSHA256, ProjectRoot: opt.ProjectRoot, Objective: opt.Objective, MaxChars: opt.MaxChars, MaxFiles: opt.MaxFiles, Packet: packet, ProviderEvidence: evidence})
	if err != nil {
		return Output{}, err
	}
	_, _, err = c.Timeline.AppendOnce(timeline.Input{EventID: "context:" + record.PacketID + ":stored", TaskID: c.TaskID, SessionID: c.SessionID, Domain: timeline.DomainArtifact, Kind: "context_packet_stored", SourceRef: opt.ProviderServerID + "@" + opt.ProviderSchemaSHA256, Summary: fmt.Sprintf("stored bounded context packet with %d source snippets and %d omitted lower-ranked evidence items", len(packet.SourceSnippets), packet.OmittedEvidenceCount), DataHash: record.PacketSHA256})
	if err != nil {
		return Output{}, err
	}
	return Output{Packet: packet, Record: record, Hygiene: hygiene, Reused: false, ProviderCallCount: runner.CallCount(), ProjectFingerprint: fingerprint, CacheKey: cacheKey}, nil
}

func normalizeBuildOptions(opt BuildOptions) (BuildOptions, error) {
	root, err := filepath.Abs(strings.TrimSpace(opt.ProjectRoot))
	if err != nil {
		return BuildOptions{}, err
	}
	opt.ProjectRoot = filepath.Clean(root)
	opt.Objective = strings.TrimSpace(opt.Objective)
	opt.ProviderServerID = strings.TrimSpace(opt.ProviderServerID)
	opt.ProviderSchemaSHA256 = strings.TrimSpace(opt.ProviderSchemaSHA256)
	if opt.Objective == "" || opt.ProviderServerID == "" || opt.ProviderSchemaSHA256 == "" {
		return BuildOptions{}, errors.New("project root, objective and exact provider identity/schema are required")
	}
	if opt.MaxChars <= 0 {
		opt.MaxChars = 12000
	}
	if opt.MaxFiles <= 0 {
		opt.MaxFiles = 6
	}
	opt.ProviderTools = normalizeTools(opt.ProviderTools)
	if len(opt.ProviderTools) == 0 {
		return BuildOptions{}, errors.New("at least one context provider tool is required")
	}
	return opt, nil
}
func buildAttemptIdentity(cacheKey, fingerprint string, nextSequence uint64) string {
	raw, _ := json.Marshal(struct {
		CacheKey           string `json:"cache_key"`
		ProjectFingerprint string `json:"project_fingerprint"`
		NextSequence       uint64 `json:"next_sequence"`
	}{cacheKey, fingerprint, nextSequence})
	return digest(raw)[:20]
}
