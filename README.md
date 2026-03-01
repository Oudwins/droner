# droner

Droner is a local session manager for running coding-agent work in isolated git worktrees.

It runs a small daemon (`dronerd`) with an HTTP API, and a CLI (`droner`) to create/list/complete/delete sessions.
When you create a session, Droner:

- creates a git worktree (default under `~/.droner/worktrees/<repo>..<sessionId>`)
- starts a `tmux` session rooted in that worktree (with `nvim`, an `opencode` window, and a terminal window)
- optionally sends an initial prompt/message to the agent

This project targets macOS and Linux (no Windows support).

## Requirements

- `git`
- `tmux`
- `nvim`
- `opencode` (CLI) available on `PATH`

## Installation

Install the CLI + daemon with Go:

```bash
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/droner@latest
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/dronerd@latest
```

## Usage

Start the server:

```bash
# background
droner serve --detach

# or foreground
dronerd
```

Create a new session from inside a git repo:

```bash
cd /path/to/repo
droner new --id readme-demo --prompt "review the repo" --wait
```

List sessions:

```bash
droner sessions
droner sessions --all
```

Complete (stop the runtime but keep the worktree for reuse) or delete:

```bash
droner complete readme-demo --wait
droner del readme-demo --wait
```

If a command returns a `taskId`, you can poll it:

```bash
droner task <taskId>
```

## HTTP API

Health/version:

```bash
curl -sS http://localhost:57876/version
```

Create a session:

```bash
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "path": "/home/you/projects/myrepo",
    "sessionId": "readme-demo",
    "agentConfig": {
      "model": "openai/gpt-5.2-codex",
      "message": {
        "parts": [{"type": "text", "text": "review the repo"}]
      }
    }
  }'
```

Complete or delete a session:

```bash
curl -sS -X POST http://localhost:57876/sessions/complete \
  -H "Content-Type: application/json" \
  -d '{"sessionId":"readme-demo"}'

curl -sS -X DELETE http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"sessionId":"readme-demo"}'
```

List sessions and check task status:

```bash
curl -sS http://localhost:57876/sessions
curl -sS http://localhost:57876/tasks/<taskId>
```

## Local development

The Go module lives in `pkgs/droner`.

If you use `nix` + `direnv`:

```bash
direnv allow
```

Run the server (kills anything on port `57876` first):

```bash
just dev
```

Build binaries into `./bin`:

```bash
just build-all
```

Run the CLI from source:

```bash
just cli --version
just cli new --id dev-demo --prompt "hello" --wait
```

Run tests:

```bash
just test
# or
cd pkgs/droner && go test ./...
```
