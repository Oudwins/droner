# Step 2 - Shared Eventlog and Sessionslog Foundation

This step is also complete. It captures the shared infrastructure that now underpins the migration.

## What Landed

- `pkgs/droner/internals/eventlog`
  - topic-scoped event logs
  - append
  - load stream
  - subscribe with durable checkpoints
- `pkgs/droner/internals/eventlog/backends/sqlite`
  - SQLite-backed event log
  - per-topic sequence
  - per-stream `stream_version`
  - checkpoint storage
- `pkgs/droner/dronerd/sessionslog`
  - canonical sessions-topic factory
  - canonical sessions event log DB path

## Why This Matters

- the event log is now a reusable platform primitive instead of hidden inside `sessionevents`
- the sessions topic can be opened consistently by the server, the debug UI, and future tooling
- the backend now owns its own SQLite file instead of sharing a mixed projection/event DB

## Constraints For Later Steps

- keep `internals/eventlog` generic; do not put session-domain logic there
- keep sessions-topic path/config logic in `dronerd/sessionslog`
- use the same sessions log in eventdebug and any rebuild/reconcile tooling
- add methods to the backend only if a real caller needs them

## Remaining Debt From This Step

- the eventlog package has the right base semantics, but higher-level session code still needs more typed structures on top
- operational tooling for replay/rebuild is still thin
- there is still no dedicated projection package outside the current tracer-bullet implementation

## Exit Criteria

This step is complete because:

- the sessions event log is reusable
- its DB is backend-owned
- it is already consumed by both `sessionevents` and the debug server
