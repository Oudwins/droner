# Step 6 - Add Reconciliation, Replay, and Rebuild Tooling

## Objective

Reintroduce reliable startup and recovery behavior on top of the event model.

## Deliverables

- projection rebuild tooling from the sessions event log
- non-terminal session reconciliation flow
- startup recovery that appends corrective facts instead of mutating rows silently
- operator tooling for inspecting session timelines and recovery outcomes

## Work

1. Add projection rebuild support that consumes `droner.sessionslog.db` and recreates projection state.
2. Define reconciliation for non-terminal sessions only.
3. Revisit startup behavior and replace the current "skip hydration" stopgap with explicit recovery logic.
4. Make reconciliation inspect real-world state and append facts like failure, cleanup-needed, or already-completed outcomes instead of silently rewriting projection state.
5. Add crash-recovery tests and rebuild tests.

## Timing Decision

Automatic recovery is required for parity, but it is intentionally deferred until after the main lifecycle paths are event-based so recovery can be designed against the real system rather than guessed early.

## Important Constraint

Recovery should be explainable as: replay facts, inspect the outside world, append new facts. If we cannot describe it that way, the design is still too muddy.

## Exit Criteria

- startup no longer depends on the old hydration model
- projections can be rebuilt from the sessions log
- reconciliation outcomes are durable and inspectable
