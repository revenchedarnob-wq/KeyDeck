package protocol

import "encoding/json"

type Usage struct {
	InputTokens              int64 `json:"input_tokens,omitempty"`
	OutputTokens             int64 `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
}

type Envelope struct {
	Output string `json:"output,omitempty"`
	Error  *struct {
		Code    string `json:"code"`
		Scope   string `json:"scope,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
	Usage Usage `json:"usage,omitempty"`
}

func DecodeEnvelope(body []byte) (Envelope, error) {
	var env Envelope
	err := json.Unmarshal(body, &env)
	return env, err
}
