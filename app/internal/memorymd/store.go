package memorymd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AutomationJob struct {
	ID        string
	Name      string
	Enabled   bool
	CronExpr  string
	TZ        string
	Prompt    string
	NextRunAt time.Time
	LastRunAt time.Time
	UpdatedAt time.Time
}

type Store struct {
	memoryRootDir string
	cronRootDir   string
	mu            sync.Mutex
}

func NewStore(dataDir string) *Store {
	return &Store{
		memoryRootDir: filepath.Join(dataDir, "memory"),
		cronRootDir:   filepath.Join(dataDir, "cron"),
	}
}

func (s *Store) Ensure() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.memoryRootDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(s.cronRootDir, 0o700); err != nil {
		return err
	}
	if err := s.migrateLegacyLayoutUnlocked(); err != nil {
		return err
	}
	return nil
}

func (s *Store) AppendLongTerm(agentName string, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	note = strings.TrimSpace(note)
	if note == "" {
		return nil
	}
	agentName = sanitizeAgentScope(agentName)
	if err := s.ensureAgentMemoryLayoutUnlocked(agentName); err != nil {
		return err
	}
	path := s.memoryFilePath(agentName)
	line := fmt.Sprintf("- [%s] %s\n", time.Now().UTC().Format(time.RFC3339), note)
	return appendLine(path, line)
}

func (s *Store) GetLongTerm(agentName string, maxChars int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxChars <= 0 {
		maxChars = 2500
	}
	agentName = sanitizeAgentScope(agentName)
	path := s.memoryFilePath(agentName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	text := strings.TrimSpace(string(raw))
	if len(text) > maxChars {
		return text[len(text)-maxChars:], nil
	}
	return text, nil
}

func (s *Store) AppendDaily(agentName string, section string, line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentName = sanitizeAgentScope(agentName)
	if err := s.ensureAgentMemoryLayoutUnlocked(agentName); err != nil {
		return err
	}

	section = strings.TrimSpace(section)
	if section == "" {
		section = "general"
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	day := time.Now().Format("2006-01-02")
	path := s.dailyFilePath(agentName, day)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		header := fmt.Sprintf("# Daily Memory %s (%s)\n\n", day, agentName)
		if err := os.WriteFile(path, []byte(header), 0o600); err != nil {
			return err
		}
	}

	entry := fmt.Sprintf("## %s\n- [%s] %s\n", section, time.Now().UTC().Format(time.RFC3339), line)
	return appendLine(path, entry)
}

func (s *Store) UpsertAutomation(agentName string, job AutomationJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentName = sanitizeAgentScope(agentName)
	if err := s.ensureAgentMemoryLayoutUnlocked(agentName); err != nil {
		return err
	}

	if strings.TrimSpace(job.ID) == "" {
		return fmt.Errorf("automation id is required")
	}
	if strings.TrimSpace(job.Name) == "" {
		job.Name = job.ID
	}
	if strings.TrimSpace(job.CronExpr) == "" {
		return fmt.Errorf("cron expression is required")
	}
	if strings.TrimSpace(job.TZ) == "" {
		job.TZ = "UTC"
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = time.Now().UTC()
	}

	jobs, err := s.loadAutomationsUnlocked(agentName)
	if err != nil {
		return err
	}

	existing, ok := jobs[job.ID]
	if ok {
		if job.LastRunAt.IsZero() {
			job.LastRunAt = existing.LastRunAt
		}
		if job.NextRunAt.IsZero() {
			job.NextRunAt = existing.NextRunAt
		}
	}
	if job.NextRunAt.IsZero() {
		next, err := NextRun(job.CronExpr, job.TZ, time.Now().UTC())
		if err != nil {
			return err
		}
		job.NextRunAt = next
	}
	jobs[job.ID] = job
	return s.saveAutomationsUnlocked(agentName, jobs)
}

func (s *Store) ListAutomationScopes() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.memoryRootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	scopes := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentName := entry.Name()
		if _, err := os.Stat(s.automationsFilePath(agentName)); err == nil {
			scopes = append(scopes, agentName)
		}
	}
	sort.Strings(scopes)
	return scopes, nil
}

func (s *Store) ListAutomations(agentName string) ([]AutomationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentName = sanitizeAgentScope(agentName)
	jobs, err := s.loadAutomationsUnlocked(agentName)
	if err != nil {
		return nil, err
	}
	out := make([]AutomationJob, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) SaveAutomationState(agentName string, job AutomationJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentName = sanitizeAgentScope(agentName)
	jobs, err := s.loadAutomationsUnlocked(agentName)
	if err != nil {
		return err
	}
	existing, ok := jobs[job.ID]
	if !ok {
		return nil
	}
	existing.NextRunAt = job.NextRunAt
	existing.LastRunAt = job.LastRunAt
	existing.UpdatedAt = time.Now().UTC()
	jobs[job.ID] = existing
	return s.saveAutomationsUnlocked(agentName, jobs)
}

func (s *Store) AppendAutomationRun(agentName string, job AutomationJob, status string, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agentName = sanitizeAgentScope(agentName)
	if err := os.MkdirAll(s.agentCronDir(agentName), 0o700); err != nil {
		return err
	}

	day := time.Now().Format("2006-01-02")
	path := s.automationRunFilePath(agentName, day)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		header := fmt.Sprintf("# Automation Runs %s (%s)\n\n", day, agentName)
		if err := os.WriteFile(path, []byte(header), 0o600); err != nil {
			return err
		}
	}
	entry := fmt.Sprintf("## %s\n- time: %s\n- status: %s\n- prompt: %s\n- note: %s\n\n",
		job.ID, time.Now().UTC().Format(time.RFC3339), status, sanitizeLine(job.Prompt), sanitizeLine(note))
	return appendLine(path, entry)
}

func (s *Store) loadAutomationsUnlocked(agentName string) (map[string]AutomationJob, error) {
	path := s.automationsFilePath(agentName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]AutomationJob{}, nil
		}
		return nil, err
	}

	result := map[string]AutomationJob{}
	var current *AutomationJob
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "## ") {
			id := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if id == "" {
				current = nil
				continue
			}
			job := AutomationJob{ID: id}
			result[id] = job
			current = &job
			continue
		}
		if current == nil || !strings.HasPrefix(line, "- ") {
			continue
		}
		kv := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		parts := strings.SplitN(kv, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"`)
		job := result[current.ID]
		switch key {
		case "name":
			job.Name = val
		case "enabled":
			job.Enabled = strings.EqualFold(val, "true")
		case "cron":
			job.CronExpr = val
		case "tz":
			job.TZ = val
		case "prompt":
			job.Prompt = val
		case "next_run_at":
			job.NextRunAt, _ = time.Parse(time.RFC3339, val)
		case "last_run_at":
			job.LastRunAt, _ = time.Parse(time.RFC3339, val)
		case "updated_at":
			job.UpdatedAt, _ = time.Parse(time.RFC3339, val)
		}
		result[current.ID] = job
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) saveAutomationsUnlocked(agentName string, jobs map[string]AutomationJob) error {
	if err := s.ensureAgentMemoryLayoutUnlocked(agentName); err != nil {
		return err
	}

	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	b.WriteString("# Semiclaw Automations\n\n")
	for _, id := range ids {
		job := jobs[id]
		b.WriteString("## " + job.ID + "\n")
		b.WriteString("- name: " + quoteValue(job.Name) + "\n")
		b.WriteString("- enabled: " + strconv.FormatBool(job.Enabled) + "\n")
		b.WriteString("- cron: " + quoteValue(job.CronExpr) + "\n")
		b.WriteString("- tz: " + quoteValue(job.TZ) + "\n")
		b.WriteString("- prompt: " + quoteValue(job.Prompt) + "\n")
		if !job.NextRunAt.IsZero() {
			b.WriteString("- next_run_at: " + job.NextRunAt.UTC().Format(time.RFC3339) + "\n")
		} else {
			b.WriteString("- next_run_at: \n")
		}
		if !job.LastRunAt.IsZero() {
			b.WriteString("- last_run_at: " + job.LastRunAt.UTC().Format(time.RFC3339) + "\n")
		} else {
			b.WriteString("- last_run_at: \n")
		}
		if !job.UpdatedAt.IsZero() {
			b.WriteString("- updated_at: " + job.UpdatedAt.UTC().Format(time.RFC3339) + "\n")
		}
		b.WriteString("\n")
	}
	path := s.automationsFilePath(agentName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) ensureAgentMemoryLayoutUnlocked(agentName string) error {
	if err := os.MkdirAll(s.agentMemoryDir(agentName), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.agentMemoryDir(agentName), "daily"), 0o700); err != nil {
		return err
	}
	if err := s.ensureFile(s.memoryFilePath(agentName), "# Semiclaw Memory\n\n## Entries\n"); err != nil {
		return err
	}
	if err := s.ensureFile(s.automationsFilePath(agentName), "# Semiclaw Automations\n\n"); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureFile(path string, content string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateLegacyLayoutUnlocked() error {
	defaultScope := sanitizeAgentScope("semiclaw")
	if err := os.MkdirAll(filepath.Join(s.agentMemoryDir(defaultScope), "daily"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(s.agentCronDir(defaultScope), 0o700); err != nil {
		return err
	}

	if err := moveIfPresent(filepath.Join(s.memoryRootDir, "MEMORY.md"), s.memoryFilePath(defaultScope)); err != nil {
		return err
	}
	if err := moveIfPresent(filepath.Join(s.memoryRootDir, "automations.md"), s.automationsFilePath(defaultScope)); err != nil {
		return err
	}
	if err := moveDirContentsIfPresent(filepath.Join(s.memoryRootDir, "daily"), filepath.Join(s.agentMemoryDir(defaultScope), "daily")); err != nil {
		return err
	}
	if err := moveDirContentsIfPresent(filepath.Join(s.memoryRootDir, "automation-runs"), s.agentCronDir(defaultScope)); err != nil {
		return err
	}
	return s.ensureAgentMemoryLayoutUnlocked(defaultScope)
}

func (s *Store) agentMemoryDir(agentName string) string {
	return filepath.Join(s.memoryRootDir, sanitizeAgentScope(agentName))
}

func (s *Store) memoryFilePath(agentName string) string {
	return filepath.Join(s.agentMemoryDir(agentName), "MEMORY.md")
}

func (s *Store) dailyFilePath(agentName string, day string) string {
	return filepath.Join(s.agentMemoryDir(agentName), "daily", day+".md")
}

func (s *Store) automationsFilePath(agentName string) string {
	return filepath.Join(s.agentMemoryDir(agentName), "automations.md")
}

func (s *Store) agentCronDir(agentName string) string {
	return filepath.Join(s.cronRootDir, sanitizeAgentScope(agentName))
}

func (s *Store) automationRunFilePath(agentName string, day string) string {
	return filepath.Join(s.agentCronDir(agentName), day+".md")
}

func sanitizeAgentScope(agentName string) string {
	trimmed := strings.TrimSpace(agentName)
	if trimmed == "" {
		return "semiclaw"
	}
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "semiclaw"
	}
	return out
}

func appendLine(path string, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

func sanitizeLine(v string) string {
	v = strings.TrimSpace(v)
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}

func quoteValue(v string) string {
	return strconv.Quote(strings.TrimSpace(v))
}

func moveIfPresent(src string, dst string) error {
	if src == dst {
		return nil
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func moveDirContentsIfPresent(srcDir string, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		if _, err := os.Stat(dstPath); err == nil {
			continue
		}
		if err := os.Rename(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}
