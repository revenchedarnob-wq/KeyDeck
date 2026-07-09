package codexapp

import (
	"encoding/json"
	"testing"
)

func TestParseThreadTokenUsageCamelCaseEnvelope(t *testing.T) {
	raw := json.RawMessage(`{"threadId":"thr","turnId":"turn","tokenUsage":{"total":{"totalTokens":120,"inputTokens":100,"cachedInputTokens":60,"outputTokens":20,"reasoningOutputTokens":4},"last":{"totalTokens":40,"inputTokens":30,"cachedInputTokens":10,"outputTokens":10,"reasoningOutputTokens":2},"modelContextWindow":272000}}`)
	u, ok := parseThreadTokenUsage(raw)
	if !ok {
		t.Fatal("expected token usage to parse")
	}
	if u.Total.InputTokens != 100 || u.Total.CachedInputTokens != 60 || u.Last.OutputTokens != 10 || u.ModelContextWindow != 272000 {
		t.Fatalf("unexpected usage: %+v", u)
	}
}

func TestParseThreadTokenUsageSnakeCase(t *testing.T) {
	raw := json.RawMessage(`{"token_usage":{"total":{"total_tokens":120,"input_tokens":100,"cached_input_tokens":60,"output_tokens":20,"reasoning_output_tokens":4},"last":{"total_tokens":40,"input_tokens":30,"cached_input_tokens":10,"output_tokens":10,"reasoning_output_tokens":2},"model_context_window":272000}}`)
	u, ok := parseThreadTokenUsage(raw)
	if !ok || u.Total.TotalTokens != 120 || u.Total.ReasoningOutputTokens != 4 {
		t.Fatalf("unexpected usage: ok=%v usage=%+v", ok, u)
	}
}

func TestMetricsFromEvents(t *testing.T) {
	events := []Notification{
		{Method: "item/completed", Params: json.RawMessage(`{"item":{"type":"commandExecution"}}`)},
		{Method: "item/completed", Params: json.RawMessage(`{"item":{"type":"fileChange"}}`)},
		{Method: "thread/tokenUsage/updated", Params: json.RawMessage(`{"tokenUsage":{"total":{"inputTokens":55,"totalTokens":60},"last":{},"modelContextWindow":1000}}`)},
	}
	m := metricsFromEvents(events)
	if m.CommandExecutions != 1 || m.FileChanges != 1 || !m.TokenUsageObserved || m.TokenUsage.Total.InputTokens != 55 {
		t.Fatalf("unexpected metrics: %+v", m)
	}
}
