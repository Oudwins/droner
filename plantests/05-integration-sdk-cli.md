# Step 5: Integration Tests for SDK and CLI

## Goal
Test SDK behavior and CLI flows against the test server.

## Step 5.1: SDK basic flows
File: `pkgs/droner/sdk/client.go`

- `Version` returns trimmed string.
- `CreateSession` returns task response.
- `TaskStatus` reflects task state.
- `DeleteSession` returns task response.

**Verify**
- `go test ./pkgs/droner/sdk` passes using server URL override.

## Step 5.2: SDK error mapping
- 4xx/5xx with JSON error payload -> `APIError` fields.
- `auth_required` -> `ErrAuthRequired`.

**Verify**
- Table tests confirm error types and messages.

## Step 5.3: CLI main flows
File: `pkgs/droner/droner/cli.go`

- `droner task <id>` prints status.
- `droner new --wait` prints final task state.
- `droner del --wait` prints final state.
- Use env override or injected base URL to avoid daemon startup.

**Verify**
- `go test ./pkgs/droner/droner` passes without starting real daemon.

## Step 5.4: CLI auth flow
- Mock `/oauth/github/start` and `/oauth/github/status` responses.
- Validate `auth github` handles:
  - complete
  - failed
  - timeout

**Verify**
- CLI auth tests pass using mocked endpoints.
