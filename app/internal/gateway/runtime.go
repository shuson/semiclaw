package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"semiclaw/app/internal/provider"
)

type AgentRuntime struct {
	provider provider.Provider
}

func NewAgentRuntime(model provider.Provider) *AgentRuntime {
	return &AgentRuntime{provider: model}
}

func (a *AgentRuntime) Reason(ctx context.Context, state SessionState, event Event, feedback []ToolResult) (ReasoningOutput, string, error) {
	messages := buildRuntimeMessages(state, event, feedback)
	raw, err := a.provider.Chat(ctx, messages)
	if err != nil {
		return ReasoningOutput{}, "", fmt.Errorf("llm reasoning: %w", err)
	}
	parsed, parseErr := parseReasoningOutput(raw)
	if parseErr != nil {
		return ReasoningOutput{Type: "final", Message: strings.TrimSpace(raw)}, raw, nil
	}
	return parsed, raw, nil
}

func buildRuntimeMessages(state SessionState, event Event, feedback []ToolResult) []provider.Message {
	system := strings.TrimSpace(state.SystemPrompt)
	if system == "" {
		system = "You are Semiclaw. Be practical, clear, and action-oriented."
	}
	system += "\n\nYou operate in a loop: event -> reasoning -> action -> feedback."
	system += "\nOutput JSON only with one of these shapes:"
	system += "\n1) {\"type\":\"final\",\"message\":\"...\"}"
	system += "\n2) {\"type\":\"tool_call\",\"tool_call\":{\"tool\":\"shell|browser|python|file\",\"input\":{...},\"reasoning\":\"why\"}}"
	system += "\nNever output markdown fences."

	conversation := make([]provider.Message, 0, len(state.History)+4)
	conversation = append(conversation, provider.Message{Role: "system", Content: system})
	for _, message := range state.History {
		conversation = append(conversation, provider.Message{Role: message.Role, Content: message.Content})
	}
	if strings.TrimSpace(state.MemoryContext) != "" {
		conversation = append(conversation, provider.Message{Role: "user", Content: "Memory context:\n" + state.MemoryContext})
	}
	conversation = append(conversation, provider.Message{Role: "user", Content: "User event:\n" + strings.TrimSpace(event.Message)})
	if len(feedback) > 0 {
		encoded, _ := json.Marshal(feedback)
		conversation = append(conversation, provider.Message{Role: "user", Content: "Tool feedback:\n" + string(encoded)})
	}
	return conversation
}

func parseReasoningOutput(raw string) (ReasoningOutput, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ReasoningOutput{}, fmt.Errorf("empty reasoning output")
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return ReasoningOutput{}, fmt.Errorf("no json object found")
	}
	trimmed = trimmed[start : end+1]

	var out ReasoningOutput
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return ReasoningOutput{}, fmt.Errorf("decode reasoning json: %w", err)
	}
	out.Type = strings.ToLower(strings.TrimSpace(out.Type))
	if out.Type == "" {
		return ReasoningOutput{}, fmt.Errorf("missing output type")
	}
	return out, nil
}
