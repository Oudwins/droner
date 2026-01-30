# Step 6: Remote Package Tests

## Goal
Validate remote registry behavior and GitHub provider parsing/auth logic.

## Step 6.1: GitHub URL parsing
File: `pkgs/droner/internals/remote/github.go`

- SSH URLs: `git@github.com:owner/repo.git`.
- HTTPS URLs: `https://github.com/owner/repo.git`.
- Invalid formats return errors.

**Verify**
- `go test ./pkgs/droner/internals/remote -run Parse` passes.

## Step 6.2: Auth resolution
- Env `GITHUB_TOKEN` wins.
- Stored token is used when env missing.
- No token returns empty and triggers `sdk.ErrAuthRequired`.

**Verify**
- `EnsureAuth` returns expected error types.

## Step 6.3: Registry lifecycle
- Subscribe is idempotent.
- Unsubscribe removes subscription and cancels.
- Poll loop calls handler for each event.

**Verify**
- Tests run with fake provider and registry reset hook.
