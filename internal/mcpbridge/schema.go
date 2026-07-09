package mcpbridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"keydeck.local/feasibilitylab/internal/secretbroker"
)

var (
	ErrArgumentSchemaDenied      = errors.New("MCP tool arguments do not satisfy KeyDeck schema policy")
	ErrSecretReferenceRequired   = errors.New("MCP tool field requires a scoped secret reference")
	ErrUnexpectedSecretReference = errors.New("MCP tool field does not allow a secret reference")
)

type ValueType string

const (
	ValueAny     ValueType = "any"
	ValueString  ValueType = "string"
	ValueNumber  ValueType = "number"
	ValueBoolean ValueType = "boolean"
	ValueObject  ValueType = "object"
	ValueArray   ValueType = "array"
)

type FieldPolicy struct {
	Type            ValueType
	Required        bool
	SecretReference bool
	Sensitive       bool
	MaxStringBytes  int
	AllowedStrings  []string
}

type ArgumentSchema struct {
	Fields       map[string]FieldPolicy
	AllowUnknown bool
}

type SchemaPolicy struct {
	Tools map[string]ArgumentSchema
}

func (p SchemaPolicy) ValidateToolArguments(tool string, args map[string]any) error {
	schema, ok := p.Tools[tool]
	if !ok {
		return fmt.Errorf("%w: tool %q has no argument schema", ErrArgumentSchemaDenied, tool)
	}
	fieldNames := make([]string, 0, len(schema.Fields))
	for name := range schema.Fields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	for _, name := range fieldNames {
		field := schema.Fields[name]
		value, exists := args[name]
		if !exists {
			if field.Required {
				return fmt.Errorf("%w: required field %q is missing", ErrArgumentSchemaDenied, name)
			}
			continue
		}
		if err := validateField(name, value, field); err != nil {
			return err
		}
	}
	if !schema.AllowUnknown {
		for name := range args {
			if _, ok := schema.Fields[name]; !ok {
				return fmt.Errorf("%w: unknown field %q", ErrArgumentSchemaDenied, name)
			}
		}
	}
	return nil
}

func (p SchemaPolicy) RedactedArguments(tool string, args map[string]any) map[string]any {
	schema, ok := p.Tools[tool]
	if !ok {
		return cloneMap(args)
	}
	out := make(map[string]any, len(args))
	for name, value := range args {
		field, known := schema.Fields[name]
		if known && (field.Sensitive || field.SecretReference) {
			out[name] = "[REDACTED]"
			continue
		}
		out[name] = cloneValue(value)
	}
	return out
}

func (p SchemaPolicy) Summary(tool string, args map[string]any, maxBytes int) string {
	redacted := p.RedactedArguments(tool, args)
	raw, err := json.Marshal(redacted)
	if err != nil {
		return "MCP tool arguments validated"
	}
	text := string(raw)
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	if maxBytes < 4 {
		return "..."[:maxBytes]
	}
	trimmed := text[:maxBytes-3]
	for !utf8.ValidString(trimmed) && len(trimmed) > 0 {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed + "..."
}

func validateField(name string, value any, field FieldPolicy) error {
	ref, isRef, err := secretbroker.ParseReference(value)
	_ = ref
	if err != nil {
		return fmt.Errorf("%w: field %q contains an invalid secret reference", ErrArgumentSchemaDenied, name)
	}
	if field.SecretReference {
		if !isRef {
			return fmt.Errorf("%w: %w: field %q", ErrArgumentSchemaDenied, ErrSecretReferenceRequired, name)
		}
		return nil
	}
	if isRef {
		return fmt.Errorf("%w: %w: field %q", ErrArgumentSchemaDenied, ErrUnexpectedSecretReference, name)
	}
	refs, err := secretbroker.CollectReferences(value)
	if err != nil {
		return fmt.Errorf("%w: field %q contains an invalid nested secret reference", ErrArgumentSchemaDenied, name)
	}
	if len(refs) > 0 {
		return fmt.Errorf("%w: %w: field %q", ErrArgumentSchemaDenied, ErrUnexpectedSecretReference, name)
	}

	if field.Type != "" && field.Type != ValueAny && !matchesType(value, field.Type) {
		return fmt.Errorf("%w: field %q must be %s", ErrArgumentSchemaDenied, name, field.Type)
	}
	if field.MaxStringBytes > 0 {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: field %q must be string for max-length policy", ErrArgumentSchemaDenied, name)
		}
		if len(text) > field.MaxStringBytes {
			return fmt.Errorf("%w: field %q exceeds %d bytes", ErrArgumentSchemaDenied, name, field.MaxStringBytes)
		}
	}
	if len(field.AllowedStrings) > 0 {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: field %q must be string for allowed-value policy", ErrArgumentSchemaDenied, name)
		}
		allowed := false
		for _, candidate := range field.AllowedStrings {
			if text == candidate {
				allowed = true
				break
			}
		}
		if !allowed {
			values := append([]string(nil), field.AllowedStrings...)
			sort.Strings(values)
			return fmt.Errorf("%w: field %q must be one of %s", ErrArgumentSchemaDenied, name, strings.Join(values, ","))
		}
	}
	return nil
}

func matchesType(value any, expected ValueType) bool {
	switch expected {
	case ValueString:
		_, ok := value.(string)
		return ok
	case ValueNumber:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
			return true
		default:
			return false
		}
	case ValueBoolean:
		_, ok := value.(bool)
		return ok
	case ValueObject:
		_, ok := value.(map[string]any)
		return ok
	case ValueArray:
		_, ok := value.([]any)
		return ok
	case ValueAny, "":
		return true
	default:
		return false
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}
