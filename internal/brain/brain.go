package brain

import "context"

// Provider is the LLM backend interface. Each adapter (Anthropic, OpenAI, Ollama) implements this.
type Provider interface {
	Decide(ctx context.Context, prompt Prompt) (Plan, error)
}

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

type Turn struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

type Prompt struct {
	SystemContext  string    `json:"systemContext"`
	SnapshotJSON   []byte    `json:"snapshotJson"`
	History        []Turn    `json:"history"`
	AvailableTools []ToolDef `json:"availableTools"`
}

type ActionRequest struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params"`
}

type Plan struct {
	Actions   []ActionRequest `json:"actions"`
	Rationale string          `json:"rationale"`
}
