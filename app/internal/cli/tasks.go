package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"semiclaw/app/internal/db"
	"semiclaw/app/internal/provider"
)

const tasksFileName = "tasks.md"

var (
	numberedTaskLinePattern = regexp.MustCompile(`^\d+\.\s+`)
	bulletTaskLinePattern   = regexp.MustCompile(`^[-*]\s+`)
	checkboxTaskLinePattern = regexp.MustCompile(`^- \[[ xX]\]\s+`)
)

func (r *Runner) maybeExecuteTasksFromMarkdown(
	ctx context.Context,
	activeAgent *db.AgentRecord,
	activeProviderKind string,
	message string,
) (string, bool, error) {
	if !shouldExecuteTasksFromMarkdown(message) {
		return "", false, nil
	}
	if r.hostCmd == nil {
		return "Task execution is unavailable because host command runner is not initialized.", true, nil
	}

	tasksPath, found, err := locateTasksMarkdown()
	if err != nil {
		return "", false, fmt.Errorf("locate tasks.md: %w", err)
	}
	if !found {
		return "No tasks.md found in current directory or parent directories.", true, nil
	}

	tasks, err := loadTasksFromMarkdown(tasksPath)
	if err != nil {
		return "", false, fmt.Errorf("read tasks.md: %w", err)
	}
	if len(tasks) == 0 {
		return fmt.Sprintf("No runnable tasks found in %s.", tasksPath), true, nil
	}

	providerClient := r.buildProviderClient(activeProviderKind, activeAgent)
	lines := make([]string, 0, len(tasks)*5+1)
	lines = append(lines, fmt.Sprintf("Executing %d task(s) from %s on this host.", len(tasks), tasksPath))

	for index, task := range tasks {
		taskNumber := index + 1
		command, inferErr := r.commandForTask(ctx, providerClient, task)
		if inferErr != nil {
			lines = append(lines,
				fmt.Sprintf("%d. %s", taskNumber, task),
				fmt.Sprintf("   Status: failed to infer command (%v)", inferErr),
			)
			continue
		}
		if command == "" {
			lines = append(lines,
				fmt.Sprintf("%d. %s", taskNumber, task),
				"   Status: skipped (no safe command inferred)",
			)
			continue
		}

		if requiresHostCommandApproval(command) {
			approved, approveErr := r.confirmHostCommand(command)
			if approveErr != nil {
				return "", false, fmt.Errorf("host command permission prompt failed: %w", approveErr)
			}
			if !approved {
				lines = append(lines,
					fmt.Sprintf("%d. %s", taskNumber, task),
					fmt.Sprintf("   Command: %s", command),
					"   Status: denied by user",
				)
				continue
			}
		}

		result, runErr := r.hostCmd.Execute(ctx, command)
		if runErr == nil {
			if fallbackCommand, ok := suggestFallbackCommand(result); ok {
				if fallbackResult, fallbackErr := r.hostCmd.Execute(ctx, fallbackCommand); fallbackErr == nil {
					result = fallbackResult
				}
			}
		}

		lines = append(lines, fmt.Sprintf("%d. %s", taskNumber, task), fmt.Sprintf("   Command: %s", command))
		if runErr != nil {
			lines = append(lines, fmt.Sprintf("   Status: failed (%v)", runErr))
			continue
		}

		lines = append(lines, fmt.Sprintf("   Exit: %d", result.ExitCode))
		output := firstNonEmpty(result.Stdout, result.Stderr)
		if output == "" {
			lines = append(lines, "   Output: (no output)")
			continue
		}
		lines = append(lines, fmt.Sprintf("   Output: %s", collapseWhitespace(limitForPrompt(output, 300))))
	}

	return strings.Join(lines, "\n"), true, nil
}

func (r *Runner) commandForTask(ctx context.Context, modelProvider provider.Provider, task string) (string, error) {
	fallback := fallbackCommandForTask(task)
	if modelProvider == nil {
		return fallback, nil
	}
	command, err := inferLinuxCommandIntentRequired(ctx, modelProvider, composeHostAwareIntentInput("Task from tasks.md:\n"+task))
	if err != nil || strings.TrimSpace(command) == "" {
		if fallback != "" {
			return fallback, nil
		}
		return "", err
	}
	return strings.TrimSpace(command), nil
}

func shouldExecuteTasksFromMarkdown(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	runKeywords := []string{"run", "execute", "do", "start"}
	targetKeywords := []string{"tasks.md", "tasks file", "task list", "tasks"}
	hasRunWord := false
	for _, keyword := range runKeywords {
		if strings.Contains(lower, keyword) {
			hasRunWord = true
			break
		}
	}
	if !hasRunWord {
		return false
	}
	for _, keyword := range targetKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func locateTasksMarkdown() (string, bool, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	dir := wd
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, tasksFileName)
		info, statErr := os.Stat(candidate)
		if statErr == nil && !info.IsDir() {
			return candidate, true, nil
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", false, statErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false, nil
}

func loadTasksFromMarkdown(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	tasks := make([]string, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 2048), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		task, ok := parseTaskLine(line)
		if ok {
			tasks = append(tasks, task)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func parseTaskLine(line string) (string, bool) {
	if line == "" {
		return "", false
	}
	switch {
	case numberedTaskLinePattern.MatchString(line):
		return strings.TrimSpace(numberedTaskLinePattern.ReplaceAllString(line, "")), true
	case checkboxTaskLinePattern.MatchString(line):
		return strings.TrimSpace(checkboxTaskLinePattern.ReplaceAllString(line, "")), true
	case bulletTaskLinePattern.MatchString(line):
		return strings.TrimSpace(bulletTaskLinePattern.ReplaceAllString(line, "")), true
	default:
		return "", false
	}
}

func fallbackCommandForTask(task string) string {
	lower := strings.ToLower(strings.TrimSpace(task))
	if lower == "" {
		return ""
	}
	switch {
	case strings.Contains(lower, "cpu") && strings.Contains(lower, "core"):
		return "lscpu"
	case strings.Contains(lower, "memory"):
		return "free -h"
	case strings.Contains(lower, "public ip"), strings.Contains(lower, "public address"):
		return "curl -fsS https://api.ipify.org"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean != "" {
			return clean
		}
	}
	return ""
}

func collapseWhitespace(value string) string {
	fields := strings.Fields(value)
	return strings.Join(fields, " ")
}
