package cli

import (
	"bytes"
	"strings"
	"testing"

	"semiclaw/app/internal/hostcmd"
	"semiclaw/app/internal/promptbuilder"
)

func TestDetectWebCrawlIntent_UsesBuiltinZaobaoChinaSource(t *testing.T) {
	url, ok := detectWebCrawlIntent("get latest 10 news in zaobao china")
	if !ok {
		t.Fatal("expected web intent to be detected")
	}
	if url != "https://www.zaobao.com/realtime/china" {
		t.Fatalf("url = %q, want https://www.zaobao.com/realtime/china", url)
	}
}

func TestDetectWebCrawlIntent_NoSourceForGenericChat(t *testing.T) {
	_, ok := detectWebCrawlIntent("hello there")
	if ok {
		t.Fatal("expected no web intent for generic chat")
	}
}

func TestInferBuiltinWebSourceURL_ZaobaoRealtimeDefault(t *testing.T) {
	url := inferBuiltinWebSourceURL("latest zaobao news")
	if url != "https://www.zaobao.com/realtime" {
		t.Fatalf("url = %q, want https://www.zaobao.com/realtime", url)
	}
}

func TestDetectWebCrawlIntentFromMemory_UsesRememberedURL(t *testing.T) {
	url, ok := detectWebCrawlIntentFromMemory(
		"crawl the saved url and summarize it",
		"remembered links:\nhttps://example.com/a\nhttps://example.com/b",
	)
	if !ok {
		t.Fatal("expected memory web intent to be detected")
	}
	if url != "https://example.com/b" {
		t.Fatalf("url = %q, want https://example.com/b", url)
	}
}

func TestDetectWebCrawlIntentFromMemory_NoMemoryRefOrWebIntent(t *testing.T) {
	_, ok := detectWebCrawlIntentFromMemory(
		"hello there",
		"https://example.com/a",
	)
	if ok {
		t.Fatal("expected no memory web intent for generic chat")
	}
}

func TestRequiresHostCommandApproval_ReadOnlyCommands(t *testing.T) {
	cases := []string{
		"cat ~/Downloads/60-cloudimg-settings.conf",
		"ls -la ~/Downloads",
		"grep -n PasswordAuthentication /etc/ssh/sshd_config",
	}
	for _, cmd := range cases {
		if requiresHostCommandApproval(cmd) {
			t.Fatalf("expected read-only command %q to skip approval", cmd)
		}
	}
}

func TestRequiresHostCommandApproval_WriteCommands(t *testing.T) {
	cases := []string{
		"sed -i 's/foo/bar/' ~/Downloads/60-cloudimg-settings.conf",
		"cat > ~/Downloads/60-cloudimg-settings.conf <<'EOF'\nfoo\nEOF",
		"echo test > /tmp/a.txt",
	}
	for _, cmd := range cases {
		if !requiresHostCommandApproval(cmd) {
			t.Fatalf("expected mutating command %q to require approval", cmd)
		}
	}
}

func TestSuggestFallbackCommand_ForBSDPsSortError(t *testing.T) {
	command, ok := suggestFallbackCommand(hostcmd.Result{
		Command:  "ps -eo pid,pcpu,pmem,comm --sort=-pcpu | head -n 11",
		Stderr:   "ps: illegal option -- -",
		ExitCode: 1,
	})
	if !ok {
		t.Fatal("expected fallback command suggestion")
	}
	if command != "ps aux | sort -nr -k 3 | head -n 11" {
		t.Fatalf("command = %q", command)
	}
}

func TestParseHostShellPermissionIntent_Enable(t *testing.T) {
	cases := []string{
		":allow-shell-all",
		"allow all shell permissions on host system",
		"please run commands without asking for permission",
	}
	for _, message := range cases {
		allowAll, handled := parseHostShellPermissionIntent(message)
		if !handled {
			t.Fatalf("expected message %q to be handled", message)
		}
		if !allowAll {
			t.Fatalf("expected message %q to enable allow-all mode", message)
		}
	}
}

func TestParseHostShellPermissionIntent_Disable(t *testing.T) {
	cases := []string{
		":ask-shell-permission",
		"disable all shell permissions",
		"do not allow all shell commands anymore",
	}
	for _, message := range cases {
		allowAll, handled := parseHostShellPermissionIntent(message)
		if !handled {
			t.Fatalf("expected message %q to be handled", message)
		}
		if allowAll {
			t.Fatalf("expected message %q to disable allow-all mode", message)
		}
	}
}

func TestDetectForgetIntent(t *testing.T) {
	cases := []struct {
		message    string
		want       string
		wantLatest bool
	}{
		{message: "forget: aws setup note", want: "aws setup note"},
		{message: "revoke: saved token", want: "saved token"},
		{message: "remove memory: terraform project", want: "terraform project"},
		{message: "revoke memory: vpn note", want: "vpn note"},
		{message: "remove aws cli memory", want: "aws cli"},
		{message: "delete old credential memory", want: "old credential"},
		{message: "revoke temp key memory", want: "temp key"},
		{message: "ok, forget it", wantLatest: true},
	}
	for _, tc := range cases {
		got, latest, ok := detectForgetIntent(tc.message)
		if !ok {
			t.Fatalf("expected forget intent for message %q", tc.message)
		}
		if got != tc.want {
			t.Fatalf("detectForgetIntent(%q) = %q, want %q", tc.message, got, tc.want)
		}
		if latest != tc.wantLatest {
			t.Fatalf("detectForgetIntent(%q) latest=%v, want %v", tc.message, latest, tc.wantLatest)
		}
	}
}

func TestDetectMemoryQueryIntent(t *testing.T) {
	cases := []string{
		"any memory",
		"any momory",
		"what do you remember",
		"show memory",
	}
	for _, message := range cases {
		if _, ok := detectMemoryQueryIntent(message); !ok {
			t.Fatalf("expected memory query intent for %q", message)
		}
	}
}

func TestConfirmHostCommand_AutoApproveStillDisplaysCommand(t *testing.T) {
	var stdout bytes.Buffer
	runner := &Runner{
		stdin:  strings.NewReader(""),
		stdout: &stdout,
		allowAllHostShellPermissions: map[string]bool{
			"owner::agent:semiclaw::session:s1": true,
		},
	}

	approved, err := runner.confirmHostCommand("owner::agent:semiclaw::session:s1", "echo hello")
	if err != nil {
		t.Fatalf("confirmHostCommand returned error: %v", err)
	}
	if !approved {
		t.Fatal("expected command to be auto-approved")
	}

	output := stdout.String()
	if !strings.Contains(output, "Host command execution requested") {
		t.Fatalf("expected execution request header in output, got %q", output)
	}
	if !strings.Contains(output, "echo hello") {
		t.Fatalf("expected command preview in output, got %q", output)
	}
	if !strings.Contains(strings.ToLower(output), "auto-approved") {
		t.Fatalf("expected auto-approved message in output, got %q", output)
	}
}

func TestConfirmHostCommand_AutoApproveIsSessionScoped(t *testing.T) {
	var stdout bytes.Buffer
	runner := &Runner{
		stdin:  strings.NewReader(""),
		stdout: &stdout,
		allowAllHostShellPermissions: map[string]bool{
			"owner::agent:semiclaw::session:s1": true,
		},
	}

	approved, err := runner.confirmHostCommand("owner::agent:semiclaw::session:s2", "echo hello")
	if err != nil {
		t.Fatalf("confirmHostCommand returned error: %v", err)
	}
	if approved {
		t.Fatal("expected different session to require confirmation")
	}
	if !strings.Contains(stdout.String(), "Non-interactive input detected") {
		t.Fatalf("expected non-interactive denial message, got %q", stdout.String())
	}
}

func TestParseChatSessionCommand(t *testing.T) {
	cases := []struct {
		message string
		want    chatSessionCommand
		ok      bool
	}{
		{message: ":session list", want: chatSessionCommand{action: "list"}, ok: true},
		{message: ":session new", want: chatSessionCommand{action: "new"}, ok: true},
		{message: ":session switch 3", want: chatSessionCommand{action: "switch", index: 3}, ok: true},
		{message: ":session delete 2", want: chatSessionCommand{action: "delete", index: 2}, ok: true},
		{message: ":session delete", want: chatSessionCommand{action: "delete"}, ok: true},
		{message: ":session nope", ok: false},
	}

	for _, tc := range cases {
		got, ok := parseChatSessionCommand(tc.message)
		if ok != tc.ok {
			t.Fatalf("parseChatSessionCommand(%q) handled=%v, want %v", tc.message, ok, tc.ok)
		}
		if got != tc.want {
			t.Fatalf("parseChatSessionCommand(%q) = %#v, want %#v", tc.message, got, tc.want)
		}
	}
}

func TestRenderMarkdownForTerminal_BasicFormatting(t *testing.T) {
	input := "# Title\n- item 1\n1. step\nUse `ls` and [docs](https://example.com)\n```\necho hi\n```"
	got := renderMarkdownForTerminal(input, cliTheme{color: false}, true)

	wantContains := []string{
		"Title",
		"• item 1",
		"1. step",
		"Use ls and docs (https://example.com)",
		"  echo hi",
	}
	for _, token := range wantContains {
		if !strings.Contains(got, token) {
			t.Fatalf("rendered output missing %q\noutput=%q", token, got)
		}
	}
}

func TestComposeSuperAdminSystemPrompt_UsesPromptBuilderSections(t *testing.T) {
	got := composeSuperAdminSystemPrompt("You are custom base.", promptbuilder.RuntimeInfo{}, "Asia/Singapore", "Use skills.", "")
	if !strings.Contains(got, "## Tooling") {
		t.Fatalf("expected tooling section in composed prompt, got %q", got)
	}
	if !strings.Contains(got, "Semiclaw Super-Admin Runtime Directive") {
		t.Fatalf("expected super-admin directive in composed prompt, got %q", got)
	}
	if !strings.Contains(got, "## Skills") {
		t.Fatalf("expected skills section in composed prompt, got %q", got)
	}
}

func TestComposeSuperAdminSystemPrompt_AddsSemiclawDomainContextForRelevantKeywords(t *testing.T) {
	got := composeSuperAdminSystemPrompt("You are custom base.", promptbuilder.RuntimeInfo{}, "Asia/Singapore", "Use skills.", "how does semiclaw MEMORY and CRON work with TOOL safety and skill routing?")
	required := []string{
		"Semiclaw Domain Reference",
		"\"MEMORY\" or \"MEMORY.md\"",
		"\"CRON\" refers to Semiclaw automation scheduling",
		"\"TOOL\" refers to Semiclaw structured tool calls",
		"\"SKILL\" refers to AGENTS.md-guided or injected skill-routing instructions",
		"\"SAFETY\" refers to Semiclaw execution constraints",
	}
	for _, token := range required {
		if !strings.Contains(got, token) {
			t.Fatalf("expected %q in composed prompt, got %q", token, got)
		}
	}
}
