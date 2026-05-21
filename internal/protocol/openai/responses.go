package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"relay-gateway/internal/core"
	"relay-gateway/internal/protocol"
)

func NormalizeResponses(r *http.Request) (core.NormalizedRequest, error) {
	defer r.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return core.NormalizedRequest{}, err
	}

	alias, ok := body["model"].(string)
	alias = strings.TrimSpace(alias)
	if !ok || alias == "" {
		return core.NormalizedRequest{}, errors.New("model is required")
	}
	stream, _ := body["stream"].(bool)

	return core.NormalizedRequest{
		Protocol: core.ProtocolOpenAI,
		Alias:    alias,
		Stream:   stream,
		Body:     body,
		Headers:  map[string][]string(r.Header),
	}, nil
}

func BuildResponsesRequest(target core.ResolvedTarget, normalized core.NormalizedRequest) (*http.Request, error) {
	bodyCopy := make(map[string]any, len(normalized.Body))
	for key, value := range normalized.Body {
		bodyCopy[key] = value
	}
	bodyCopy["model"] = target.Mapping.UpstreamModel

	raw, err := json.Marshal(bodyCopy)
	if err != nil {
		return nil, err
	}

	url := strings.TrimRight(target.BaseURL, "/") + "/v1/responses"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+target.APIKey)
	req.Header.Set("Content-Type", "application/json")
	protocol.ApplyClientUserAgent(req, normalized.Headers)
	return req, nil
}
