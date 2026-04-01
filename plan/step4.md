# Step 4 - Expand The Core Lifecycle To Completion, Deletion, and Nuke

## Objective

Migrate the remaining user-triggered session lifecycle endpoints onto the event-based architecture proven by the create flow.

## Deliverables

- event-based handling for:
  - `POST /sessions/complete`
  - `DELETE /sessions`
  - `POST /sessions/nuke`
- new lifecycle facts for completion and cleanup
- projection updates for terminal states and cleanup visibility
- removal of legacy task orchestration for these endpoints rather than reintroducing it elsewhere

## Work

1. Define the next coarse facts needed for completion and deletion, using dotted namespaced event types, for example:
   - `session.completion.requested`
   - `session.completion.started`
   - `session.completion.success`
   - `session.deletion.requested`
   - `session.deletion.success`
   - `session.completion.failed`
   - `session.deletion.started`
   - `session.deletion.failed`
2. Add corresponding process-manager handlers in the same style as the create slice.
3. Move the endpoint internals off legacy `tasky` orchestration one endpoint at a time.
4. Preserve endpoint functionality while allowing task-specific behavior and wait semantics to disappear.
5. Expand the projection so terminal and in-progress states remain understandable without leaning on legacy task state.
6. Add tests covering success, failure, and repeated delivery.

## Important Constraint

Do not redesign every backend method here. Wrap the existing coarse methods first, then split them only where the event flow proves the need.

## Exit Criteria

- create, complete, delete, and nuke are all fact-driven internally
- legacy tasky orchestration is no longer needed for user-triggered lifecycle actions
- the server no longer needs task-style wait flows to provide the old session functionality
