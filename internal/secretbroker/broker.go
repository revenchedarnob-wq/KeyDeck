package secretbroker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

const ReferenceKey = "$secret_ref"

var (
	ErrInvalidReference = errors.New("invalid scoped secret reference")
	ErrScopeDenied      = errors.New("secret scope is not allowed for tool")
	ErrSecretNotFound   = errors.New("referenced secret is not available")
	ErrPlanMismatch     = errors.New("secret resolution plan does not match arguments")
)

// Reference identifies a secret without containing the secret value itself.
// References are safe to persist in canonical state, Tool Journal argument hashes,
// and timelines. Secret values remain inside the Broker until the adapter boundary.
type Reference struct {
	Scope string `json:"scope"`
	Name  string `json:"name"`
}

// Value returns the JSON-like representation models and tool operations should use
// instead of embedding a raw credential in arguments.
func Value(scope, name string) map[string]any {
	return map[string]any{ReferenceKey: map[string]any{"scope": scope, "name": name}}
}

type Entry struct {
	Scope string
	Name  string
	Value string
}

type Policy struct {
	ToolScopes map[string]map[string]bool
}

// Plan is a value-free authorization result. It may be persisted or hashed because
// it contains only scoped references and the digest of the unresolved arguments.
type Plan struct {
	Tool       string      `json:"tool"`
	ArgsDigest string      `json:"args_digest"`
	References []Reference `json:"references"`
}

type Resolution struct {
	Arguments    map[string]any
	SecretValues []string
	References   []Reference
}

// Broker is intentionally immutable after construction. Immutability makes a
// successful preflight plan stable until its immediate adapter-boundary resolve.
type Broker struct {
	mu              sync.Mutex
	secrets         map[string]string
	policy          Policy
	planCount       int
	resolutionCount int
}

func New(entries []Entry, policy Policy) (*Broker, error) {
	b := &Broker{secrets: map[string]string{}, policy: clonePolicy(policy)}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Scope) == "" || strings.TrimSpace(entry.Name) == "" || entry.Value == "" {
			return nil, errors.New("secret entry requires scope, name and non-empty value")
		}
		key := secretKey(entry.Scope, entry.Name)
		if _, exists := b.secrets[key]; exists {
			return nil, fmt.Errorf("duplicate secret reference %q/%q", entry.Scope, entry.Name)
		}
		b.secrets[key] = entry.Value
	}
	for tool, scopes := range b.policy.ToolScopes {
		if strings.TrimSpace(tool) == "" {
			return nil, errors.New("secret policy contains an empty tool name")
		}
		for scope, allowed := range scopes {
			if strings.TrimSpace(scope) == "" {
				return nil, fmt.Errorf("tool %q contains an empty secret scope", tool)
			}
			if !allowed {
				delete(scopes, scope)
			}
		}
	}
	return b, nil
}

// PlanArguments validates every reference, its tool/scope authorization, and the
// referenced secret's existence without returning or exposing any secret value.
func (b *Broker) PlanArguments(tool string, args map[string]any) (Plan, error) {
	if b == nil {
		return Plan{}, errors.New("secret broker is not configured")
	}
	refs, err := CollectReferences(args)
	if err != nil {
		return Plan{}, err
	}
	digest, err := digestArguments(args)
	if err != nil {
		return Plan{}, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.planCount++
	for _, ref := range refs {
		if !b.scopeAllowedLocked(tool, ref.Scope) {
			return Plan{}, fmt.Errorf("%w: tool %q scope %q", ErrScopeDenied, tool, ref.Scope)
		}
		if _, ok := b.secrets[secretKey(ref.Scope, ref.Name)]; !ok {
			return Plan{}, fmt.Errorf("%w: scope %q name %q", ErrSecretNotFound, ref.Scope, ref.Name)
		}
	}
	return Plan{Tool: tool, ArgsDigest: digest, References: refs}, nil
}

// ResolveArguments replaces authorized references with values only after the Tool
// Journal has decided the operation really needs execution. Completed replays can
// therefore return without resolving a secret a second time.
func (b *Broker) ResolveArguments(plan Plan, args map[string]any) (Resolution, error) {
	if b == nil {
		return Resolution{}, errors.New("secret broker is not configured")
	}
	digest, err := digestArguments(args)
	if err != nil {
		return Resolution{}, err
	}
	if plan.Tool == "" || plan.ArgsDigest == "" || plan.ArgsDigest != digest {
		return Resolution{}, ErrPlanMismatch
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.resolutionCount++
	resolved, values, err := b.resolveValueLocked(plan.Tool, args)
	if err != nil {
		return Resolution{}, err
	}
	resolvedMap, ok := resolved.(map[string]any)
	if !ok {
		return Resolution{}, errors.New("resolved arguments are not an object")
	}
	values = uniqueStrings(values)
	return Resolution{Arguments: resolvedMap, SecretValues: values, References: append([]Reference(nil), plan.References...)}, nil
}

func (b *Broker) Counts() (plans, resolutions int) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.planCount, b.resolutionCount
}

func (b *Broker) resolveValueLocked(tool string, value any) (any, []string, error) {
	if ref, ok, err := ParseReference(value); err != nil {
		return nil, nil, err
	} else if ok {
		if !b.scopeAllowedLocked(tool, ref.Scope) {
			return nil, nil, fmt.Errorf("%w: tool %q scope %q", ErrScopeDenied, tool, ref.Scope)
		}
		secret, exists := b.secrets[secretKey(ref.Scope, ref.Name)]
		if !exists {
			return nil, nil, fmt.Errorf("%w: scope %q name %q", ErrSecretNotFound, ref.Scope, ref.Name)
		}
		return secret, []string{secret}, nil
	}

	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		var values []string
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			resolved, nestedValues, err := b.resolveValueLocked(tool, typed[key])
			if err != nil {
				return nil, nil, err
			}
			out[key] = resolved
			values = append(values, nestedValues...)
		}
		return out, values, nil
	case []any:
		out := make([]any, len(typed))
		var values []string
		for i, item := range typed {
			resolved, nestedValues, err := b.resolveValueLocked(tool, item)
			if err != nil {
				return nil, nil, err
			}
			out[i] = resolved
			values = append(values, nestedValues...)
		}
		return out, values, nil
	default:
		return value, nil, nil
	}
}

func (b *Broker) scopeAllowedLocked(tool, scope string) bool {
	scopes := b.policy.ToolScopes[tool]
	return scopes != nil && scopes[scope]
}

func CollectReferences(value any) ([]Reference, error) {
	refs := make([]Reference, 0)
	if err := walkReferences(value, &refs); err != nil {
		return nil, err
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Scope == refs[j].Scope {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Scope < refs[j].Scope
	})
	return refs, nil
}

func walkReferences(value any, refs *[]Reference) error {
	if ref, ok, err := ParseReference(value); err != nil {
		return err
	} else if ok {
		*refs = append(*refs, ref)
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		for _, item := range typed {
			if err := walkReferences(item, refs); err != nil {
				return err
			}
		}
	case []any:
		for _, item := range typed {
			if err := walkReferences(item, refs); err != nil {
				return err
			}
		}
	}
	return nil
}

func ParseReference(value any) (Reference, bool, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return Reference{}, false, nil
	}
	raw, exists := object[ReferenceKey]
	if !exists {
		return Reference{}, false, nil
	}
	if len(object) != 1 {
		return Reference{}, true, ErrInvalidReference
	}
	payload, ok := raw.(map[string]any)
	if !ok || len(payload) != 2 {
		return Reference{}, true, ErrInvalidReference
	}
	scope, okScope := payload["scope"].(string)
	name, okName := payload["name"].(string)
	if !okScope || !okName || strings.TrimSpace(scope) == "" || strings.TrimSpace(name) == "" {
		return Reference{}, true, ErrInvalidReference
	}
	return Reference{Scope: scope, Name: name}, true, nil
}

func RedactText(text string, secretValues []string) string {
	values := uniqueStrings(secretValues)
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, "[REDACTED_SECRET]")
	}
	return text
}

func digestArguments(args map[string]any) (string, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func secretKey(scope, name string) string { return scope + "\x00" + name }

func clonePolicy(in Policy) Policy {
	out := Policy{ToolScopes: map[string]map[string]bool{}}
	for tool, scopes := range in.ToolScopes {
		copyScopes := map[string]bool{}
		for scope, allowed := range scopes {
			copyScopes[scope] = allowed
		}
		out.ToolScopes[tool] = copyScopes
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
