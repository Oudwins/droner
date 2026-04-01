# Step 7 - Remove Legacy Orchestration and Finish The Cutover

## Objective

Delete the remaining legacy lifecycle engine once the full session lifecycle and recovery behavior exist in the new model.

## Deliverables

- old tasky-driven session lifecycle code removed
- old direct session-row mutation paths removed
- old hydration/subscription orchestration removed or replaced
- backend coupling to legacy DB state removed
- task endpoints and wait-oriented compatibility paths removed where they only exist to preserve the legacy task model

## Work

1. Audit every remaining server path still depending on:
   - `core/tasks.go`
   - legacy session-row mutations
   - old hydration logic
   - legacy subscription job enqueueing
   - task wait/status compatibility that no longer serves the final product
2. Migrate those paths to events + projections + process managers.
3. Remove compatibility shims that are only necessary because legacy and new systems coexist internally.
4. Revisit backend code that still reaches into legacy DB tables, especially local completed-worktree reuse behavior, and redesign or delete it.
5. Delete dead code promptly once the replacement path is proved.
6. Remove the old `--wait`/task-style assumptions from server-side behavior once no migrated flow depends on them.

## Exit Criteria

- there is one session lifecycle engine in the server and it is event-based
- legacy tasky session orchestration is gone
- the server no longer needs the old lifecycle/session mutation model or legacy task compatibility model to function
