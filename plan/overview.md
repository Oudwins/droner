# Event-Based Server Migration Plan

This plan now starts from a proved tracer-bullet, not a blank slate. The server already has one live event-based slice for session creation. The rest of the plan is the staged migration from that proved baseline to an event-based server architecture across the whole session lifecycle.

## Scope Guardrails

- Rewrite server internals only.
- Aim for functional parity with the old implementation by the end of the migration.
- Preserve the REST API, SDK contract, and TUI behavior where it does not conflict with the new event-based model, but do not preserve legacy task mechanics or exact visible state labels just for their own sake.
- Keep single-machine SQLite deployment as the default architecture.
- Prefer coarse, readable event flows over a large number of tiny abstractions.

## Current Baseline

The concept is already proved in code.

- `pkgs/droner/internals/eventlog` is the shared event-log abstraction.
- `pkgs/droner/internals/eventlog/backends/sqlite` is the first backend.
- `pkgs/droner/dronerd/sessionslog` is the factory for the reusable sessions event log.
- The sessions event log lives in `<data dir>/db/droner.sessionslog.db` and is owned exclusively by the eventlog backend.
- The tracer-bullet projection data lives in `<data dir>/db/droner.new.db`.
- `pkgs/droner/dronerd/sessionevents` currently powers the event-based create-session flow.
- `POST /sessions`, `GET /sessions`, and create-task status compatibility now run through the new path.
- `GET /tasks/{id}` for create is currently a compatibility shim synthesized from the projection, not a real `tasky` record.
- `tasky` still exists for legacy lifecycle flows that have not been migrated yet.
- Old startup hydration is currently skipped instead of being reimplemented in the new model.
- `just eventdebug` now reads the sessions event log DB by default.

## Locked Decisions

- Functional parity is the goal; exact legacy task semantics are not.
- `GET /tasks/{id}` and CLI `--wait` are not migration goals and should be removed instead of reimplemented long-term.
- Projection-visible session states may expand beyond the old set when there is a concrete reason, but unnecessary churn should still be avoided.
- Event names should use dotted namespaces, for example `session.completion.requested` and `remote.pr.created`.
- Add finer-grained events only when a concrete debugging or recovery gap appears.
- Automatic recovery remains a requirement, but the full recovery design can land later in the migration after the main lifecycle paths are moved.
- External signals should always be logged as observed facts before they trigger derived lifecycle actions.
- Legacy data does not need migration; old sessions can be discarded at cutover.
- Completed-worktree reuse is deferred until after parity work unless a concrete gap forces it back into scope.

## Architecture Direction

- Session event streams become the authoritative write-side history for session lifecycle.
- Projections serve reads and remain disposable.
- Process managers orchestrate side effects in reaction to facts.
- External observations are recorded durably before derived actions happen.
- Reconciliation emits corrective facts instead of silently mutating state.

## What We Learned From The Tracer Bullet

- The architecture is viable without changing client-facing APIs.
- We do not need to preserve legacy task polling or wait flows to achieve real parity.
- A shared reusable eventlog package is worth extracting early.
- The event log DB should be owned exclusively by the backend; projections should not share that file.
- Separate DBs for event history and projections are cleaner operationally than one mixed file.
- Coarse existing backend methods can be reused initially from a process manager without blocking the migration.
- Compatibility shims are acceptable temporarily, but they should not become the long-term internal model.

## Remaining Migration Strategy

Do not restart from scratch. Continue from the proved baseline in narrow vertical slices.

Recommended order:

1. Lock in the proved tracer-bullet decisions.
2. Stabilize the shared eventlog and sessionslog infrastructure as the permanent base.
3. Treat the create-session path as the reference implementation and clean up its internal shape.
4. Expand the event-based flow to completion, deletion, and nuke.
5. Move GitHub/subscription-driven behavior into the event model and decide how opencode-derived signals should be represented.
6. Reintroduce startup recovery as event-driven reconciliation.
7. Remove legacy tasky/session-row orchestration, old task endpoints/wait assumptions, and old hydration paths.
8. Harden the architecture, tests, and package boundaries.
9. Consider optional follow-on work like Lua hooks, richer projections, and snapshots.

## Step Index

- `plan/step1.md` - Proven scope and tracer-bullet decision record.
- `plan/step2.md` - Shared eventlog and sessionslog foundation.
- `plan/step3.md` - Proven create-session tracer bullet and immediate cleanup debt.
- `plan/step4.md` - Expand the lifecycle to completion, deletion, and nuke.
- `plan/step5.md` - Add external observations and reactive workflows.
- `plan/step6.md` - Add reconciliation, replay, and rebuild tooling.
- `plan/step7.md` - Remove legacy orchestration and finish cutover.
- `plan/step8.md` - Harden and simplify the resulting event-based server architecture.
- `plan/step9.md` - Optional follow-on work after the migration is stable.

## Principles

- Events are facts; process managers do work.
- Rebuild projections, not side effects.
- Prefer typed Go structs over generic payload handling in application code.
- Keep eventlog generic and topic-scoped; keep session semantics in `dronerd` packages.
- Preserve externally visible functionality while being willing to delete legacy task mechanics and internal compatibility code aggressively.

## Success Criteria

- The full session lifecycle is represented as durable facts.
- Server reads come from projections built from the event log.
- External signals are durably recorded and traceable.
- Startup recovery works through reconciliation, not ad hoc mutation.
- Legacy tasky-orchestrated session lifecycle code is deleted.
- The old implementation's session functionality exists on the new event-based path even if some task/status affordances are intentionally removed.
- The REST API, SDK, and TUI do not require a separate rewrite project beyond deliberate task/wait cleanup.
