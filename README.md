# Droner

## TODO
- Need to support droner giving it an existing branch

## Installation

```bash
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/droner@latest
GOPROXY=direct go install github.com/Oudwins/droner/pkgs/droner/dronerd@latest

```


## Useful commands


```bash
# dev cli
just cli *args

# help command
droner help

#

```

```bash
# is server running
curl -X GET http://localhost:57876/version
# Create session (auto session_id):
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/"}'
# Create session (explicit session_id):
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/","session_id":"abc-12"}'
# Create session with agent prompt/model:
curl -sS -X POST http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/","agentConfig":{"model":"openai/gpt-5.2-codex","prompt":"review the repo"}}'
# Delete session by path:
curl -sS -X DELETE http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"path":"/home/tmx/projects/droner/"}'
# Delete session by session_id:
curl -sS -X DELETE http://localhost:57876/sessions \
  -H "Content-Type: application/json" \
  -d '{"session_id":"abc-12"}'
```

## Cursor worktree setup

When using the local backend, droner will look for an optional repo-local config at `.cursor/worktrees.json` when creating a new session. If the file exists, droner runs each command in `setup-worktree` independently inside the new worktree before tmux and agent startup.

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

- `ROOT_WORKTREE_PATH`: path to the root repo/worktree droner was started from
- `WORKTREE_PATH`: path to the newly created session worktree
- `SESSION_ID`: droner session id for the new worktree

Notes:

- Commands run with `sh -lc` and use the new worktree as the working directory
- Commands run independently, so environment changes from one command do not carry over to later commands
- If any setup command fails, session creation fails
- This is currently supported by the local backend only







