package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"semiclaw/app/internal/promptbuilder"
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
	if !strings.Contains(system, "## Tooling") {
		system = promptbuilder.Build(promptbuilder.BuildParams{
			BasePrompt:     system,
			Mode:           promptbuilder.PromptModeFull,
			AvailableTools: allowedToolNames(state.ToolPolicy),
			MemoryEnabled:  strings.TrimSpace(state.MemoryContext) != "",
			Timezone:       state.UserTimezone,
			SkillsPrompt:   state.SkillsPrompt,
			Runtime: promptbuilder.RuntimeInfo{
				OS:       state.Runtime.OS,
				Arch:     state.Runtime.Arch,
				Shell:    state.Runtime.Shell,
				Provider: state.Runtime.Provider,
				Model:    state.Runtime.Model,
				Agent:    firstNonEmpty(state.Runtime.Agent, state.AgentName),
			},
		})
	}

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

func allowedToolNames(policy map[string]ToolPermission) []string {
	if len(policy) == 0 {
		return nil
	}
	tools := make([]string, 0, len(policy))
	for name, permission := range policy {
		if !permission.Allowed {
			continue
		}
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed == "" {
			continue
		}
		tools = append(tools, trimmed)
	}
	sort.Strings(tools)
	return tools
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseReasoningOutput(raw string) (ReasoningOutput, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ReasoningOutput{}, fmt.Errorf("empty reasoning output")
	}
	objectJSON, err := extractFirstJSONObject(trimmed)
	if err != nil {
		return ReasoningOutput{}, err
	}
	trimmed = objectJSON

	var out ReasoningOutput
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return ReasoningOutput{}, fmt.Errorf("decode reasoning json: %w", err)
	}
	out.Type = strings.ToLower(strings.TrimSpace(out.Type))
	if out.Type == "" {
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			if inferred := inferReasoningType(decoded); inferred != "" {
				out.Type = inferred
			}
		}
	}
	if out.Type == "" {
		return ReasoningOutput{}, fmt.Errorf("missing output type")
	}

	if out.Type == "tool_call" && out.ToolCall == nil {
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			if normalized := normalizeToolCall(decoded); normalized != nil {
				out.ToolCall = normalized
			}
		}
	}
	return out, nil
}

func inferReasoningType(decoded map[string]interface{}) string {
	if decoded == nil {
		return ""
	}
	if _, ok := decoded["tool_call"]; ok {
		return "tool_call"
	}
	if _, ok := decoded["action"]; ok {
		return "tool_call"
	}
	if _, ok := decoded["tool"]; ok {
		return "tool_call"
	}
	if _, ok := decoded["message"]; ok {
		return "final"
	}
	return ""
}

func normalizeToolCall(decoded map[string]interface{}) *ToolCall {
	if decoded == nil {
		return nil
	}

	tryFromMap := func(raw map[string]interface{}) *ToolCall {
		if raw == nil {
			return nil
		}
		tool := strings.TrimSpace(stringFromAny(raw["tool"]))
		if tool == "" {
			tool = strings.TrimSpace(stringFromAny(raw["name"]))
		}
		if tool == "" {
			tool = strings.TrimSpace(stringFromAny(raw["tool_name"]))
		}
		if tool == "" {
			return nil
		}

		input := mapFromAny(raw["input"])
		if len(input) == 0 {
			input = mapFromAny(raw["parameters"])
		}
		if len(input) == 0 {
			input = mapFromAny(raw["params"])
		}
		if len(input) == 0 {
			input = mapFromAny(raw["arguments"])
		}
		if len(input) == 0 {
			input = mapFromAny(raw["args"])
		}
		if input == nil {
			input = map[string]interface{}{}
		}

		return &ToolCall{
			Tool:      tool,
			Input:     input,
			Reasoning: strings.TrimSpace(stringFromAny(raw["reasoning"])),
		}
	}

	if nested, ok := decoded["tool_call"].(map[string]interface{}); ok {
		if normalized := tryFromMap(nested); normalized != nil {
			return normalized
		}
	}
	if nested, ok := decoded["action"].(map[string]interface{}); ok {
		if normalized := tryFromMap(nested); normalized != nil {
			return normalized
		}
	}
	if normalized := tryFromMap(decoded); normalized != nil {
		return normalized
	}
	return nil
}

func mapFromAny(v interface{}) map[string]interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		return typed
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
	}
	return nil
}

func stringFromAny(v interface{}) string {
	if v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func extractFirstJSONObject(raw string) (string, error) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return "", fmt.Errorf("no json object found")
	}

	inString := false
	escaped := false
	depth := 0

	for i := start; i < len(raw); i++ {
		ch := raw[i]

		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], nil
			}
		}
	}

	return "", fmt.Errorf("incomplete json object")
}
