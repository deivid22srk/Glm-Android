package openai

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

type TokenRequirement struct {
	RequestedOutput int64  `json:"requestedOutput"`
	UpstreamMax     int64  `json:"upstreamMax"`
	ThinkingBudget  int64  `json:"thinkingBudget"`
	EstimatedInput  int64  `json:"estimatedInput"`
	Total           int64  `json:"total"`
	Source          string `json:"source"`
}

func EstimateTokenRequirement(original map[string]any, translated map[string]any) TokenRequirement {
	requestedOutput, source := requestedOutputTokens(original)
	upstreamMax := int64Value(translated["max_tokens"])
	thinkingBudget := int64Value(object(translated["thinking"])["budget_tokens"])
	estimatedInput := estimateInputTokens(original)

	outputBudget := upstreamMax
	if requestedOutput > 0 {
		outputBudget = requestedOutput + thinkingBudget
	}
	if outputBudget < upstreamMax {
		outputBudget = upstreamMax
	}
	total := outputBudget + estimatedInput
	return TokenRequirement{
		RequestedOutput: requestedOutput,
		UpstreamMax:     upstreamMax,
		ThinkingBudget:  thinkingBudget,
		EstimatedInput:  estimatedInput,
		Total:           total,
		Source:          source,
	}
}

func requestedOutputTokens(body map[string]any) (int64, string) {
	for _, key := range []string{"max_tokens", "max_completion_tokens"} {
		if value := int64Value(body[key]); value > 0 {
			return value, key
		}
	}
	return 0, "translated_max_tokens"
}

func estimateInputTokens(body map[string]any) int64 {
	var bytes int64
	for _, key := range []string{"messages", "tools", "tool_choice", "system"} {
		bytes += textualSize(body[key])
	}
	if bytes <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(bytes) / 4.0))
}

func textualSize(value any) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case string:
		return int64(len(typed))
	case []any:
		var total int64
		for _, item := range typed {
			total += textualSize(item)
		}
		return total
	case map[string]any:
		var total int64
		for _, item := range typed {
			total += textualSize(item)
		}
		return total
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return 0
		}
		return int64(len(raw))
	}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return result
	default:
		return 0
	}
}
