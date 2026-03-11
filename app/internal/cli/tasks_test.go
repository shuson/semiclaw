package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldExecuteTasksFromMarkdown(t *testing.T) {
	cases := []struct {
		message string
		want    bool
	}{
		{message: "run tasks", want: true},
		{message: "execute tasks.md now", want: true},
		{message: "please do task list", want: true},
		{message: "show tasks.md", want: false},
		{message: "hello", want: false},
	}
	for _, tc := range cases {
		got := shouldExecuteTasksFromMarkdown(tc.message)
		if got != tc.want {
			t.Fatalf("message %q -> %v, want %v", tc.message, got, tc.want)
		}
	}
}

func TestParseTaskLine(t *testing.T) {
	cases := []struct {
		line     string
		wantTask string
		wantOK   bool
	}{
		{line: "1. check cpu", wantTask: "check cpu", wantOK: true},
		{line: "- check memory", wantTask: "check memory", wantOK: true},
		{line: "- [ ] check disk", wantTask: "check disk", wantOK: true},
		{line: "plain text", wantTask: "", wantOK: false},
	}
	for _, tc := range cases {
		gotTask, gotOK := parseTaskLine(tc.line)
		if gotOK != tc.wantOK || gotTask != tc.wantTask {
			t.Fatalf("line %q -> (%q,%v), want (%q,%v)", tc.line, gotTask, gotOK, tc.wantTask, tc.wantOK)
		}
	}
}

func TestLoadTasksFromMarkdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.md")
	content := "1. check cpu\n- check memory\n- [ ] check ip\nnote\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write tasks file: %v", err)
	}

	tasks, err := loadTasksFromMarkdown(path)
	if err != nil {
		t.Fatalf("load tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("len(tasks) = %d, want 3", len(tasks))
	}
	if tasks[0] != "check cpu" || tasks[1] != "check memory" || tasks[2] != "check ip" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
}

func TestFallbackCommandForTask(t *testing.T) {
	cases := []struct {
		task string
		want string
	}{
		{task: "check host cpu type and cores", want: "lscpu"},
		{task: "check memory size", want: "free -h"},
		{task: "check my public IP", want: "curl -fsS https://api.ipify.org"},
		{task: "do something custom", want: ""},
	}
	for _, tc := range cases {
		got := fallbackCommandForTask(tc.task)
		if got != tc.want {
			t.Fatalf("task %q -> %q, want %q", tc.task, got, tc.want)
		}
	}
}
