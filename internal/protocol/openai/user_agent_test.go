package openai_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"relay-gateway/internal/core"
	"relay-gateway/internal/protocol/openai"
)

func TestBuildOpenAIRequestsForwardClientUserAgent(t *testing.T) {
	builders := map[string]func(core.ResolvedTarget, core.NormalizedRequest) (*http.Request, error){
		"responses":        openai.BuildResponsesRequest,
		"chat completions": openai.BuildChatCompletionsRequest,
	}

	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			got, present := sendBuiltOpenAIRequest(t, build, map[string][]string{
				"User-Agent": {"codex-tui/0.130.0 (Windows 10.0.26200; x86_64) WindowsTerminal (codex-tui; 0.130.0)"},
			})

			if !present {
				t.Fatal("upstream request did not include User-Agent")
			}
			want := "codex-tui/0.130.0 (Windows 10.0.26200; x86_64) WindowsTerminal (codex-tui; 0.130.0)"
			if got != want {
				t.Fatalf("User-Agent = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildOpenAIRequestsSuppressDefaultUserAgentWhenClientOmittedIt(t *testing.T) {
	builders := map[string]func(core.ResolvedTarget, core.NormalizedRequest) (*http.Request, error){
		"responses":        openai.BuildResponsesRequest,
		"chat completions": openai.BuildChatCompletionsRequest,
	}

	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			got, present := sendBuiltOpenAIRequest(t, build, nil)

			if present {
				t.Fatalf("User-Agent present upstream as %q, want header omitted", got)
			}
		})
	}
}

func sendBuiltOpenAIRequest(
	t *testing.T,
	build func(core.ResolvedTarget, core.NormalizedRequest) (*http.Request, error),
	headers map[string][]string,
) (string, bool) {
	t.Helper()

	var got string
	var present bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values, ok := r.Header["User-Agent"]
		present = ok
		if len(values) > 0 {
			got = values[0]
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	req, err := build(core.ResolvedTarget{
		BaseURL: upstream.URL,
		APIKey:  "UPSTREAM_KEY",
		Mapping: core.ModelMapping{UpstreamModel: "gpt-5.1"},
	}, core.NormalizedRequest{
		Body:    map[string]any{"model": "gpt-5"},
		Headers: headers,
	})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return got, present
}
