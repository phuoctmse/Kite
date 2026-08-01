package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider implements Provider via the Anthropic Messages API.
type AnthropicProvider struct {
	APIKey   string
	Model    string
	Endpoint string // optional; defaults to api.anthropic.com
}

func NewAnthropic(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		APIKey:   apiKey,
		Model:    model,
		Endpoint: "https://api.anthropic.com",
	}
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string         `json:"type"` // "text" | "tool_use" | "tool_result"
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
}

func (p *AnthropicProvider) Decide(ctx context.Context, prompt Prompt) (Plan, error) {
	// Build system prompt with cluster snapshot context
	systemPrompt := fmt.Sprintf("%s\n\nYou are an AI ops agent monitoring a Kubernetes cluster. "+
		"Analyze the cluster state, identify problems, and propose actions. "+
		"Use the available read-only tools (e.g. get_pods, get_logs, describe_node) to gather more "+
		"detail before proposing any write action.", prompt.SystemContext)

	// Convert available tools to Anthropic format
	tools := make([]anthropicTool, 0, len(prompt.AvailableTools))
	for _, t := range prompt.AvailableTools {
		schema := t.InputSchema
		if schema == nil {
			// Provide minimal schema if not specified
			schema = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
		tools = append(tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}

	// Build message history + current snapshot
	messages := make([]anthropicMessage, 0, len(prompt.History)+1)
	for _, turn := range prompt.History {
		messages = append(messages, anthropicMessage{
			Role: turn.Role,
			Content: []anthropicContentBlock{{
				Type: "text",
				Text: turn.Content,
			}},
		})
	}

	// Add current snapshot as user message
	snapshotText := fmt.Sprintf("Current cluster snapshot (JSON):\n```json\n%s\n```\n\nAnalyze this and propose actions if needed.",
		string(prompt.SnapshotJSON))
	messages = append(messages, anthropicMessage{
		Role: "user",
		Content: []anthropicContentBlock{{
			Type: "text",
			Text: snapshotText,
		}},
	})

	req := anthropicRequest{
		Model:     p.Model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages:  messages,
		Tools:     tools,
	}

	if req.Model == "" {
		req.Model = "claude-3-5-sonnet-20241022"
	}

	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = "https://api.anthropic.com"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Plan{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Plan{}, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Plan{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Plan{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Plan{}, fmt.Errorf("anthropic API error: %s (status %d)", string(respBody), resp.StatusCode)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return Plan{}, fmt.Errorf("unmarshal response: %w", err)
	}

	// Parse response content blocks
	var plan Plan
	var rationale string

	for _, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			rationale += block.Text + "\n"
		case "tool_use":
			plan.Actions = append(plan.Actions, ActionRequest{
				Name:   block.Name,
				Params: block.Input,
			})
		}
	}

	plan.Rationale = rationale

	return plan, nil
}
