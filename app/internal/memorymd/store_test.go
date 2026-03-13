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

func TestUpsertAutomation_DefaultsApprovalModeAndPersistsIt(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	job := AutomationJob{
		ID:       "daily_summary",
		Name:     "Daily Summary",
		Enabled:  true,
		CronExpr: "0 18 * * *",
		TZ:       "UTC",
		Prompt:   "summarize updates",
	}
	if err := store.UpsertAutomation("agent_a", job); err != nil {
		t.Fatalf("UpsertAutomation() error = %v", err)
	}

	jobs, err := store.ListAutomations("agent_a")
	if err != nil {
		t.Fatalf("ListAutomations() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].ApprovalMode != "deny_sensitive" {
		t.Fatalf("ApprovalMode = %q, want deny_sensitive", jobs[0].ApprovalMode)
	}
	if _, err := os.Stat(filepath.Join(tmp, "cron", "agent_a", "CRON.md")); err != nil {
		t.Fatalf("expected CRON.md to be written: %v", err)
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
	if _, err := os.Stat(filepath.Join(tmp, "cron", "semiclaw", "CRON.md")); err != nil {
		t.Fatalf("expected migrated CRON.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "memory", "semiclaw", "daily", "2026-03-09.md")); err != nil {
		t.Fatalf("expected migrated daily file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "cron", "semiclaw", "2026-03-09.md")); err != nil {
		t.Fatalf("expected migrated automation run file: %v", err)
	}
}

func TestEnsureMigratesAgentScopedAutomationFilesIntoCronDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "memory", "agent_a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "memory", "agent_a", "automations.md"), []byte("# old cron"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, "cron", "agent_a", "CRON.md")); err != nil {
		t.Fatalf("expected migrated agent CRON.md: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "memory", "agent_a", "automations.md")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy automations.md to move away, stat err = %v", err)
	}
}

func TestRemoveLongTermMatching_RemovesOnlyMatchingEntries(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if err := store.AppendLongTerm("agent_a", "deploy aws ec2"); err != nil {
		t.Fatalf("AppendLongTerm() error = %v", err)
	}
	if err := store.AppendLongTerm("agent_a", "read kubernetes docs"); err != nil {
		t.Fatalf("AppendLongTerm() error = %v", err)
	}

	removed, err := store.RemoveLongTermMatching("agent_a", "aws ec2")
	if err != nil {
		t.Fatalf("RemoveLongTermMatching() error = %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}

	content, err := store.GetLongTerm("agent_a", 4000)
	if err != nil {
		t.Fatalf("GetLongTerm() error = %v", err)
	}
	if strings.Contains(strings.ToLower(content), "aws ec2") {
		t.Fatalf("expected matching entry to be removed, got %q", content)
	}
	if !strings.Contains(strings.ToLower(content), "kubernetes docs") {
		t.Fatalf("expected non-matching entry to remain, got %q", content)
	}
}

func TestRemoveLatestLongTerm_RemovesNewestEntry(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if err := store.AppendLongTerm("agent_a", "first note"); err != nil {
		t.Fatalf("AppendLongTerm() error = %v", err)
	}
	if err := store.AppendLongTerm("agent_a", "second note"); err != nil {
		t.Fatalf("AppendLongTerm() error = %v", err)
	}

	removed, ok, err := store.RemoveLatestLongTerm("agent_a")
	if err != nil {
		t.Fatalf("RemoveLatestLongTerm() error = %v", err)
	}
	if !ok {
		t.Fatal("expected latest entry to be removed")
	}
	if removed != "second note" {
		t.Fatalf("removed = %q, want second note", removed)
	}

	entries, err := store.ListLongTermEntries("agent_a", 10)
	if err != nil {
		t.Fatalf("ListLongTermEntries() error = %v", err)
	}
	if len(entries) != 1 || entries[0] != "first note" {
		t.Fatalf("entries = %#v, want only first note", entries)
	}
}

func TestListLongTermEntries_ReturnsParsedEntries(t *testing.T) {
	tmp := t.TempDir()
	store := NewStore(tmp)
	if err := store.Ensure(); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	if err := store.AppendLongTerm("agent_a", "alpha"); err != nil {
		t.Fatalf("AppendLongTerm() error = %v", err)
	}
	if err := store.AppendLongTerm("agent_a", "beta"); err != nil {
		t.Fatalf("AppendLongTerm() error = %v", err)
	}

	entries, err := store.ListLongTermEntries("agent_a", 1)
	if err != nil {
		t.Fatalf("ListLongTermEntries() error = %v", err)
	}
	if len(entries) != 1 || entries[0] != "beta" {
		t.Fatalf("entries = %#v, want [beta]", entries)
	}
}
