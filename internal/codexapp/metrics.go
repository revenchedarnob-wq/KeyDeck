package codexapp

import (
	"encoding/json"
	"strings"
)

// TokenUsageBreakdown mirrors the stable numeric fields exposed by Codex App
// Server token-usage notifications. Fields are kept int64 because long-running
// KeyDeck sessions may exceed 32-bit counters.
type TokenUsageBreakdown struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
}

type ThreadTokenUsage struct {
	Total              TokenUsageBreakdown `json:"total"`
	Last               TokenUsageBreakdown `json:"last"`
	ModelContextWindow int64               `json:"modelContextWindow"`
}

type TurnMetrics struct {
	TokenUsage          ThreadTokenUsage  `json:"token_usage"`
	TokenUsageObserved  bool              `json:"token_usage_observed"`
	RawTokenUsageEvents []json.RawMessage `json:"raw_token_usage_events,omitempty"`
	CommandExecutions   int               `json:"command_executions"`
	FileChanges         int               `json:"file_changes"`
	MCPToolCalls        int               `json:"mcp_tool_calls"`
	WebSearches         int               `json:"web_searches"`
	DynamicToolCalls    int               `json:"dynamic_tool_calls"`
	CompletedItems      int               `json:"completed_items"`
}

func metricsFromEvents(events []Notification) TurnMetrics {
	var m TurnMetrics
	for _, note := range events {
		switch note.Method {
		case "thread/tokenUsage/updated":
			m.RawTokenUsageEvents = append(m.RawTokenUsageEvents, append(json.RawMessage(nil), note.Params...))
			if usage, ok := parseThreadTokenUsage(note.Params); ok {
				m.TokenUsage = usage
				m.TokenUsageObserved = true
			}
		case "item/completed":
			m.CompletedItems++
			var p struct {
				Item struct {
					Type string `json:"type"`
				} `json:"item"`
			}
			if json.Unmarshal(note.Params, &p) != nil {
				continue
			}
			switch p.Item.Type {
			case "commandExecution":
				m.CommandExecutions++
			case "fileChange":
				m.FileChanges++
			case "mcpToolCall":
				m.MCPToolCalls++
			case "webSearch":
				m.WebSearches++
			case "dynamicToolCall":
				m.DynamicToolCalls++
			}
		}
	}
	return m
}

// parseThreadTokenUsage accepts the current v2 camelCase notification shape and
// also tolerates snake_case wrappers. KeyDeck stores the raw payload alongside
// parsed counters, so an upstream schema change cannot silently become zero.
func parseThreadTokenUsage(raw json.RawMessage) (ThreadTokenUsage, bool) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ThreadTokenUsage{}, false
	}
	candidate := raw
	for _, key := range []string{"tokenUsage", "token_usage"} {
		if v, ok := envelope[key]; ok && len(v) > 0 {
			candidate = v
			break
		}
	}

	var canonical ThreadTokenUsage
	if err := json.Unmarshal(candidate, &canonical); err == nil && usageHasNumbers(canonical) {
		return canonical, true
	}

	// Defensive fallback for clients/servers that serialize Rust snake_case.
	var generic any
	if err := json.Unmarshal(candidate, &generic); err != nil {
		return ThreadTokenUsage{}, false
	}
	mapped := ThreadTokenUsage{
		Total:              breakdownFromAny(findMapValue(generic, "total")),
		Last:               breakdownFromAny(findMapValue(generic, "last")),
		ModelContextWindow: numberFromAny(findMapValue(generic, "modelContextWindow", "model_context_window")),
	}
	return mapped, usageHasNumbers(mapped)
}

func usageHasNumbers(u ThreadTokenUsage) bool {
	return u.ModelContextWindow > 0 ||
		u.Total.TotalTokens > 0 || u.Total.InputTokens > 0 || u.Total.CachedInputTokens > 0 || u.Total.OutputTokens > 0 || u.Total.ReasoningOutputTokens > 0 ||
		u.Last.TotalTokens > 0 || u.Last.InputTokens > 0 || u.Last.CachedInputTokens > 0 || u.Last.OutputTokens > 0 || u.Last.ReasoningOutputTokens > 0
}

func breakdownFromAny(v any) TokenUsageBreakdown {
	return TokenUsageBreakdown{
		TotalTokens:           numberFromAny(findMapValue(v, "totalTokens", "total_tokens")),
		InputTokens:           numberFromAny(findMapValue(v, "inputTokens", "input_tokens")),
		CachedInputTokens:     numberFromAny(findMapValue(v, "cachedInputTokens", "cached_input_tokens")),
		OutputTokens:          numberFromAny(findMapValue(v, "outputTokens", "output_tokens")),
		ReasoningOutputTokens: numberFromAny(findMapValue(v, "reasoningOutputTokens", "reasoning_output_tokens")),
	}
}

func findMapValue(v any, keys ...string) any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range keys {
		if value, exists := m[key]; exists {
			return value
		}
	}
	// Case-insensitive fallback is intentionally narrow and local.
	for actual, value := range m {
		for _, key := range keys {
			if strings.EqualFold(actual, key) {
				return value
			}
		}
	}
	return nil
}

func numberFromAny(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
