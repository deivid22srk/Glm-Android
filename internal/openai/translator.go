package openai

import (
	"encoding/json"
	"strings"

	"glm5.2proxy/internal/models"
	"glm5.2proxy/internal/state"
)

func ToAnthropic(body map[string]any, template map[string]any, model models.Model, thinking state.ThinkingSettings, defaultMaxTokens int) map[string]any {
	out := cloneMap(template)
	if out == nil {
		out = map[string]any{}
	}
	out["model"] = model.UpstreamID
	maxTokens := numberOr(out["max_tokens"], float64(defaultMaxTokens))
	requestedOutput, hasRequestedOutput := requestedMaxOutput(body)
	if hasRequestedOutput {
		maxTokens = requestedOutput
		if thinking.Enabled && thinking.BudgetTokens > 0 {
			maxTokens += float64(thinking.BudgetTokens)
		}
		if maxTokens > float64(defaultMaxTokens) {
			maxTokens = float64(defaultMaxTokens)
		}
	}
	out["max_tokens"] = maxTokens
	out["stream"] = true
	system, messages := convertMessages(array(body["messages"]), template != nil)
	out["messages"] = messages
	if len(system) > 0 {
		if baseSystem := array(out["system"]); len(baseSystem) > 0 {
			out["system"] = append(baseSystem, system...)
		} else {
			out["system"] = system
		}
	} else if len(array(out["system"])) == 0 {
		delete(out, "system")
	}
	tools := convertTools(array(body["tools"]))
	if len(tools) > 0 {
		out["tools"] = tools
		out["tool_choice"] = convertToolChoice(body["tool_choice"])
	} else {
		delete(out, "tools")
		delete(out, "tool_choice")
	}
	thinkingBudget := adjustedThinkingBudget(thinking, int(maxTokens))
	if thinkingBudget > 0 {
		out["thinking"] = map[string]any{"type": "enabled", "budget_tokens": thinkingBudget}
		out["output_config"] = map[string]any{"effort": thinking.Effort}
	} else {
		delete(out, "thinking")
		delete(out, "output_config")
	}
	if thinkingBudget > 0 {
		delete(out, "temperature")
		delete(out, "top_p")
	} else {
		for _, key := range []string{"temperature", "top_p"} {
			if _, ok := body[key].(float64); ok {
				out[key] = body[key]
			}
		}
	}
	switch stop := body["stop"].(type) {
	case string:
		out["stop_sequences"] = []string{stop}
	case []any:
		out["stop_sequences"] = stop
	}
	return out
}

func adjustedThinkingBudget(thinking state.ThinkingSettings, maxTokens int) int {
	if !thinking.Enabled || thinking.BudgetTokens <= 0 || maxTokens <= 1024 {
		return 0
	}
	budget := thinking.BudgetTokens
	if maximum := maxTokens - 1; budget > maximum {
		budget = maximum
	}
	if budget < 1024 {
		return 0
	}
	return budget
}

func requestedMaxOutput(body map[string]any) (float64, bool) {
	for _, key := range []string{"max_tokens", "max_completion_tokens"} {
		if value, ok := body[key].(float64); ok && value > 0 {
			return value, true
		}
	}
	return 0, false
}

func convertMessages(input []any, forceBlocks bool) ([]any, []any) {
	system := []any{}
	messages := []any{}
	for _, raw := range input {
		message := object(raw)
		role := text(message["role"])
		if role == "system" || role == "developer" {
			if value := contentText(message["content"]); value != "" {
				system = append(system, map[string]any{"type": "text", "text": value})
			}
			continue
		}
		if role == "tool" {
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": first(text(message["tool_call_id"]), randomID()),
				"content":     toolResultText(message["content"]),
			}
			messages = appendMessage(messages, "user", []any{block})
			continue
		}
		targetRole := "user"
		if role == "assistant" {
			targetRole = "assistant"
		}
		content := anthropicContent(message["content"], forceBlocks)
		if calls := array(message["tool_calls"]); len(calls) > 0 {
			blocks := []any{}
			if value := contentText(message["content"]); value != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": value})
			}
			for _, rawCall := range calls {
				call := object(rawCall)
				function := object(call["function"])
				if text(call["type"]) != "function" {
					continue
				}
				var input any = map[string]any{}
				if json.Unmarshal([]byte(text(function["arguments"])), &input) != nil {
					input = map[string]any{"_raw": text(function["arguments"])}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": first(text(call["id"]), randomID()), "name": first(text(function["name"]), "tool"), "input": input})
			}
			content = blocks
		}
		messages = appendMessage(messages, targetRole, content)
	}
	if len(messages) == 0 {
		messages = append(messages, map[string]any{"role": "user", "content": ""})
	}
	return system, messages
}

func appendMessage(messages []any, role string, content any) []any {
	if len(messages) == 0 {
		return append(messages, map[string]any{"role": role, "content": content})
	}
	last := object(messages[len(messages)-1])
	if text(last["role"]) != role {
		return append(messages, map[string]any{"role": role, "content": content})
	}
	last["content"] = append(contentBlocks(last["content"]), contentBlocks(content)...)
	return messages
}

func contentBlocks(value any) []any {
	if blocks, ok := value.([]any); ok {
		return blocks
	}
	if text, ok := value.(string); ok && text != "" {
		return []any{map[string]any{"type": "text", "text": text}}
	}
	return []any{}
}

func anthropicContent(value any, forceBlocks bool) any {
	if textValue, ok := value.(string); ok {
		if forceBlocks {
			return []any{map[string]any{"type": "text", "text": textValue}}
		}
		return textValue
	}
	parts := array(value)
	blocks := []any{}
	for _, raw := range parts {
		if value, ok := raw.(string); ok {
			blocks = append(blocks, map[string]any{"type": "text", "text": value})
			continue
		}
		part := object(raw)
		switch text(part["type"]) {
		case "text", "input_text":
			blocks = append(blocks, map[string]any{"type": "text", "text": text(part["text"])})
		case "image_url":
			image := object(part["image_url"])
			url := text(image["url"])
			if strings.HasPrefix(url, "data:") {
				if separator := strings.Index(url, ";base64,"); separator > 5 {
					blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": url[5:separator], "data": url[separator+8:]}})
				}
			}
		}
	}
	if !forceBlocks && len(blocks) == 1 && text(object(blocks[0])["type"]) == "text" {
		return text(object(blocks[0])["text"])
	}
	return blocks
}

func convertTools(input []any) []any {
	result := []any{}
	for _, raw := range input {
		tool := object(raw)
		function := object(tool["function"])
		if text(tool["type"]) != "function" || text(function["name"]) == "" {
			continue
		}
		schema := sanitizeJSONSchema(function["parameters"])
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		result = append(result, map[string]any{"name": text(function["name"]), "description": text(function["description"]), "input_schema": schema})
	}
	return result
}

func convertToolChoice(value any) any {
	if value == nil || value == "auto" {
		return map[string]any{"type": "auto"}
	}
	if value == "none" {
		return map[string]any{"type": "none"}
	}
	if value == "required" {
		return map[string]any{"type": "any"}
	}
	choice := object(value)
	function := object(choice["function"])
	if text(choice["type"]) == "function" && text(function["name"]) != "" {
		return map[string]any{"type": "tool", "name": text(function["name"])}
	}
	return map[string]any{"type": "auto"}
}

func sanitizeJSONSchema(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeJSONSchemaObject(typed)
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, sanitizeJSONSchema(item))
		}
		return result
	default:
		return value
	}
}

func sanitizeJSONSchemaObject(schema map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{
		"$ref", "$schema", "$id", "title", "description", "default", "const", "enum",
		"type", "format", "pattern", "minLength", "maxLength", "minimum", "maximum",
		"exclusiveMinimum", "exclusiveMaximum", "multipleOf",
	} {
		if value, ok := schema[key]; ok {
			out[key] = sanitizeJSONSchema(value)
		}
	}
	if value, ok := schema["anyOf"]; ok {
		out["anyOf"] = sanitizeJSONSchema(value)
	} else if value, ok := schema["oneOf"]; ok {
		out["anyOf"] = sanitizeJSONSchema(value)
	}
	if value, ok := schema["allOf"]; ok {
		out["allOf"] = sanitizeJSONSchema(value)
	}
	if value, ok := schema["not"]; ok {
		out["not"] = sanitizeJSONSchema(value)
	}
	if value, ok := schema["items"]; ok {
		out["items"] = sanitizeJSONSchema(value)
	}
	if value, ok := schema["prefixItems"]; ok {
		out["prefixItems"] = sanitizeJSONSchema(value)
	}
	if value, ok := schema["required"]; ok {
		out["required"] = sanitizeJSONSchema(value)
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		outProps := make(map[string]any, len(props))
		for name, prop := range props {
			outProps[name] = sanitizeJSONSchema(prop)
		}
		out["properties"] = outProps
	}
	if defs, ok := schema["definitions"].(map[string]any); ok {
		outDefs := make(map[string]any, len(defs))
		for name, def := range defs {
			outDefs[name] = sanitizeJSONSchema(def)
		}
		out["definitions"] = outDefs
	}
	if defs, ok := schema["$defs"].(map[string]any); ok {
		outDefs := make(map[string]any, len(defs))
		for name, def := range defs {
			outDefs[name] = sanitizeJSONSchema(def)
		}
		out["$defs"] = outDefs
	}
	if isObjectSchema(schema) {
		if _, ok := out["properties"]; !ok {
			out["properties"] = map[string]any{}
		}
		out["additionalProperties"] = false
	}
	return out
}

func isObjectSchema(schema map[string]any) bool {
	if text(schema["type"]) == "object" {
		return true
	}
	_, hasProperties := schema["properties"]
	return hasProperties
}

func contentText(value any) string {
	if textValue, ok := value.(string); ok {
		return textValue
	}
	var values []string
	for _, raw := range array(value) {
		if textValue, ok := raw.(string); ok {
			values = append(values, textValue)
			continue
		}
		part := object(raw)
		if value := text(part["text"]); value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, "\n")
}

func toolResultText(value any) string {
	if result := contentText(value); result != "" {
		return result
	}
	raw, err := json.Marshal(value)
	if err == nil && string(raw) != "null" {
		return string(raw)
	}
	return ""
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	raw, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	return result
}

func numberOr(value any, fallback float64) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	return fallback
}

func object(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func array(value any) []any {
	result, _ := value.([]any)
	return result
}

func text(value any) string {
	result, _ := value.(string)
	return result
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
