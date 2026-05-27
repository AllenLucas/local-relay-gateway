package gateway

import (
	"encoding/json"
	"math"

	"relay-gateway/internal/core"
)

func extractTokenUsage(body []byte) core.TokenUsage {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return core.TokenUsage{}
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		return core.TokenUsage{}
	}

	inputTokens := tokenInt(usage["input_tokens"])
	if inputTokens == 0 {
		inputTokens = tokenInt(usage["prompt_tokens"])
	}
	outputTokens := tokenInt(usage["output_tokens"])
	if outputTokens == 0 {
		outputTokens = tokenInt(usage["completion_tokens"])
	}
	totalTokens := tokenInt(usage["total_tokens"])
	if totalTokens == 0 && (inputTokens > 0 || outputTokens > 0) {
		totalTokens = inputTokens + outputTokens
	}

	return core.TokenUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}
}

func tokenInt(value any) int {
	switch v := value.(type) {
	case float64:
		if v <= 0 || math.Trunc(v) != v {
			return 0
		}
		return int(v)
	case int:
		if v <= 0 {
			return 0
		}
		return v
	case json.Number:
		parsed, err := v.Int64()
		if err != nil || parsed <= 0 {
			return 0
		}
		return int(parsed)
	default:
		return 0
	}
}
