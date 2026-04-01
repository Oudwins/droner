# Step 9 - Optional Follow-On Work After The Migration Is Stable

## Objective

Capture the next layer of value that becomes possible after the event-based server migration is complete.

## Possible Follow-On Work

- Lua hook points that react to lifecycle events and record applied decisions as facts
- richer activity projections and operator views
- more detailed backend-specific events for future EC2 support
- snapshots if replay becomes a measured bottleneck
- additional eventdebug capabilities built directly on `dronerd/sessionslog`
- topic-specific tooling beyond the sessions stream if other event domains emerge

## Rule

None of this should block the core server migration. If a proposed follow-on feature starts distorting the migration sequence, it belongs in a separate plan.
