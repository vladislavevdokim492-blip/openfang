# ClawForge CLI Reference

Complete command-line reference for `clawforge`, the CLI tool for the ClawForge Agent OS.

## Overview

The `clawforge` binary is the primary interface for managing the ClawForge Agent OS. It supports two modes of operation:

- **Daemon mode** -- When a daemon is running (`clawforge start`), CLI commands communicate with it over HTTP. This is the recommended mode for production use.
- **In-process mode** -- When no daemon is detected, commands that support it will boot an ephemeral in-process kernel. Agents spawned in this mode are not persisted and will be lost when the process exits.

Running `clawforge` with no subcommand launches the interactive TUI (terminal user interface) built with ratatui, which provides a full dashboard experience in the terminal.

## Installation

### From source (cargo)

```bash
cargo install --path crates/clawforge-cli
```

### Build from workspace

```bash
cargo build --release -p clawforge-cli
# Binary: target/release/clawforge (or clawforge.exe on Windows)
```

### Docker

```bash
docker run -it clawforge/clawforge:latest
```

### Shell installer

```bash
curl -fsSL https://get.clawforge.ai | sh
```

## Global Options

These options apply to all commands.

| Option | Description |
|---|---|
| `--config <PATH>` | Path to a custom config file. Overrides the default `~/.clawforge/config.toml`. |
| `--help` | Print help information for any command or subcommand. |
| `--version` | Print the version of the `clawforge` binary. |

**Environment variables:**

| Variable | Description |
|---|---|
| `RUST_LOG` | Controls log verbosity (e.g. `info`, `debug`, `clawforge_kernel=trace`). |
| `CLAWFORGE_AGENTS_DIR` | Override the agent templates directory. |
| `EDITOR` / `VISUAL` | Editor used by `clawforge config edit`. Falls back to `notepad` (Windows) or `vi` (Unix). |

---

## Command Reference

### clawforge (no subcommand)

Launch the interactive TUI dashboard.

```
clawforge [--config <PATH>]
```

The TUI provides a full-screen terminal interface with panels for agents, chat, workflows, channels, skills, settings, and more. Tracing output is redirected to `~/.clawforge/tui.log` to avoid corrupting the terminal display.

Press `Ctrl+C` to exit. A second `Ctrl+C` force-exits the process.

---

### clawforge init

Initialize the ClawForge workspace. Creates `~/.clawforge/` with subdirectories (`data/`, `agents/`) and a default `config.toml`.

```
clawforge init [--quick]
```

**Options:**

| Option | Description |
|---|---|
| `--quick` | Skip interactive prompts. Auto-detects the best available LLM provider and writes config immediately. Suitable for CI/scripts. |

**Behavior:**

- Without `--quick`: Launches an interactive 5-step onboarding wizard (ratatui TUI) that walks through provider selection, API key configuration, and optionally starts the daemon.
- With `--quick`: Auto-detects providers by checking environment variables in priority order: Groq, Gemini, DeepSeek, Anthropic, OpenAI, OpenRouter. Falls back to Groq if none are found.
- File permissions are restricted to owner-only (`0600` for files, `0700` for directories) on Unix.

**Example:**

```bash
# Interactive setup
clawforge init

# Non-interactive (CI/scripts)
export GROQ_API_KEY="gsk_..."
clawforge init --quick
```

---

### clawforge start

Start the ClawForge daemon (kernel + API server).

```
clawforge start [--config <PATH>]
```

**Behavior:**

- Checks if a daemon is already running; exits with an error if so.
- Boots the ClawForge kernel (loads config, initializes SQLite database, loads agents, connects MCP servers, starts background tasks).
- Starts the HTTP API server on the address specified in `config.toml` (default: `127.0.0.1:4200`).
- Writes `daemon.json` to `~/.clawforge/` so other CLI commands can discover the running daemon.
- Blocks until interrupted with `Ctrl+C`.

**Output:**

```
  ClawForge Agent OS v0.1.0

  Starting daemon...

  [ok] Kernel booted (groq/llama-3.3-70b-versatile)
  [ok] 50 models available
  [ok] 3 agent(s) loaded

  API:        http://127.0.0.1:4200
  Dashboard:  http://127.0.0.1:4200/
  Provider:   groq
  Model:      llama-3.3-70b-versatile

  hint: Open the dashboard in your browser, or run `clawforge chat`
  hint: Press Ctrl+C to stop the daemon
```

**Example:**

```bash
# Start with default config
clawforge start

# Start with custom config
clawforge start --config /path/to/config.toml
```

---

### clawforge status

Show the current kernel/daemon status.

```
clawforge status [--json]
```

**Options:**

| Option | Description |
|---|---|
| `--json` | Output machine-readable JSON for scripting. |

**Behavior:**

- If a daemon is running: queries `GET /api/status` and displays agent count, provider, model, uptime, API URL, data directory, and lists active agents.
- If no daemon is running: boots an in-process kernel and shows persisted state. Displays a warning that the daemon is not running.

**Example:**

```bash
clawforge status

clawforge status --json | jq '.agent_count'
```

---

### clawforge doctor

Run diagnostic checks on the ClawForge installation.

```
clawforge doctor [--json] [--repair]
```

**Options:**

| Option | Description |
|---|---|
| `--json` | Output results as JSON for scripting. |
| `--repair` | Attempt to auto-fix issues (create missing directories, config, remove stale files). Prompts for confirmation before each repair. |

**Checks performed:**

1. **ClawForge directory** -- `~/.clawforge/` exists
2. **.env file** -- exists and has correct permissions (0600 on Unix)
3. **Config TOML syntax** -- `config.toml` parses without errors
4. **Daemon status** -- whether a daemon is running
5. **Port 4200 availability** -- if daemon is not running, checks if the port is free
6. **Stale daemon.json** -- leftover `daemon.json` from a crashed daemon
7. **Database file** -- SQLite magic bytes validation
8. **Disk space** -- warns if less than 100MB available (Unix only)
9. **Agent manifests** -- validates all `.toml` files in `~/.clawforge/agents/`
10. **LLM provider keys** -- checks env vars for 10 providers (Groq, OpenRouter, Anthropic, OpenAI, DeepSeek, Gemini, Google, Together, Mistral, Fireworks), performs live validation (401/403 detection)
11. **Channel tokens** -- format validation for Telegram, Discord, Slack tokens
12. **Config consistency** -- checks that `api_key_env` references in config match actual environment variables
13. **Rust toolchain** -- `rustc --version`

**Example:**

```bash
clawforge doctor

clawforge doctor --repair

clawforge doctor --json
```

---

### clawforge dashboard

Open the web dashboard in the default browser.

```
clawforge dashboard
```

**Behavior:**

- Requires a running daemon.
- Opens the daemon URL (e.g. `http://127.0.0.1:4200/`) in the system browser.
- Copies the URL to the system clipboard (uses PowerShell on Windows, `pbcopy` on macOS, `xclip`/`xsel` on Linux).

**Example:**

```bash
clawforge dashboard
```

---

### clawforge completion

Generate shell completion scripts.

```
clawforge completion <SHELL>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<SHELL>` | Target shell. One of: `bash`, `zsh`, `fish`, `elvish`, `powershell`. |

**Example:**

```bash
# Bash
clawforge completion bash > ~/.bash_completion.d/clawforge

# Zsh
clawforge completion zsh > ~/.zfunc/_clawforge

# Fish
clawforge completion fish > ~/.config/fish/completions/clawforge.fish

# PowerShell
clawforge completion powershell > clawforge.ps1
```

---

## Agent Commands

### clawforge agent new

Spawn an agent from a built-in template.

```
clawforge agent new [<TEMPLATE>]
```

**Arguments:**

| Argument | Description |
|---|---|
| `<TEMPLATE>` | Template name (e.g. `coder`, `assistant`, `researcher`). If omitted, displays an interactive picker listing all available templates. |

**Behavior:**

- Templates are discovered from: the repo `agents/` directory (dev builds), `~/.clawforge/agents/` (installed), and `CLAWFORGE_AGENTS_DIR` (env override).
- Each template is a directory containing an `agent.toml` manifest.
- In daemon mode: sends `POST /api/agents` with the manifest. Agent is persistent.
- In standalone mode: boots an in-process kernel. Agent is ephemeral.

**Example:**

```bash
# Interactive picker
clawforge agent new

# Spawn by name
clawforge agent new coder

# Spawn the assistant template
clawforge agent new assistant
```

---

### clawforge agent spawn

Spawn an agent from a custom manifest file.

```
clawforge agent spawn <MANIFEST>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<MANIFEST>` | Path to an agent manifest TOML file. |

**Behavior:**

- Reads and parses the TOML manifest file.
- In daemon mode: sends the raw TOML to `POST /api/agents`.
- In standalone mode: boots an in-process kernel and spawns the agent locally.

**Example:**

```bash
clawforge agent spawn ./my-agent/agent.toml
```

---

### clawforge agent list

List all running agents.

```
clawforge agent list [--json]
```

**Options:**

| Option | Description |
|---|---|
| `--json` | Output as JSON array for scripting. |

**Output columns:** ID, NAME, STATE, PROVIDER, MODEL (daemon mode) or ID, NAME, STATE, CREATED (in-process mode).

**Example:**

```bash
clawforge agent list

clawforge agent list --json | jq '.[].name'
```

---

### clawforge agent chat

Start an interactive chat session with a specific agent.

```
clawforge agent chat <AGENT_ID>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<AGENT_ID>` | Agent UUID. Obtain from `clawforge agent list`. |

**Behavior:**

- Opens a REPL-style chat loop.
- Type messages at the `you>` prompt.
- Agent responses display at the `agent>` prompt, followed by token usage and iteration count.
- Type `exit`, `quit`, or press `Ctrl+C` to end the session.

**Example:**

```bash
clawforge agent chat a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

---

### clawforge agent kill

Terminate a running agent.

```
clawforge agent kill <AGENT_ID>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<AGENT_ID>` | Agent UUID to terminate. |

**Example:**

```bash
clawforge agent kill a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

---

## Workflow Commands

All workflow commands require a running daemon.

### clawforge workflow list

List all registered workflows.

```
clawforge workflow list
```

**Output columns:** ID, NAME, STEPS, CREATED.

---

### clawforge workflow create

Create a workflow from a JSON definition file.

```
clawforge workflow create <FILE>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<FILE>` | Path to a JSON file describing the workflow steps. |

**Example:**

```bash
clawforge workflow create ./my-workflow.json
```

---

### clawforge workflow run

Execute a workflow by ID.

```
clawforge workflow run <WORKFLOW_ID> <INPUT>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<WORKFLOW_ID>` | Workflow UUID. Obtain from `clawforge workflow list`. |
| `<INPUT>` | Input text to pass to the workflow. |

**Example:**

```bash
clawforge workflow run abc123 "Analyze this code for security issues"
```

---

## Trigger Commands

All trigger commands require a running daemon.

### clawforge trigger list

List all event triggers.

```
clawforge trigger list [--agent-id <ID>]
```

**Options:**

| Option | Description |
|---|---|
| `--agent-id <ID>` | Filter triggers by the owning agent's UUID. |

**Output columns:** TRIGGER ID, AGENT ID, ENABLED, FIRES, PATTERN.

---

### clawforge trigger create

Create an event trigger for an agent.

```
clawforge trigger create <AGENT_ID> <PATTERN_JSON> [--prompt <TEMPLATE>] [--max-fires <N>]
```

**Arguments:**

| Argument | Description |
|---|---|
| `<AGENT_ID>` | UUID of the agent that owns the trigger. |
| `<PATTERN_JSON>` | Trigger pattern as a JSON string. |

**Options:**

| Option | Default | Description |
|---|---|---|
| `--prompt <TEMPLATE>` | `"Event: {{event}}"` | Prompt template. Use `{{event}}` as a placeholder for the event data. |
| `--max-fires <N>` | `0` (unlimited) | Maximum number of times the trigger will fire. |

**Pattern examples:**

```bash
# Fire on any lifecycle event
clawforge trigger create <AGENT_ID> '{"lifecycle":{}}'

# Fire when a specific agent is spawned
clawforge trigger create <AGENT_ID> '{"agent_spawned":{"name_pattern":"*"}}'

# Fire on agent termination
clawforge trigger create <AGENT_ID> '{"agent_terminated":{}}'

# Fire on all events (limited to 10 fires)
clawforge trigger create <AGENT_ID> '{"all":{}}' --max-fires 10
```

---

### clawforge trigger delete

Delete a trigger by ID.

```
clawforge trigger delete <TRIGGER_ID>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<TRIGGER_ID>` | UUID of the trigger to delete. |

---

## Skill Commands

### clawforge skill list

List all installed skills.

```
clawforge skill list
```

**Output columns:** NAME, VERSION, TOOLS, DESCRIPTION.

Loads skills from `~/.clawforge/skills/` plus bundled skills compiled into the binary.

---

### clawforge skill install

Install a skill from a local directory, git URL, or FangHub marketplace.

```
clawforge skill install <SOURCE>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<SOURCE>` | Skill name (FangHub), local directory path, or git URL. |

**Behavior:**

- **Local directory:** Looks for `skill.toml` in the directory. If not found, checks for OpenClaw-format skills (SKILL.md with YAML frontmatter) and auto-converts them.
- **Remote (FangHub):** Fetches and installs from the FangHub marketplace. Skills pass through SHA256 verification and prompt injection scanning.

**Example:**

```bash
# Install from local directory
clawforge skill install ./my-skill/

# Install from FangHub
clawforge skill install web-search

# Install an OpenClaw-format skill
clawforge skill install ./openclaw-skill/
```

---

### clawforge skill remove

Remove an installed skill.

```
clawforge skill remove <NAME>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<NAME>` | Name of the skill to remove. |

**Example:**

```bash
clawforge skill remove web-search
```

---

### clawforge skill search

Search the FangHub marketplace for skills.

```
clawforge skill search <QUERY>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<QUERY>` | Search query string. |

**Example:**

```bash
clawforge skill search "docker kubernetes"
```

---

### clawforge skill create

Interactively scaffold a new skill project.

```
clawforge skill create
```

**Behavior:**

Prompts for:
- Skill name
- Description
- Runtime (`python`, `node`, or `wasm`; defaults to `python`)

Creates a directory under `~/.clawforge/skills/<name>/` with:
- `skill.toml` -- manifest file
- `src/main.py` (or `src/index.js`) -- entry point with boilerplate

**Example:**

```bash
clawforge skill create
# Skill name: my-tool
# Description: A custom analysis tool
# Runtime (python/node/wasm) [python]: python
```

---

## Channel Commands

### clawforge channel list

List configured channels and their status.

```
clawforge channel list
```

**Output columns:** CHANNEL, ENV VAR, STATUS.

Checks `config.toml` for channel configuration sections and environment variables for required tokens. Status is one of: `Ready`, `Missing env`, `Not configured`.

**Channels checked:** webchat, telegram, discord, slack, whatsapp, signal, matrix, email.

---

### clawforge channel setup

Interactive setup wizard for a channel integration.

```
clawforge channel setup [<CHANNEL>]
```

**Arguments:**

| Argument | Description |
|---|---|
| `<CHANNEL>` | Channel name. If omitted, displays an interactive picker. |

**Supported channels:** `telegram`, `discord`, `slack`, `whatsapp`, `email`, `signal`, `matrix`.

Each wizard:
1. Displays step-by-step instructions for obtaining credentials.
2. Prompts for tokens/credentials.
3. Saves tokens to `~/.clawforge/.env` with owner-only permissions.
4. Appends the channel configuration block to `config.toml` (prompts for confirmation).
5. Warns to restart the daemon if one is running.

**Example:**

```bash
# Interactive picker
clawforge channel setup

# Direct setup
clawforge channel setup telegram
clawforge channel setup discord
clawforge channel setup slack
```

---

### clawforge channel test

Send a test message through a configured channel.

```
clawforge channel test <CHANNEL>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<CHANNEL>` | Channel name to test. |

Requires a running daemon. Sends `POST /api/channels/<channel>/test`.

**Example:**

```bash
clawforge channel test telegram
```

---

### clawforge channel enable

Enable a channel integration.

```
clawforge channel enable <CHANNEL>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<CHANNEL>` | Channel name to enable. |

In daemon mode: sends `POST /api/channels/<channel>/enable`. Without a daemon: prints a note that the change will take effect on next start.

---

### clawforge channel disable

Disable a channel without removing its configuration.

```
clawforge channel disable <CHANNEL>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<CHANNEL>` | Channel name to disable. |

In daemon mode: sends `POST /api/channels/<channel>/disable`. Without a daemon: prints a note to edit `config.toml`.

---

## Config Commands

### clawforge config show

Display the current configuration file.

```
clawforge config show
```

Prints the contents of `~/.clawforge/config.toml` with the file path as a header comment.

---

### clawforge config edit

Open the configuration file in your editor.

```
clawforge config edit
```

Uses `$EDITOR`, then `$VISUAL`, then falls back to `notepad` (Windows) or `vi` (Unix).

---

### clawforge config get

Get a single configuration value by dotted key path.

```
clawforge config get <KEY>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<KEY>` | Dotted key path into the TOML structure. |

**Example:**

```bash
clawforge config get default_model.provider
# groq

clawforge config get api_listen
# 127.0.0.1:4200

clawforge config get memory.decay_rate
# 0.05
```

---

### clawforge config set

Set a configuration value by dotted key path.

```
clawforge config set <KEY> <VALUE>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<KEY>` | Dotted key path. |
| `<VALUE>` | New value. Type is inferred from the existing value (integer, float, boolean, or string). |

**Warning:** This command re-serializes the TOML file, which strips all comments.

**Example:**

```bash
clawforge config set default_model.provider anthropic
clawforge config set default_model.model claude-sonnet-4-20250514
clawforge config set api_listen "0.0.0.0:4200"
```

---

### clawforge config set-key

Save an LLM provider API key to `~/.clawforge/.env`.

```
clawforge config set-key <PROVIDER>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<PROVIDER>` | Provider name (e.g. `groq`, `anthropic`, `openai`, `gemini`, `deepseek`, `openrouter`, `together`, `mistral`, `fireworks`, `perplexity`, `cohere`, `xai`, `brave`, `tavily`). |

**Behavior:**

- Prompts interactively for the API key.
- Saves to `~/.clawforge/.env` as `<PROVIDER_NAME>_API_KEY=<value>`.
- Runs a live validation test against the provider's API.
- File permissions are restricted to owner-only on Unix.

**Example:**

```bash
clawforge config set-key groq
# Paste your groq API key: gsk_...
# [ok] Saved GROQ_API_KEY to ~/.clawforge/.env
# Testing key... OK
```

---

### clawforge config delete-key

Remove an API key from `~/.clawforge/.env`.

```
clawforge config delete-key <PROVIDER>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<PROVIDER>` | Provider name. |

**Example:**

```bash
clawforge config delete-key openai
```

---

### clawforge config test-key

Test provider connectivity with the stored API key.

```
clawforge config test-key <PROVIDER>
```

**Arguments:**

| Argument | Description |
|---|---|
| `<PROVIDER>` | Provider name. |

**Behavior:**

- Reads the API key from the environment (loaded from `~/.clawforge/.env`).
- Hits the provider's models/health endpoint.
- Reports `OK` (key accepted) or `FAILED (401/403)` (key rejected).
- Exits with code 1 on failure.

**Example:**

```bash
clawforge config test-key groq
# Testing groq (GROQ_API_KEY)... OK
```

---

## Quick Chat

### clawforge chat

Quick alias for starting a chat session.

```
clawforge chat [<AGENT>]
```

**Arguments:**

| Argument | Description |
|---|---|
| `<AGENT>` | Optional agent name or UUID. |

**Behavior:**

- **Daemon mode:** Finds the agent by name or ID among running agents. If no agent name is given, uses the first available agent. If no agents exist, suggests `clawforge agent new`.
- **Standalone mode (no daemon):** Boots an in-process kernel and auto-spawns an agent from templates. Searches for an agent matching the given name, then falls back to `assistant`, then to the first available template.

This is the simplest way to start chatting -- it works with or without a daemon.

**Example:**

```bash
# Chat with the default agent
clawforge chat

# Chat with a specific agent by name
clawforge chat coder

# Chat with a specific agent by UUID
clawforge chat a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

---

## Migration

### clawforge migrate

Migrate configuration and agents from another agent framework.

```
clawforge migrate --from <FRAMEWORK> [--source-dir <PATH>] [--dry-run]
```

**Options:**

| Option | Description |
|---|---|
| `--from <FRAMEWORK>` | Source framework. One of: `openclaw`, `langchain`, `autogpt`. |
| `--source-dir <PATH>` | Path to the source workspace. Auto-detected if not set (e.g. `~/.openclaw`, `~/.langchain`, `~/Auto-GPT`). |
| `--dry-run` | Show what would be imported without making changes. |

**Behavior:**

- Converts agent configurations, YAML manifests, and settings from the source framework into ClawForge format.
- Saves imported data to `~/.clawforge/`.
- Writes a `migration_report.md` summarizing what was imported.

**Example:**

```bash
# Dry run migration from OpenClaw
clawforge migrate --from openclaw --dry-run

# Migrate from OpenClaw (auto-detect source)
clawforge migrate --from openclaw

# Migrate from LangChain with explicit source
clawforge migrate --from langchain --source-dir /home/user/.langchain

# Migrate from AutoGPT
clawforge migrate --from autogpt
```

---

## MCP Server

### clawforge mcp

Start an MCP (Model Context Protocol) server over stdio.

```
clawforge mcp
```

**Behavior:**

- Exposes running ClawForge agents as MCP tools via JSON-RPC 2.0 over stdin/stdout with Content-Length framing.
- Each agent becomes a callable tool named `clawforge_agent_<name>` (hyphens replaced with underscores).
- Connects to a running daemon via HTTP if available; otherwise boots an in-process kernel.
- Protocol version: `2024-11-05`.
- Maximum message size: 10MB (security limit).

**Supported MCP methods:**

| Method | Description |
|---|---|
| `initialize` | Returns server capabilities and info. |
| `tools/list` | Lists all available agent tools. |
| `tools/call` | Sends a message to an agent and returns the response. |

**Tool input schema:**

Each agent tool accepts a single `message` (string) argument.

**Integration with Claude Desktop / other MCP clients:**

Add to your MCP client configuration:

```json
{
  "mcpServers": {
    "clawforge": {
      "command": "clawforge",
      "args": ["mcp"]
    }
  }
}
```

---

## Daemon Auto-Detect

The CLI uses a two-step mechanism to detect a running daemon:

1. **Read `daemon.json`:** On startup, the daemon writes `~/.clawforge/daemon.json` containing the listen address (e.g. `127.0.0.1:4200`). The CLI reads this file to learn where the daemon is.

2. **Health check:** The CLI sends `GET http://<listen_addr>/api/health` with a 2-second timeout. If the health check succeeds, the daemon is considered running and the CLI uses HTTP to communicate with it.

If either step fails (no `daemon.json`, stale file, health check timeout), the CLI falls back to in-process mode for commands that support it. Commands that require a daemon (workflows, triggers, channel test/enable/disable, dashboard) will exit with an error and a helpful message.

**Daemon lifecycle:**

```
clawforge start          # Starts daemon, writes daemon.json
                        # Other CLI instances detect daemon.json
clawforge status         # Connects to daemon via HTTP
Ctrl+C                  # Daemon shuts down, daemon.json removed

clawforge doctor --repair  # Cleans up stale daemon.json from crashes
```

---

## Environment File

ClawForge loads `~/.clawforge/.env` into the process environment on every CLI invocation. System environment variables take priority over `.env` values.

The `.env` file stores API keys and secrets:

```bash
GROQ_API_KEY=gsk_...
ANTHROPIC_API_KEY=sk-ant-...
GEMINI_API_KEY=AIza...
TELEGRAM_BOT_TOKEN=123456:ABC-DEF...
```

Manage keys with the `config set-key` / `config delete-key` commands rather than editing the file directly, as these commands enforce correct permissions.

---

## Exit Codes

| Code | Meaning |
|---|---|
| `0` | Success. |
| `1` | General error (invalid arguments, failed operations, missing daemon, parse errors, spawn failures). |
| `130` | Interrupted by second `Ctrl+C` (force exit). |

---

## Examples

### First-time setup

```bash
# 1. Set your API key
export GROQ_API_KEY="gsk_your_key_here"

# 2. Initialize ClawForge
clawforge init --quick

# 3. Start the daemon
clawforge start
```

### Daily usage

```bash
# Quick chat (auto-spawns agent if needed)
clawforge chat

# Chat with a specific agent
clawforge chat coder

# Check what's running
clawforge status

# Open the web dashboard
clawforge dashboard
```

### Agent management

```bash
# Spawn from a template
clawforge agent new assistant

# Spawn from a custom manifest
clawforge agent spawn ./agents/custom-agent/agent.toml

# List running agents
clawforge agent list

# Chat with an agent by UUID
clawforge agent chat <UUID>

# Kill an agent
clawforge agent kill <UUID>
```

### Workflow automation

```bash
# Create a workflow
clawforge workflow create ./review-pipeline.json

# List workflows
clawforge workflow list

# Run a workflow
clawforge workflow run <WORKFLOW_ID> "Review the latest PR"
```

### Event triggers

```bash
# Create a trigger that fires on agent spawn
clawforge trigger create <AGENT_ID> '{"agent_spawned":{"name_pattern":"*"}}' \
  --prompt "New agent spawned: {{event}}" \
  --max-fires 100

# List all triggers
clawforge trigger list

# List triggers for a specific agent
clawforge trigger list --agent-id <AGENT_ID>

# Delete a trigger
clawforge trigger delete <TRIGGER_ID>
```

### Skill management

```bash
# Search FangHub
clawforge skill search "code review"

# Install a skill
clawforge skill install code-reviewer

# List installed skills
clawforge skill list

# Create a new skill
clawforge skill create

# Remove a skill
clawforge skill remove code-reviewer
```

### Channel setup

```bash
# Interactive channel picker
clawforge channel setup

# Direct channel setup
clawforge channel setup telegram

# Check channel status
clawforge channel list

# Test a channel
clawforge channel test telegram

# Enable/disable channels
clawforge channel enable discord
clawforge channel disable slack
```

### Configuration

```bash
# View config
clawforge config show

# Get a specific value
clawforge config get default_model.provider

# Change provider
clawforge config set default_model.provider anthropic
clawforge config set default_model.model claude-sonnet-4-20250514
clawforge config set default_model.api_key_env ANTHROPIC_API_KEY

# Manage API keys
clawforge config set-key anthropic
clawforge config test-key anthropic
clawforge config delete-key openai

# Open in editor
clawforge config edit
```

### Migration from other frameworks

```bash
# Preview migration
clawforge migrate --from openclaw --dry-run

# Run migration
clawforge migrate --from openclaw

# Migrate from LangChain
clawforge migrate --from langchain --source-dir ~/.langchain
```

### MCP integration

```bash
# Start MCP server for Claude Desktop or other MCP clients
clawforge mcp
```

### Diagnostics

```bash
# Run all diagnostic checks
clawforge doctor

# Auto-repair issues
clawforge doctor --repair

# Machine-readable diagnostics
clawforge doctor --json
```

### Shell completions

```bash
# Generate and install completions for your shell
clawforge completion bash >> ~/.bashrc
clawforge completion zsh > "${fpath[1]}/_clawforge"
clawforge completion fish > ~/.config/fish/completions/clawforge.fish
```

---

## Supported LLM Providers

The following providers are recognized by `clawforge config set-key` and `clawforge doctor`:

| Provider | Environment Variable | Default Model |
|---|---|---|
| Groq | `GROQ_API_KEY` | `llama-3.3-70b-versatile` |
| Gemini | `GEMINI_API_KEY` or `GOOGLE_API_KEY` | `gemini-2.5-flash` |
| DeepSeek | `DEEPSEEK_API_KEY` | `deepseek-chat` |
| Anthropic | `ANTHROPIC_API_KEY` | `claude-sonnet-4-20250514` |
| OpenAI | `OPENAI_API_KEY` | `gpt-4o` |
| OpenRouter | `OPENROUTER_API_KEY` | `openrouter/auto` |
| Together | `TOGETHER_API_KEY` | -- |
| Mistral | `MISTRAL_API_KEY` | -- |
| Fireworks | `FIREWORKS_API_KEY` | -- |
| Perplexity | `PERPLEXITY_API_KEY` | -- |
| Cohere | `COHERE_API_KEY` | -- |
| xAI | `XAI_API_KEY` | -- |

Additional search/fetch provider keys: `BRAVE_API_KEY`, `TAVILY_API_KEY`.
