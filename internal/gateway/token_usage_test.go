package gateway

import (
	"testing"

	"relay-gateway/internal/core"
)

func TestExtractTokenUsageFromUpstreamResponse(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want core.TokenUsage
	}{
		{
			name: "openai responses usage",
			body: []byte(`{"id":"resp_1","usage":{"input_tokens":12,"output_tokens":8,"total_tokens":20}}`),
			want: core.TokenUsage{InputTokens: 12, OutputTokens: 8, TotalTokens: 20},
		},
		{
			name: "openai chat completions usage",
			body: []byte(`{"id":"chatcmpl_1","usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`),
			want: core.TokenUsage{InputTokens: 11, OutputTokens: 7, TotalTokens: 18},
		},
		{
			name: "anthropic messages usage",
			body: []byte(`{"id":"msg_1","usage":{"input_tokens":9,"output_tokens":4}}`),
			want: core.TokenUsage{InputTokens: 9, OutputTokens: 4, TotalTokens: 13},
		},
		{
			name: "missing usage",
			body: []byte(`{"id":"resp_1","status":"completed"}`),
			want: core.TokenUsage{},
		},
		{
			name: "invalid json",
			body: []byte(`not-json`),
			want: core.TokenUsage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTokenUsage(tt.body)
			if got != tt.want {
				t.Fatalf("extractTokenUsage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
