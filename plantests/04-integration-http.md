# Step 4: Integration Tests for HTTP Handlers

## Goal
Validate handlers end-to-end using `httptest.Server` with real routing.

## Step 4.1: Test server setup
- Build `Server` with temp `WORKTREES_DIR` and sqlite DB.
- Inject fake exec for git/tmux/sh.
- Start `httptest.NewServer(server.Router())`.

**Verify**
- `/version` returns 200.

## Step 4.2: GET /version
- Verify status 200 and text/plain content type.
- Body matches configured `VERSION`.

**Verify**
- Single test passes.

## Step 4.3: POST /sessions
- Invalid JSON -> 400, code `invalid_json`.
- Missing `path` -> 400, `validation_failed`.
- `path` not found -> 400.
- `path` not git repo -> 400.
- Valid request -> 202, `TaskResponse` with `TaskID` and `Result`.

**Verify**
- Test uses temp repo + fake git.

## Step 4.4: DELETE /sessions
- Invalid JSON -> 400.
- Missing `path/session_id` -> 400.
- Unknown `session_id` -> 404.
- Valid delete -> 202 with `TaskResponse`.

**Verify**
- Test uses a created worktree entry.

## Step 4.5: GET /tasks/{id}
- Empty id -> 400.
- Unknown id -> 404.
- Known id -> JSON payload with status fields.

**Verify**
- Create a task via manager, then query it.
