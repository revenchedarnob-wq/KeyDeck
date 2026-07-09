package mcpmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpregistry"
	"keydeck.local/feasibilitylab/internal/secretbroker"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

var (
	ErrExecutionUnbound       = errors.New("MCP execution blocked: server has no local runtime binding")
	ErrExecutionDisabled      = errors.New("MCP execution blocked: server is disabled")
	ErrExecutionUnavailable   = errors.New("MCP execution blocked: local runtime is unavailable")
	ErrExecutionUnhealthy     = errors.New("MCP execution blocked: local runtime is not healthy")
	ErrExecutionUndiscovered  = errors.New("MCP execution blocked: server has no trusted discovery snapshot")
	ErrExecutionUnapproved    = errors.New("MCP execution blocked: tool is not explicitly approved")
	ErrExecutionStaleApproval = errors.New("MCP execution blocked: permission approval is stale for the current schema")
	ErrExecutionProfileDenied = errors.New("MCP execution blocked: active permission profile is insufficient")
)

type ExecutionPlan struct {
	ServerID     string                        `json:"server_id"`
	Tool         string                        `json:"tool"`
	Registration mcpregistry.Registration      `json:"registration"`
	Discovery    mcpregistry.DiscoverySnapshot `json:"discovery"`
	Binding      LocalBinding                  `json:"binding"`
	Health       HealthObservation             `json:"health"`
	Approval     ApprovalState                 `json:"approval"`
	Policy       mcpbridge.PermissionPolicy    `json:"policy"`
}

type AdapterFactory interface {
	Build(ctx context.Context, plan ExecutionPlan) (mcpbridge.Adapter, error)
}

type AdapterFactoryFunc func(context.Context, ExecutionPlan) (mcpbridge.Adapter, error)

func (f AdapterFactoryFunc) Build(ctx context.Context, plan ExecutionPlan) (mcpbridge.Adapter, error) {
	if f == nil {
		return nil, errors.New("MCP adapter factory is not configured")
	}
	return f(ctx, plan)
}

type CommandAdapterFactory struct{}

func (CommandAdapterFactory) Build(_ context.Context, plan ExecutionPlan) (mcpbridge.Adapter, error) {
	if plan.Registration.Runtime.Transport != mcpregistry.TransportStdio {
		return nil, fmt.Errorf("unsupported MCP transport %q", plan.Registration.Runtime.Transport)
	}
	args := []string{plan.Binding.EntrypointPath}
	for _, slot := range plan.Registration.Runtime.ArgumentSlots {
		value, ok := plan.Binding.Arguments[slot]
		if !ok {
			return nil, fmt.Errorf("MCP local binding is missing required argument slot %q", slot)
		}
		args = append(args, value)
	}
	client := mcpbridge.NewClient(mcpbridge.CommandConfig{
		Path:          plan.Binding.RuntimePath,
		Args:          args,
		MaxFrameBytes: plan.Registration.Runtime.MaxFrameBytes,
	})
	return mcpbridge.NewIdentifiedCommandAdapter(client, plan.Registration.Identity), nil
}

type ExecutionRouter struct {
	Manager       *Manager
	Factory       AdapterFactory
	Journal       *tooljournal.Journal
	Timeline      *timeline.Store
	TaskID        string
	SessionID     string
	ActiveProfile mcpbridge.PermissionProfile
	Schemas       *mcpbridge.SchemaPolicy
	Secrets       *secretbroker.Broker
}

func (r *ExecutionRouter) Execute(ctx context.Context, serverID string, op mcpbridge.Operation) (mcpbridge.Result, error) {
	if r == nil || r.Journal == nil || r.Timeline == nil || r.TaskID == "" || r.SessionID == "" {
		return mcpbridge.Result{}, errors.New("MCP execution router is not configured")
	}
	plan, err := r.plan(serverID, op.Tool)
	if err != nil {
		r.recordDenial(serverID, op, err)
		return mcpbridge.Result{}, err
	}
	r.recordApprovedPlan(plan, op)
	if r.Factory == nil {
		return mcpbridge.Result{}, errors.New("MCP execution adapter factory is required")
	}
	if r.Schemas == nil {
		return mcpbridge.Result{}, errors.New("MCP execution schema policy is required")
	}
	adapter, err := r.Factory.Build(ctx, plan)
	if err != nil {
		return mcpbridge.Result{}, err
	}
	if adapter == nil {
		return mcpbridge.Result{}, errors.New("MCP adapter factory returned nil adapter")
	}
	bridge := &mcpbridge.Bridge{
		Journal: r.Journal, Timeline: r.Timeline, TaskID: r.TaskID, SessionID: r.SessionID,
		Permissions: &plan.Policy, Schemas: r.Schemas, Secrets: r.Secrets,
		ServerIdentity: &plan.Registration.Identity, Adapter: adapter,
	}
	return bridge.Execute(ctx, op)
}

// Preflight applies the exact same manager readiness, current-schema approval,
// tool grant and active-profile gates as Execute, but performs no adapter
// construction, Tool Journal begin, secret resolution or server invocation.
func (r *ExecutionRouter) Preflight(serverID, tool string) (ExecutionPlan, error) {
	return r.plan(serverID, tool)
}

func (r *ExecutionRouter) plan(serverID, tool string) (ExecutionPlan, error) {
	if r == nil || r.Manager == nil {
		return ExecutionPlan{}, errors.New("MCP execution router manager is not configured")
	}
	view, err := r.Manager.View(serverID)
	if err != nil {
		return ExecutionPlan{}, err
	}
	if view.Binding == nil {
		return ExecutionPlan{}, ErrExecutionUnbound
	}
	if !view.Enabled {
		return ExecutionPlan{}, ErrExecutionDisabled
	}
	switch view.Health.Status {
	case HealthUnavailable:
		return ExecutionPlan{}, ErrExecutionUnavailable
	case HealthHealthy:
	default:
		return ExecutionPlan{}, ErrExecutionUnhealthy
	}
	if view.Discovery == nil {
		return ExecutionPlan{}, ErrExecutionUndiscovered
	}
	if view.Approval == nil {
		return ExecutionPlan{}, ErrExecutionUnapproved
	}
	if !view.ApprovalCurrent || view.Approval.SchemaSHA256 != view.Discovery.SchemaSHA256 {
		return ExecutionPlan{}, ErrExecutionStaleApproval
	}
	approved := false
	for _, candidate := range view.Approval.ApprovedTools {
		if candidate == tool {
			approved = true
			break
		}
	}
	if !approved {
		return ExecutionPlan{}, ErrExecutionUnapproved
	}
	policy, err := r.Manager.EffectivePolicy(serverID, r.ActiveProfile)
	if err != nil {
		return ExecutionPlan{}, err
	}
	if !policy.Allows(tool) {
		return ExecutionPlan{}, ErrExecutionProfileDenied
	}
	policyCopy := mcpbridge.PermissionPolicy{Profile: policy.Profile, ToolProfiles: map[string]mcpbridge.PermissionProfile{}}
	keys := make([]string, 0, len(policy.ToolProfiles))
	for name := range policy.ToolProfiles {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		policyCopy.ToolProfiles[name] = policy.ToolProfiles[name]
	}
	return ExecutionPlan{
		ServerID: serverID, Tool: tool, Registration: view.Registration, Discovery: *view.Discovery,
		Binding: *view.Binding, Health: view.Health, Approval: *view.Approval, Policy: policyCopy,
	}, nil
}

func (r *ExecutionRouter) recordApprovedPlan(plan ExecutionPlan, op mcpbridge.Operation) {
	if r == nil || r.Timeline == nil || r.TaskID == "" || r.SessionID == "" {
		return
	}
	raw, _ := json.Marshal(plan)
	sum := sha256.Sum256(raw)
	_, _, _ = r.Timeline.AppendOnce(timeline.Input{
		EventID: "mcp-route:" + op.OperationID + ":approved", TaskID: r.TaskID, SessionID: r.SessionID,
		Domain: timeline.DomainTool, Kind: "mcp_execution_route_approved",
		SourceRef: plan.ServerID + "@" + plan.Discovery.SchemaSHA256 + "#" + plan.Tool,
		Summary:   "manager approved exact binding, health, schema-bound tool grant and active permission profile",
		DataHash:  hex.EncodeToString(sum[:]),
	})
}

func (r *ExecutionRouter) recordDenial(serverID string, op mcpbridge.Operation, cause error) {
	if r == nil || r.Timeline == nil || r.TaskID == "" || r.SessionID == "" || cause == nil {
		return
	}
	raw, _ := json.Marshal(op.Arguments)
	sum := sha256.Sum256(raw)
	_, _, _ = r.Timeline.AppendOnce(timeline.Input{
		EventID: "mcp-route:" + op.OperationID + ":denied", TaskID: r.TaskID, SessionID: r.SessionID,
		Domain: timeline.DomainTool, Kind: "mcp_execution_route_denied", SourceRef: serverID + "#" + op.Tool,
		Summary: cause.Error(), DataHash: hex.EncodeToString(sum[:]),
	})
}
