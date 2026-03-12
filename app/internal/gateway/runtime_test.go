package gateway

import "testing"

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
