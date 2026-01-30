# Step 3: Unit Tests for Server

## Goal
Cover helpers, task store/manager, middleware, OAuth state, and subscriptions.

## Step 3.1: Helper functions
Files: `pkgs/droner/dronerd/server/handlers.go`, `serverUtils.go`

- `expandPath` handles `~`, `~/`, empty.
- `generateSessionID` returns unique IDs when worktree missing.
- `sessionIDFromName` parses `repo#id`.
- `findWorktreeBySessionID` handles zero/single/multi matches.
- `resolveDeleteWorktreePath` uses `Path` or `SessionID`.
- `jsonUnmarshal` errors on empty string.
- `nullIfEmpty` returns nil for empty.

**Verify**
- `go test ./pkgs/droner/dronerd/server -run Helper` passes.

## Step 3.2: taskStore
File: `pkgs/droner/dronerd/server/task_store.go`

- `newTaskStore` initializes sqlite schema.
- `newRecord` includes payload JSON when provided.
- `create/get/update` roundtrip.
- `marshalResult` returns empty on nil.

**Verify**
- `go test ./pkgs/droner/dronerd/server -run TaskStore` passes using temp DB.

## Step 3.3: taskManager
File: `pkgs/droner/dronerd/server/task_manager.go`

- `Enqueue` sets pending, then running, then success.
- `Enqueue` handles runner error -> failed status.
- `Get` decodes result correctly.

**Verify**
- `go test ./pkgs/droner/dronerd/server -run TaskManager` passes.

## Step 3.4: OAuth state store
File: `pkgs/droner/dronerd/server/handlers_oauth.go`

- `create` stores state with interval and nextPoll.
- `status` returns unknown state when missing.
- TTL expiry marks failed.
- `updatePoll` updates interval and nextPoll.

**Verify**
- `go test ./pkgs/droner/dronerd/server -run OAuthState` passes without sleeps.

## Step 3.5: MiddlewareLogger
File: `pkgs/droner/dronerd/server/middleware_logger.go`

- Status recorder sets status on WriteHeader and Write.
- Panic inside handler returns 500 and logs.

**Verify**
- `go test ./pkgs/droner/dronerd/server -run Middleware` passes.

## Step 3.6: subscriptionManager
File: `pkgs/droner/dronerd/server/subscriptions.go`

- Subscribe is idempotent.
- Unsubscribe on missing key is no-op.
- `PRClosed/PRMerged/BranchDeleted` triggers cleanup.

**Verify**
- `go test ./pkgs/droner/dronerd/server -run Subscription` passes with fake remote provider.
