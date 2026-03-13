package promptbuilder

import (
	"fmt"
	"sort"
	"strings"
)

type PromptMode string

const (
	PromptModeFull    PromptMode = "full"
	PromptModeMinimal PromptMode = "minimal"
	PromptModeNone    PromptMode = "none"
)

const defaultBasePrompt = "You are Semiclaw, a command line oriented AI agent.\nBe practical, clear, and action-oriented.\nPrefer short responses unless the user asks for details."

type RuntimeInfo struct {
	OS       string
	Arch     string
	Shell    string
	Provider string
	Model    string
	Agent    string
}

type BuildParams struct {
	BasePrompt     string
	Mode           PromptMode
	AvailableTools []string
	SkillsPrompt   string
	MemoryEnabled  bool
	Timezone       string
	Runtime        RuntimeInfo
}

func Build(params BuildParams) string {
	base := strings.TrimSpace(params.BasePrompt)
	if base == "" {
		base = defaultBasePrompt
	}

	mode := normalizeMode(params.Mode)
	if mode == PromptModeNone {
		return base
	}

	sections := make([]string, 0, 8)
	sections = append(sections, buildIdentitySection(base))
	sections = append(sections, buildToolingSection(params.AvailableTools))
	sections = append(sections, buildRuntimeSection(params.Runtime, params.Timezone))
	sections = append(sections, buildSafetySection())
	sections = append(sections, buildFormattingSection())

	if mode == PromptModeFull {
		if memory := buildMemorySection(params.MemoryEnabled); memory != "" {
			sections = append(sections, memory)
		}
		if skills := buildSkillsSection(params.SkillsPrompt); skills != "" {
			sections = append(sections, skills)
		}
	}

	return strings.Join(sections, "\n\n")
}

func normalizeMode(mode PromptMode) PromptMode {
	switch PromptMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case PromptModeMinimal:
		return PromptModeMinimal
	case PromptModeNone:
		return PromptModeNone
	default:
		return PromptModeFull
	}
}

func buildIdentitySection(base string) string {
	return "## Identity\n" + strings.TrimSpace(base)
}

func buildToolingSection(availableTools []string) string {
	tools := normalizeTools(availableTools)
	lines := []string{
		"## Tooling",
		"Output JSON only with one of these shapes:",
		`1) {"type":"final","message":"..."}`,
		`2) {"type":"tool_call","tool_call":{"tool":"shell|browser|python|file","input":{...},"reasoning":"why"}}`,
		"Never output markdown fences for tool actions.",
	}
	if len(tools) > 0 {
		lines = append(lines, "Available tools:")
		for _, tool := range tools {
			lines = append(lines, fmt.Sprintf("- %s", tool))
		}
	}
	return strings.Join(lines, "\n")
}

func buildMemorySection(enabled bool) string {
	if !enabled {
		return ""
	}
	lines := []string{
		"## Memory",
		"Before answering questions about prior decisions, preferences, or history, check memory first when available.",
		"If memory is unavailable or insufficient, state uncertainty clearly.",
	}
	return strings.Join(lines, "\n")
}

func buildSkillsSection(skillsPrompt string) string {
	skillsPrompt = strings.TrimSpace(skillsPrompt)
	if skillsPrompt == "" {
		return ""
	}
	lines := []string{
		"## Skills",
		"Check available skills before responding.",
		"If exactly one skill clearly applies, follow it.",
		"If multiple apply, choose the most specific one first.",
		skillsPrompt,
	}
	return strings.Join(lines, "\n")
}

func buildRuntimeSection(runtime RuntimeInfo, timezone string) string {
	lines := []string{"## Runtime"}
	if strings.TrimSpace(runtime.Agent) != "" {
		lines = append(lines, "- Agent: "+strings.TrimSpace(runtime.Agent))
	}
	if strings.TrimSpace(runtime.OS) != "" || strings.TrimSpace(runtime.Arch) != "" {
		osArch := strings.TrimSpace(runtime.OS)
		if strings.TrimSpace(runtime.Arch) != "" {
			if osArch != "" {
				osArch += "/"
			}
			osArch += strings.TrimSpace(runtime.Arch)
		}
		lines = append(lines, "- Platform: "+osArch)
	}
	if strings.TrimSpace(runtime.Shell) != "" {
		lines = append(lines, "- Shell: "+strings.TrimSpace(runtime.Shell))
	}
	if strings.TrimSpace(runtime.Provider) != "" {
		lines = append(lines, "- Provider: "+strings.TrimSpace(runtime.Provider))
	}
	if strings.TrimSpace(runtime.Model) != "" {
		lines = append(lines, "- Model: "+strings.TrimSpace(runtime.Model))
	}
	if strings.TrimSpace(timezone) != "" {
		lines = append(lines, "- Time zone: "+strings.TrimSpace(timezone))
	}
	return strings.Join(lines, "\n")
}

func buildSafetySection() string {
	lines := []string{
		"## Safety",
		"Never claim tool execution if it did not run.",
		"Respect permission gates for host or file mutations.",
		"When blocked or denied, report clearly and continue with best effort.",
	}
	return strings.Join(lines, "\n")
}

func buildFormattingSection() string {
	lines := []string{
		"## Formatting",
		"Keep responses concise and actionable.",
		"Use markdown only for user-facing final responses.",
	}
	return strings.Join(lines, "\n")
}

func normalizeTools(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, tool := range in {
		normalized := strings.ToLower(strings.TrimSpace(tool))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}
