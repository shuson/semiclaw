package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"semiclaw/app/internal/agent"
	"semiclaw/app/internal/auth"
	"semiclaw/app/internal/config"
	"semiclaw/app/internal/db"
	"semiclaw/app/internal/filecmd"
	"semiclaw/app/internal/gateway"
	"semiclaw/app/internal/hostcmd"
	"semiclaw/app/internal/memorymd"
	"semiclaw/app/internal/provider"
	"semiclaw/app/internal/pythoncmd"
	"semiclaw/app/internal/webcrawl"
)

const defaultAgentName = "semiclaw"
const (
	providerKindOllama = "ollama"
	providerKindOpenAI = "openai"
)

var agentNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

type Runner struct {
	cfg       config.Config
	store     *db.Store
	secretBox *auth.SecretBox
	agent     *agent.Service
	crawler   *webcrawl.Fetcher
	hostCmd   *hostcmd.Runner
	pythonCmd *pythoncmd.Runner
	fileCmd   *filecmd.Runner
	memory    *memorymd.Store
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

type cliTheme struct {
	color bool
}

type chatTurnState struct {
	systemPrompt     string
	effectiveMessage string
	userScope        string
	webTargetURL     string
	useWebAgent      bool
	longTermMemory   string
}

type hostCommandOutcome struct {
	result        *hostcmd.Result
	failureReason string
}

func NewRunner(
	cfg config.Config,
	store *db.Store,
	secretBox *auth.SecretBox,
	agent *agent.Service,
	crawler *webcrawl.Fetcher,
	hostCmd *hostcmd.Runner,
	pythonCmd *pythoncmd.Runner,
	fileCmd *filecmd.Runner,
	memory *memorymd.Store,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) *Runner {
	return &Runner{
		cfg:       cfg,
		store:     store,
		secretBox: secretBox,
		agent:     agent,
		crawler:   crawler,
		hostCmd:   hostCmd,
		pythonCmd: pythonCmd,
		fileCmd:   fileCmd,
		memory:    memory,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
	}
}

func (r *Runner) themeFor(w io.Writer) cliTheme {
	return cliTheme{color: isTerminalFile(w) && strings.TrimSpace(os.Getenv("NO_COLOR")) == ""}
}

func (t cliTheme) paint(code string, text string) string {
	if !t.color || strings.TrimSpace(code) == "" {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}

func (t cliTheme) title(text string) string   { return t.paint("1;38;5;45", text) }
func (t cliTheme) section(text string) string { return t.paint("1;38;5;51", text) }
func (t cliTheme) key(text string) string     { return t.paint("38;5;250", text) }
func (t cliTheme) value(text string) string   { return t.paint("1;38;5;229", text) }
func (t cliTheme) command(text string) string { return t.paint("38;5;122", text) }
func (t cliTheme) success(text string) string { return t.paint("1;38;5;84", "✔ "+text) }
func (t cliTheme) info(text string) string    { return t.paint("1;38;5;117", "ℹ "+text) }
func (t cliTheme) warn(text string) string    { return t.paint("1;38;5;214", "⚠ "+text) }

func (t cliTheme) kv(label string, value string) string {
	return fmt.Sprintf("%s %s", t.key(label+":"), t.value(value))
}

func (r *Runner) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		r.printHelp()
		return nil
	}

	switch args[0] {
	case "help", "--help", "-h":
		r.printHelp()
		return nil
	case "setup":
		return r.runSetup(ctx, args[1:])
	case "login":
		return r.runLogin(ctx, args[1:])
	case "logout":
		return r.runLogout(ctx)
	case "status":
		return r.runStatus(ctx)
	case "chat":
		return r.runChat(ctx, args[1:])
	case "history":
		return r.runHistory(ctx, args[1:])
	case "agent":
		return r.runAgent(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (r *Runner) runSetup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	password := fs.String("password", "", "Owner password")
	apiKey := fs.String("api-key", "", "Default provider API key")
	openAIBaseURL := fs.String("openai-base-url", "", "Optional OpenAI-compatible base URL")
	openAIAPIKey := fs.String("openai-api-key", "", "Optional OpenAI-compatible API key")
	openAIModel := fs.String("openai-model", "", "Optional OpenAI-compatible model")
	openAPIBaseURL := fs.String("open-api-base-url", "", "Optional OpenAI-compatible base URL (alias)")
	openAPIAPIKey := fs.String("open-api-api-key", "", "Optional OpenAI-compatible API key (alias)")
	openAPIModel := fs.String("open-api-model", "", "Optional OpenAI-compatible model (alias)")
	soulSeed := fs.String("soul-seed", "", "Soul seed (optional)")
	skipProfile := fs.Bool("skip-profile", false, "Skip interactive profile questions")
	if err := fs.Parse(args); err != nil {
		return err
	}

	selectedDataDir, err := r.promptSetupDataDir()
	if err != nil {
		return err
	}
	if filepath.Clean(selectedDataDir) != filepath.Clean(r.cfg.DataDir) {
		if err := r.switchDataDir(selectedDataDir); err != nil {
			return err
		}
	}
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.kv("📁 Data Directory", r.cfg.DataDir))

	nonEmpty := func(values ...string) string {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
		return ""
	}

	selectedOpenAIBaseURL := nonEmpty(*openAIBaseURL, *openAPIBaseURL)
	selectedOpenAIAPIKey := nonEmpty(*openAIAPIKey, *openAPIAPIKey)
	selectedOpenAIModel := nonEmpty(*openAIModel, *openAPIModel)

	requestedAPIKey := nonEmpty(selectedOpenAIAPIKey, *apiKey)

	claimed, err := r.store.IsOwnerClaimed(ctx)
	if err != nil {
		return err
	}

	if !claimed {
		if strings.TrimSpace(*password) == "" {
			prompted, promptErr := r.prompt("Owner password: ")
			if promptErr != nil {
				return promptErr
			}
			*password = prompted
		}

		passwordHash, err := auth.HashPassword(strings.TrimSpace(*password))
		if err != nil {
			return fmt.Errorf("hash owner password: %w", err)
		}

		sessionToken, err := auth.GenerateSessionToken()
		if err != nil {
			return fmt.Errorf("generate session token: %w", err)
		}

		owner := db.Owner{
			OwnerID:          r.cfg.OwnerID,
			PasswordHash:     passwordHash,
			SessionTokenHash: auth.HashToken(sessionToken),
			ClaimedAt:        time.Now().UnixMilli(),
		}
		if err := r.store.CreateOwner(ctx, owner); err != nil {
			return fmt.Errorf("create owner: %w", err)
		}

		setupProviderKind, err := r.promptWithDefault("Provider type (ollama/openai)", providerKindOllama)
		if err != nil {
			return err
		}
		setupProviderKind = strings.ToLower(strings.TrimSpace(setupProviderKind))
		if setupProviderKind != providerKindOllama && setupProviderKind != providerKindOpenAI {
			return errors.New("provider type must be either 'ollama' or 'openai'")
		}

		setupBaseURL := r.cfg.OllamaBaseURL
		setupModel := r.cfg.DefaultModel
		if setupProviderKind == providerKindOpenAI {
			setupBaseURL, err = r.promptWithDefault("OpenAI-compatible base URL", nonEmpty(selectedOpenAIBaseURL, "https://api.openai.com"))
			if err != nil {
				return err
			}
			setupModel, err = r.promptWithDefault("OpenAI model", nonEmpty(selectedOpenAIModel, r.cfg.DefaultModel))
			if err != nil {
				return err
			}
			if selectedOpenAIAPIKey == "" && requestedAPIKey == "" {
				requestedAPIKey, err = r.prompt("OpenAI API key (optional): ")
				if err != nil {
					return err
				}
				requestedAPIKey = strings.TrimSpace(requestedAPIKey)
			}
		} else {
			setupBaseURL, err = r.promptWithDefault("Ollama base URL", preferredOllamaBaseURL(r.cfg.OllamaBaseURL))
			if err != nil {
				return err
			}
			setupModel, err = r.promptWithDefault("Ollama model", nonEmpty(r.cfg.DefaultModel, "llama3.2"))
			if err != nil {
				return err
			}
		}

		fmt.Fprintln(r.stdout, "")
		fmt.Fprintln(r.stdout, out.section("⚙ Configuring Agent: "+defaultAgentName))

		if requestedAPIKey != "" {
			ciphertext, nonce, err := r.secretBox.Encrypt(requestedAPIKey)
			if err != nil {
				return fmt.Errorf("encrypt provider key: %w", err)
			}
			if err := r.store.UpsertSecret(ctx, agentSecretKey(defaultAgentName), ciphertext, nonce); err != nil {
				return fmt.Errorf("store provider key: %w", err)
			}
		}

		seed := strings.TrimSpace(*soulSeed)
		if seed == "" {
			seed = strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		if err := r.store.UpsertConfig(ctx, "heartware.seed", seed); err != nil {
			return fmt.Errorf("store soul seed: %w", err)
		}

		if !*skipProfile {
			if err := r.collectUserProfile(ctx); err != nil {
				return err
			}
		}

		if err := r.store.UpsertAgent(ctx, db.AgentRecord{
			Name:         defaultAgentName,
			SystemPrompt: agent.DefaultSystemPrompt,
			Model:        setupModel,
			BaseURL:      setupBaseURL,
		}); err != nil {
			return err
		}
		if err := r.setAgentProviderKind(ctx, defaultAgentName, setupProviderKind); err != nil {
			return err
		}
		if err := r.store.UpsertConfig(ctx, "agent.current", defaultAgentName); err != nil {
			return err
		}
		if err := r.ensureDefaultAgent(ctx); err != nil {
			return err
		}

		if err := r.writeSessionToken(sessionToken); err != nil {
			return fmt.Errorf("persist session token: %w", err)
		}

		fmt.Fprintln(r.stdout, out.success("Setup complete"))
		fmt.Fprintln(r.stdout, out.kv("👤 Owner", r.cfg.OwnerID))
		fmt.Fprintln(r.stdout, out.kv("🤖 Current Agent", defaultAgentName))
		fmt.Fprintln(r.stdout, out.kv("🔌 Provider", setupProviderKind))
		fmt.Fprintln(r.stdout, out.kv("🧠 Model", setupModel))
		return nil
	}

	owner, err := r.store.GetOwner(ctx)
	if err != nil {
		return err
	}
	if owner == nil {
		return errors.New("owner record missing")
	}

	authenticated, err := r.isAuthenticated(owner)
	if err != nil {
		return err
	}
	if !authenticated {
		if strings.TrimSpace(*password) == "" {
			prompted, promptErr := r.prompt("Password (required to reconfigure): ")
			if promptErr != nil {
				return promptErr
			}
			*password = prompted
		}

		ok, err := auth.VerifyPassword(strings.TrimSpace(*password), owner.PasswordHash)
		if err != nil {
			return fmt.Errorf("verify password: %w", err)
		}
		if !ok {
			return errors.New("invalid password")
		}

		sessionToken, err := auth.GenerateSessionToken()
		if err != nil {
			return fmt.Errorf("generate session token: %w", err)
		}
		if err := r.store.UpdateOwnerSession(ctx, owner.OwnerID, auth.HashToken(sessionToken)); err != nil {
			return fmt.Errorf("update owner session: %w", err)
		}
		if err := r.writeSessionToken(sessionToken); err != nil {
			return fmt.Errorf("persist session token: %w", err)
		}
	}

	if err := r.ensureDefaultAgent(ctx); err != nil {
		return err
	}

	currentAgent, err := r.getCurrentAgent(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintln(r.stdout, "")
	fmt.Fprintln(r.stdout, out.section("⚙ Configuring Agent: "+currentAgent.Name))
	currentProviderKind, err := r.getAgentProviderKind(ctx, currentAgent.Name)
	if err != nil {
		return err
	}

	defaultProvider := currentProviderKind
	if defaultProvider == "" {
		defaultProvider = providerKindOllama
	}
	setupProviderKind, err := r.promptWithDefault("Provider type (ollama/openai)", defaultProvider)
	if err != nil {
		return err
	}
	setupProviderKind = strings.ToLower(strings.TrimSpace(setupProviderKind))
	if setupProviderKind != providerKindOllama && setupProviderKind != providerKindOpenAI {
		return errors.New("provider type must be either 'ollama' or 'openai'")
	}
	setupBaseURL := strings.TrimSpace(currentAgent.BaseURL)
	setupModel := strings.TrimSpace(currentAgent.Model)
	if setupProviderKind == providerKindOpenAI {
		defaultOpenAIBase := nonEmpty(selectedOpenAIBaseURL, setupBaseURL)
		if currentProviderKind != providerKindOpenAI && defaultOpenAIBase == "" {
			defaultOpenAIBase = "https://api.openai.com"
		}
		setupBaseURL, err = r.promptWithDefault("OpenAI-compatible base URL", defaultOpenAIBase)
		if err != nil {
			return err
		}

		defaultOpenAIModel := nonEmpty(selectedOpenAIModel, setupModel, r.cfg.DefaultModel)
		setupModel, err = r.promptWithDefault("OpenAI model", defaultOpenAIModel)
		if err != nil {
			return err
		}

		if selectedOpenAIAPIKey == "" && requestedAPIKey == "" {
			requestedAPIKey, err = r.prompt("OpenAI API key (optional, blank=keep existing): ")
			if err != nil {
				return err
			}
			requestedAPIKey = strings.TrimSpace(requestedAPIKey)
		}
	} else {
		defaultOllamaBase := preferredOllamaBaseURL(nonEmpty(setupBaseURL, r.cfg.OllamaBaseURL, "http://127.0.0.1:11434"))
		if currentProviderKind != providerKindOllama {
			defaultOllamaBase = preferredOllamaBaseURL(nonEmpty(r.cfg.OllamaBaseURL, "http://127.0.0.1:11434"))
		}
		setupBaseURL, err = r.promptWithDefault("Ollama base URL", defaultOllamaBase)
		if err != nil {
			return err
		}
		defaultOllamaModel := nonEmpty(setupModel, r.cfg.DefaultModel, "llama3.2")
		if currentProviderKind != providerKindOllama {
			defaultOllamaModel = nonEmpty(r.cfg.DefaultModel, "llama3.2")
		}
		setupModel, err = r.promptWithDefault("Ollama model", defaultOllamaModel)
		if err != nil {
			return err
		}
	}

	if err := r.store.UpsertAgent(ctx, db.AgentRecord{
		Name:         currentAgent.Name,
		SystemPrompt: currentAgent.SystemPrompt,
		Model:        setupModel,
		BaseURL:      setupBaseURL,
	}); err != nil {
		return err
	}
	if err := r.setAgentProviderKind(ctx, currentAgent.Name, setupProviderKind); err != nil {
		return err
	}
	if requestedAPIKey != "" {
		ciphertext, nonce, err := r.secretBox.Encrypt(requestedAPIKey)
		if err != nil {
			return fmt.Errorf("encrypt provider key: %w", err)
		}
		if err := r.store.UpsertSecret(ctx, agentSecretKey(currentAgent.Name), ciphertext, nonce); err != nil {
			return fmt.Errorf("store provider key: %w", err)
		}
	}

	seed := strings.TrimSpace(*soulSeed)
	if seed != "" {
		if err := r.store.UpsertConfig(ctx, "heartware.seed", seed); err != nil {
			return fmt.Errorf("store soul seed: %w", err)
		}
	}

	if !*skipProfile {
		if err := r.collectUserProfile(ctx); err != nil {
			return err
		}
	}

	fmt.Fprintln(r.stdout, out.success("Setup reconfigured current agent"))
	fmt.Fprintln(r.stdout, out.kv("🤖 Current Agent", currentAgent.Name))
	fmt.Fprintln(r.stdout, out.kv("🔌 Provider", setupProviderKind))
	fmt.Fprintln(r.stdout, out.kv("🧠 Model", setupModel))
	fmt.Fprintln(r.stdout, out.kv("🌐 Base URL", setupBaseURL))
	return nil
}

func (r *Runner) collectUserProfile(ctx context.Context) error {
	fmt.Fprintln(r.stdout, "")
	fmt.Fprintln(r.stdout, "Let's personalize Semiclaw. Press Enter to skip any question.")

	questions := []struct {
		key   string
		label string
	}{
		{key: "user.profile.name", label: "What should I call you?"},
		{key: "user.profile.role", label: "What do you do (job/study)?"},
		{key: "user.profile.location", label: "Where are you based (city/country or timezone)?"},
		{key: "user.profile.goals", label: "What are your main goals with Semiclaw?"},
		{key: "user.profile.response_style", label: "How do you prefer responses (brief/detailed, tone)?"},
		{key: "user.profile.notes", label: "Anything else important I should know about you?"},
	}

	for _, question := range questions {
		answer, err := r.prompt(question.label + " ")
		if err != nil {
			return err
		}
		answer = strings.TrimSpace(answer)
		if answer == "" {
			continue
		}
		if err := r.store.UpsertConfig(ctx, question.key, answer); err != nil {
			return fmt.Errorf("store %s: %w", question.key, err)
		}
	}

	if err := r.store.UpsertConfig(ctx, "user.profile.collectedAt", strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
		return fmt.Errorf("store user profile metadata: %w", err)
	}
	return nil
}

func (r *Runner) runLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	password := fs.String("password", "", "Owner password")
	if err := fs.Parse(args); err != nil {
		return err
	}

	owner, err := r.store.GetOwner(ctx)
	if err != nil {
		return err
	}
	if owner == nil {
		return errors.New("no owner configured, run setup first")
	}

	alreadyAuthenticated, err := r.isAuthenticated(owner)
	if err != nil {
		return err
	}
	if alreadyAuthenticated {
		if err := r.ensureDefaultAgent(ctx); err != nil {
			return err
		}
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.info("Already authenticated"))
		return nil
	}

	if strings.TrimSpace(*password) == "" {
		prompted, promptErr := r.prompt("Password: ")
		if promptErr != nil {
			return promptErr
		}
		*password = prompted
	}

	ok, err := auth.VerifyPassword(strings.TrimSpace(*password), owner.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return errors.New("invalid password")
	}

	sessionToken, err := auth.GenerateSessionToken()
	if err != nil {
		return fmt.Errorf("generate session token: %w", err)
	}
	if err := r.store.UpdateOwnerSession(ctx, owner.OwnerID, auth.HashToken(sessionToken)); err != nil {
		return fmt.Errorf("update owner session: %w", err)
	}
	if err := r.writeSessionToken(sessionToken); err != nil {
		return fmt.Errorf("persist session token: %w", err)
	}

	if err := r.ensureDefaultAgent(ctx); err != nil {
		return err
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.success("Login successful"))
	return nil
}

func (r *Runner) runLogout(ctx context.Context) error {
	owner, err := r.store.GetOwner(ctx)
	if err != nil {
		return err
	}
	if owner != nil {
		if err := r.store.UpdateOwnerSession(ctx, owner.OwnerID, ""); err != nil {
			return err
		}
	}
	if err := os.Remove(r.cfg.SessionTokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.info("Logged out"))
	return nil
}

func (r *Runner) runStatus(ctx context.Context) error {
	owner, err := r.store.GetOwner(ctx)
	if err != nil {
		return err
	}

	if owner == nil {
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.warn("Owner claimed: no"))
		return nil
	}

	authenticated, err := r.isAuthenticated(owner)
	if err != nil {
		return err
	}

	if err := r.ensureDefaultAgent(ctx); err != nil {
		return err
	}

	currentAgent, err := r.getCurrentAgent(ctx)
	if err != nil {
		return err
	}
	currentAgentProvider, err := r.getAgentProviderKind(ctx, currentAgent.Name)
	if err != nil {
		return err
	}

	agents, err := r.store.ListAgents(ctx)
	if err != nil {
		return err
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.success("Owner claimed: yes"))
	fmt.Fprintln(r.stdout, out.kv("👤 Owner ID", owner.OwnerID))
	fmt.Fprintln(r.stdout, out.kv("🔐 Authenticated", strconv.FormatBool(authenticated)))
	fmt.Fprintln(r.stdout, out.kv("🤖 Current Agent", currentAgent.Name))
	fmt.Fprintln(r.stdout, out.kv("🔌 Agent Provider", currentAgentProvider))
	fmt.Fprintln(r.stdout, out.kv("🧠 Agent Model", currentAgent.Model))
	fmt.Fprintln(r.stdout, out.kv("🌐 Agent Base URL", currentAgent.BaseURL))
	fmt.Fprintln(r.stdout, out.kv("🗂 Total Agents", strconv.Itoa(len(agents))))
	return nil
}

func (r *Runner) runChat(ctx context.Context, args []string) error {
	owner, err := r.requireAuthenticatedOwner(ctx)
	if err != nil {
		return err
	}

	activeAgent, err := r.getCurrentAgent(ctx)
	if err != nil {
		return err
	}
	activeProviderKind, err := r.getAgentProviderKind(ctx, activeAgent.Name)
	if err != nil {
		return err
	}

	message := strings.TrimSpace(strings.Join(args, " "))
	if message != "" {
		if strings.EqualFold(message, ":clear") {
			return r.clearCurrentChatHistory(ctx, owner, activeAgent)
		}
		if strings.EqualFold(message, ":history") {
			return r.printCurrentInputHistory(ctx, owner, activeAgent, 10)
		}
		response, handled, err := r.processChatTurn(ctx, owner, activeAgent, activeProviderKind, message)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
		fmt.Fprintln(r.stdout, response)
		return nil
	}
	return r.runInteractiveChat(ctx, owner, activeAgent, activeProviderKind)
}

func (r *Runner) runInteractiveChat(
	ctx context.Context,
	owner *db.Owner,
	activeAgent *db.AgentRecord,
	activeProviderKind string,
) error {
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.info("Interactive chat mode ("+activeAgent.Name+")"))
	fmt.Fprintln(r.stdout, out.key("Type your message and press Enter. Type 'exit' or 'quit' to leave. Type ':clear' to clear this chat history. Type ':history' to list last 10 inputs."))

	scanner := bufio.NewScanner(r.stdin)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	type inputEvent struct {
		line string
		err  error
		eof  bool
	}
	type turnResult struct {
		response string
		handled  bool
		err      error
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	inputHistory, err := r.collectUserInputHistory(ctx, owner, activeAgent, 200)
	if err != nil {
		return err
	}
	arrowCursor := -1
	pendingRecall := ""

	for {
		fmt.Fprintf(r.stdout, "\n%s> ", activeAgent.Name)
		inputCh := make(chan inputEvent, 1)
		go func() {
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					inputCh <- inputEvent{err: err}
					return
				}
				inputCh <- inputEvent{eof: true}
				return
			}
			inputCh <- inputEvent{line: scanner.Text()}
		}()

		var ev inputEvent
		select {
		case <-sigCh:
			fmt.Fprintln(r.stdout, "\n"+out.info("Ctrl+C received at prompt. Exiting chat."))
			return nil
		case next := <-inputCh:
			ev = next
		}

		if ev.err != nil {
			return ev.err
		}
		if ev.eof {
			fmt.Fprintln(r.stdout, "\n"+out.info("Session ended."))
			return nil
		}

		rawLine := ev.line
		message := strings.TrimSpace(rawLine)
		if pendingRecall != "" && message == "" {
			message = pendingRecall
			pendingRecall = ""
		}
		if message == "" {
			continue
		}
		if isArrowUpSequence(rawLine) {
			if len(inputHistory) == 0 {
				fmt.Fprintln(r.stdout, out.warn("No previous command to reuse."))
				continue
			}
			if arrowCursor == -1 {
				arrowCursor = len(inputHistory) - 1
			} else if arrowCursor > 0 {
				arrowCursor--
			}
			pendingRecall = inputHistory[arrowCursor]
			// Replace the visible raw escape sequence with the recalled input preview.
			fmt.Fprintf(r.stdout, "\r\033[2K%s> %s\n", activeAgent.Name, pendingRecall)
			fmt.Fprintln(r.stdout, out.info("Loaded from history. Press Enter to run, or type a new message."))
			continue
		}
		if !isArrowUpSequence(rawLine) {
			arrowCursor = -1
		}
		switch strings.ToLower(message) {
		case "exit", "quit", ":q":
			fmt.Fprintln(r.stdout, out.info("Bye."))
			return nil
		case ":clear":
			if err := r.clearCurrentChatHistory(ctx, owner, activeAgent); err != nil {
				fmt.Fprintf(r.stderr, "chat error: %v\n", err)
			}
			inputHistory = nil
			arrowCursor = -1
			pendingRecall = ""
			continue
		case ":history":
			if err := r.printInputHistoryFromSlice(inputHistory, 10); err != nil {
				fmt.Fprintf(r.stderr, "chat error: %v\n", err)
			}
			continue
		}
		inputHistory = appendInputHistory(inputHistory, message, 500)
		arrowCursor = -1

		turnCtx, cancelTurn := context.WithCancel(ctx)
		resultCh := make(chan turnResult, 1)
		go func() {
			response, handled, err := r.processChatTurn(turnCtx, owner, activeAgent, activeProviderKind, message)
			resultCh <- turnResult{response: response, handled: handled, err: err}
		}()

		interrupted := false
		for {
			select {
			case <-sigCh:
				if !interrupted {
					interrupted = true
					cancelTurn()
					fmt.Fprintln(r.stdout, "\n"+out.warn("Interrupted current execution."))
				}
			case result := <-resultCh:
				cancelTurn()
				if interrupted {
					if result.err != nil && !errors.Is(result.err, context.Canceled) {
						fmt.Fprintf(r.stderr, "chat error: %v\n", result.err)
					}
					goto nextTurn
				}
				if result.err != nil {
					fmt.Fprintf(r.stderr, "chat error: %v\n", result.err)
					goto nextTurn
				}
				if result.handled {
					goto nextTurn
				}
				fmt.Fprintf(r.stdout, "\n%s\n", result.response)
				goto nextTurn
			}
		}

	nextTurn:
		if interrupted {
			continue
		}
	}
}

func isArrowUpSequence(input string) bool {
	trimmed := strings.TrimSpace(input)
	return trimmed == "\x1b[A" || trimmed == "[A"
}

func (r *Runner) collectUserInputHistory(ctx context.Context, owner *db.Owner, activeAgent *db.AgentRecord, maxItems int) ([]string, error) {
	if owner == nil || activeAgent == nil {
		return nil, nil
	}
	history, err := r.store.GetRecentMessages(ctx, scopedUserID(owner.OwnerID, activeAgent.Name), 500)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(history))
	for _, msg := range history {
		if strings.ToLower(strings.TrimSpace(msg.Role)) != "user" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" || isHistoryControlInput(content) {
			continue
		}
		out = append(out, content)
	}
	if maxItems > 0 && len(out) > maxItems {
		out = out[len(out)-maxItems:]
	}
	return out, nil
}

func (r *Runner) clearCurrentChatHistory(ctx context.Context, owner *db.Owner, activeAgent *db.AgentRecord) error {
	if owner == nil || activeAgent == nil {
		return errors.New("chat context is required")
	}
	if err := r.store.DeleteMessagesByUserID(ctx, scopedUserID(owner.OwnerID, activeAgent.Name)); err != nil {
		return err
	}
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.info("Cleared chat history for current session."))
	return nil
}

func isHistoryControlInput(input string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	return trimmed == ":clear" || trimmed == ":history" || isArrowUpSequence(trimmed)
}

func appendInputHistory(history []string, input string, maxItems int) []string {
	input = strings.TrimSpace(input)
	if input == "" || isHistoryControlInput(input) {
		return history
	}
	history = append(history, input)
	if maxItems > 0 && len(history) > maxItems {
		history = history[len(history)-maxItems:]
	}
	return history
}

func (r *Runner) printCurrentInputHistory(ctx context.Context, owner *db.Owner, activeAgent *db.AgentRecord, limit int) error {
	items, err := r.collectUserInputHistory(ctx, owner, activeAgent, 200)
	if err != nil {
		return err
	}
	return r.printInputHistoryFromSlice(items, limit)
}

func (r *Runner) printInputHistoryFromSlice(items []string, limit int) error {
	out := r.themeFor(r.stdout)
	if limit <= 0 {
		limit = 10
	}
	if len(items) == 0 {
		fmt.Fprintln(r.stdout, out.warn("No input history found."))
		return nil
	}
	start := 0
	if len(items) > limit {
		start = len(items) - limit
	}
	fmt.Fprintln(r.stdout, out.section("⌛ Last Inputs"))
	index := 1
	for _, item := range items[start:] {
		fmt.Fprintf(r.stdout, "%s %s\n", out.key(fmt.Sprintf("%d.", index)), item)
		index++
	}
	return nil
}

func (r *Runner) processChatTurn(
	ctx context.Context,
	owner *db.Owner,
	activeAgent *db.AgentRecord,
	activeProviderKind string,
	message string,
) (string, bool, error) {
	if strings.TrimSpace(message) == "" {
		return "", false, errors.New("message is required")
	}
	if r.memory != nil {
		_ = r.memory.AppendDaily(activeAgent.Name, "chat.user", fmt.Sprintf("%s: %s", activeAgent.Name, message))
	}

	if taskResponse, taskHandled, taskErr := r.maybeExecuteTasksFromMarkdown(ctx, activeAgent, activeProviderKind, message); taskErr != nil || taskHandled {
		if taskErr != nil {
			return "", false, taskErr
		}
		if r.memory != nil {
			_ = r.memory.AppendDaily(activeAgent.Name, "chat.assistant", fmt.Sprintf("%s: %s", activeAgent.Name, strings.TrimSpace(taskResponse)))
		}
		return taskResponse, false, nil
	}

	if handled, err := r.handleMemoryIntents(activeAgent.Name, message); err != nil {
		return "", false, err
	} else if handled {
		_ = r.store.SaveMessage(ctx, scopedUserID(owner.OwnerID, activeAgent.Name), "user", message)
		return "", true, nil
	}

	var response string
	var executedCommandResult *hostcmd.Result
	var executionFailureReason string
	chatErr := r.withRainbowPhaseSpinner(func(setPhase func(string)) error {
		setPhase("Processing")

		providerClient := r.buildProviderClient(activeProviderKind, activeAgent)
		if providerClient == nil {
			return errors.New("failed to initialize provider client")
		}

		state := r.newChatTurnState(owner, activeAgent, message)
		r.applyLongTermMemoryContext(activeAgent, message, &state)
		r.resolveWebIntentFromMemory(message, &state)

		outcome, err := r.runHostCommandPhase(ctx, setPhase, providerClient, activeAgent, message, &state)
		if err != nil {
			return err
		}
		executedCommandResult = outcome.result
		executionFailureReason = strings.TrimSpace(outcome.failureReason)

		if err := r.runWebCrawlPhase(ctx, setPhase, owner, message, &state); err != nil {
			return err
		}

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
			func(tool string, call gateway.ToolCall) (bool, error) {
				switch strings.ToLower(strings.TrimSpace(tool)) {
				case "shell":
					command := strings.TrimSpace(fmt.Sprintf("%v", call.Input["command"]))
					if command == "" || !requiresHostCommandApproval(command) {
						return true, nil
					}
					setPhase("Awaiting permission")
					return r.confirmHostCommand(command)
				case "python", "file":
					setPhase("Awaiting permission")
					return r.confirmToolApproval(tool, gateway.EncodeToolCall(call))
				default:
					return true, nil
				}
			},
		)

		setPhase("Thinking..")
		result, runErr := gatewayService.HandleEvent(ctx, gateway.Request{
			OwnerScopedID: state.userScope,
			AgentName:     activeAgent.Name,
			SystemPrompt:  state.systemPrompt,
			Event:         gateway.Event{Message: state.effectiveMessage},
		})
		if runErr != nil {
			return runErr
		}

		response = result.Response
		setPhase("Coming..")
		response = strings.TrimSpace(response)
		return nil
	})
	if chatErr != nil {
		return "", false, chatErr
	}
	if executionFailureReason != "" {
		response = ensureExecutionFailureInResponse(response, executionFailureReason)
	}
	if executedCommandResult != nil && looksLikeFilesystemCapabilityRefusal(response) {
		response = composeCommandResultFallback(*executedCommandResult)
	}

	if r.memory != nil {
		_ = r.memory.AppendDaily(activeAgent.Name, "chat.assistant", fmt.Sprintf("%s: %s", activeAgent.Name, strings.TrimSpace(response)))
	}

	return response, false, nil
}

func (r *Runner) newChatTurnState(owner *db.Owner, activeAgent *db.AgentRecord, message string) chatTurnState {
	webTargetURL, useWebAgent := detectWebCrawlIntent(message)
	return chatTurnState{
		systemPrompt:     composeSuperAdminSystemPrompt(activeAgent.SystemPrompt),
		effectiveMessage: message,
		userScope:        scopedUserID(owner.OwnerID, activeAgent.Name),
		webTargetURL:     webTargetURL,
		useWebAgent:      useWebAgent,
	}
}

func (r *Runner) applyLongTermMemoryContext(activeAgent *db.AgentRecord, message string, state *chatTurnState) {
	if r.memory == nil {
		return
	}
	lt, err := r.memory.GetLongTerm(activeAgent.Name, 2200)
	if err != nil || strings.TrimSpace(lt) == "" {
		return
	}
	state.longTermMemory = lt
	state.effectiveMessage = "Long-term memory context:\n" + lt + "\n\nUser message:\n" + message
}

func (r *Runner) resolveWebIntentFromMemory(message string, state *chatTurnState) {
	if state.useWebAgent {
		return
	}
	rememberedURL, fromMemory := detectWebCrawlIntentFromMemory(message, state.longTermMemory)
	if fromMemory {
		state.webTargetURL = rememberedURL
		state.useWebAgent = true
	}
}

func (r *Runner) runHostCommandPhase(
	ctx context.Context,
	setPhase func(string),
	providerClient provider.Provider,
	activeAgent *db.AgentRecord,
	message string,
	state *chatTurnState,
) (hostCommandOutcome, error) {
	if r.hostCmd == nil {
		return hostCommandOutcome{}, nil
	}

	likelyHostTask := isLikelyHostTaskRequest(message)
	if !likelyHostTask {
		return hostCommandOutcome{}, nil
	}

	hostAwareMessage := composeHostAwareIntentInput(message)
	intentCommand, intentErr := inferLinuxCommandIntent(ctx, providerClient, hostAwareMessage)
	if (intentErr != nil || strings.TrimSpace(intentCommand) == "") && likelyHostTask {
		// Second pass for host/file tasks: force command synthesis before giving up.
		forcedCommand, forcedErr := inferLinuxCommandIntentRequired(ctx, providerClient, hostAwareMessage)
		if forcedErr == nil && strings.TrimSpace(forcedCommand) != "" {
			intentCommand = forcedCommand
			intentErr = nil
		}
	}

	if intentErr != nil || strings.TrimSpace(intentCommand) == "" {
		if likelyHostTask {
			state.effectiveMessage = "This appears to require host/file execution, but no safe command could be inferred automatically.\n\nUser message:\n" + message
		}
		return hostCommandOutcome{}, nil
	}

	approved := true
	if requiresHostCommandApproval(intentCommand) {
		setPhase("Awaiting permission")
		var approveErr error
		approved, approveErr = r.confirmHostCommand(intentCommand)
		if approveErr != nil {
			return hostCommandOutcome{}, fmt.Errorf("host command permission prompt failed: %w", approveErr)
		}
		if !approved {
			if r.memory != nil {
				_ = r.memory.AppendDaily(activeAgent.Name, "linux.command", fmt.Sprintf("denied: %s", intentCommand))
			}
			state.effectiveMessage = "Host command candidate was not executed because user denied permission.\nDenied command: " + intentCommand + "\n\nUser message:\n" + message
			return hostCommandOutcome{}, nil
		}
	}

	setPhase("Executing linux command")
	commandResult, runErr := r.hostCmd.Execute(ctx, intentCommand)
	if runErr != nil {
		execReason := fmt.Sprintf("Command: %s\nError: %s", intentCommand, strings.TrimSpace(runErr.Error()))
		state.effectiveMessage = "Host command execution attempt failed.\nCommand: " + intentCommand + "\nError: " + runErr.Error() + "\n\nUser message:\n" + message
		if r.memory != nil {
			_ = r.memory.AppendDaily(activeAgent.Name, "linux.command", fmt.Sprintf("failed: %s (%v)", intentCommand, runErr))
		}
		return hostCommandOutcome{failureReason: execReason}, nil
	}
	if fallbackCommand, ok := suggestFallbackCommand(commandResult); ok {
		setPhase("Retrying compatible command")
		fallbackResult, fallbackErr := r.hostCmd.Execute(ctx, fallbackCommand)
		if fallbackErr == nil {
			commandResult = fallbackResult
		}
	}

	state.systemPrompt = composeLinuxToolSystemPrompt(state.systemPrompt, commandResult)
	state.effectiveMessage = composeLinuxToolUserMessage(message, commandResult)
	state.useWebAgent = false
	if r.memory != nil {
		_ = r.memory.AppendDaily(activeAgent.Name, "linux.command", fmt.Sprintf("%s (exit=%d)", commandResult.Command, commandResult.ExitCode))
	}
	resultCopy := commandResult
	return hostCommandOutcome{result: &resultCopy}, nil
}

func (r *Runner) runWebCrawlPhase(
	ctx context.Context,
	setPhase func(string),
	owner *db.Owner,
	message string,
	state *chatTurnState,
) error {
	if !state.useWebAgent || r.crawler == nil {
		return nil
	}

	setPhase("Thinking..")
	page, crawlErr := r.crawler.Fetch(ctx, state.webTargetURL, 12000, 20)
	if crawlErr != nil {
		return fmt.Errorf("web agent crawl failed: %w", crawlErr)
	}

	state.systemPrompt = composeSuperAdminSystemPrompt(builtinWebAgentPrompt())
	state.effectiveMessage = composeWebAgentMessage(message, page)
	state.userScope = scopedUserID(owner.OwnerID, "builtin:web")
	return nil
}

func (r *Runner) handleMemoryIntents(agentName string, message string) (bool, error) {
	if r.memory == nil {
		return false, nil
	}
	if note, ok := detectRememberIntent(message); ok {
		if err := r.memory.AppendLongTerm(agentName, note); err != nil {
			return true, err
		}
		_ = r.memory.AppendDaily(agentName, "memory", "remembered: "+note)
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.success("Noted. I saved that to long-term memory."))
		return true, nil
	}
	if job, ok := parseAutomationIntent(message); ok {
		if err := r.memory.UpsertAutomation(agentName, job); err != nil {
			return true, err
		}
		_ = r.memory.AppendDaily(agentName, "automation", fmt.Sprintf("upserted job %s (%s)", job.ID, job.CronExpr))
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.success("Scheduled automation saved"))
		fmt.Fprintln(r.stdout, out.kv("🆔 ID", job.ID))
		fmt.Fprintln(r.stdout, out.kv("⏱ Cron", job.CronExpr))
		fmt.Fprintln(r.stdout, out.kv("📅 Next Run", job.NextRunAt.Format(time.RFC3339)))
		return true, nil
	}
	return false, nil
}

func (r *Runner) runHistory(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	limit := fs.Int("limit", 20, "Maximum number of messages")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *limit <= 0 {
		return errors.New("limit must be greater than zero")
	}

	owner, err := r.requireAuthenticatedOwner(ctx)
	if err != nil {
		return err
	}

	activeAgent, err := r.getCurrentAgent(ctx)
	if err != nil {
		return err
	}

	history, err := r.store.GetRecentMessages(ctx, scopedUserID(owner.OwnerID, activeAgent.Name), *limit)
	if err != nil {
		return err
	}
	if len(history) == 0 {
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.warn(fmt.Sprintf("No messages found for agent %q.", activeAgent.Name)))
		return nil
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.section("🕘 Chat History"))
	fmt.Fprintln(r.stdout, out.kv("🤖 Agent", activeAgent.Name))
	for _, msg := range history {
		timeLabel := time.UnixMilli(msg.CreatedAt).Format(time.RFC3339)
		fmt.Fprintf(r.stdout, "[%s] %s: %s\n", timeLabel, msg.Role, msg.Content)
	}
	return nil
}

func (r *Runner) buildProviderClient(providerKind string, activeAgent *db.AgentRecord) provider.Provider {
	apiKeyResolver := func(ctx context.Context) (string, error) {
		return r.resolveAPIKey(ctx, activeAgent.Name)
	}

	var base provider.Provider
	switch providerKind {
	case providerKindOpenAI:
		base = provider.NewOpenAIProvider(
			activeAgent.BaseURL,
			activeAgent.Model,
			r.cfg.OllamaTimeout,
			apiKeyResolver,
		)
	default:
		base = provider.NewOllamaProvider(
			activeAgent.BaseURL,
			activeAgent.Model,
			r.cfg.OllamaTimeout,
			apiKeyResolver,
		)
	}
	if isLLMDebugEnabled() {
		return provider.WithDebugLogging(base, r.stderr)
	}
	return base
}

func isLLMDebugEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("SEMICLAW_DEBUG_LLM")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (r *Runner) runAgent(ctx context.Context, args []string) error {
	owner, err := r.requireAuthenticatedOwner(ctx)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		r.printAgentHelp()
		return nil
	}

	switch args[0] {
	case "list":
		return r.runAgentList(ctx)
	case "new":
		return r.runAgentNew(ctx, args[1:])
	case "config":
		return r.runAgentConfig(ctx, args[1:])
	case "switch":
		return r.runAgentSwitch(ctx, args[1:])
	case "delete":
		return r.runAgentDelete(ctx, owner, args[1:])
	default:
		return fmt.Errorf("unknown agent subcommand %q", args[0])
	}
}

func (r *Runner) runAgentList(ctx context.Context) error {
	if err := r.ensureDefaultAgent(ctx); err != nil {
		return err
	}

	currentName, err := r.getCurrentAgentName(ctx)
	if err != nil {
		return err
	}

	agents, err := r.store.ListAgents(ctx)
	if err != nil {
		return err
	}

	if len(agents) == 0 {
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.warn("No agents found."))
		return nil
	}

	out := r.themeFor(r.stdout)
	for _, record := range agents {
		prefix := "•"
		if record.Name == currentName {
			prefix = "◆"
		}
		kind, err := r.getAgentProviderKind(ctx, record.Name)
		if err != nil {
			return err
		}
		fmt.Fprintf(
			r.stdout,
			"%s %s (%s, %s, %s)\n",
			out.paint("1;38;5;51", prefix),
			out.value(record.Name),
			out.paint("38;5;220", "provider="+kind),
			out.paint("38;5;159", "model="+record.Model),
			out.paint("38;5;146", "base="+record.BaseURL),
		)
	}
	return nil
}

func (r *Runner) runAgentConfig(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent config", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	systemPrompt := fs.String("system-prompt", "", "Override system prompt")
	model := fs.String("model", "", "Override model")
	baseURL := fs.String("base-url", "", "Override provider base URL")
	providerKind := fs.String("provider", "", "Provider type: ollama|openai")
	apiKey := fs.String("api-key", "", "Provider API key")
	clearAPIKey := fs.Bool("clear-api-key", false, "Clear provider API key for current agent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) > 0 {
		return errors.New("usage: semiclaw agent config [--system-prompt ...] [--model ...] [--base-url ...] [--provider ollama|openai] [--api-key ...] [--clear-api-key]")
	}
	if *clearAPIKey && strings.TrimSpace(*apiKey) != "" {
		return errors.New("use either --api-key or --clear-api-key, not both")
	}

	currentAgent, err := r.getCurrentAgent(ctx)
	if err != nil {
		return err
	}
	currentProviderKind, err := r.getAgentProviderKind(ctx, currentAgent.Name)
	if err != nil {
		return err
	}

	seen := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	interactive := len(seen) == 0

	nextSystemPrompt := strings.TrimSpace(currentAgent.SystemPrompt)
	nextModel := strings.TrimSpace(currentAgent.Model)
	nextBaseURL := strings.TrimSpace(currentAgent.BaseURL)
	nextProviderKind := strings.TrimSpace(currentProviderKind)

	if interactive {
		promptVal, promptErr := r.prompt("System prompt (blank=keep current): ")
		if promptErr != nil {
			return promptErr
		}
		if strings.TrimSpace(promptVal) != "" {
			nextSystemPrompt = strings.TrimSpace(promptVal)
		}

		modelVal, modelErr := r.promptWithDefault("Model", nextModel)
		if modelErr != nil {
			return modelErr
		}
		nextModel = strings.TrimSpace(modelVal)

		providerVal, providerErr := r.promptWithDefault("Provider type (ollama/openai)", nextProviderKind)
		if providerErr != nil {
			return providerErr
		}
		nextProviderKind = normalizeProviderKind(providerVal)
		if nextProviderKind != providerKindOllama && nextProviderKind != providerKindOpenAI {
			return errors.New("provider type must be either 'ollama' or 'openai'")
		}

		baseURLPrompt := "Ollama base URL"
		if nextProviderKind == providerKindOpenAI {
			baseURLPrompt = "OpenAI-compatible base URL"
		}
		baseURLVal, baseURLErr := r.promptWithDefault(baseURLPrompt, nextBaseURL)
		if baseURLErr != nil {
			return baseURLErr
		}
		nextBaseURL = strings.TrimSpace(baseURLVal)
	} else {
		if seen["system-prompt"] {
			nextSystemPrompt = strings.TrimSpace(*systemPrompt)
		}
		if seen["model"] {
			nextModel = strings.TrimSpace(*model)
		}
		if seen["base-url"] {
			nextBaseURL = strings.TrimSpace(*baseURL)
		}
		if seen["provider"] {
			nextProviderKind = normalizeProviderKind(*providerKind)
			if nextProviderKind != providerKindOllama && nextProviderKind != providerKindOpenAI {
				return errors.New("provider type must be either 'ollama' or 'openai'")
			}
		}
	}

	if nextSystemPrompt == "" {
		nextSystemPrompt = defaultPromptForAgent(currentAgent.Name)
	}
	if nextModel == "" {
		nextModel = r.cfg.DefaultModel
	}
	if nextBaseURL == "" {
		if nextProviderKind == providerKindOpenAI {
			nextBaseURL = "https://api.openai.com"
		} else {
			nextBaseURL = r.cfg.OllamaBaseURL
		}
	}

	if err := r.store.UpsertAgent(ctx, db.AgentRecord{
		Name:         currentAgent.Name,
		SystemPrompt: nextSystemPrompt,
		Model:        nextModel,
		BaseURL:      nextBaseURL,
	}); err != nil {
		return err
	}
	if err := r.setAgentProviderKind(ctx, currentAgent.Name, nextProviderKind); err != nil {
		return err
	}

	if *clearAPIKey {
		if err := r.store.DeleteSecret(ctx, agentSecretKey(currentAgent.Name)); err != nil {
			return err
		}
	} else if strings.TrimSpace(*apiKey) != "" {
		ciphertext, nonce, encErr := r.secretBox.Encrypt(strings.TrimSpace(*apiKey))
		if encErr != nil {
			return fmt.Errorf("encrypt provider key: %w", encErr)
		}
		if err := r.store.UpsertSecret(ctx, agentSecretKey(currentAgent.Name), ciphertext, nonce); err != nil {
			return fmt.Errorf("store provider key: %w", err)
		}
	} else if interactive {
		apiKeyInput, apiErr := r.prompt("Provider API key (optional, blank=keep current, -=clear): ")
		if apiErr != nil {
			return apiErr
		}
		apiKeyInput = strings.TrimSpace(apiKeyInput)
		switch apiKeyInput {
		case "":
			// keep
		case "-":
			if err := r.store.DeleteSecret(ctx, agentSecretKey(currentAgent.Name)); err != nil {
				return err
			}
		default:
			ciphertext, nonce, encErr := r.secretBox.Encrypt(apiKeyInput)
			if encErr != nil {
				return fmt.Errorf("encrypt provider key: %w", encErr)
			}
			if err := r.store.UpsertSecret(ctx, agentSecretKey(currentAgent.Name), ciphertext, nonce); err != nil {
				return fmt.Errorf("store provider key: %w", err)
			}
		}
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.success(fmt.Sprintf("Updated current agent %q.", currentAgent.Name)))
	fmt.Fprintln(r.stdout, out.kv("🔌 Provider", nextProviderKind))
	fmt.Fprintln(r.stdout, out.kv("🧠 Model", nextModel))
	fmt.Fprintln(r.stdout, out.kv("🌐 Base URL", nextBaseURL))
	return nil
}

func (r *Runner) runAgentNew(ctx context.Context, args []string) error {
	if err := r.ensureDefaultAgent(ctx); err != nil {
		return err
	}

	var name string
	if len(args) > 0 {
		name = strings.TrimSpace(args[0])
	} else {
		prompted, err := r.prompt("Agent name: ")
		if err != nil {
			return err
		}
		name = strings.TrimSpace(prompted)
	}
	if err := validateAgentName(name); err != nil {
		return err
	}

	existing, err := r.store.GetAgent(ctx, name)
	if err != nil {
		return err
	}

	isUpdate := existing != nil
	if isUpdate {
		ok, err := r.promptYesNo(fmt.Sprintf("Agent %q exists. Update config? [y/N]: ", name), false)
		if err != nil {
			return err
		}
		if !ok {
			out := r.themeFor(r.stdout)
			fmt.Fprintln(r.stdout, out.info("No changes made."))
			return nil
		}
	}

	defaultPrompt := defaultPromptForAgent(name)
	defaultModel := r.cfg.DefaultModel
	defaultBaseURL := r.cfg.OllamaBaseURL
	defaultProviderKind := providerKindOllama
	if existing != nil {
		defaultPrompt = existing.SystemPrompt
		defaultModel = existing.Model
		defaultBaseURL = existing.BaseURL
		defaultProviderKind, err = r.getAgentProviderKind(ctx, name)
		if err != nil {
			return err
		}
	}

	systemPrompt, err := r.prompt("System prompt (blank=default): ")
	if err != nil {
		return err
	}
	systemPrompt = strings.TrimSpace(systemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultPrompt
	}
	model, err := r.promptWithDefault("Model", defaultModel)
	if err != nil {
		return err
	}
	providerKindInput, err := r.promptWithDefault("Provider type (ollama/openai)", defaultProviderKind)
	if err != nil {
		return err
	}
	providerKindInput = normalizeProviderKind(providerKindInput)
	if providerKindInput != providerKindOllama && providerKindInput != providerKindOpenAI {
		return errors.New("provider type must be either 'ollama' or 'openai'")
	}

	baseURLPrompt := "Ollama base URL"
	if providerKindInput == providerKindOpenAI {
		baseURLPrompt = "OpenAI-compatible base URL"
	}
	baseURLDefault := defaultBaseURL
	if providerKindInput == providerKindOpenAI && strings.TrimSpace(baseURLDefault) == strings.TrimSpace(r.cfg.OllamaBaseURL) {
		baseURLDefault = "https://api.openai.com"
	}
	baseURL, err := r.promptWithDefault(baseURLPrompt, baseURLDefault)
	if err != nil {
		return err
	}

	record := db.AgentRecord{
		Name:         name,
		SystemPrompt: strings.TrimSpace(systemPrompt),
		Model:        strings.TrimSpace(model),
		BaseURL:      strings.TrimSpace(baseURL),
	}
	if record.SystemPrompt == "" {
		record.SystemPrompt = defaultPromptForAgent(name)
	}
	if record.Model == "" {
		record.Model = r.cfg.DefaultModel
	}
	if record.BaseURL == "" {
		record.BaseURL = r.cfg.OllamaBaseURL
	}

	if err := r.store.UpsertAgent(ctx, record); err != nil {
		return err
	}
	if err := r.setAgentProviderKind(ctx, name, providerKindInput); err != nil {
		return err
	}

	var keyPrompt string
	if isUpdate {
		keyPrompt = "Provider API key (optional, blank=keep current, -=clear): "
	} else {
		keyPrompt = "Provider API key (optional): "
	}
	apiKeyInput, err := r.prompt(keyPrompt)
	if err != nil {
		return err
	}
	apiKeyInput = strings.TrimSpace(apiKeyInput)

	switch {
	case isUpdate && apiKeyInput == "":
		// keep current key
	case apiKeyInput == "-":
		if err := r.store.DeleteSecret(ctx, agentSecretKey(name)); err != nil {
			return err
		}
	default:
		if apiKeyInput != "" {
			ciphertext, nonce, err := r.secretBox.Encrypt(apiKeyInput)
			if err != nil {
				return fmt.Errorf("encrypt provider key: %w", err)
			}
			if err := r.store.UpsertSecret(ctx, agentSecretKey(name), ciphertext, nonce); err != nil {
				return fmt.Errorf("store provider key: %w", err)
			}
		}
	}

	if isUpdate {
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.success(fmt.Sprintf("Updated agent %q.", name)))
	} else {
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.success(fmt.Sprintf("Created agent %q.", name)))
	}

	switchNow, err := r.promptYesNo("Switch to this agent now? [Y/n]: ", true)
	if err != nil {
		return err
	}
	if switchNow {
		if err := r.store.UpsertConfig(ctx, "agent.current", name); err != nil {
			return err
		}
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.info(fmt.Sprintf("Current agent set to %q.", name)))
	}
	return nil
}

func (r *Runner) runAgentSwitch(ctx context.Context, args []string) error {
	if err := r.ensureDefaultAgent(ctx); err != nil {
		return err
	}

	if len(args) != 1 {
		return errors.New(`usage: semiclaw agent switch <name>`)
	}
	name := strings.TrimSpace(args[0])
	if err := validateAgentName(name); err != nil {
		return err
	}

	record, err := r.store.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("agent %q does not exist", name)
	}

	if err := r.store.UpsertConfig(ctx, "agent.current", name); err != nil {
		return err
	}
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.info(fmt.Sprintf("Current agent switched to %q.", name)))
	return nil
}

func (r *Runner) runAgentDelete(ctx context.Context, owner *db.Owner, args []string) error {
	if err := r.ensureDefaultAgent(ctx); err != nil {
		return err
	}

	if len(args) != 1 {
		return errors.New(`usage: semiclaw agent delete <name>`)
	}
	name := strings.TrimSpace(args[0])
	if err := validateAgentName(name); err != nil {
		return err
	}
	if name == defaultAgentName {
		return errors.New(`cannot delete default agent "semiclaw"`)
	}

	record, err := r.store.GetAgent(ctx, name)
	if err != nil {
		return err
	}
	if record == nil {
		return fmt.Errorf("agent %q does not exist", name)
	}

	ok, err := r.promptYesNo(fmt.Sprintf("Delete agent %q? [y/N]: ", name), false)
	if err != nil {
		return err
	}
	if !ok {
		out := r.themeFor(r.stdout)
		fmt.Fprintln(r.stdout, out.info("No changes made."))
		return nil
	}

	currentName, err := r.getCurrentAgentName(ctx)
	if err != nil {
		return err
	}
	if currentName == name {
		if err := r.store.UpsertConfig(ctx, "agent.current", defaultAgentName); err != nil {
			return err
		}
	}

	if err := r.store.DeleteMessagesByUserID(ctx, scopedUserID(owner.OwnerID, name)); err != nil {
		return err
	}
	if err := r.store.DeleteSecret(ctx, agentSecretKey(name)); err != nil {
		return err
	}
	if err := r.store.DeleteConfig(ctx, agentProviderKindConfigKey(name)); err != nil {
		return err
	}
	if err := r.store.DeleteAgent(ctx, name); err != nil {
		return err
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.success(fmt.Sprintf("Deleted agent %q.", name)))
	return nil
}

func (r *Runner) requireAuthenticatedOwner(ctx context.Context) (*db.Owner, error) {
	owner, err := r.store.GetOwner(ctx)
	if err != nil {
		return nil, err
	}
	if owner == nil {
		return nil, errors.New("no owner configured, run setup first")
	}

	authenticated, err := r.isAuthenticated(owner)
	if err != nil {
		return nil, err
	}
	if !authenticated {
		return nil, errors.New("not authenticated, run login")
	}

	return owner, nil
}

func (r *Runner) resolveAPIKey(ctx context.Context, agentName string) (string, error) {
	agentSecret, err := r.store.GetSecret(ctx, agentSecretKey(agentName))
	if err != nil {
		return "", err
	}
	if agentSecret != nil {
		return r.secretBox.Decrypt(agentSecret.Ciphertext, agentSecret.Nonce)
	}

	globalSecret, err := r.store.GetSecret(ctx, "provider.ollama.apiKey")
	if err != nil {
		return "", err
	}
	if globalSecret == nil {
		return "", nil
	}
	return r.secretBox.Decrypt(globalSecret.Ciphertext, globalSecret.Nonce)
}

func (r *Runner) ensureDefaultAgent(ctx context.Context) error {
	existing, err := r.store.GetAgent(ctx, defaultAgentName)
	if err != nil {
		return err
	}
	if existing == nil {
		if err := r.store.UpsertAgent(ctx, db.AgentRecord{
			Name:         defaultAgentName,
			SystemPrompt: agent.DefaultSystemPrompt,
			Model:        r.cfg.DefaultModel,
			BaseURL:      r.cfg.OllamaBaseURL,
		}); err != nil {
			return err
		}
		if err := r.setAgentProviderKind(ctx, defaultAgentName, providerKindOllama); err != nil {
			return err
		}
	}
	if _, err := r.getAgentProviderKind(ctx, defaultAgentName); err != nil {
		return err
	}

	current, ok, err := r.store.GetConfig(ctx, "agent.current")
	if err != nil {
		return err
	}
	current = strings.TrimSpace(current)
	if !ok || current == "" {
		return r.store.UpsertConfig(ctx, "agent.current", defaultAgentName)
	}

	record, err := r.store.GetAgent(ctx, current)
	if err != nil {
		return err
	}
	if record == nil {
		return r.store.UpsertConfig(ctx, "agent.current", defaultAgentName)
	}
	return nil
}

func (r *Runner) getCurrentAgentName(ctx context.Context) (string, error) {
	if err := r.ensureDefaultAgent(ctx); err != nil {
		return "", err
	}

	name, ok, err := r.store.GetConfig(ctx, "agent.current")
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		name = defaultAgentName
	}
	return name, nil
}

func (r *Runner) getCurrentAgent(ctx context.Context) (*db.AgentRecord, error) {
	name, err := r.getCurrentAgentName(ctx)
	if err != nil {
		return nil, err
	}
	record, err := r.store.GetAgent(ctx, name)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("current agent %q is missing", name)
	}
	return record, nil
}

func (r *Runner) getAgentProviderKind(ctx context.Context, agentName string) (string, error) {
	key := agentProviderKindConfigKey(agentName)
	val, ok, err := r.store.GetConfig(ctx, key)
	if err != nil {
		return "", err
	}

	if !ok || strings.TrimSpace(val) == "" {
		if err := r.setAgentProviderKind(ctx, agentName, providerKindOllama); err != nil {
			return "", err
		}
		return providerKindOllama, nil
	}

	kind := normalizeProviderKind(val)
	if kind != providerKindOllama && kind != providerKindOpenAI {
		kind = providerKindOllama
		if err := r.setAgentProviderKind(ctx, agentName, kind); err != nil {
			return "", err
		}
	}
	return kind, nil
}

func (r *Runner) setAgentProviderKind(ctx context.Context, agentName string, kind string) error {
	kind = normalizeProviderKind(kind)
	if kind != providerKindOllama && kind != providerKindOpenAI {
		return errors.New("provider kind must be either 'ollama' or 'openai'")
	}
	return r.store.UpsertConfig(ctx, agentProviderKindConfigKey(agentName), kind)
}

func preferredOllamaBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return "http://127.0.0.1:11434"
	}
	if strings.EqualFold(base, "https://ollama.com") {
		return "http://127.0.0.1:11434"
	}
	return base
}

func normalizeProviderKind(raw string) string {
	val := strings.TrimSpace(strings.ToLower(raw))
	switch val {
	case providerKindOpenAI:
		return providerKindOpenAI
	default:
		return providerKindOllama
	}
}

func validateAgentName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("agent name is required")
	}
	if !agentNamePattern.MatchString(name) {
		return errors.New("invalid agent name: use letters, numbers, '-' or '_' (max 64 chars)")
	}
	return nil
}

func defaultPromptForAgent(name string) string {
	if name == defaultAgentName {
		return agent.DefaultSystemPrompt
	}
	return fmt.Sprintf(
		"You are %s, a specialized AI assistant.\nBe practical, clear, and action-oriented.\nPrefer short responses unless the user asks for details.",
		name,
	)
}

func agentSecretKey(name string) string {
	return "provider.ollama.apiKey." + name
}

func agentProviderKindConfigKey(name string) string {
	return "agent.provider_kind." + name
}

func scopedUserID(ownerID string, agentName string) string {
	return ownerID + "::agent:" + agentName
}

func detectWebCrawlIntent(message string) (string, bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", false
	}

	targetURL := firstURLInText(message)
	if targetURL == "" {
		targetURL = inferBuiltinWebSourceURL(message)
		if targetURL == "" {
			return "", false
		}
		return targetURL, true
	}

	if hasWebIntentKeyword(message, targetURL) {
		return targetURL, true
	}
	return targetURL, true
}

func detectWebCrawlIntentFromMemory(message string, longTermMemory string) (string, bool) {
	message = strings.TrimSpace(message)
	longTermMemory = strings.TrimSpace(longTermMemory)
	if message == "" || longTermMemory == "" {
		return "", false
	}

	targetURL := lastURLInText(longTermMemory)
	if targetURL == "" {
		return "", false
	}

	lower := strings.ToLower(message)
	memoryRefKeywords := []string{
		"saved url",
		"remembered url",
		"from memory",
		"that url",
		"that link",
		"the url",
	}
	for _, keyword := range memoryRefKeywords {
		if strings.Contains(lower, keyword) {
			return targetURL, true
		}
	}

	if hasWebIntentKeyword(message, targetURL) {
		return targetURL, true
	}
	return "", false
}

func hasWebIntentKeyword(message string, targetURL string) bool {
	lower := strings.ToLower(message)
	keywords := []string{
		"crawl",
		"scrape",
		"extract",
		"visit",
		"browse",
		"go to",
		"open url",
		"read url",
		"fetch url",
		"visit url",
		"open this url",
		"summarize this url",
		"summarise this url",
		"analyze this url",
		"analyse this url",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	trimmed := strings.TrimSpace(lower)
	if trimmed == strings.ToLower(targetURL) {
		return true
	}

	return false
}

type linuxIntent struct {
	Action     string  `json:"action"`
	Command    string  `json:"command"`
	Confidence float64 `json:"confidence"`
}

func inferLinuxCommandIntent(ctx context.Context, model provider.Provider, userMessage string) (string, error) {
	if model == nil {
		return "", nil
	}
	decisionPrompt := `You classify whether a host shell command should be executed for the user request.
Return JSON only with this schema:
{"action":"command|none","command":"string","confidence":0.0}
Rules:
- The user message includes host context (OS, CPU architecture, shell). Use it to choose OS/arch-appropriate command syntax and tools.
- Use action=command when the request involves host state, files, paths, services, processes, networking, package/tool checks, or explicit shell operations.
- If the user references a file/path (e.g. /etc/..., ~/..., *.conf/*.log/*.txt/*.md), strongly prefer action=command.
- command must be one single executable shell command without markdown fences or prose.
- The command must be runnable in the provided shell context and match the host OS conventions.
- Prefer deterministic, non-interactive commands.
- For read/inspect, prefer built-in/native utilities for the target OS (e.g. cat/head/tail/grep/rg/stat/ls/find on Unix-like systems; OS-native introspection tools when needed).
- For file write/update, prefer safe single-command edits with redirection or in-place tooling valid for that OS/shell.
- Never use interactive editors (vi/vim/nano/less/more), prompts, or commands waiting for stdin.
- If unsure, set action=none and command="".
- Never output explanations or markdown.`
	raw, err := model.Chat(ctx, []provider.Message{
		{Role: "system", Content: decisionPrompt},
		{Role: "user", Content: userMessage},
	})
	if err != nil {
		return "", err
	}

	intent, err := parseLinuxIntent(raw)
	if err != nil {
		return "", err
	}
	if strings.ToLower(strings.TrimSpace(intent.Action)) != "command" {
		return "", nil
	}
	return strings.TrimSpace(intent.Command), nil
}

func inferLinuxCommandIntentRequired(ctx context.Context, model provider.Provider, userMessage string) (string, error) {
	if model == nil {
		return "", nil
	}
	decisionPrompt := `You must propose one safe host shell command to execute for this request when possible.
Return JSON only with this schema:
{"action":"command|none","command":"string","confidence":0.0}
Rules:
- The user message includes host context (OS, CPU architecture, shell). Use it to produce an OS/arch-appropriate command.
- Prefer action=command for host/file/system tasks.
- command must be one single shell command without markdown fences or prose.
- The command must be runnable in the provided shell context and match the host OS conventions.
- If a file/path is mentioned, produce a command that operates on that path.
- For read/inspect, prefer native tools for the detected OS/shell.
- For write/update, prefer single-command safe edits valid for the target OS/shell.
- Never use interactive editors (vi/vim/nano/less/more), prompts, or commands waiting for stdin.
- Ensure command is practical for the detected host environment and does not require multi-step interaction.
- Avoid destructive commands.
- Use action=none only if no safe single command can help.`
	raw, err := model.Chat(ctx, []provider.Message{
		{Role: "system", Content: decisionPrompt},
		{Role: "user", Content: userMessage},
	})
	if err != nil {
		return "", err
	}

	intent, err := parseLinuxIntent(raw)
	if err != nil {
		return "", err
	}
	if strings.ToLower(strings.TrimSpace(intent.Action)) != "command" {
		return "", nil
	}
	return strings.TrimSpace(intent.Command), nil
}

func isLikelyHostTaskRequest(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}

	keywords := []string{
		"read file", "open file", "show file", "cat ", "tail ", "head ",
		"list files", "ls ", "find ", "grep ", "rg ", "search in ",
		"write file", "edit file", "create file", "delete file", "chmod", "chown",
		"run command", "execute", "shell", "terminal", "bash", "sh ",
		"process", "ps ", "service", "systemctl", "journalctl",
		"disk", "memory usage", "cpu", "network", "port", "ubuntu",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	// Basic path-like hints.
	if strings.Contains(message, "/") || strings.Contains(message, ".txt") || strings.Contains(message, ".log") || strings.Contains(message, ".md") {
		return true
	}
	return false
}

func composeHostAwareIntentInput(userMessage string) string {
	var b strings.Builder
	b.WriteString("Host execution context:\n")
	b.WriteString("- OS: ")
	b.WriteString(runtime.GOOS)
	b.WriteString("\n- Arch: ")
	b.WriteString(runtime.GOARCH)
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell != "" {
		b.WriteString("\n- Shell: ")
		b.WriteString(shell)
	}
	b.WriteString("\n\nUser request:\n")
	b.WriteString(strings.TrimSpace(userMessage))
	return b.String()
}

func requiresHostCommandApproval(command string) bool {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)

	// Any obvious write/mutation patterns require confirmation.
	mutationPatterns := []string{
		">", ">>", "| tee", " tee ",
		"sed -i", "perl -i", "ed ", "ex ",
		"mv ", "cp ", "rm ", "rmdir ", "mkdir ", "touch ",
		"chmod ", "chown ", "chgrp ", "ln ", "truncate ",
		"dd ", "install ", "echo ", "printf ",
	}
	for _, p := range mutationPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	// Read-only command starters are auto-approved.
	readOnlyPrefixes := []string{
		"cat ", "head ", "tail ", "less ", "more ",
		"ls", "find ", "grep ", "rg ", "awk ", "sed ",
		"wc ", "stat ", "file ", "readlink ", "pwd",
		"whoami", "id", "uname", "ps ", "ss ", "netstat ",
		"journalctl ", "systemctl status", "du ", "df ",
	}
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}

	// Unknown command shape stays protected.
	return true
}

func parseLinuxIntent(raw string) (linuxIntent, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return linuxIntent{}, errors.New("empty intent response")
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		trimmed = trimmed[start : end+1]
	}
	var out linuxIntent
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return linuxIntent{}, fmt.Errorf("parse linux intent json: %w", err)
	}
	return out, nil
}

func composeLinuxToolSystemPrompt(basePrompt string, result hostcmd.Result) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(basePrompt))
	b.WriteString("\n\nLinux command execution context (ground truth):\n")
	b.WriteString("Command: ")
	b.WriteString(result.Command)
	b.WriteString("\nExit code: ")
	b.WriteString(strconv.Itoa(result.ExitCode))
	b.WriteString("\nDuration: ")
	b.WriteString(result.Duration.Truncate(time.Millisecond).String())
	if strings.TrimSpace(result.Stdout) != "" {
		b.WriteString("\nstdout:\n")
		b.WriteString(limitForPrompt(result.Stdout, 5000))
	}
	if strings.TrimSpace(result.Stderr) != "" {
		b.WriteString("\nstderr:\n")
		b.WriteString(limitForPrompt(result.Stderr, 3000))
	}
	if strings.TrimSpace(result.Stdout) == "" && strings.TrimSpace(result.Stderr) == "" {
		b.WriteString("\n(no output)")
	}
	b.WriteString("\nUse this context directly when answering.")
	return b.String()
}

func composeLinuxToolUserMessage(originalUserMessage string, result hostcmd.Result) string {
	var b strings.Builder
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(originalUserMessage))
	b.WriteString("\n\nHost command already executed. You must answer using this ground truth output and must not claim lack of filesystem access.\n")
	b.WriteString("Command: ")
	b.WriteString(result.Command)
	b.WriteString("\nExit code: ")
	b.WriteString(strconv.Itoa(result.ExitCode))
	if strings.TrimSpace(result.Stdout) != "" {
		b.WriteString("\nstdout:\n")
		b.WriteString(limitForPrompt(result.Stdout, 7000))
	}
	if strings.TrimSpace(result.Stderr) != "" {
		b.WriteString("\nstderr:\n")
		b.WriteString(limitForPrompt(result.Stderr, 4000))
	}
	if strings.TrimSpace(result.Stdout) == "" && strings.TrimSpace(result.Stderr) == "" {
		b.WriteString("\n(no command output)")
	}
	return b.String()
}

func looksLikeFilesystemCapabilityRefusal(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	patterns := []string{
		"cannot directly access files",
		"can't directly access files",
		"cannot access files on your local filesystem",
		"can't access files on your local filesystem",
		"please run:",
		"paste the output here",
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

func composeCommandResultFallback(result hostcmd.Result) string {
	var b strings.Builder
	b.WriteString("I executed the host command and here is the result.\n")
	b.WriteString("Command: ")
	b.WriteString(result.Command)
	b.WriteString("\nExit code: ")
	b.WriteString(strconv.Itoa(result.ExitCode))
	if strings.TrimSpace(result.Stdout) != "" {
		b.WriteString("\n\nstdout:\n")
		b.WriteString(strings.TrimSpace(result.Stdout))
	}
	if strings.TrimSpace(result.Stderr) != "" {
		b.WriteString("\n\nstderr:\n")
		b.WriteString(strings.TrimSpace(result.Stderr))
	}
	if strings.TrimSpace(result.Stdout) == "" && strings.TrimSpace(result.Stderr) == "" {
		b.WriteString("\n\n(no output)")
	}
	return b.String()
}

func ensureExecutionFailureInResponse(response string, failureReason string) string {
	response = strings.TrimSpace(response)
	failureReason = strings.TrimSpace(failureReason)
	if failureReason == "" {
		return response
	}

	lower := strings.ToLower(response)
	if strings.Contains(lower, strings.ToLower(failureReason)) {
		return response
	}

	var b strings.Builder
	b.WriteString("Host execution failed.\n")
	b.WriteString(failureReason)
	if response != "" {
		b.WriteString("\n\n")
		b.WriteString(response)
	}
	return b.String()
}

func suggestFallbackCommand(result hostcmd.Result) (string, bool) {
	if result.ExitCode == 0 {
		return "", false
	}
	commandLower := strings.ToLower(strings.TrimSpace(result.Command))
	stderrLower := strings.ToLower(strings.TrimSpace(result.Stderr))

	if strings.Contains(commandLower, "ps") &&
		strings.Contains(commandLower, "--sort") &&
		strings.Contains(stderrLower, "ps: illegal option") {
		return "ps aux | sort -nr -k 3 | head -n 11", true
	}
	return "", false
}

func limitForPrompt(input string, maxChars int) string {
	input = strings.TrimSpace(input)
	if maxChars <= 0 || len(input) <= maxChars {
		return input
	}
	return input[:maxChars] + "\n...[truncated]"
}

func firstURLInText(text string) string {
	matches := urlPattern.FindAllString(text, -1)
	for _, match := range matches {
		candidate := strings.TrimSpace(match)
		candidate = strings.TrimRight(candidate, ".,);]}>\"'")
		if _, err := url.ParseRequestURI(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func lastURLInText(text string) string {
	matches := urlPattern.FindAllString(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(matches[i])
		candidate = strings.TrimRight(candidate, ".,);]}>\"'")
		if _, err := url.ParseRequestURI(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func builtinWebAgentPrompt() string {
	return "You are Semiclaw Web Agent, specialized in webpage extraction and grounded answers.\nUse only the provided webpage content and links as evidence.\nNever ask the user to visit URLs when enough content is already provided.\nIf user asks for latest news, return a numbered list with title + short summary + source URL for each item.\nBe concise and include source URLs when making claims."
}

func composeSuperAdminSystemPrompt(basePrompt string) string {
	basePrompt = strings.TrimSpace(basePrompt)
	if basePrompt == "" {
		basePrompt = agent.DefaultSystemPrompt
	}

	const superAdmin = `Semiclaw Super-Admin Runtime Directive:
- You run inside Semiclaw with built-in capabilities: web crawling, safe host command execution (with explicit user permission), and memory/automation handling.
- Prefer using available built-in capabilities when they directly improve factual grounding or task completion.
- If the user asks to read/summarize/extract from a URL or remembered link, use Semiclaw web-crawl context instead of generic speculation.
- If host state is required (files/processes/system info), propose a concrete Linux command and rely on Semiclaw's permission gate before execution.
- Respect safety: never claim a command/web action ran if it did not run; if denied or unavailable, clearly state that and continue with best effort.
- Keep outputs actionable and concise; include evidence/source context when provided by tools.`

	return basePrompt + "\n\n" + superAdmin
}

func composeWebAgentMessage(userMessage string, page *webcrawl.Page) string {
	var b strings.Builder
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(userMessage))
	b.WriteString("\n\nFetched webpage data:\n")
	b.WriteString("URL: ")
	b.WriteString(strings.TrimSpace(page.URL))
	b.WriteString("\n")
	if strings.TrimSpace(page.Title) != "" {
		b.WriteString("Title: ")
		b.WriteString(strings.TrimSpace(page.Title))
		b.WriteString("\n")
	}
	b.WriteString("Content:\n")
	if strings.TrimSpace(page.Text) == "" {
		b.WriteString("(no extractable text)\n")
	} else {
		b.WriteString(strings.TrimSpace(page.Text))
		b.WriteString("\n")
	}
	if len(page.Links) > 0 {
		b.WriteString("Links:\n")
		for i, link := range page.Links {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, link))
		}
	}
	return b.String()
}

func inferBuiltinWebSourceURL(message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return ""
	}

	isNewsRequest := strings.Contains(lower, "news") ||
		strings.Contains(lower, "latest") ||
		strings.Contains(lower, "headlines") ||
		strings.Contains(lower, "update")
	if !isNewsRequest {
		return ""
	}

	if strings.Contains(lower, "zaobao") {
		if strings.Contains(lower, "china") {
			return "https://www.zaobao.com/realtime/china"
		}
		return "https://www.zaobao.com/realtime"
	}

	return ""
}

func detectRememberIntent(message string) (string, bool) {
	trimmed := strings.TrimSpace(message)
	lower := strings.ToLower(trimmed)

	prefixes := []string{
		"remember:",
		"remember ",
		"note this:",
		"note that:",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			value := strings.TrimSpace(trimmed[len(prefix):])
			if value != "" {
				return value, true
			}
		}
	}
	return "", false
}

func parseAutomationIntent(message string) (memorymd.AutomationJob, bool) {
	trimmed := strings.TrimSpace(message)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "schedule:") && !strings.HasPrefix(lower, "cron:") {
		return memorymd.AutomationJob{}, false
	}

	payload := trimmed[strings.Index(trimmed, ":")+1:]
	parts := strings.Split(payload, "|")
	if len(parts) < 3 {
		return memorymd.AutomationJob{}, false
	}

	name := strings.TrimSpace(parts[0])
	cronExpr := strings.TrimSpace(parts[1])
	prompt := strings.TrimSpace(strings.Join(parts[2:], "|"))
	if name == "" || cronExpr == "" || prompt == "" {
		return memorymd.AutomationJob{}, false
	}

	id := sanitizeAutomationID(name)
	if id == "" {
		id = fmt.Sprintf("job_%d", time.Now().Unix())
	}

	nextRun, err := memorymd.NextRun(cronExpr, "UTC", time.Now().UTC())
	if err != nil {
		return memorymd.AutomationJob{}, false
	}

	return memorymd.AutomationJob{
		ID:        id,
		Name:      name,
		Enabled:   true,
		CronExpr:  cronExpr,
		TZ:        "UTC",
		Prompt:    prompt,
		NextRunAt: nextRun,
		UpdatedAt: time.Now().UTC(),
	}, true
}

func sanitizeAutomationID(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	var b strings.Builder
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			b.WriteRune(ch)
		}
	}
	return strings.Trim(b.String(), "_-")
}

func (r *Runner) withRainbowPhaseSpinner(run func(setPhase func(string)) error) error {
	errTheme := r.themeFor(r.stderr)
	if !isTerminalFile(r.stderr) {
		return run(func(string) {})
	}

	var mu sync.RWMutex
	phase := "Processing.."
	setPhase := func(next string) {
		clean := strings.TrimSpace(next)
		if clean == "" {
			return
		}
		mu.Lock()
		phase = clean
		mu.Unlock()
	}

	done := make(chan error, 1)
	go func() {
		done <- run(setPhase)
	}()

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	colors := []int{45, 51, 87, 123, 159, 195, 201}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	index := 0
	for {
		select {
		case err := <-done:
			fmt.Fprint(r.stderr, "\r\033[2K")
			return err
		case <-ticker.C:
			frame := frames[index%len(frames)]
			color := colors[index%len(colors)]
			mu.RLock()
			currentPhase := phase
			mu.RUnlock()
			if errTheme.color {
				fmt.Fprintf(r.stderr, "\r\033[1;38;5;%dm%s\033[0m \033[38;5;250m%s\033[0m", color, frame, currentPhase)
			} else {
				fmt.Fprintf(r.stderr, "\r%s %s", frame, currentPhase)
			}
			index++
		}
	}
}

func isTerminalFile(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func (r *Runner) promptSetupDataDir() (string, error) {
	defaultDir, err := config.DefaultDataDir()
	if err != nil {
		return "", err
	}

	answer, err := r.prompt(fmt.Sprintf("Data directory [%s]: ", defaultDir))
	if err != nil {
		return "", err
	}

	selected := strings.TrimSpace(answer)
	if selected == "" {
		selected = defaultDir
	}
	if strings.HasPrefix(selected, "~"+string(os.PathSeparator)) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		selected = filepath.Join(homeDir, strings.TrimPrefix(selected, "~"+string(os.PathSeparator)))
	}

	if !filepath.IsAbs(selected) {
		absPath, err := filepath.Abs(selected)
		if err != nil {
			return "", fmt.Errorf("resolve data directory path: %w", err)
		}
		selected = absPath
	}

	return filepath.Clean(selected), nil
}

func (r *Runner) switchDataDir(dataDir string) error {
	targetPaths := config.BuildPaths(dataDir)
	if err := os.MkdirAll(targetPaths.DataSubdir, 0o700); err != nil {
		return fmt.Errorf("create selected data directory: %w", err)
	}
	if err := os.Chmod(targetPaths.DataSubdir, 0o700); err != nil {
		return fmt.Errorf("secure selected data directory: %w", err)
	}

	newDB, err := db.Open(targetPaths.DBPath)
	if err != nil {
		return fmt.Errorf("open selected database: %w", err)
	}
	if err := db.RunMigrations(newDB, r.cfg.MigrationsDir); err != nil {
		_ = newDB.Close()
		return fmt.Errorf("run migrations in selected data directory: %w", err)
	}

	newSecretBox, err := auth.LoadOrCreateSecretBox(targetPaths.EncryptionKeyPath)
	if err != nil {
		_ = newDB.Close()
		return fmt.Errorf("initialize secret key in selected data directory: %w", err)
	}

	oldDB := r.store.DB()
	r.store = db.NewStore(newDB)
	r.secretBox = newSecretBox
	r.cfg.DataDir = targetPaths.DataDir
	r.cfg.DBPath = targetPaths.DBPath
	r.cfg.EncryptionKeyPath = targetPaths.EncryptionKeyPath
	r.cfg.SessionTokenPath = targetPaths.SessionTokenPath

	if oldDB != nil {
		_ = oldDB.Close()
	}
	return nil
}

func (r *Runner) prompt(label string) (string, error) {
	fmt.Fprint(r.stdout, label)
	reader := bufio.NewReader(r.stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (r *Runner) promptWithDefault(label string, fallback string) (string, error) {
	text, err := r.prompt(fmt.Sprintf("%s [%s]: ", label, fallback))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return fallback, nil
	}
	return text, nil
}

func (r *Runner) promptYesNo(label string, defaultYes bool) (bool, error) {
	text, err := r.prompt(label)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "":
		return defaultYes, nil
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, errors.New("please answer yes or no")
	}
}

func (r *Runner) confirmHostCommand(command string) (bool, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return false, nil
	}

	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.warn("Host command execution requested"))
	fmt.Fprintln(r.stdout, out.kv("💻 Command", command))

	file, ok := r.stdin.(*os.File)
	if !ok {
		fmt.Fprintln(r.stdout, out.warn("Non-interactive input detected. Command execution denied by default."))
		return false, nil
	}
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if (info.Mode() & os.ModeCharDevice) == 0 {
		fmt.Fprintln(r.stdout, out.warn("Piped input detected. Command execution denied by default."))
		return false, nil
	}

	for {
		allowed, promptErr := r.promptYesNo("Allow this command to run on the host? [Y/n]: ", true)
		if promptErr == nil {
			if allowed {
				fmt.Fprintln(r.stdout, out.success("Command approved."))
			} else {
				fmt.Fprintln(r.stdout, out.info("Command denied."))
			}
			return allowed, nil
		}
		if promptErr.Error() != "please answer yes or no" {
			return false, promptErr
		}
		fmt.Fprintln(r.stdout, out.warn("Please answer with 'y' or 'n'."))
	}
}

func (r *Runner) confirmToolApproval(tool string, detail string) (bool, error) {
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.warn("Tool execution requested"))
	fmt.Fprintln(r.stdout, out.kv("🧰 Tool", strings.TrimSpace(tool)))
	fmt.Fprintln(r.stdout, out.kv("📝 Payload", strings.TrimSpace(detail)))

	file, ok := r.stdin.(*os.File)
	if !ok {
		fmt.Fprintln(r.stdout, out.warn("Non-interactive input detected. Tool execution denied by default."))
		return false, nil
	}
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if (info.Mode() & os.ModeCharDevice) == 0 {
		fmt.Fprintln(r.stdout, out.warn("Piped input detected. Tool execution denied by default."))
		return false, nil
	}

	for {
		allowed, promptErr := r.promptYesNo("Allow this tool call? [Y/n]: ", true)
		if promptErr == nil {
			if allowed {
				fmt.Fprintln(r.stdout, out.success("Tool call approved."))
			} else {
				fmt.Fprintln(r.stdout, out.info("Tool call denied."))
			}
			return allowed, nil
		}
		if promptErr.Error() != "please answer yes or no" {
			return false, promptErr
		}
		fmt.Fprintln(r.stdout, out.warn("Please answer with 'y' or 'n'."))
	}
}

func (r *Runner) isAuthenticated(owner *db.Owner) (bool, error) {
	if owner == nil || strings.TrimSpace(owner.SessionTokenHash) == "" {
		return false, nil
	}

	raw, err := os.ReadFile(r.cfg.SessionTokenPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	token := strings.TrimSpace(string(raw))
	if token == "" {
		return false, nil
	}
	return auth.TokenMatchesHash(token, owner.SessionTokenHash), nil
}

func (r *Runner) writeSessionToken(token string) error {
	if err := os.MkdirAll(filepathDir(r.cfg.SessionTokenPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(r.cfg.SessionTokenPath, []byte(token+"\n"), 0o600)
}

func filepathDir(path string) string {
	idx := strings.LastIndex(path, string(os.PathSeparator))
	if idx <= 0 {
		return "."
	}
	return path[:idx]
}

func (r *Runner) printHelp() {
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.title("🚀 Semiclaw CLI"))
	fmt.Fprintln(r.stdout, "")
	fmt.Fprintln(r.stdout, out.section("Usage"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw setup")+" [--password <value>] [--api-key <value>] [--openai-base-url <url>] [--openai-api-key <key>] [--openai-model <model>] [--soul-seed <value>] [--skip-profile]")
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw login")+" [--password <value>]")
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw logout"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw status"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw chat")+" [message]")
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw history")+" [--limit 20]")
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent list"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent new"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent config")+" [--system-prompt ... --model ... --base-url ... --provider ollama|openai]")
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent switch <name>"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent delete <name>"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw help"))
}

func (r *Runner) printAgentHelp() {
	out := r.themeFor(r.stdout)
	fmt.Fprintln(r.stdout, out.section("🤖 Agent Commands"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent list"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent new"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent config")+" [flags]")
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent switch <name>"))
	fmt.Fprintln(r.stdout, "  "+out.command("semiclaw agent delete <name>"))
}
