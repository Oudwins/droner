# Droner

Droner manages coding sessions in git worktrees and opens each session in a tmux workspace with `nvim`, `opencode`, and a shell ready to go.

## What it does

- creates a new git worktree per session
- starts a tmux session with `nvim`, `opencode`, and `terminal` windows
- optionally seeds the agent with a model, agent name, and prompt
- tracks long-running work as async tasks through a local HTTP server
- can keep completed worktrees around or fully delete them later

Worktrees are named `<repo>..<session-id>`.

## Requirements

- macOS or Linux
- Go 1.22+ if building from source
- `git`
- `tmux`
- `nvim`
- `opencode`

## Installation

Install both binaries:

```bash
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/droner@latest
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/dronerd@latest
```

Or build from source:

```bash
just build-all
```

## Quick start

Start the server:

```bash
droner serve
```

Create a session from the current repo:

```bash
droner new --prompt "review the repo"
```

Create a named session for another repo and wait for the setup task:

```bash
droner new \
  --path /path/to/repo \
  --id review/api-cleanup \
  --model openai/gpt-5-mini \
  --agent build \
  --prompt "trace the failing tests and fix them" \
  --wait
```

List active sessions:

```bash
droner sessions
```

Complete a session but keep its worktree:

```bash
droner complete review/api-cleanup --wait
```

Delete a session and remove its worktree:

```bash
droner del review/api-cleanup --wait
```

Open the terminal UI:

```bash
droner tui
```

## CLI commands

```bash
droner --version
droner serve --detach
droner new --help
droner sessions --all
droner task <task-id>
droner auth github
droner nuke
```

Notes:

- `droner` with no subcommand opens the TUI when run in an interactive terminal
- `droner new` uses the current repo if `--path` is omitted
- `droner complete` stops the tmux session but leaves the worktree on disk
- `droner del` stops tmux, removes the worktree, and deletes the backing branch

## Local server API

The server listens on `http://localhost:57876` by default.

```bash
# health
curl -sS http://localhost:57876/version

# create session with an auto-generated session id
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/path/to/repo"}'

# create session with an explicit session id and agent config
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "path":"/path/to/repo",
    "sessionId":"review/api-cleanup",
    "agentConfig":{
      "model":"openai/gpt-5-mini",
      "agentName":"build",
      "message":{
        "parts":[
          {"type":"text","text":"review the repo and fix the failing tests"}
        ]
      }
    }
  }'

# list queued and running sessions
curl -sS http://localhost:57876/sessions

# list up to the last 100 sessions of any status
curl -sS "http://localhost:57876/sessions?all=true"

# check an async task
curl -sS http://localhost:57876/tasks/<task-id>

# stop a session but keep its worktree
curl -sS -X POST http://localhost:57876/sessions/complete \
  -H "Content-Type: application/json" \
  -d '{"sessionId":"review/api-cleanup"}'

# delete a session and remove its worktree
curl -sS -X DELETE http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"sessionId":"review/api-cleanup"}'
```

## Configuration

Optional config lives at `~/.droner/droner.json`.

Example:

```json
{
  "server": {
    "data_dir": "~/.droner"
  },
  "sessions": {
    "backends": {
      "default": "local",
      "local": {
        "worktreeDir": "~/.droner/worktrees"
      }
    },
    "agent": {
      "defaultProvider": "opencode",
      "defaultModel": "openai/gpt-5-mini",
      "providers": {
        "openCode": {
          "hostname": "127.0.0.1",
          "port": 4096
        }
      }
    },
    "naming": {
      "strategy": "opencode_prompt",
      "model": "openai/gpt-5-mini"
    }
  }
}
```

Environment variables:

- `DRONER_ENV_PORT`: change the local server port
- `GITHUB_TOKEN`: optional GitHub token

GitHub auth obtained through `droner auth github` is stored in `~/.droner/auth.json`.

## Cursor worktree setup

When using the local backend, droner looks for an optional repo-local config at `.cursor/worktrees.json` while creating a new session. If the file exists, droner runs each command in `setup-worktree` independently inside the new worktree before tmux and agent startup.

Example:

```json
{
  "setup-worktree": [
    "nix develop",
    "cp $ROOT_WORKTREE_PATH/README.md README_COPY.md"
  ]
}
```

Available environment variables for each command:

- `ROOT_WORKTREE_PATH`: path to the repo/worktree droner was started from
- `WORKTREE_PATH`: path to the new session worktree
- `SESSION_ID`: droner session id for the new worktree

Notes:

- commands run with `sh -lc`
- commands run independently, so environment changes do not carry over
- if any setup command fails, session creation fails
- this is currently supported only by the local backend

## Development

```bash
just dev        # run the server on port 57876
just cli --help # build the CLI and run it
just test       # run Go tests
just build-all  # build both binaries into ./bin
```
