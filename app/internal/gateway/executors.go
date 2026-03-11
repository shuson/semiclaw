package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"semiclaw/app/internal/filecmd"
	"semiclaw/app/internal/hostcmd"
	"semiclaw/app/internal/pythoncmd"
	"semiclaw/app/internal/webcrawl"
)

type Executor interface {
	Name() string
	Execute(ctx context.Context, input map[string]interface{}) (ToolResult, error)
}

type ShellExecutor struct {
	runner *hostcmd.Runner
}

func NewShellExecutor(runner *hostcmd.Runner) *ShellExecutor {
	return &ShellExecutor{runner: runner}
}

func (e *ShellExecutor) Name() string { return "shell" }

func (e *ShellExecutor) Execute(ctx context.Context, input map[string]interface{}) (ToolResult, error) {
	command := asString(input["command"])
	start := time.Now()
	if strings.TrimSpace(command) == "" {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("shell command is required")
	}
	if e.runner == nil {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("shell runner is not configured")
	}

	res, err := e.runner.Execute(ctx, command)
	toolRes := ToolResult{
		Tool:      e.Name(),
		Duration:  time.Since(start),
		Truncated: res.Truncated,
		Success:   err == nil,
		Output:    formatShellResult(res),
	}
	if err != nil {
		toolRes.Error = err.Error()
		return toolRes, nil
	}
	return toolRes, nil
}

type BrowserExecutor struct {
	fetcher *webcrawl.Fetcher
}

func NewBrowserExecutor(fetcher *webcrawl.Fetcher) *BrowserExecutor {
	return &BrowserExecutor{fetcher: fetcher}
}

func (e *BrowserExecutor) Name() string { return "browser" }

func (e *BrowserExecutor) Execute(ctx context.Context, input map[string]interface{}) (ToolResult, error) {
	url := asString(input["url"])
	start := time.Now()
	if strings.TrimSpace(url) == "" {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("url is required")
	}
	if e.fetcher == nil {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("browser fetcher is not configured")
	}

	page, err := e.fetcher.Fetch(ctx, url, asInt(input["max_chars"], 12000), asInt(input["max_links"], 20))
	if err != nil {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start), Success: false, Error: err.Error()}, nil
	}

	payload := struct {
		URL   string   `json:"url"`
		Title string   `json:"title"`
		Text  string   `json:"text"`
		Links []string `json:"links"`
	}{URL: page.URL, Title: page.Title, Text: page.Text, Links: page.Links}
	out, _ := json.Marshal(payload)
	return ToolResult{Tool: e.Name(), Duration: time.Since(start), Success: true, Output: string(out)}, nil
}

type PythonExecutor struct {
	runner *pythoncmd.Runner
}

func NewPythonExecutor(runner *pythoncmd.Runner) *PythonExecutor {
	return &PythonExecutor{runner: runner}
}

func (e *PythonExecutor) Name() string { return "python" }

func (e *PythonExecutor) Execute(ctx context.Context, input map[string]interface{}) (ToolResult, error) {
	code := asString(input["code"])
	start := time.Now()
	if strings.TrimSpace(code) == "" {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("python code is required")
	}
	if e.runner == nil {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("python runner is not configured")
	}

	res, err := e.runner.Execute(ctx, code)
	result := ToolResult{
		Tool:      e.Name(),
		Duration:  time.Since(start),
		Truncated: res.Truncated,
		Success:   err == nil,
		Output:    formatPythonResult(res),
	}
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	return result, nil
}

type FileExecutor struct {
	runner *filecmd.Runner
}

func NewFileExecutor(runner *filecmd.Runner) *FileExecutor {
	return &FileExecutor{runner: runner}
}

func (e *FileExecutor) Name() string { return "file" }

func (e *FileExecutor) Execute(ctx context.Context, input map[string]interface{}) (ToolResult, error) {
	action := strings.ToLower(strings.TrimSpace(asString(input["action"])))
	start := time.Now()
	path := asString(input["path"])
	if strings.TrimSpace(path) == "" {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("file path is required")
	}
	if e.runner == nil {
		return ToolResult{Tool: e.Name(), Duration: time.Since(start)}, errors.New("file runner is not configured")
	}

	res, err := e.runner.Execute(ctx, filecmd.Request{
		Action:   action,
		Path:     path,
		Content:  asString(input["content"]),
		MaxBytes: asInt(input["max_bytes"], 16*1024),
	})
	result := ToolResult{
		Tool:      e.Name(),
		Duration:  time.Since(start),
		Truncated: res.Truncated,
		Success:   err == nil,
		Output:    formatFileResult(res),
	}
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	return result, nil
}

func formatShellResult(result hostcmd.Result) string {
	payload := struct {
		Command  string `json:"command"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}{
		Command:  result.Command,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}
	out, _ := json.Marshal(payload)
	return string(out)
}

func formatPythonResult(result pythoncmd.Result) string {
	payload := struct {
		Code     string `json:"code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}{
		Code:     result.Code,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}
	out, _ := json.Marshal(payload)
	return string(out)
}

func formatFileResult(result filecmd.Result) string {
	payload := struct {
		Action    string   `json:"action"`
		Path      string   `json:"path"`
		Content   string   `json:"content,omitempty"`
		Entries   []string `json:"entries,omitempty"`
		Written   bool     `json:"written,omitempty"`
		Truncated bool     `json:"truncated,omitempty"`
	}{
		Action:    result.Action,
		Path:      result.Path,
		Content:   result.Content,
		Entries:   result.Entries,
		Written:   result.Written,
		Truncated: result.Truncated,
	}
	out, _ := json.Marshal(payload)
	return string(out)
}

func asString(v interface{}) string {
	switch raw := v.(type) {
	case string:
		return raw
	case fmt.Stringer:
		return raw.String()
	default:
		if raw == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
}

func asInt(v interface{}, fallback int) int {
	switch raw := v.(type) {
	case float64:
		return int(raw)
	case float32:
		return int(raw)
	case int:
		return raw
	case int64:
		return int(raw)
	case json.Number:
		i, err := raw.Int64()
		if err == nil {
			return int(i)
		}
	case string:
		parsed, err := json.Number(strings.TrimSpace(raw)).Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return fallback
}
