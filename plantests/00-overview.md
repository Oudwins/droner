# Test Plan Overview

## Goal
Build unit and integration coverage for server, SDK, CLI, and internal helpers without hitting real git/tmux/GitHub.

## Current surface area
- HTTP server endpoints: `/version`, `/sessions`, `/tasks/{id}`, `/oauth/github/start`, `/oauth/github/status`.
- Background task queue with sqlite persistence.
- CLI flows: new/del/task/auth.
- OAuth device flow and auth token storage.
- Remote subscription manager and GitHub provider parsing/auth checks.

## Plan structure
1. Add fixtures and seams to isolate external dependencies.
2. Unit tests for internals.
3. Unit tests for server helpers, task store/manager, and OAuth state.
4. Integration tests for HTTP handlers.
5. Integration tests for SDK + CLI flows.
6. Remote subscription integration tests.
7. Execution checklist.

## Definition of done
- Each step delivers small, verifiable tests that pass on a fresh checkout.
- `go test ./pkgs/droner/...` is green.
- No tests require network or real tmux sessions.
