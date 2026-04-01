# Step 5 - Add External Observations and Reactive Workflows

## Objective

Move GitHub and other asynchronous server-owned observations into the event model, then add opencode-derived signals deliberately once we decide how they should be represented.

## Deliverables

- durable observation events for the first external signals that matter to session lifecycle
- process-manager reactions that convert observed facts into internal lifecycle facts
- timeline visibility for both the observation and the resulting action

## Work

1. Identify the first external observations worth migrating:
   - PR created
   - PR deleted
   - PR merged
   - PR reviewed
   - review requested/tagged
2. Record these as observed facts before doing derived work.
3. Rebuild remote-subscription behavior so it appends facts instead of directly mutating server state or enqueuing opaque jobs.
4. Decide separately how opencode-derived signals should enter the event model and which namespace they should use.
5. Ensure these flows target the appropriate session stream when they are session-specific.
6. Add tests for repeated delivery, ordering assumptions, and causation chains.

## Naming Rule

Use dotted namespaced event types for remote observations, for example:

- `remote.pr.created`
- `remote.pr.deleted`
- `remote.pr.merged`
- `remote.pr.reviewed`
- `remote.review.requested`

Do not treat opencode as a `remote.*` source. Add its event names later once the opencode integration shape is settled.

## Design Rule

If an external signal affects a session, the durable timeline should answer both:

- what was observed
- what the server decided to do because of it

## Exit Criteria

- important external signals are durably visible in the event model
- subscription-driven lifecycle changes no longer depend on legacy ad hoc paths
- debugging a session affected by GitHub and other migrated observations becomes materially easier
