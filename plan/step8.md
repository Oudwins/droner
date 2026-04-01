# Step 8 - Harden and Simplify The Resulting Architecture

## Objective

Once the cutover is complete, turn the migrated system from "working migration" into a clean long-term architecture.

## Deliverables

- stronger tests around event ordering, process-manager behavior, and rebuilds
- clearer package boundaries in `dronerd`
- better docs for operators and future contributors
- removal of temporary migration naming/scaffolding

## Work

1. Add direct tests for process managers, projections, and replay/rebuild behavior.
2. Decide whether `sessionevents` should remain the long-term package or be split into clearer domain/app/projection packages.
3. Remove temporary compatibility-oriented naming where it no longer buys anything.
4. Document the final event model, DB layout, rebuild process, and recovery model.
5. Update `AGENTS.md` and related docs so the repo map matches reality.

## Exit Criteria

- the architecture is understandable without migration folklore
- future work can build on the event-based model directly
- temporary scaffolding has been minimized or removed
