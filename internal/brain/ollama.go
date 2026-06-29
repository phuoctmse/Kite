package brain

import "context"

// OllamaProvider implements Provider for local Ollama instances.
type OllamaProvider struct {
	Endpoint string // e.g. http://localhost:11434
	Model    string
}

func NewOllama(endpoint, model string) *OllamaProvider {
	return &OllamaProvider{Endpoint: endpoint, Model: model}
}

func (p *OllamaProvider) Decide(ctx context.Context, prompt Prompt) (Plan, error) {
	// TODO: build Ollama /api/chat request with tools
	// TODO: parse tool_calls from response → Plan.Actions
	return Plan{}, nil
}
