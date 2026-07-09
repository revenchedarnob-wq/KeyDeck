package mcpbridge

import (
	"context"
	"errors"
	"fmt"
)

// Adapter is the production seam between KeyDeck safety policy and an MCP
// transport implementation. The Tool Journal and permission model live above
// this interface so changing transports cannot change replay safety.
type Adapter interface {
	Invoke(ctx context.Context, tool string, args map[string]any) (CallToolResult, error)
}

// ServerIdentityProvider is implemented by adapters whose process/package identity
// has been explicitly bound. A Bridge configured with ServerIdentity requires this
// interface so trusted provenance cannot be attached to an unrelated adapter.
type ServerIdentityProvider interface {
	BoundServerIdentity() *ServerIdentity
}

// CommandAdapter invokes an MCP server through the local stdio wire client.
// A future official-SDK adapter can implement the same interface.
type CommandAdapter struct {
	Client   *Client
	Identity *ServerIdentity
}

func NewCommandAdapter(client *Client) *CommandAdapter { return &CommandAdapter{Client: client} }

func NewIdentifiedCommandAdapter(client *Client, identity ServerIdentity) *CommandAdapter {
	copy := identity
	return &CommandAdapter{Client: client, Identity: &copy}
}

func (a *CommandAdapter) BoundServerIdentity() *ServerIdentity {
	if a == nil || a.Identity == nil {
		return nil
	}
	copy := *a.Identity
	return &copy
}

func (a *CommandAdapter) Invoke(ctx context.Context, tool string, args map[string]any) (CallToolResult, error) {
	if a == nil || a.Client == nil {
		return CallToolResult{}, errors.New("MCP command adapter is not configured")
	}
	session, err := a.Client.Connect(ctx)
	if err != nil {
		return CallToolResult{}, contextAwareError(ctx, err)
	}
	defer session.Close()
	if err := session.Initialize(); err != nil {
		return CallToolResult{}, contextAwareError(ctx, err)
	}
	tools, err := session.ListTools()
	if err != nil {
		return CallToolResult{}, contextAwareError(ctx, err)
	}
	found := false
	for _, candidate := range tools {
		if candidate.Name == tool {
			found = true
			break
		}
	}
	if !found {
		return CallToolResult{}, fmt.Errorf("MCP server did not advertise tool %q", tool)
	}
	result, err := session.CallTool(tool, args)
	if err != nil {
		return CallToolResult{}, contextAwareError(ctx, err)
	}
	return result, nil
}

func contextAwareError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
