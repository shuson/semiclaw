package pythoncmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Result struct {
	Code      string
	Stdout    string
	Stderr    string
	ExitCode  int
	Duration  time.Duration
	Truncated bool
}

type Runner struct {
	timeout  time.Duration
	maxBytes int
}

func NewRunner(timeout time.Duration, maxBytes int) *Runner {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	return &Runner{timeout: timeout, maxBytes: maxBytes}
}

func (r *Runner) Execute(ctx context.Context, code string) (Result, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Result{}, errors.New("python code is required")
	}

	runCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "python3", "-c", code)
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
		Code:      code,
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
		return result, fmt.Errorf("python execution timed out after %s", r.timeout)
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
