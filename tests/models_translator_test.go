package tests

import (
	"testing"

	"glm5.2proxy/internal/models"
	"glm5.2proxy/internal/openai"
	"glm5.2proxy/internal/state"
)

func TestModelsAndOpenAITranslation(t *testing.T) {
	turbo, ok := models.Resolve("glm-5turbo")
	if !ok || turbo.UpstreamID != "GLM-5-Turbo" || turbo.DailyTokenAllowance != 2_000_000 {
		t.Fatalf("unexpected model resolution: %+v", turbo)
	}
	body := map[string]any{
		"model": "glm-5-turbo",
		"messages": []any{
			map[string]any{"role": "system", "content": "system"},
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "lookup", "description": "test", "parameters": map[string]any{"type": "object"}}}},
	}
	translated := openai.ToAnthropic(body, nil, turbo, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	if translated["model"] != "GLM-5-Turbo" {
		t.Fatalf("wrong upstream model: %+v", translated)
	}
	thinking := translated["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 32000 || translated["output_config"].(map[string]any)["effort"] != "max" {
		t.Fatalf("thinking settings missing: %+v", translated)
	}
	if len(translated["tools"].([]any)) != 1 || len(translated["messages"].([]any)) != 1 {
		t.Fatalf("message/tool translation failed: %+v", translated)
	}
}

func TestTranslationKeepsThinkingParametersValidForClientTokenLimit(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model":       "glm-5.2",
		"max_tokens":  float64(8192),
		"temperature": float64(0.2),
		"top_p":       float64(0.9),
		"messages":    []any{map[string]any{"role": "user", "content": "hello"}},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	thinking := translated["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 32000 || translated["max_tokens"] != float64(40192) {
		t.Fatalf("upstream max_tokens must include requested output plus thinking budget: %+v", translated)
	}
	if _, ok := translated["temperature"]; ok {
		t.Fatalf("temperature must be omitted while thinking is enabled: %+v", translated)
	}
	if _, ok := translated["top_p"]; ok {
		t.Fatalf("top_p must be omitted while thinking is enabled: %+v", translated)
	}
}

func TestTokenRequirementUsesClientRequestedBudgetPlusEstimatedInput(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model":      "glm-5.2",
		"max_tokens": float64(500000),
		"messages": []any{
			map[string]any{"role": "system", "content": "system"},
			map[string]any{"role": "user", "content": "hello world"},
		},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 640000)
	requirement := openai.EstimateTokenRequirement(body, translated)
	if requirement.RequestedOutput != 500000 || requirement.ThinkingBudget != 32000 || requirement.UpstreamMax != 532000 {
		t.Fatalf("unexpected explicit output budget: %+v translated=%+v", requirement, translated)
	}
	if requirement.EstimatedInput <= 0 {
		t.Fatalf("input tokens should be estimated from request messages: %+v", requirement)
	}
	if requirement.Total != requirement.RequestedOutput+requirement.ThinkingBudget+requirement.EstimatedInput {
		t.Fatalf("total requirement must combine output, thinking, and input: %+v", requirement)
	}
}

func TestTokenRequirementFallsBackToTranslatedMaxTokens(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model":    "glm-5.2",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	requirement := openai.EstimateTokenRequirement(body, translated)
	if requirement.RequestedOutput != 0 || requirement.UpstreamMax != 64000 || requirement.Source != "translated_max_tokens" {
		t.Fatalf("unexpected fallback token requirement: %+v translated=%+v", requirement, translated)
	}
	if requirement.Total < requirement.UpstreamMax {
		t.Fatalf("total requirement must at least reserve translated max tokens: %+v", requirement)
	}
}

func TestTranslationAddsThinkingBudgetToSmallClientOutputLimit(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model":       "glm-5.2",
		"max_tokens":  float64(512),
		"temperature": float64(0.2),
		"messages":    []any{map[string]any{"role": "user", "content": "hello"}},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	if translated["thinking"].(map[string]any)["budget_tokens"] != 32000 || translated["max_tokens"] != float64(32512) {
		t.Fatalf("small client output limit must remain separate from thinking budget: %+v", translated)
	}
	if _, ok := translated["temperature"]; ok {
		t.Fatalf("temperature must remain omitted while thinking is enabled: %+v", translated)
	}
}

func TestTranslationCapsCombinedOutputAndThinkingAtProviderLimit(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model":      "glm-5.2",
		"max_tokens": float64(64000),
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
	}
	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{Enabled: true, BudgetTokens: 32000, Effort: "max"}, 64000)
	if translated["max_tokens"] != float64(64000) || translated["thinking"].(map[string]any)["budget_tokens"] != 32000 {
		t.Fatalf("combined token budget must respect provider limit: %+v", translated)
	}
}

func TestTranslationUsesNativeToolResultsAndGroupsConsecutiveResults(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model": "glm-5.2",
		"messages": []any{
			map[string]any{"role": "user", "content": "inspect files"},
			map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				map[string]any{"id": "call-one", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"one"}`}},
				map[string]any{"id": "call-two", "type": "function", "function": map[string]any{"name": "read_file", "arguments": `{"path":"two"}`}},
			}},
			map[string]any{"role": "tool", "tool_call_id": "call-one", "content": "first"},
			map[string]any{"role": "tool", "tool_call_id": "call-two", "content": "second"},
		},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{}, 64000)
	messages := translated["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("expected grouped user/tool-result messages, got %+v", messages)
	}
	assistantBlocks := messages[1].(map[string]any)["content"].([]any)
	if assistantBlocks[0].(map[string]any)["type"] != "tool_use" || assistantBlocks[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("assistant tool calls were not translated natively: %+v", assistantBlocks)
	}
	resultBlocks := messages[2].(map[string]any)["content"].([]any)
	if len(resultBlocks) != 2 || resultBlocks[0].(map[string]any)["type"] != "tool_result" || resultBlocks[1].(map[string]any)["tool_use_id"] != "call-two" {
		t.Fatalf("tool results were not grouped using Anthropic blocks: %+v", resultBlocks)
	}
}

func TestTranslationSanitizesToolJSONSchemaForAnthropic(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	body := map[string]any{
		"model": "glm-5.2",
		"messages": []any{
			map[string]any{"role": "user", "content": "click the button"},
		},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "browser_click",
				"description": "Click an element",
				"parameters": map[string]any{
					"type":                  "object",
					"unevaluatedProperties": false,
					"properties": map[string]any{
						"target": map[string]any{
							"type":        "string",
							"description": "Exact target selector",
							"oneOf": []any{
								map[string]any{"const": "button", "title": "Button"},
								map[string]any{"const": "link", "title": "Link"},
							},
						},
						"options": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"doubleClick": map[string]any{"type": "boolean"},
							},
						},
					},
					"required": []any{"target"},
				},
			},
		}},
	}

	translated := openai.ToAnthropic(body, nil, model, state.ThinkingSettings{}, 64000)
	tool := translated["tools"].([]any)[0].(map[string]any)
	schema := tool["input_schema"].(map[string]any)
	if _, ok := schema["unevaluatedProperties"]; ok {
		t.Fatalf("unsupported schema field leaked to upstream: %+v", schema)
	}
	if schema["additionalProperties"] != false {
		t.Fatalf("object schema must be closed for Anthropic-style tools: %+v", schema)
	}
	target := schema["properties"].(map[string]any)["target"].(map[string]any)
	if _, ok := target["oneOf"]; ok {
		t.Fatalf("oneOf should be converted before upstream request: %+v", target)
	}
	if _, ok := target["anyOf"]; !ok {
		t.Fatalf("anyOf missing after schema conversion: %+v", target)
	}
	options := schema["properties"].(map[string]any)["options"].(map[string]any)
	if options["additionalProperties"] != false {
		t.Fatalf("nested object schema must also be closed: %+v", options)
	}
}

func TestTranslationPreservesTemplateSystemWhenClientDoesNotProvideOne(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	template := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "You are ZCode", "cache_control": map[string]any{"type": "ephemeral"}},
		},
	}
	body := map[string]any{
		"model": "glm-5.2",
		"messages": []any{
			map[string]any{"role": "user", "content": "oi"},
		},
	}

	translated := openai.ToAnthropic(body, template, model, state.ThinkingSettings{}, 64000)
	system := translated["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("template system should be preserved when client has no system message: %+v", translated)
	}
	block := system[0].(map[string]any)
	if block["text"] != "You are ZCode" {
		t.Fatalf("unexpected preserved system block: %+v", block)
	}
	if _, ok := block["cache_control"]; !ok {
		t.Fatalf("template system metadata should survive translation: %+v", block)
	}
}

func TestTranslationAppendsClientSystemToTemplateSystem(t *testing.T) {
	model, _ := models.Resolve("glm-5.2")
	template := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "You are ZCode", "cache_control": map[string]any{"type": "ephemeral"}},
		},
	}
	body := map[string]any{
		"model": "glm-5.2",
		"messages": []any{
			map[string]any{"role": "system", "content": "follow the user's repo conventions"},
			map[string]any{"role": "user", "content": "oi"},
		},
	}

	translated := openai.ToAnthropic(body, template, model, state.ThinkingSettings{}, 64000)
	system := translated["system"].([]any)
	if len(system) != 2 {
		t.Fatalf("client system must be appended to the template system: %+v", translated)
	}
	if system[0].(map[string]any)["text"] != "You are ZCode" {
		t.Fatalf("template system must stay first: %+v", system)
	}
	if system[1].(map[string]any)["text"] != "follow the user's repo conventions" {
		t.Fatalf("client system must be preserved after template system: %+v", system)
	}
}
