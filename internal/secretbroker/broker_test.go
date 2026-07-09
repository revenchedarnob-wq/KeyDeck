package secretbroker

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanAndResolveScopedReference(t *testing.T) {
	broker, err := New([]Entry{{Scope: "provider.read", Name: "primary", Value: "secret-value-123"}}, Policy{ToolScopes: map[string]map[string]bool{"secure.fetch": {"provider.read": true}}})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"credential": Value("provider.read", "primary"), "resource": "alpha"}
	plan, err := broker.PlanArguments("secure.fetch", args)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.References) != 1 || plan.References[0].Scope != "provider.read" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	_, resolutions := broker.Counts()
	if resolutions != 0 {
		t.Fatalf("preflight must not resolve secret, got %d resolutions", resolutions)
	}
	resolved, err := broker.ResolveArguments(plan, args)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolved.Arguments["credential"]; got != "secret-value-123" {
		t.Fatalf("unexpected resolved credential %#v", got)
	}
	if len(resolved.SecretValues) != 1 || resolved.SecretValues[0] != "secret-value-123" {
		t.Fatalf("unexpected secret values %#v", resolved.SecretValues)
	}
}

func TestScopeDenialDoesNotResolve(t *testing.T) {
	broker, err := New([]Entry{{Scope: "provider.admin", Name: "primary", Value: "secret-value-123"}}, Policy{ToolScopes: map[string]map[string]bool{"secure.fetch": {"provider.read": true}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.PlanArguments("secure.fetch", map[string]any{"credential": Value("provider.admin", "primary")})
	if !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("expected ErrScopeDenied, got %v", err)
	}
	_, resolutions := broker.Counts()
	if resolutions != 0 {
		t.Fatalf("scope denial must not resolve secret, got %d", resolutions)
	}
}

func TestPlanMismatchRejected(t *testing.T) {
	broker, _ := New([]Entry{{Scope: "scope", Name: "name", Value: "value-123456"}}, Policy{ToolScopes: map[string]map[string]bool{"tool": {"scope": true}}})
	args := map[string]any{"credential": Value("scope", "name")}
	plan, err := broker.PlanArguments("tool", args)
	if err != nil {
		t.Fatal(err)
	}
	args["extra"] = "changed"
	_, err = broker.ResolveArguments(plan, args)
	if !errors.Is(err, ErrPlanMismatch) {
		t.Fatalf("expected ErrPlanMismatch, got %v", err)
	}
}

func TestRedactTextReplacesKnownSecret(t *testing.T) {
	text := RedactText("credential secret-value-123 rejected", []string{"secret-value-123"})
	if strings.Contains(text, "secret-value-123") || !strings.Contains(text, "[REDACTED_SECRET]") {
		t.Fatalf("unexpected redaction %q", text)
	}
}
