package brain

import "context"

// AnthropicProvider implements Provider via the Anthropic Messages API.
type AnthropicProvider struct {
	APIKey   string
	Model    string
	Endpoint string // optional; defaults to api.anthropic.com
}

func NewAnthropic(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{APIKey: apiKey, Model: model}
}

func (p *AnthropicProvider) Decide(ctx context.Context, prompt Prompt) (Plan, error) {
	// TODO: build Anthropic messages request from prompt
	// TODO: map AvailableTools → Anthropic tool definitions
	// TODO: parse tool_use content blocks → Plan.Actions
	return Plan{}, nil
}
