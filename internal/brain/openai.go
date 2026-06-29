package brain

import "context"

// OpenAIProvider implements Provider for any OpenAI-compatible /v1/chat/completions endpoint.
type OpenAIProvider struct {
	APIKey   string
	Model    string
	Endpoint string // e.g. https://api.openai.com or Azure endpoint
}

func NewOpenAI(apiKey, model, endpoint string) *OpenAIProvider {
	return &OpenAIProvider{APIKey: apiKey, Model: model, Endpoint: endpoint}
}

func (p *OpenAIProvider) Decide(ctx context.Context, prompt Prompt) (Plan, error) {
	// TODO: build OpenAI chat completion request with tools array
	// TODO: parse tool_calls from response → Plan.Actions
	return Plan{}, nil
}
