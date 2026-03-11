package memorymd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentScopedLongTermMemory(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if err := store.AppendLongTerm("agent_a", "alpha note"); err != nil {
		t.Fatalf("AppendLongTerm(agent_a) error = %v", err)
	}
	if err := store.AppendLongTerm("agent_b", "beta note"); err != nil {
		t.Fatalf("AppendLongTerm(agent_b) error = %v", err)
	}

	a, err := store.GetLongTerm("agent_a", 4000)
	if err != nil {
		t.Fatalf("GetLongTerm(agent_a) error = %v", err)
	}
	if strings.Contains(a, "beta note") || !strings.Contains(a, "alpha note") {
		t.Fatalf("unexpected agent_a memory: %q", a)
	}

	b, err := store.GetLongTerm("agent_b", 4000)
	if err != nil {
		t.Fatalf("GetLongTerm(agent_b) error = %v", err)
	}
	if strings.Contains(b, "alpha note") || !strings.Contains(b, "beta note") {
		t.Fatalf("unexpected agent_b memory: %q", b)
	}
}

func TestAutomationRunsWrittenUnderCronScope(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	job := AutomationJob{ID: "daily_summary", Prompt: "summarize updates"}
	if err := store.AppendAutomationRun("agent_a", job, "success", "executed"); err != nil {
		t.Fatalf("AppendAutomationRun() error = %v", err)
	}

	day := time.Now().Format("2006-01-02")
	path := filepath.Join(tmp, "cron", "agent_a", day+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cron run file: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "daily_summary") {
		t.Fatalf("expected job id in cron run file, got: %q", content)
	}
}

func TestEnsureMigratesLegacyGlobalFiles(t *testing.T) {
	tmp := t.TempDir()
	legacyMemoryDir := filepath.Join(tmp, "memory")
	if err := os.MkdirAll(filepath.Join(legacyMemoryDir, "daily"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(legacyMemoryDir, "automation-runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyMemoryDir, "MEMORY.md"), []byte("legacy memory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyMemoryDir, "automations.md"), []byte("legacy automations"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyMemoryDir, "daily", "2026-03-09.md"), []byte("legacy daily"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyMemoryDir, "automation-runs", "2026-03-09.md"), []byte("legacy run"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, "memory", "semiclaw", "MEMORY.md")); err != nil {
		t.Fatalf("expected migrated MEMORY.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "memory", "semiclaw", "automations.md")); err != nil {
		t.Fatalf("expected migrated automations.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "memory", "semiclaw", "daily", "2026-03-09.md")); err != nil {
		t.Fatalf("expected migrated daily file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "cron", "semiclaw", "2026-03-09.md")); err != nil {
		t.Fatalf("expected migrated automation run file: %v", err)
	}
}
