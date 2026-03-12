package cli

import (
	"bytes"
	"strings"
	"testing"

	"semiclaw/app/internal/hostcmd"
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

func TestConfirmHostCommand_AutoApproveStillDisplaysCommand(t *testing.T) {
	var stdout bytes.Buffer
	runner := &Runner{
		stdin:                        strings.NewReader(""),
		stdout:                       &stdout,
		allowAllHostShellPermissions: true,
	}

	approved, err := runner.confirmHostCommand("echo hello")
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
