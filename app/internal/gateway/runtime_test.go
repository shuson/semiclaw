package gateway

import (
	"strings"
	"testing"
)

func TestParseReasoningOutput_NormalizesTopLevelToolCallShape(t *testing.T) {
	raw := `{"type":"tool_call","tool":"shell","input":{"command":"df -h"},"reasoning":"check disk"}`
	out, err := parseReasoningOutput(raw)
	if err != nil {
		t.Fatalf("parseReasoningOutput returned error: %v", err)
	}
	if out.Type != "tool_call" {
		t.Fatalf("Type = %q, want tool_call", out.Type)
	}
	if out.ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if out.ToolCall.Tool != "shell" {
		t.Fatalf("ToolCall.Tool = %q, want shell", out.ToolCall.Tool)
	}
	if got := out.ToolCall.Input["command"]; got != "df -h" {
		t.Fatalf("ToolCall.Input[command] = %v, want df -h", got)
	}
}

func TestParseReasoningOutput_NormalizesActionShape(t *testing.T) {
	raw := `{"type":"tool_call","action":{"name":"shell","parameters":{"command":"df -h"}}}`
	out, err := parseReasoningOutput(raw)
	if err != nil {
		t.Fatalf("parseReasoningOutput returned error: %v", err)
	}
	if out.ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if out.ToolCall.Tool != "shell" {
		t.Fatalf("ToolCall.Tool = %q, want shell", out.ToolCall.Tool)
	}
	if got := out.ToolCall.Input["command"]; got != "df -h" {
		t.Fatalf("ToolCall.Input[command] = %v, want df -h", got)
	}
}

func TestParseReasoningOutput_InfersToolCallTypeWhenMissing(t *testing.T) {
	raw := `{"tool":"shell","input":{"command":"df -h"}}`
	out, err := parseReasoningOutput(raw)
	if err != nil {
		t.Fatalf("parseReasoningOutput returned error: %v", err)
	}
	if out.Type != "tool_call" {
		t.Fatalf("Type = %q, want tool_call", out.Type)
	}
	if out.ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
}

func TestParseReasoningOutput_UsesFirstJSONWhenMultipleObjectsReturned(t *testing.T) {
	raw := `{"type":"tool_call","tool":"shell","input":{"command":"which aws"}}
{"type":"tool_call","tool":"shell","input":{"command":"aws --version"}}`

	out, err := parseReasoningOutput(raw)
	if err != nil {
		t.Fatalf("parseReasoningOutput returned error: %v", err)
	}
	if out.Type != "tool_call" {
		t.Fatalf("Type = %q, want tool_call", out.Type)
	}
	if out.ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if out.ToolCall.Tool != "shell" {
		t.Fatalf("ToolCall.Tool = %q, want shell", out.ToolCall.Tool)
	}
	if got := out.ToolCall.Input["command"]; got != "which aws" {
		t.Fatalf("ToolCall.Input[command] = %v, want which aws", got)
	}
}

func TestAllowedToolNames_SortedAndAllowedOnly(t *testing.T) {
	got := allowedToolNames(map[string]ToolPermission{
		"python":  {Allowed: true},
		"browser": {Allowed: true},
		"shell":   {Allowed: true},
		"file":    {Allowed: false},
	})
	want := []string{"browser", "python", "shell"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildRuntimeMessages_UsesPromptBuilderToolingSection(t *testing.T) {
	messages := buildRuntimeMessages(SessionState{
		AgentName:    "semiclaw",
		SystemPrompt: "You are Semiclaw.",
		SkillsPrompt: "Use skill routing.",
		UserTimezone: "Asia/Singapore",
		Runtime: RuntimeMetadata{
			OS:       "darwin",
			Arch:     "arm64",
			Shell:    "/bin/zsh",
			Provider: "openai",
			Model:    "gpt-test",
			Agent:    "semiclaw",
		},
		ToolPolicy: map[string]ToolPermission{
			"shell":   {Allowed: true},
			"python":  {Allowed: true},
			"browser": {Allowed: true},
		},
	}, Event{Message: "hello"}, nil)

	if len(messages) == 0 {
		t.Fatal("expected at least one message")
	}
	system := messages[0].Content
	if !strings.Contains(system, "## Tooling") {
		t.Fatalf("expected tooling section in system prompt, got %q", system)
	}
	if !strings.Contains(system, "- browser") || !strings.Contains(system, "- python") || !strings.Contains(system, "- shell") {
		t.Fatalf("expected allowed tools listed in system prompt, got %q", system)
	}
	if !strings.Contains(system, "## Skills") || !strings.Contains(system, "Use skill routing.") {
		t.Fatalf("expected skills section in system prompt, got %q", system)
	}
	if !strings.Contains(system, "## Runtime") || !strings.Contains(system, "darwin/arm64") {
		t.Fatalf("expected runtime metadata in system prompt, got %q", system)
	}
	if !strings.Contains(system, "Time zone: Asia/Singapore") {
		t.Fatalf("expected timezone in system prompt, got %q", system)
	}
}

func TestRequestUserMessageForStorage_PrefersOriginalInput(t *testing.T) {
	req := Request{
		Event:             Event{Message: "Long-term memory context:\n...\n\nUser message:\nhello"},
		OriginalUserInput: "hello",
	}
	if got := req.userMessageForStorage(); got != "hello" {
		t.Fatalf("userMessageForStorage() = %q, want hello", got)
	}
}

func TestRequestUserMessageForStorage_FallsBackToEventMessage(t *testing.T) {
	req := Request{
		Event: Event{Message: "hello"},
	}
	if got := req.userMessageForStorage(); got != "hello" {
		t.Fatalf("userMessageForStorage() = %q, want hello", got)
	}
}
