# Guidance for coding agents working in this repository.

## CRITICAL INSTRUCTIONS
1. After you think you have completed a task always aim to verify your work by running the tests, build, etc before handing control back to the user.
2. This file is for you. Please update it if you figure out something important about the codebase or find that some information is out of date. We should try to keep it small with only the most important details 
3. This is a green field project with no users. We do not need to consider backwards compatibility. Lets aim to make the code as clean as possible without worrying about breaking existing users since no one is using it yet.
4. We will not support windows. Only macos and linux

## Repository quick map
- Go module lives in `pkgs/droner` (module name `droner`, Go 1.22).
- Server entry points live under `pkgs/droner/dronerd/cmd`: dronerd startup code is shared in `cmd/cmd.go`, the dronerd binary is in `cmd/dronerd`, and the event debug command plus its helpers live in `cmd/eventdebug`.
- Dronerd's SQLite package and `sqlc` output live under `pkgs/droner/dronerd/db`, with queries in `db/queries` and Goose migrations in `db/migrations`.
- CLI entry point: `pkgs/droner/droner/cli.go`.
- Shared cross-package helpers live under `pkgs/droner/internals/...`; dronerd-only helpers now live under `pkgs/droner/dronerd/internals/...`.
- Shared event log abstraction lives in `pkgs/droner/internals/eventlog` with the first backend in `internals/eventlog/backends/sqlite`.
- SDK package lives under `pkgs/droner/sdk`.
- HTTP server code lives in `pkgs/droner/dronerd/server`.
- Dronerd-owned helper packages such as backends, remote integration, repo/session id helpers, and assertions live under `pkgs/droner/dronerd/internals`.
- JSON schemas/validation live in `pkgs/droner/internals/schemas`.

## Environment setup
- `direnv` is used; `.envrc` calls `use flake`.
- `flake.nix` provides Go, gopls, git, just, sqlc, and psmisc.
- Inside the flake dev shell, `droner` is provided as a wrapper for `just cli` so repo-local CLI changes can be used without a global install.
- Optional env vars can be set in `.env` (loaded by justfile).
- Create/list/task-status tracer-bullet projection data lives in `<data dir>/db/droner.db`; the sessions event log now lives separately in `<data dir>/db/droner.sessionslog.db`, owned exclusively by the shared SQLite-backed `internals/eventlog` backend via `dronerd/sessionslog`. On startup, `sessionevents` now appends `session.hydration.requested` for hydratable projection rows, re-enters the normal started-event flow for provisioning/completion/deletion, and restores live GitHub branch/PR subscriptions after `session.ready`.
- Both SQLite databases are Goose-managed in-process on startup. Use `just migrate-up|down|status|version` for explicit migration commands, and `sqlc` reads main DB DDL from `dronerd/db/migrations`.
- TUI clipboard image paste uses `pngpaste` on macOS and `wl-paste` or `xclip` on Linux; when no image tool or image payload is available, `Ctrl+V` falls back to normal text paste.
- Root config lives at `~/.droner/droner.json`; TUI agent tabs come from `tui.agentNames`, which are trimmed and default to `build`, `plan` during config parsing.
- GitHub API auth is sourced from `GITHUB_TOKEN` or `gh auth token`; there is no repo-managed GitHub OAuth flow.

## Build and run
- Run server (kills port 57876 first): `just dev`.
- Manual server run: `go run ./pkgs/droner/dronerd/cmd/dronerd`.
- Run event debug UI: `just eventdebug`.
- Build binaries into `./bin`: `just build`.
- Build server only: `go build -o ./bin/dronerd ./pkgs/droner/dronerd/cmd/dronerd`.
- Build CLI only: `go build -o ./bin/droner ./pkgs/droner/droner`.
- Run CLI (builds first): `just cli <args>`.

## Tests
- Run all tests: `cd pkgs/droner && go test ./...`.
- Run a single package: `cd pkgs/droner && go test ./internals/server`.
- Run a single test by name:
  `cd pkgs/droner && go test ./... -run ^TestName$ -count=1`.
- Run a single subtest:
  `cd pkgs/droner && go test ./... -run ^TestName$/^SubtestName$ -count=1`.
- Use `-count=1` when you need to bypass cached results.

## Lint and formatting
- Format files: `gofmt -w <files>`.
- Format entire module: `gofmt -w ./pkgs/droner`.
- Basic vetting: `go vet ./pkgs/droner/...`.
- No repo-managed linter (no golangci config found).

## HTTP service notes
- Server listens on `env.PORT` / `env.LISTEN_ADDR` (default port 57876).
- Health check: `GET /version`.
- Other endpoints: `POST /sessions`, `DELETE /sessions`, `POST /sessions/complete`, `POST /sessions/nuke`, `GET /sessions`.
- The server is event-only now; legacy `GET /tasks/{id}` polling is removed.
- See `README.md` for curl examples.

## Cursor/Copilot rules
- No `.cursor/rules`, `.cursorrules`, or `.github/copilot-instructions.md` found.
- If you add any of these in the future, update this file.

## Code style: imports
- Use standard Go import grouping: stdlib, internal module, third-party.
- Avoid aliasing unless it improves clarity or avoids conflict.
- Prefer full module paths (e.g. `droner/internals/server`).
- Keep imports gofmt-sorted; blank lines separate groups.

## Code style: formatting
- Use gofmt for all Go files; do not hand-align.
- 1 tab indentation; no trailing whitespace.
- Keep line lengths reasonable; wrap long literals where practical.
- Keep error strings lower-case, no trailing punctuation.
- Keep comments short and only where logic is non-obvious.

## Code style: naming
- Exported identifiers are `PascalCase`; unexported are `camelCase`.
- Use Go initialisms: `ID`, `URL`, `HTTP`, `JSON`.
- Error vars use `ErrX` (e.g. `ErrUsage`).
- Function names should be verbs: `HandlerCreateSession`.

## Code style: types and structs
- Use `struct` types for request/response payloads (see `schemas`).
- Prefer `time.Duration` for timeouts and intervals.
- Use pointer receivers when mutating or to avoid copies.
- Use slices over arrays unless fixed-size is required.
- Keep interfaces small and close to the caller.

## Code style: error handling
- Check and handle errors immediately; avoid ignoring errors.
- Wrap errors with context: `fmt.Errorf("...: %w", err)`.
- Return early on failure; keep the happy path unindented.
- HTTP handlers return JSON errors using `RenderJSON`.
- Use `http.Error` for simple text errors only when consistent.

## Code style: logging
- Server uses `slog`; keep messages short.
- Prefer structured fields over string concatenation.
- Avoid `fmt.Println` for production logs.
- Server logs fan out to colored `tint` output on stdout and plain text in `<data dir>/log.txt`.

## Code style: HTTP handlers
- Validate request bodies with schemas before using data.
- Set content type explicitly when returning plain text.
- For JSON, use `RenderJSON` and status helpers (`Render.Status`).
- Keep handlers thin; move logic to helpers if it grows.

## Concurrency and process management
- Server startup uses goroutines; keep shared state safe.
- External commands run via `exec.Command`; capture output and errors.
- Use timeouts for HTTP clients where appropriate.

## Paths and filesystem
- Use `filepath` helpers and `filepath.Clean`.
- Expand `~` paths with `expandPath` when needed.
- Validate directories before operating on them.
- Avoid hard-coding OS-specific separators or paths.

## Worktrees and sessions
- Worktrees are named `<repo>..<sessionID>`.
- Local backend worktree setup reads optional `<repo>/.cursor/worktrees.json` and runs `setup-worktree` commands independently inside the new worktree with `ROOT_WORKTREE_PATH`, `WORKTREE_PATH`, and `SESSION_ID` env vars.
- Use `git worktree add`/`remove` helpers in `server`.
- On server startup, persisted `running` sessions are hydrated via `Backend.HydrateSession`; local backend treats an existing tmux session as already hydrated, deletes sessions whose worktree is missing, and marks hydration failures as `failed`.

## When adding new code
- Follow existing package layout under `pkgs/droner`.
- Prefer small, focused functions with clear error messages.
- Keep new APIs consistent with existing JSON payload shapes.
- Keep schema validation close to handler entry points.
- Update this file if you add new build/test tooling.

## Quick sanity checks
- `just dev` starts the server and frees port 57876.
- `go test ./pkgs/droner/...` should pass before PRs.
- `gofmt -w` should produce no diffs on committed Go files.
