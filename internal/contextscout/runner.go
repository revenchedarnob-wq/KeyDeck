package contextscout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"keydeck.local/feasibilitylab/internal/mcpbridge"
	"keydeck.local/feasibilitylab/internal/mcpmanager"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

type RouterRunner struct {
	Router          *mcpmanager.ExecutionRouter
	ServerID        string
	OperationPrefix string
	ProviderName    string
	ProviderVersion string
	mu              sync.Mutex
	calls           int
}

func (r *RouterRunner) Name() string {
	if r.ProviderName == "" {
		return "keydeck-production-context-provider"
	}
	return r.ProviderName
}
func (r *RouterRunner) Version(context.Context) string { return r.ProviderVersion }
func (r *RouterRunner) Call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	if r == nil || r.Router == nil || r.ServerID == "" || r.OperationPrefix == "" {
		return nil, errors.New("context provider runner is not configured")
	}
	r.mu.Lock()
	r.calls++
	sequence := r.calls
	r.mu.Unlock()
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	opID := fmt.Sprintf("context:%s:%02d:%s:%s", r.OperationPrefix, sequence, tool, digest(raw)[:12])
	result, err := r.Router.Execute(ctx, r.ServerID, mcpbridge.Operation{OperationID: opID, Tool: tool, Arguments: args, Policy: tooljournal.ReplayIdempotent})
	if err != nil {
		return nil, err
	}
	return []byte(result.Text), nil
}
func (r *RouterRunner) CallCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}
