package mcpbridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"keydeck.local/feasibilitylab/internal/secretbroker"
	"keydeck.local/feasibilitylab/internal/timeline"
	"keydeck.local/feasibilitylab/internal/tooljournal"
)

var (
	ErrServerIdentityMismatch = errors.New("MCP server identity does not match adapter binding")
	ErrToolNotAllowed         = errors.New("MCP tool is not allowed by KeyDeck permissions")
	ErrSecretBrokerNeeded     = errors.New("MCP operation contains secret references but no Secret Broker is configured")
)

type Operation struct {
	OperationID string
	Tool        string
	Arguments   map[string]any
	Policy      tooljournal.ReplayPolicy
}

type Result struct {
	Text   string
	Reused bool
}

type Bridge struct {
	Journal        *tooljournal.Journal
	Timeline       *timeline.Store
	TaskID         string
	SessionID      string
	Permissions    *PermissionPolicy
	Schemas        *SchemaPolicy
	Secrets        *secretbroker.Broker
	ServerIdentity *ServerIdentity
	AllowedTools   map[string]bool // legacy compatibility; prefer Permissions
	Adapter        Adapter
	Client         *Client // legacy compatibility; wrapped in CommandAdapter when Adapter is nil
}

func (b *Bridge) Execute(ctx context.Context, op Operation) (Result, error) {
	adapter := b.adapter()
	if b.Journal == nil || b.Timeline == nil || adapter == nil || b.TaskID == "" || b.SessionID == "" {
		return Result{}, errors.New("MCP bridge is not configured")
	}
	if b.ServerIdentity != nil {
		if err := b.ServerIdentity.Validate(); err != nil {
			return Result{}, err
		}
		provider, ok := adapter.(ServerIdentityProvider)
		if !ok || provider.BoundServerIdentity() == nil {
			return Result{}, ErrServerIdentityMismatch
		}
		bound := provider.BoundServerIdentity()
		if err := bound.Validate(); err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrServerIdentityMismatch, err)
		}
		expectedHash, _ := b.ServerIdentity.Hash()
		boundHash, _ := bound.Hash()
		if expectedHash != boundHash {
			return Result{}, ErrServerIdentityMismatch
		}
	}
	if b.Secrets != nil && b.Schemas == nil {
		return Result{}, errors.New("MCP bridge with Secret Broker requires schema policy")
	}
	if !b.toolAllowed(op.Tool) {
		b.recordPreflightDenial(op, "mcp_tool_permission_denied", ErrToolNotAllowed)
		return Result{}, ErrToolNotAllowed
	}
	if b.Schemas != nil {
		if err := b.Schemas.ValidateToolArguments(op.Tool, op.Arguments); err != nil {
			b.recordPreflightDenial(op, "mcp_tool_schema_denied", err)
			return Result{}, err
		}
	}

	refs, err := secretbroker.CollectReferences(op.Arguments)
	if err != nil {
		b.recordPreflightDenial(op, "mcp_tool_secret_reference_denied", err)
		return Result{}, err
	}
	var plan secretbroker.Plan
	if len(refs) > 0 {
		if b.Secrets == nil {
			b.recordPreflightDenial(op, "mcp_tool_secret_broker_missing", ErrSecretBrokerNeeded)
			return Result{}, ErrSecretBrokerNeeded
		}
		plan, err = b.Secrets.PlanArguments(op.Tool, op.Arguments)
		if err != nil {
			b.recordPreflightDenial(op, "mcp_tool_secret_scope_denied", err)
			return Result{}, err
		}
	}

	// Journal the unresolved arguments. Scoped references are safe to hash and
	// completed operations can be reused without resolving secrets again.
	args, err := json.Marshal(op.Arguments)
	if err != nil {
		return Result{}, err
	}
	decision, err := b.Journal.Begin(op.OperationID, "mcp:"+op.Tool, args, op.Policy)
	if err != nil {
		return Result{}, err
	}
	if decision.Kind == tooljournal.DecisionReturnPrevious {
		_, _, _ = b.Timeline.AppendOnce(timeline.Input{
			EventID: "mcp:" + op.OperationID + ":reused", TaskID: b.TaskID, SessionID: b.SessionID,
			Domain: timeline.DomainTool, Kind: "mcp_tool_result_reused", SourceRef: b.sourceRef(op.Tool),
			Summary: "reused completed MCP tool result without resolving secrets", DataHash: digest([]byte(decision.Result)),
		})
		return Result{Text: decision.Result, Reused: true}, nil
	}

	_, _, err = b.Timeline.AppendOnce(timeline.Input{
		EventID: "mcp:" + op.OperationID + ":started", TaskID: b.TaskID, SessionID: b.SessionID,
		Domain: timeline.DomainTool, Kind: "mcp_tool_started", SourceRef: b.sourceRef(op.Tool),
		Summary: b.startedSummary(op), DataHash: digest(args),
	})
	if err != nil {
		return Result{}, err
	}

	resolvedArgs := op.Arguments
	var secretValues []string
	if len(refs) > 0 {
		resolution, resolveErr := b.Secrets.ResolveArguments(plan, op.Arguments)
		if resolveErr != nil {
			return Result{}, b.handleKnownFailure(op, resolveErr, nil)
		}
		resolvedArgs = resolution.Arguments
		secretValues = resolution.SecretValues
	}

	callResult, err := adapter.Invoke(ctx, op.Tool, resolvedArgs)
	if err != nil {
		return Result{}, b.handleTransportFailure(op, err, secretValues)
	}
	text := secretbroker.RedactText(resultText(callResult), secretValues)
	if callResult.IsError {
		err := errors.New("MCP tool returned error: " + text)
		return Result{}, b.handleKnownFailure(op, err, secretValues)
	}
	if err := b.Journal.Complete(op.OperationID, text); err != nil {
		return Result{}, err
	}
	_, _, err = b.Timeline.AppendOnce(timeline.Input{
		EventID: "mcp:" + op.OperationID + ":completed", TaskID: b.TaskID, SessionID: b.SessionID,
		Domain: timeline.DomainTool, Kind: "mcp_tool_completed", SourceRef: b.sourceRef(op.Tool),
		Summary: "MCP tool completed", DataHash: digest([]byte(text)),
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Text: text}, nil
}

func (b *Bridge) sourceRef(tool string) string {
	if b.ServerIdentity == nil {
		return tool
	}
	return b.ServerIdentity.CanonicalRef() + "#" + tool
}

func (b *Bridge) adapter() Adapter {
	if b.Adapter != nil {
		return b.Adapter
	}
	if b.Client != nil {
		return NewCommandAdapter(b.Client)
	}
	return nil
}

func (b *Bridge) toolAllowed(tool string) bool {
	if b.Permissions != nil {
		if b.Permissions.Validate() != nil {
			return false
		}
		return b.Permissions.Allows(tool)
	}
	return b.AllowedTools[tool]
}

func (b *Bridge) handleTransportFailure(op Operation, cause error, secretValues []string) error {
	safeCause := sanitizeError(cause, secretValues)
	kind := "mcp_tool_ambiguous"
	isCancellation := errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
	if op.Policy == tooljournal.ReplayIdempotent {
		_ = b.Journal.Fail(op.OperationID, safeCause.Error())
		if isCancellation {
			kind = "mcp_tool_cancelled_retryable"
		} else {
			kind = "mcp_tool_retryable_failure"
		}
	} else if isCancellation {
		kind = "mcp_tool_cancelled_ambiguous"
	}
	_, _, _ = b.Timeline.AppendOnce(timeline.Input{
		EventID: "mcp:" + op.OperationID + ":transport-failure", TaskID: b.TaskID, SessionID: b.SessionID,
		Domain: timeline.DomainTool, Kind: kind, SourceRef: b.sourceRef(op.Tool),
		Summary: safeCause.Error(), DataHash: digest([]byte(safeCause.Error())),
	})
	return safeCause
}

func (b *Bridge) handleKnownFailure(op Operation, cause error, secretValues []string) error {
	safeCause := sanitizeError(cause, secretValues)
	_ = b.Journal.Fail(op.OperationID, safeCause.Error())
	_, _, _ = b.Timeline.AppendOnce(timeline.Input{
		EventID: "mcp:" + op.OperationID + ":known-failure", TaskID: b.TaskID, SessionID: b.SessionID,
		Domain: timeline.DomainTool, Kind: "mcp_tool_known_failure", SourceRef: b.sourceRef(op.Tool),
		Summary: safeCause.Error(), DataHash: digest([]byte(safeCause.Error())),
	})
	return safeCause
}

func (b *Bridge) recordPreflightDenial(op Operation, kind string, cause error) {
	args, _ := json.Marshal(op.Arguments)
	_, _, _ = b.Timeline.AppendOnce(timeline.Input{
		EventID: "mcp:" + op.OperationID + ":preflight-denied", TaskID: b.TaskID, SessionID: b.SessionID,
		Domain: timeline.DomainTool, Kind: kind, SourceRef: b.sourceRef(op.Tool),
		Summary: cause.Error(), DataHash: digest(args),
	})
}

func (b *Bridge) startedSummary(op Operation) string {
	if b.Schemas == nil {
		return "MCP tool operation journaled before execution"
	}
	return "MCP tool operation journaled before execution; arguments=" + b.Schemas.Summary(op.Tool, op.Arguments, 512)
}

type safeError struct {
	message string
	cause   error
}

func (e safeError) Error() string { return e.message }
func (e safeError) Unwrap() error { return e.cause }

func sanitizeError(cause error, secretValues []string) error {
	if cause == nil {
		return nil
	}
	message := secretbroker.RedactText(cause.Error(), secretValues)
	if message == cause.Error() {
		return cause
	}
	return safeError{message: message, cause: cause}
}

func resultText(result CallToolResult) string {
	parts := make([]string, 0, len(result.Content))
	for _, c := range result.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	b, _ := json.Marshal(result)
	return string(b)
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
