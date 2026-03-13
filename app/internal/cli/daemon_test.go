package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRenderLaunchdPlist_IncludesDaemonCommandAndLogs(t *testing.T) {
	spec := daemonServiceSpec{
		platform:      "darwin",
		label:         "com.semiclaw.daemon",
		executable:    "/usr/local/bin/semiclaw",
		dataDir:       "/Users/test/.semiclaw",
		stdoutLogPath: "/Users/test/.semiclaw/logs/semiclawd.log",
		stderrLogPath: "/Users/test/.semiclaw/logs/semiclawd.err.log",
	}

	got := renderDaemonServiceFile(spec)
	if !strings.Contains(got, "<string>/usr/local/bin/semiclaw</string>") {
		t.Fatalf("expected executable in plist, got %q", got)
	}
	if !strings.Contains(got, "<string>daemon</string>") || !strings.Contains(got, "<string>run</string>") {
		t.Fatalf("expected daemon run arguments in plist, got %q", got)
	}
	if !strings.Contains(got, spec.stdoutLogPath) || !strings.Contains(got, spec.stderrLogPath) {
		t.Fatalf("expected log paths in plist, got %q", got)
	}
}

func TestRenderSystemdUnit_IncludesDataDirAndExecStart(t *testing.T) {
	spec := daemonServiceSpec{
		platform:      "linux",
		serviceName:   "semiclaw.service",
		executable:    "/home/test/bin/semiclaw",
		dataDir:       "/home/test/.semiclaw",
		stdoutLogPath: "/home/test/.semiclaw/logs/semiclawd.log",
		stderrLogPath: "/home/test/.semiclaw/logs/semiclawd.err.log",
	}

	got := renderDaemonServiceFile(spec)
	if !strings.Contains(got, "ExecStart=/home/test/bin/semiclaw daemon run") {
		t.Fatalf("expected ExecStart in unit, got %q", got)
	}
	if !strings.Contains(got, "Environment=DATA_DIR=/home/test/.semiclaw") {
		t.Fatalf("expected DATA_DIR in unit, got %q", got)
	}
	if !strings.Contains(got, "StandardOutput=append:/home/test/.semiclaw/logs/semiclawd.log") {
		t.Fatalf("expected stdout log path in unit, got %q", got)
	}
}

func TestParseDaemonRuntimeStatus_ParsesPidAndStartTime(t *testing.T) {
	pid, startedAt := parseDaemonRuntimeStatus([]byte("pid: 1234\nstarted_at: 2026-03-13T00:00:00Z\n"))
	if pid != 1234 {
		t.Fatalf("pid = %d, want 1234", pid)
	}
	if startedAt != "2026-03-13T00:00:00Z" {
		t.Fatalf("startedAt = %q, want RFC3339 value", startedAt)
	}
}

func TestDaemonRuntimeStatus_DetectsCurrentProcess(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "daemon.status")
	if err := os.WriteFile(path, []byte("pid: "+strconv.Itoa(os.Getpid())+"\nstarted_at: 2026-03-13T00:00:00Z\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	running, details := daemonRuntimeStatus(path)
	if !running {
		t.Fatal("expected runtime status to be running")
	}
	if !strings.Contains(details, "manual daemon pid=") {
		t.Fatalf("details = %q, want manual daemon pid", details)
	}
}
