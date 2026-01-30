# Step 2: Unit Tests for Internals

## Goal
Validate all pure helpers and config/schema logic.

## Step 2.1: Schemas
Files: `pkgs/droner/internals/schemas/*.go`

- `SessionCreateSchema`:
  - trims `Path`, `SessionID`, `Agent.Model`, `Agent.Prompt`.
  - defaults `Agent` when nil.
  - defaults `Model` when empty.
  - fails when `Path` missing.
- `SessionDeleteSchema`:
  - requires `Path` or `SessionID`.
  - trims inputs.

**Verify**
- `go test ./pkgs/droner/internals/schemas` passes.

## Step 2.2: env
File: `pkgs/droner/internals/env/env.go`

- Default `PORT` is 57876.
- `LISTEN_ADDR` and `BASE_URL` derived from port.
- Optional `GITHUB_TOKEN` missing does not error.

**Verify**
- `go test ./pkgs/droner/internals/env` passes with env overrides.

## Step 2.3: conf
File: `pkgs/droner/internals/conf/conf.go`

- Defaults for `WORKTREES_DIR`, `DATA_DIR`, `DEFAULT_MODEL`.
- `VERSION` is set after parsing.

**Verify**
- `go test ./pkgs/droner/internals/conf` passes.

## Step 2.4: auth
File: `pkgs/droner/internals/auth/auth.go`

- `ReadGitHubAuth`:
  - missing file -> ok=false
  - malformed JSON -> error
  - missing token -> ok=false
- `WriteGitHubAuth` writes a valid JSON file.
- `authFilePath` handles `~` expansion.

**Verify**
- `go test ./pkgs/droner/internals/auth` passes using temp dirs.

## Step 2.5: logbuf
Files: `pkgs/droner/internals/logbuf/*.go`

- `With` preserves parent attrs and buffer.
- `Add` appends attrs.
- `Flush` resets buffer and seq.
- `entriesToPayload` merges attrs without overwriting reserved fields.
- Concurrency: parallel logging does not race.

**Verify**
- `go test ./pkgs/droner/internals/logbuf` and `-race` pass locally.

## Step 2.6: term
File: `pkgs/droner/internals/term/term.go`

- `SupportsHyperlinks` variations by env.
- `ClickableLink` returns raw label when unsupported.

**Verify**
- `go test ./pkgs/droner/internals/term` passes.

## Step 2.7: desktop
File: `pkgs/droner/internals/desktop/desktop.go`

- Empty URL returns error.
- Unsupported platform returns error.
- Supported platforms use exec seam to avoid opening browser.

**Verify**
- `go test ./pkgs/droner/internals/desktop` passes.
