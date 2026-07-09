package mcpbridge

import (
	"errors"
	"strings"
	"testing"

	"keydeck.local/feasibilitylab/internal/secretbroker"
)

func proofSchema() SchemaPolicy {
	return SchemaPolicy{Tools: map[string]ArgumentSchema{
		"secure.fetch": {
			Fields: map[string]FieldPolicy{
				"resource":   {Type: ValueString, Required: true, MaxStringBytes: 32},
				"credential": {Required: true, SecretReference: true, Sensitive: true},
			},
		},
	}}
}

func TestSchemaRequiresScopedSecretReference(t *testing.T) {
	policy := proofSchema()
	err := policy.ValidateToolArguments("secure.fetch", map[string]any{"resource": "alpha", "credential": "raw-secret-value"})
	if !errors.Is(err, ErrSecretReferenceRequired) {
		t.Fatalf("expected ErrSecretReferenceRequired, got %v", err)
	}
}

func TestSchemaRejectsUnknownAndWrongType(t *testing.T) {
	policy := proofSchema()
	ref := secretbroker.Value("provider.read", "primary")
	if err := policy.ValidateToolArguments("secure.fetch", map[string]any{"resource": true, "credential": ref}); !errors.Is(err, ErrArgumentSchemaDenied) {
		t.Fatalf("expected schema denial for type, got %v", err)
	}
	if err := policy.ValidateToolArguments("secure.fetch", map[string]any{"resource": "alpha", "credential": ref, "extra": "no"}); !errors.Is(err, ErrArgumentSchemaDenied) {
		t.Fatalf("expected schema denial for unknown field, got %v", err)
	}
}

func TestSchemaRedactsSensitiveFieldsFromSummary(t *testing.T) {
	policy := proofSchema()
	summary := policy.Summary("secure.fetch", map[string]any{"resource": "alpha", "credential": secretbroker.Value("provider.read", "primary")}, 512)
	if strings.Contains(summary, "provider.read") || !strings.Contains(summary, "[REDACTED]") {
		t.Fatalf("unexpected summary %q", summary)
	}
}

func TestSchemaValidationOrderIsDeterministicAndSpecificErrorsRemainSchemaDenials(t *testing.T) {
	policy := SchemaPolicy{Tools: map[string]ArgumentSchema{"tool": {Fields: map[string]FieldPolicy{
		"resource":   {Type: ValueString, Required: true},
		"credential": {Required: true, SecretReference: true},
	}}}}
	args := map[string]any{"resource": true, "credential": "raw-value"}
	var first string
	for i := 0; i < 100; i++ {
		err := policy.ValidateToolArguments("tool", args)
		if !errors.Is(err, ErrArgumentSchemaDenied) || !errors.Is(err, ErrSecretReferenceRequired) {
			t.Fatalf("error does not preserve both schema and specific classification: %v", err)
		}
		if i == 0 {
			first = err.Error()
		} else if err.Error() != first {
			t.Fatalf("nondeterministic validation error: first=%q now=%q", first, err.Error())
		}
	}
}
