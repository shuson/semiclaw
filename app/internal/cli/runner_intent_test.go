package cli

import (
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
