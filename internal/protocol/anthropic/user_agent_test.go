package anthropic_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"relay-gateway/internal/core"
	"relay-gateway/internal/protocol/anthropic"
)

func TestBuildMessagesRequestForwardsClientUserAgent(t *testing.T) {
	got, present := sendBuiltMessagesRequest(t, map[string][]string{
		"User-Agent": {"claude-cli/2.1.97 (external, cli)"},
	})

	if !present {
		t.Fatal("upstream request did not include User-Agent")
	}
	want := "claude-cli/2.1.97 (external, cli)"
	if got != want {
		t.Fatalf("User-Agent = %q, want %q", got, want)
	}
}

func TestBuildMessagesRequestSuppressesDefaultUserAgentWhenClientOmittedIt(t *testing.T) {
	got, present := sendBuiltMessagesRequest(t, nil)

	if present {
		t.Fatalf("User-Agent present upstream as %q, want header omitted", got)
	}
}

func sendBuiltMessagesRequest(t *testing.T, headers map[string][]string) (string, bool) {
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

	req, err := anthropic.BuildMessagesRequest(core.ResolvedTarget{
		BaseURL: upstream.URL,
		APIKey:  "UPSTREAM_KEY",
		Mapping: core.ModelMapping{UpstreamModel: "claude-sonnet-4-5"},
	}, core.NormalizedRequest{
		Body: map[string]any{
			"model":      "claude-sonnet",
			"max_tokens": 16,
			"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
		},
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
