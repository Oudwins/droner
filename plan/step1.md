# Step 1 - Proven Scope and Tracer-Bullet Decision Record

This step is complete. It is the frozen decision record for the first event-based slice that is now live in the codebase.

## Scope Decision

The migration remains limited to server internals and server-owned infrastructure code.

- In scope:
  - `pkgs/droner/dronerd/**`
  - `pkgs/droner/internals/backends/**`
  - server-owned integration/orchestration code used by `dronerd`
- Out of scope for the migration itself:
  - `pkgs/droner/sdk/**`
  - `pkgs/droner/tui/**`
  - public REST route shapes
  - public JSON request/response payload shapes in `pkgs/droner/internals/schemas/**`

## What Is Proved

- `POST /sessions` now persists a session event stream instead of directly creating the old session row.
- `GET /sessions` now reads from the new projection for the migrated create flow.
- `GET /tasks/{id}` for create still works through a compatibility shim synthesized from the projection.
- The create flow is driven by facts plus a process manager.
- The event log is reusable outside `sessionevents` through `internals/eventlog` and `dronerd/sessionslog`.

## Event Set Proved In The Tracer Bullet

- `session.queued`
- `session.environment_provisioning.started`
- `session.environment_provisioning.success`
- `session.environment_provisioning.failed`
- `session.ready`

Notes:

- `session.queued` is the first persisted fact for an accepted create request.
- `session.ready` is the standalone fact that maps to a usable session from current client point of view.
- create-path failures currently land in `session.environment_provisioning.failed` because `backend.CreateSession(...)` is still a coarse step.

## Concrete Architecture Decisions Learned

- The sessions event log should have its own DB file and be owned exclusively by the eventlog backend.
- Projection data should live in a separate DB file from the event log.
- The shared sessions event log path should be opened through `dronerd/sessionslog`, not reconstructed ad hoc in each caller.
- Existing coarse backend lifecycle methods are acceptable temporarily when called from a process manager.
- Compatibility shims are fine in the migration, but not as the final internal shape.

## Current Files That Embody Step 1

- `pkgs/droner/internals/eventlog/**`
- `pkgs/droner/dronerd/sessionslog/**`
- `pkgs/droner/dronerd/sessionevents/**`
- `pkgs/droner/dronerd/server/handlers.go`
- `pkgs/droner/dronerd/server/server.go`
- `pkgs/droner/droner-eventdebug/main.go`

## Tracer-Bullet DB Layout

- Sessions event log:
  - `<data dir>/db/droner.sessionslog.db`
- Tracer-bullet projection/read-model data:
  - `<data dir>/db/droner.new.db`

## Explicitly Deferred Beyond Step 1

- `DELETE /sessions`
- `POST /sessions/complete`
- `POST /sessions/nuke`
- GitHub-driven observations and derived actions
- opencode-driven observations and derived actions
- startup reconciliation
- projection rebuild tooling
- full removal of `tasky`
- replacement of every legacy session-row mutation path

## Exit Criteria

Step 1 is done because all of the following are now true:

- the migration direction is proved by a real running slice
- the external server contract still works for create/list/create-task-status
- a reusable eventlog foundation exists
- the event log DB is no longer coupled to projection writes
- the next steps can build from a real baseline instead of a hypothetical plan
