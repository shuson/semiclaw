package hostcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type Result struct {
	Command   string
	Stdout    string
	Stderr    string
	ExitCode  int
	Duration  time.Duration
	Truncated bool
}

type Runner struct {
	timeout    time.Duration
	maxBytes   int
	blockedRes []*regexp.Regexp
}

func NewRunner(timeout time.Duration, maxBytes int) *Runner {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	return &Runner{
		timeout:  timeout,
		maxBytes: maxBytes,
		blockedRes: []*regexp.Regexp{
			regexp.MustCompile(`(^|[;&|]\s*)sudo\s+`),
			regexp.MustCompile(`(^|[;&|]\s*)(shutdown|reboot|halt|poweroff)\b`),
			regexp.MustCompile(`(^|[;&|]\s*)rm\s+-rf\s+/`),
			regexp.MustCompile(`(^|[;&|]\s*)(mkfs|fdisk|parted)\b`),
			regexp.MustCompile(`:\(\)\{:\|:\&\};:`),
		},
	}
}

func (r *Runner) Execute(ctx context.Context, command string) (Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Result{}, errors.New("command is required")
	}
	for _, blocked := range r.blockedRes {
		if blocked.MatchString(strings.ToLower(command)) {
			return Result{}, errors.New("command blocked by safety policy")
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-lc", command)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	stdout, stdoutTruncated := truncateOutput(stdoutBuf.String(), r.maxBytes)
	stderr, stderrTruncated := truncateOutput(stderrBuf.String(), r.maxBytes)
	result := Result{
		Command:   command,
		Stdout:    strings.TrimSpace(stdout),
		Stderr:    strings.TrimSpace(stderr),
		Duration:  duration,
		Truncated: stdoutTruncated || stderrTruncated,
	}

	if runErr == nil {
		result.ExitCode = 0
		return result, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		return result, fmt.Errorf("command timed out after %s", r.timeout)
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}

	result.ExitCode = -1
	return result, runErr
}

func truncateOutput(output string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(output) <= maxBytes {
		return output, false
	}
	const suffix = "\n...[truncated]"
	if maxBytes <= len(suffix) {
		return suffix, true
	}
	return output[:maxBytes-len(suffix)] + suffix, true
}
