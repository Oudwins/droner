# Step 1: Fixtures and Seams

## Goal
Create test scaffolding and seams for deterministic tests.

## Step 1.1: Add test utilities
Create `pkgs/droner/internals/testutil` with helpers:
- `TempRepo(t *testing.T) string`: initializes a git repo with one commit.
- `TempWorktreeRoot(t *testing.T) string`: temp dir for worktrees.
- `TempDBPath(t *testing.T) string`: temp sqlite path.

**Verify**
- `go test ./pkgs/droner/internals/testutil` passes.

## Step 1.2: Exec seam for git/tmux/sh
Add package-level var in `pkgs/droner/dronerd/server`:
- `var execCommand = exec.Command`
Replace all `exec.Command` uses in:
- `handlers.go`
- `serverUtils.go`

**Verify**
- Unit test swaps `execCommand` and asserts calls.

## Step 1.3: OAuth HTTP client seam
Introduce `oauthHTTPClient` (or Server field) used by:
- `requestGitHubDeviceCode`
- `exchangeGitHubDeviceToken`
Allow injecting `httptest.Server` client in tests.

**Verify**
- OAuth unit tests can run without outbound network.

## Step 1.4: Time seam for OAuth state
Add `var now = time.Now` in OAuth file and use `now()` for:
- `oauthStateStore.create`
- `oauthStateStore.status`
- `HandlerGitHubOAuthStatus` expiry checks

**Verify**
- Tests can simulate expiry without sleeping.

## Step 1.5: Remote registry reset hook
Add a test-only helper in `internals/remote` (build tag `//go:build test`):
- Resets `globalRegistry` and `once`.

**Verify**
- Remote tests start from a clean registry state.
