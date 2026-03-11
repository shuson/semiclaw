package hostcmd

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecuteSuccess(t *testing.T) {
	runner := NewRunner(3*time.Second, 1024)
	res, err := runner.Execute(context.Background(), "printf 'ok'")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", res.ExitCode)
	}
	if res.Stdout != "ok" {
		t.Fatalf("Stdout = %q, want ok", res.Stdout)
	}
}

func TestExecuteBlocked(t *testing.T) {
	runner := NewRunner(3*time.Second, 1024)
	_, err := runner.Execute(context.Background(), "sudo ls")
	if err == nil {
		t.Fatal("expected blocked command error")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteTimeout(t *testing.T) {
	runner := NewRunner(500*time.Millisecond, 1024)
	_, err := runner.Execute(context.Background(), "sleep 2")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected error: %v", err)
	}
}
