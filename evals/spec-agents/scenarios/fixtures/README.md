# Captured-then-curated fixtures (#356)

Each directory is a frozen `specs/` state a section-alone scenario starts
from. Provenance: produced by one real run of the upstream section, then
reviewed and hand-tuned before freezing. Refreshing a fixture is a conscious
event — re-run the upstream section, re-curate, re-freeze — never silent
drift.

- `lunch-coordinator-requirements/` — requirements output of the
  `req-lunch-coordinator` run (2026-08-02), curated: Slack open/cutoff
  notifications moved from out-of-scope into Functional Requirements #9 (the
  interview had not elicited them; the brief wants them).
- `expense-tracker-requirements/` — the Expense Tracker PRD from the
  generated-app scopes design (its P6 spec bundle,
  `docs/design/draft/spikes/artifacts/p6-project/`), not a captured run: two
  actors, seven numbered stories, and an own-rows-vs-every-row permission
  split, so the design section's security step has something to catalog.
- `lunch-coordinator-design/` — design output of the `design-lunch-coordinator`
  run (2026-08-02, pass band 93), frozen as produced: `lunch-api` +
  `lunch-webapp` components with design.json / openapi.yaml / wireframes,
  design.cell, and validation-criteria.json.
