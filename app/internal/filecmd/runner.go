package filecmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Request struct {
	Action   string
	Path     string
	Content  string
	MaxBytes int
}

type Result struct {
	Action    string
	Path      string
	Content   string
	Entries   []string
	Written   bool
	Truncated bool
}

type Runner struct {
	baseDir         string
	defaultMaxBytes int
}

func NewRunner(baseDir string, defaultMaxBytes int) *Runner {
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	if defaultMaxBytes <= 0 {
		defaultMaxBytes = 16 * 1024
	}
	return &Runner{baseDir: baseDir, defaultMaxBytes: defaultMaxBytes}
}

func (r *Runner) Execute(ctx context.Context, req Request) (Result, error) {
	_ = ctx
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "read"
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return Result{}, errors.New("file path is required")
	}

	resolved, err := r.resolvePath(path)
	if err != nil {
		return Result{}, err
	}

	switch action {
	case "read":
		data, readErr := os.ReadFile(resolved)
		if readErr != nil {
			return Result{}, readErr
		}
		maxBytes := req.MaxBytes
		if maxBytes <= 0 {
			maxBytes = r.defaultMaxBytes
		}
		truncated := false
		if maxBytes > 0 && len(data) > maxBytes {
			data = data[:maxBytes]
			truncated = true
		}
		return Result{Action: action, Path: resolved, Content: string(data), Truncated: truncated}, nil
	case "write":
		if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
			return Result{}, err
		}
		if writeErr := os.WriteFile(resolved, []byte(req.Content), 0o644); writeErr != nil {
			return Result{}, writeErr
		}
		return Result{Action: action, Path: resolved, Written: true}, nil
	case "list":
		entries, listErr := os.ReadDir(resolved)
		if listErr != nil {
			return Result{}, listErr
		}
		items := make([]string, 0, len(entries))
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			items = append(items, name)
		}
		return Result{Action: action, Path: resolved, Entries: items}, nil
	default:
		return Result{}, fmt.Errorf("unsupported file action %q", action)
	}
}

func (r *Runner) resolvePath(path string) (string, error) {
	candidate := strings.TrimSpace(path)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(r.baseDir, candidate)
	}
	cleanBase, err := filepath.Abs(r.baseDir)
	if err != nil {
		return "", err
	}
	cleanPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if cleanPath != cleanBase && !strings.HasPrefix(cleanPath, cleanBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q is outside allowed workspace", path)
	}
	return cleanPath, nil
}
