package gateway

import "time"

type Event struct {
	Message string
}

type ToolPermission struct {
	Allowed             bool
	RequireUserApproval bool
}

type SessionState struct {
	SessionID     string
	AgentName     string
	SystemPrompt  string
	History       []ChatMessage
	MemoryContext string
	ToolPolicy    map[string]ToolPermission
}

type ChatMessage struct {
	Role    string
	Content string
}

type ToolCall struct {
	Tool      string                 `json:"tool"`
	Input     map[string]interface{} `json:"input"`
	Reasoning string                 `json:"reasoning,omitempty"`
}

type ReasoningOutput struct {
	Type     string    `json:"type"`
	Message  string    `json:"message,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
}

type ToolResult struct {
	Tool      string        `json:"tool"`
	Success   bool          `json:"success"`
	Output    string        `json:"output,omitempty"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	Truncated bool          `json:"truncated,omitempty"`
}

type Result struct {
	Response     string
	Actions      []ToolCall
	FeedbackLoop []ToolResult
}
