package promptbuilder

import (
	"strings"
	"testing"
)

func TestBuild_NoneMode_ReturnsBasePromptOnly(t *testing.T) {
	got := Build(BuildParams{
		Mode:       PromptModeNone,
		BasePrompt: "You are custom.",
	})

	if got != "You are custom." {
		t.Fatalf("Build() = %q, want %q", got, "You are custom.")
	}
	if strings.Contains(got, "## ") {
		t.Fatalf("none mode should not include section headings, got %q", got)
	}
}

func TestBuild_MinimalMode_ExcludesMemoryAndSkills(t *testing.T) {
	got := Build(BuildParams{
		Mode:          PromptModeMinimal,
		MemoryEnabled: true,
		SkillsPrompt:  "Use a skill if needed.",
	})

	if strings.Contains(got, "## Memory") {
		t.Fatalf("minimal mode should exclude memory section, got %q", got)
	}
	if strings.Contains(got, "## Skills") {
		t.Fatalf("minimal mode should exclude skills section, got %q", got)
	}

	required := []string{"## Identity", "## Tooling", "## Runtime", "## Safety", "## Formatting"}
	for _, token := range required {
		if !strings.Contains(got, token) {
			t.Fatalf("expected %q in minimal prompt, got %q", token, got)
		}
	}
}

func TestBuild_FullMode_IncludesOptionalSectionsWhenEnabled(t *testing.T) {
	got := Build(BuildParams{
		Mode:          PromptModeFull,
		MemoryEnabled: true,
		SkillsPrompt:  "Skill guidance.",
		AvailableTools: []string{
			"shell", "browser",
		},
	})

	required := []string{"## Memory", "## Skills", "Skill guidance.", "- browser", "- shell"}
	for _, token := range required {
		if !strings.Contains(got, token) {
			t.Fatalf("expected %q in full prompt, got %q", token, got)
		}
	}
}

func TestBuild_FullMode_SuppressesOptionalSectionsWhenDisabled(t *testing.T) {
	got := Build(BuildParams{
		Mode:          PromptModeFull,
		MemoryEnabled: false,
		SkillsPrompt:  "   ",
	})

	if strings.Contains(got, "## Memory") {
		t.Fatalf("memory section should be omitted, got %q", got)
	}
	if strings.Contains(got, "## Skills") {
		t.Fatalf("skills section should be omitted, got %q", got)
	}
}

func TestBuild_DeterministicOutput_WithUnorderedTools(t *testing.T) {
	paramsA := BuildParams{
		Mode:           PromptModeFull,
		AvailableTools: []string{"python", "shell", "browser", "shell"},
	}
	paramsB := BuildParams{
		Mode:           PromptModeFull,
		AvailableTools: []string{"shell", "browser", "python"},
	}

	a := Build(paramsA)
	b := Build(paramsB)
	if a != b {
		t.Fatalf("expected deterministic output.\na=%q\nb=%q", a, b)
	}

	if strings.Index(a, "- browser") > strings.Index(a, "- python") || strings.Index(a, "- python") > strings.Index(a, "- shell") {
		t.Fatalf("expected sorted tool ordering in output, got %q", a)
	}
}

