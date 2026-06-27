package models

import "strings"

type Model struct {
	ID                  string   `json:"id"`
	UpstreamID          string   `json:"upstreamId"`
	Aliases             []string `json:"aliases"`
	DailyTokenAllowance int64    `json:"dailyTokenAllowance"`
	SupportsThinking    bool     `json:"supportsThinking"`
	SupportsTools       bool     `json:"supportsTools"`
	SupportsStreaming   bool     `json:"supportsStreaming"`
	APIFormat           string   `json:"apiFormat"`
}

var catalog = []Model{
	{ID: "glm-5.2", UpstreamID: "GLM-5.2", Aliases: []string{"glm-5.2", "GLM-5.2"}, DailyTokenAllowance: 3_000_000, SupportsThinking: true, SupportsTools: true, SupportsStreaming: true, APIFormat: "anthropic-messages"},
	{ID: "glm-5-turbo", UpstreamID: "GLM-5-Turbo", Aliases: []string{"glm-5-turbo", "glm-5turbo", "GLM-5-Turbo"}, DailyTokenAllowance: 2_000_000, SupportsThinking: true, SupportsTools: true, SupportsStreaming: true, APIFormat: "anthropic-messages"},
}

func List() []Model {
	out := make([]Model, len(catalog))
	copy(out, catalog)
	return out
}

func Resolve(value string) (Model, bool) {
	if strings.TrimSpace(value) == "" {
		return catalog[0], true
	}
	for _, model := range catalog {
		for _, alias := range append(model.Aliases, model.ID, model.UpstreamID) {
			if strings.EqualFold(strings.TrimSpace(value), alias) {
				return model, true
			}
		}
	}
	return Model{}, false
}
