# AGENTS.md
# Guidance for coding agents working in this repository.
# Keep instructions in sync with the repo and Makefile.

## Repository quick map
- Go module lives in `pkgs/droner` (module name `droner`, Go 1.22).
- CLI/server entry point: `pkgs/droner/cmd/droner/main.go`.
- Core packages: `pkgs/droner/internals/...` and `pkgs/droner/conf`.
- HTTP server lives under `pkgs/droner/internals/server`.
- JSON schemas/validation live in `pkgs/droner/internals/schemas`.

## Environment setup
- `direnv` is used; `.envrc` calls `use flake`.
- `flake.nix` provides Go, gopls, git, make, and psmisc.
- Optional env vars can be set in `.env` (loaded by Makefile).

## Build and run
- Run server (kills port 57876 first): `make dev`.
- Run server without kill: `make run`.
- Manual run: `go run ./pkgs/droner/cmd/droner/main.go`.
- Build binary: `go build -o droner ./pkgs/droner/cmd/droner`.

## Tests
- Run all tests: `go test ./pkgs/droner/...`.
- Run a single package: `go test ./pkgs/droner/internals/server`.
- Run a single test by name:
  `go test ./pkgs/droner/... -run ^TestName$ -count=1`.
- Run a single subtest: `-run ^TestName$/^SubtestName$`.

## Lint and formatting
- Formatting is standard Go formatting: `gofmt -w <files>`.
- Format entire module: `gofmt -w ./pkgs/droner`.
- Basic vetting: `go vet ./pkgs/droner/...`.
- No repo-managed linter (no golangci config found).

## HTTP service notes
- Server listens on `env.PORT` / `env.LISTEN_ADDR` (default port 57876).
- Health check: `GET /version`.
- Other endpoints: `GET /sum`, `POST /sessions`, `DELETE /sessions`.
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

## Code style: types and structs
- Use `struct` types for request/response payloads (see `schemas`).
- Prefer `time.Duration` for timeouts and intervals.
- Use pointer receivers when mutating or to avoid copies.
- Use slices over arrays unless fixed-size is required.

## Code style: naming
- Exported identifiers are `PascalCase`; unexported are `camelCase`.
- Use Go initialisms: `ID`, `URL`, `HTTP`, `JSON`.
- Error vars use `ErrX` (e.g. `ErrUsage`).
- Function names should be verbs: `HandlerCreateSession`.

## Code style: error handling
- Check and handle errors immediately; avoid ignoring errors.
- Wrap errors with context: `fmt.Errorf("...: %w", err)`.
- Return early on failure; keep the happy path unindented.
- HTTP handlers return JSON errors using `RenderJSON`.
- Use `http.Error` for simple text errors only when consistent.

## Code style: logging
- Server uses `slog` JSON logger; keep messages short.
- Prefer structured fields over string concatenation.
- Avoid `fmt.Println` for production logs.

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

## Worktrees and sessions
- Worktrees are named `<repo>#<sessionID>`.
- Worktree setup can read `.cursor/worktrees.json` in the root repo.
- Use `git worktree add`/`remove` helpers in `server`.

## When adding new code
- Follow existing package layout under `pkgs/droner`.
- Prefer small, focused functions with clear error messages.
- Keep new APIs consistent with existing JSON payload shapes.
- Update this file if you add new build/test tooling.

## Quick sanity checks
- `make dev` starts the server and frees port 57876.
- `go test ./pkgs/droner/...` should pass before PRs.
- `gofmt -w` should produce no diffs on committed Go files.
