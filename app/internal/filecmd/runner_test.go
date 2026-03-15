package filecmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteRead_AllowsApprovedAbsolutePathOutsideWorkspace(t *testing.T) {
	base := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "config.txt")
	if err := os.WriteFile(outsidePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	runner := NewRunner(base, 1024)
	result, err := runner.Execute(context.Background(), Request{
		Action: "read",
		Path:   outsidePath,
	})
	if err != nil {
		t.Fatalf("Execute(read) error = %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("Content = %q, want hello", result.Content)
	}
}

func TestExecuteWrite_StillBlocksAbsolutePathOutsideWorkspace(t *testing.T) {
	base := t.TempDir()
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "config.txt")

	runner := NewRunner(base, 1024)
	_, err := runner.Execute(context.Background(), Request{
		Action:  "write",
		Path:    outsidePath,
		Content: "hello",
	})
	if err == nil {
		t.Fatal("expected write outside workspace to fail")
	}
	if !strings.Contains(err.Error(), "outside allowed workspace") {
		t.Fatalf("error = %v, want outside allowed workspace", err)
	}
}

