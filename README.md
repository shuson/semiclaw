# Semiclaw
<p align="left">
  <img src="assets/semiclaw-mark.svg" alt="Semiclaw half-paw claw icon" width="88" />
</p>

A specified use case local focused. Surely, this is inspired by OpenClaw.

## Stack
- Backend: Go CLI
- Storage: SQLite (`modernc.org/sqlite`)

## Features
- First-time setup via CLI
- Setup asks for data directory (default: `~/.semiclaw`)
- Interactive setup questions for basic user profile
- Multi-agent management (`agent list/new/switch/delete`)
- Owner login/logout via CLI session token
- Owner status command
- Chat command using Ollama-compatible `/api/chat`
- Built-in Linux command executor from LLM-interpreted chat intents (no command-prefix syntax required)
- Built-in web agent auto-triggered from chat when URL crawling intent is detected
- Agent-scoped markdown long-term memory at `~/.semiclaw/memory/<agent>/MEMORY.md` via chat intent (`remember: ...`)
- Agent-scoped daily markdown memory logs at `~/.semiclaw/memory/<agent>/daily/YYYY-MM-DD.md`
- Agent-scoped automation definitions at `~/.semiclaw/memory/<agent>/automations.md` via chat intent (`schedule: name | cron | prompt`)
- Cron automation run logs at `~/.semiclaw/cron/<agent>/YYYY-MM-DD.md`
- Chat history command
- SQLite persistence for owner/config/secrets/messages
- Optional provider API key (not required for local Ollama)
- Gateway-centric runtime flow: `event -> reasoning -> action -> feedback`
- Programmatic tool executors: shell, browser, python, file

## Run
From repo root:

```bash
npm run cli -- help
```

Or directly:

```bash
cd app
go run ./cmd/semiclaw help
```

## CLI Commands
```bash
semiclaw setup [--password <value>] [--api-key <value>] [--openai-base-url <url>] [--openai-api-key <key>] [--openai-model <model>] [--soul-seed <value>] [--skip-profile]
semiclaw login [--password <value>]
semiclaw logout
semiclaw status
semiclaw chat [message]
semiclaw history [--limit 20]
semiclaw agent list
semiclaw agent new
semiclaw agent config [--system-prompt <text>] [--model <model>] [--base-url <url>] [--provider ollama|openai] [--api-key <key>] [--clear-api-key]
semiclaw agent switch <name>
semiclaw agent delete <name>
semiclaw help
```

## Memory And Automation Intents
- Long-term memory: `remember: I prefer short answers`
- Cron automation memory: `schedule: daily_summary | 0 18 * * * | summarize key updates`
- Linux host command execution via natural-language requests (LLM infers command intent and executes safely)
- URL browsing/content retrieval: `visit https://example.com` or include any `http(s)://...` URL in chat
- Built-in source inference for Zaobao latest news requests (e.g. `latest 10 news in zaobao china`)

## Agent Management
- `semiclaw` is the default general-purpose agent and is created automatically.
- Running `semiclaw setup` again reconfigures the current agent instead of failing.
- `semiclaw agent list` shows all created agents and marks the current one with `*`.
- `semiclaw agent new` starts an interactive guide (asks name first). If the name already exists, it asks whether to update config.
- `semiclaw agent switch <name>` changes the current serving agent.
- `semiclaw agent delete <name>` removes that agent setup (default `semiclaw` cannot be deleted).

## Chat Modes
- `semiclaw chat` starts interactive terminal chat mode. Type `exit` or `quit` to leave.
- `semiclaw chat "your message"` runs a single one-shot chat turn.

## Runtime Architecture (Gateway Flow)
Semiclaw chat now runs as a local orchestration loop:

```text
User/System Event
  -> CLI Chat Channel
  -> Gateway Router
  -> Session + Memory Load
  -> Agent Runtime
  -> LLM Reasoning (JSON contract)
  -> Tool/Skill Call
  -> ShellExecutor | BrowserExecutor | PythonExecutor | FileExecutor
  -> Result Feedback
  -> Final Response
  -> CLI Chat Channel
```

Key responsibilities:
- `Gateway`: session routing, tool policy checks, tool orchestration, feedback loop, final result.
- `SessionManager`: conversation history + memory state assembly.
- `AgentRuntime`: calls provider and parses structured reasoning output.
- `Executors`: run concrete actions and return normalized tool results.

## Example Local Ollama Flow
```bash
export OLLAMA_BASE_URL=http://127.0.0.1:11434
export OLLAMA_MODEL=qwen3.5:latest
export OLLAMA_TIMEOUT_SECONDS=180
export DATA_DIR=/Users/you/.semiclaw

cd app
go run ./cmd/semiclaw setup --password 'StrongPass123!'
go run ./cmd/semiclaw login --password 'StrongPass123!'
go run ./cmd/semiclaw chat "hello"
go run ./cmd/semiclaw history --limit 10
```

## Optional OpenAI-Compatible Setup
```bash
cd app
go run ./cmd/semiclaw setup \
  --password 'StrongPass123!' \
  --openai-base-url 'https://your-remote-api.example.com' \
  --openai-api-key 'YOUR_API_KEY' \
  --openai-model 'gpt-4o-mini'
```

## Tests
```bash
cd app && go test ./...
```

## Environment Variables
- `DATA_DIR` (default: `~/.semiclaw`)
- `OLLAMA_BASE_URL` (default: `https://ollama.com`)
- `OLLAMA_MODEL` (default: `kimi-k2.5:cloud`)
- `OLLAMA_TIMEOUT_SECONDS` (default: `180`)
- `MIGRATIONS_DIR` (default: auto-detected `app/migrations`)
  - If not found, built-in embedded migrations are used automatically.
