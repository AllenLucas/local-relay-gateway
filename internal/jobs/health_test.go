package jobs

import (
	"errors"
	"net/http"
	"testing"

	"relay-gateway/internal/core"
)

func TestHealthURLUsesModelsEndpoint(t *testing.T) {
	got := healthURL(core.Station{
		OpenAIBaseURL: "https://station-a.example.com/openai/",
		OpenAIAPIKey:  "OPENAI_A",
	})
	want := "https://station-a.example.com/openai/models"
	if got != want {
		t.Fatalf("healthURL() = %q, want %q", got, want)
	}
}

func TestHealthURLUsesAnthropicMessagesEndpointWhenOpenAIIsNotConfigured(t *testing.T) {
	got := healthURL(core.Station{
		AnthropicBaseURL: "https://station-b.example.com/anthropic/",
		AnthropicAPIKey:  "ANTHROPIC_B",
	})
	want := "https://station-b.example.com/anthropic/v1/messages"
	if got != want {
		t.Fatalf("healthURL() = %q, want %q", got, want)
	}
}

func TestHealthCheckFailedOnlyTreatsServerErrorsAsFailures(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		resp *http.Response
		want bool
	}{
		{
			name: "transport error",
			err:  errors.New("dial tcp timeout"),
			want: true,
		},
		{
			name: "404 counts as success",
			resp: &http.Response{StatusCode: http.StatusNotFound},
			want: false,
		},
		{
			name: "429 counts as success",
			resp: &http.Response{StatusCode: http.StatusTooManyRequests},
			want: false,
		},
		{
			name: "500 counts as failure",
			resp: &http.Response{StatusCode: http.StatusInternalServerError},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := healthCheckFailed(tc.err, tc.resp)
			if got != tc.want {
				t.Fatalf("healthCheckFailed() = %t, want %t", got, tc.want)
			}
		})
	}
}
