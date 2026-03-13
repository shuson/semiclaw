package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"semiclaw/app/internal/gateway"
	"semiclaw/app/internal/memorymd"
)

const (
	daemonApprovalModeDenySensitive = "deny_sensitive"
	daemonApprovalModeAllowAll      = "allow_all"
)

type daemonServiceSpec struct {
	platform      string
	serviceName   string
	label         string
	servicePath   string
	statusPath    string
	stdoutLogPath string
	stderrLogPath string
	executable    string
	dataDir       string
}

type daemonStatus struct {
	Installed bool
	Running   bool
	Service   daemonServiceSpec
	Details   string
}

func (r *Runner) runInstall(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw install")
	}

	spec, err := r.daemonServiceSpec()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(spec.servicePath), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(spec.stdoutLogPath), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(spec.servicePath, []byte(renderDaemonServiceFile(spec)), 0o600); err != nil {
		return err
	}
	if err := r.serviceEnableAndStart(ctx, spec); err != nil {
		return err
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.success("Daemon service installed and started"))
	fmt.Fprintln(r.stdout, out.kv("Service", spec.servicePath))
	fmt.Fprintln(r.stdout, out.kv("Stdout Log", spec.stdoutLogPath))
	fmt.Fprintln(r.stdout, out.kv("Stderr Log", spec.stderrLogPath))
	return nil
}

func (r *Runner) runUninstall(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw uninstall")
	}

	spec, err := r.daemonServiceSpec()
	if err != nil {
		return err
	}
	_ = r.serviceDisableAndStop(ctx, spec)
	if err := os.Remove(spec.servicePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if runtime.GOOS == "linux" {
		_ = runManagedCommand(ctx, "systemctl", "--user", "daemon-reload")
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.success("Daemon service uninstalled"))
	fmt.Fprintln(r.stdout, out.kv("Service", spec.servicePath))
	return nil
}

func (r *Runner) runDaemon(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: semiclaw daemon <run|status|start|stop|restart>")
	}
	switch args[0] {
	case "run":
		return r.runDaemonRun(ctx, args[1:])
	case "status":
		return r.runDaemonStatus(ctx, args[1:])
	case "start":
		return r.runDaemonStart(ctx, args[1:])
	case "stop":
		return r.runDaemonStop(ctx, args[1:])
	case "restart":
		return r.runDaemonRestart(ctx, args[1:])
	default:
		return fmt.Errorf("unknown daemon subcommand %q", args[0])
	}
}

func (r *Runner) runDaemonRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon run", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	runOnce := fs.Bool("once", false, "Run one scheduler poll and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw daemon run [--once]")
	}

	scheduler := memorymd.NewScheduler(r.memory, 30*time.Second, func(ctx context.Context, agentName string, job memorymd.AutomationJob) error {
		return r.runAutomationJob(ctx, agentName, job)
	})

	if *runOnce {
		return scheduler.RunOnce(ctx)
	}

	spec, err := r.daemonServiceSpec()
	if err == nil {
		if writeErr := writeDaemonRuntimeStatus(spec.statusPath); writeErr == nil {
			defer removeDaemonRuntimeStatus(spec.statusPath)
		}
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.info("Semiclaw daemon loop started"))

	runCtx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	scheduler.Start(runCtx)
	return nil
}

func (r *Runner) runDaemonStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon status", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw daemon status")
	}
	status, err := r.detectDaemonStatus(ctx)
	if err != nil {
		return err
	}
	out := r.themeFor(r.stdout)
	state := "stopped"
	if status.Running {
		state = "running"
	} else if !status.Installed {
		state = "not installed"
	}
	fmt.Fprintln(r.stdout, out.section("Daemon Status"))
	fmt.Fprintln(r.stdout, out.kv("State", state))
	fmt.Fprintln(r.stdout, out.kv("Service", status.Service.servicePath))
	fmt.Fprintln(r.stdout, out.kv("Runtime Status", status.Service.statusPath))
	fmt.Fprintln(r.stdout, out.kv("Stdout Log", status.Service.stdoutLogPath))
	fmt.Fprintln(r.stdout, out.kv("Stderr Log", status.Service.stderrLogPath))
	if strings.TrimSpace(status.Details) != "" {
		fmt.Fprintln(r.stdout, out.kv("Details", status.Details))
	}
	return nil
}

func (r *Runner) runDaemonStart(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon start", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw daemon start")
	}
	spec, err := r.daemonServiceSpec()
	if err != nil {
		return err
	}
	if err := r.serviceStart(ctx, spec); err != nil {
		return err
	}
	fmt.Fprintln(r.stdout, r.themeFor(r.stdout).success("Daemon service started"))
	return nil
}

func (r *Runner) runDaemonStop(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon stop", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw daemon stop")
	}
	spec, err := r.daemonServiceSpec()
	if err != nil {
		return err
	}
	if err := r.serviceStop(ctx, spec); err != nil {
		return err
	}
	fmt.Fprintln(r.stdout, r.themeFor(r.stdout).success("Daemon service stopped"))
	return nil
}

func (r *Runner) runDaemonRestart(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("daemon restart", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw daemon restart")
	}
	spec, err := r.daemonServiceSpec()
	if err != nil {
		return err
	}
	if err := r.serviceRestart(ctx, spec); err != nil {
		return err
	}
	fmt.Fprintln(r.stdout, r.themeFor(r.stdout).success("Daemon service restarted"))
	return nil
}

func (r *Runner) runAutomationJob(ctx context.Context, agentName string, job memorymd.AutomationJob) error {
	activeAgent, err := r.store.GetAgent(ctx, agentName)
	if err != nil {
		return err
	}
	if activeAgent == nil {
		return fmt.Errorf("automation agent %q is missing", agentName)
	}

	activeProviderKind, err := r.getAgentProviderKind(ctx, agentName)
	if err != nil {
		return err
	}
	providerClient := r.buildProviderClient(activeProviderKind, activeAgent)
	if providerClient == nil {
		return errors.New("failed to initialize provider client")
	}

	userScope := fmt.Sprintf("automation:%s:%s", agentName, job.ID)
	state := r.newChatTurnState(nil, activeAgent, activeProviderKind, userScope, job.Prompt)
	r.applyLongTermMemoryContext(activeAgent, job.Prompt, &state)

	agentRuntime := gateway.NewAgentRuntime(providerClient)
	gatewayService := gateway.New(
		r.store,
		gateway.NewSessionManager(r.store, r.memory),
		agentRuntime,
		[]gateway.Executor{
			gateway.NewShellExecutor(r.hostCmd),
			gateway.NewBrowserExecutor(r.crawler),
			gateway.NewPythonExecutor(r.pythonCmd),
			gateway.NewFileExecutor(r.fileCmd),
		},
		nil,
	)

	_, err = gatewayService.HandleEvent(ctx, gateway.Request{
		OwnerScopedID:     userScope,
		AgentName:         activeAgent.Name,
		SystemPrompt:      state.systemPrompt,
		Event:             gateway.Event{Message: state.effectiveMessage},
		OriginalUserInput: job.Prompt,
		MaxSteps:          continueGatewayReasoningSteps,
		SkillsPrompt:      r.buildSkillsPrompt(),
		UserTimezone:      r.currentUserTimezone(),
		Runtime: gateway.RuntimeMetadata{
			OS:       runtime.GOOS,
			Arch:     runtime.GOARCH,
			Shell:    strings.TrimSpace(os.Getenv("SHELL")),
			Provider: activeProviderKind,
			Model:    strings.TrimSpace(activeAgent.Model),
			Agent:    strings.TrimSpace(activeAgent.Name),
		},
		ToolPolicyMode: automationToolPolicyMode(job.ApprovalMode),
	})
	return err
}

func automationToolPolicyMode(approvalMode string) gateway.ToolPolicyMode {
	switch strings.ToLower(strings.TrimSpace(approvalMode)) {
	case daemonApprovalModeAllowAll:
		return gateway.ToolPolicyModeAutomationAllowAll
	default:
		return gateway.ToolPolicyModeAutomationSafe
	}
}

func (r *Runner) detectDaemonStatus(ctx context.Context) (daemonStatus, error) {
	spec, err := r.daemonServiceSpec()
	if err != nil {
		return daemonStatus{}, err
	}
	status := daemonStatus{Service: spec}
	if _, err := os.Stat(spec.servicePath); err == nil {
		status.Installed = true
	} else if !os.IsNotExist(err) {
		return daemonStatus{}, err
	}

	running, details := daemonRuntimeStatus(spec.statusPath)
	if !running {
		running, details = daemonRunningStatus(ctx, spec)
	}
	status.Running = running
	status.Details = details
	return status, nil
}

func (r *Runner) daemonServiceSpec() (daemonServiceSpec, error) {
	executable, err := os.Executable()
	if err != nil {
		return daemonServiceSpec{}, err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return daemonServiceSpec{}, err
	}
	logDir := filepath.Join(r.cfg.DataDir, "logs")
	dataStateDir := filepath.Join(r.cfg.DataDir, "data")
	spec := daemonServiceSpec{
		platform:      runtime.GOOS,
		serviceName:   "semiclaw.service",
		label:         "com.semiclaw.daemon",
		statusPath:    filepath.Join(dataStateDir, "daemon.status"),
		stdoutLogPath: filepath.Join(logDir, "semiclawd.log"),
		stderrLogPath: filepath.Join(logDir, "semiclawd.err.log"),
		executable:    executable,
		dataDir:       r.cfg.DataDir,
	}

	switch runtime.GOOS {
	case "darwin":
		spec.servicePath = filepath.Join(homeDir, "Library", "LaunchAgents", spec.label+".plist")
	case "linux":
		spec.servicePath = filepath.Join(homeDir, ".config", "systemd", "user", spec.serviceName)
	default:
		return daemonServiceSpec{}, fmt.Errorf("daemon install is unsupported on %s", runtime.GOOS)
	}
	return spec, nil
}

func renderDaemonServiceFile(spec daemonServiceSpec) string {
	switch spec.platform {
	case "darwin":
		return renderLaunchdPlist(spec)
	default:
		return renderSystemdUnit(spec)
	}
}

func renderLaunchdPlist(spec daemonServiceSpec) string {
	values := []string{
		xmlArrayValue(spec.executable),
		xmlArrayValue("daemon"),
		xmlArrayValue("run"),
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    %s
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>DATA_DIR</key>
    <string>%s</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, html.EscapeString(spec.label), strings.Join(values, "\n    "), html.EscapeString(spec.dataDir), html.EscapeString(spec.stdoutLogPath), html.EscapeString(spec.stderrLogPath))
}

func renderSystemdUnit(spec daemonServiceSpec) string {
	return fmt.Sprintf(`[Unit]
Description=Semiclaw user daemon
After=network.target

[Service]
Type=simple
Environment=DATA_DIR=%s
ExecStart=%s daemon run
Restart=always
RestartSec=5
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, spec.dataDir, spec.executable, spec.stdoutLogPath, spec.stderrLogPath)
}

func xmlArrayValue(value string) string {
	return "<string>" + html.EscapeString(value) + "</string>"
}

func (r *Runner) serviceEnableAndStart(ctx context.Context, spec daemonServiceSpec) error {
	switch spec.platform {
	case "darwin":
		_ = runManagedCommand(ctx, "launchctl", "bootout", launchctlDomain()+"/"+spec.label)
		return runManagedCommand(ctx, "launchctl", "bootstrap", launchctlDomain(), spec.servicePath)
	case "linux":
		if err := runManagedCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return runManagedCommand(ctx, "systemctl", "--user", "enable", "--now", spec.serviceName)
	default:
		return fmt.Errorf("daemon install is unsupported on %s", spec.platform)
	}
}

func (r *Runner) serviceDisableAndStop(ctx context.Context, spec daemonServiceSpec) error {
	switch spec.platform {
	case "darwin":
		return runManagedCommand(ctx, "launchctl", "bootout", launchctlDomain()+"/"+spec.label)
	case "linux":
		return runManagedCommand(ctx, "systemctl", "--user", "disable", "--now", spec.serviceName)
	default:
		return fmt.Errorf("daemon install is unsupported on %s", spec.platform)
	}
}

func (r *Runner) serviceStart(ctx context.Context, spec daemonServiceSpec) error {
	switch spec.platform {
	case "darwin":
		if _, err := os.Stat(spec.servicePath); err != nil {
			return err
		}
		_ = runManagedCommand(ctx, "launchctl", "bootout", launchctlDomain()+"/"+spec.label)
		return runManagedCommand(ctx, "launchctl", "bootstrap", launchctlDomain(), spec.servicePath)
	case "linux":
		return runManagedCommand(ctx, "systemctl", "--user", "start", spec.serviceName)
	default:
		return fmt.Errorf("daemon control is unsupported on %s", spec.platform)
	}
}

func (r *Runner) serviceStop(ctx context.Context, spec daemonServiceSpec) error {
	switch spec.platform {
	case "darwin":
		return runManagedCommand(ctx, "launchctl", "bootout", launchctlDomain()+"/"+spec.label)
	case "linux":
		return runManagedCommand(ctx, "systemctl", "--user", "stop", spec.serviceName)
	default:
		return fmt.Errorf("daemon control is unsupported on %s", spec.platform)
	}
}

func (r *Runner) serviceRestart(ctx context.Context, spec daemonServiceSpec) error {
	switch spec.platform {
	case "darwin":
		if err := r.serviceStop(ctx, spec); err != nil {
			_ = err
		}
		return r.serviceStart(ctx, spec)
	case "linux":
		return runManagedCommand(ctx, "systemctl", "--user", "restart", spec.serviceName)
	default:
		return fmt.Errorf("daemon control is unsupported on %s", spec.platform)
	}
}

func daemonRunningStatus(ctx context.Context, spec daemonServiceSpec) (bool, string) {
	switch spec.platform {
	case "darwin":
		output, err := runManagedCommandOutput(ctx, "launchctl", "print", launchctlDomain()+"/"+spec.label)
		if err != nil {
			return false, strings.TrimSpace(output)
		}
		return true, firstLine(output)
	case "linux":
		output, err := runManagedCommandOutput(ctx, "systemctl", "--user", "is-active", spec.serviceName)
		if err != nil {
			return false, strings.TrimSpace(output)
		}
		return strings.TrimSpace(output) == "active", strings.TrimSpace(output)
	default:
		return false, ""
	}
}

func runManagedCommand(ctx context.Context, name string, args ...string) error {
	_, err := runManagedCommandOutput(ctx, name, args...)
	return err
}

func runManagedCommandOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return "", err
		}
		return string(out), fmt.Errorf("%s: %w", trimmed, err)
	}
	return string(out), nil
}

func launchctlDomain() string {
	current, err := user.Current()
	if err != nil {
		return "gui/501"
	}
	return "gui/" + current.Uid
}

func firstLine(value string) string {
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func writeDaemonRuntimeStatus(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	content := fmt.Sprintf("pid: %d\nstarted_at: %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	return os.WriteFile(path, []byte(content), 0o600)
}

func removeDaemonRuntimeStatus(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	_ = os.Remove(path)
}

func daemonRuntimeStatus(path string) (bool, string) {
	if strings.TrimSpace(path) == "" {
		return false, ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	pid, startedAt := parseDaemonRuntimeStatus(raw)
	if pid <= 0 {
		return false, ""
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false, ""
	}
	if startedAt != "" {
		return true, fmt.Sprintf("manual daemon pid=%d started_at=%s", pid, startedAt)
	}
	return true, fmt.Sprintf("manual daemon pid=%d", pid)
}

func parseDaemonRuntimeStatus(raw []byte) (int, string) {
	lines := bytes.Split(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), []byte("\n"))
	pid := 0
	startedAt := ""
	for _, line := range lines {
		parts := bytes.SplitN(bytes.TrimSpace(line), []byte(":"), 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(string(parts[0]))
		val := strings.TrimSpace(string(parts[1]))
		switch key {
		case "pid":
			fmt.Sscanf(val, "%d", &pid)
		case "started_at":
			startedAt = val
		}
	}
	return pid, startedAt
}
