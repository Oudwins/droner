# Step 3 - Proven Create-Session Tracer Bullet and Immediate Cleanup Debt

This step is partially complete in implementation and complete as proof of concept. It should now be treated as the reference vertical slice for the rest of the migration.

## What Exists Today

- `POST /sessions` appends `session.queued`
- a create-process subscription reacts to `session.queued`
- follow-up facts are appended through the shared sessions log
- `session_projection` is updated from subscribed events
- `GET /sessions` reads from the projection
- create-task status is synthesized from the projection for compatibility

## What This Step Still Needs As Cleanup

Before copying this pattern everywhere else, tighten the shape of the implementation.

1. Keep `sessionevents` readable and typed.
2. Replace remaining ad hoc payload shapes where needed with clearer typed event payload structs.
3. Add direct unit tests around `sessionevents` behavior, not just server-level coverage.
4. Keep the create flow as the reference implementation for future lifecycle migrations.

## Rules Learned From The Create Slice

- compatibility can be preserved while replacing internals aggressively
- the process manager can call coarse backend methods initially
- a projection-backed task compatibility layer is acceptable temporarily
- keeping event history and projection state in separate DBs is cleaner

## Exit Criteria

Treat this step as complete when:

- the create slice remains stable under continued refactoring
- the rest of the migration can copy its structure instead of inventing a new one
- direct legacy create orchestration is no longer the model anyone reaches for
