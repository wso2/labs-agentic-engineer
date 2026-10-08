# ADR-0039 — Requirements are features with stable IDs

**Status:** Accepted · 2026-10-01
**Supersedes:** the flat PRD with numbered stories (`#7`, `@story-7`).
**Related:** [ADR-0040](ADR-0040-a-design-goes-out-of-date-per-feature.md),
[ADR-0041](ADR-0041-a-build-is-a-selection-of-features.md); the contract is
`skills/prd-contract`; the glossary is `CONTEXT.md` § Requirements model.

## Context

The requirements were one PRD with numbered stories. Everything downstream —
design citations, acceptance tags, task issues, validation — keyed on a number
that changed whenever a story moved, and the only unit the platform could
interview, design or build was the whole product. Users think in features
("Approvals"), and want to work on, and ship, one at a time.

## Decision

1. **The requirements are a product page, one file per feature, and a
   product-wide page.** `specs/requirements/prd.md` (frame, actors, the
   feature list, Fog), `features/F<n>-<slug>.md` (Purpose, User Stories,
   Decisions, Out of Scope, Open Questions), `product-wide.md` (P items that
   span features, each with `Applies to:`). One home per line: the file where
   it would have to change.
2. **IDs are stable and never reused.** Features `F<n>`, stories `F<n>.<m>`,
   product-wide items `P<n>`. A moved story gets a new ID that records the old
   (`F5.1 (was F2.3)`); a removed one is retired in its file's `## Retired`
   section ("moved to X" / "merged into X" / dropped). A rename changes the
   name and slug, never the ID.
3. **A line's tail is fixed:** words, then `[source · place]`, then
   `Needs:` / `Applies to:`, then the closing `*assumed*` or `*blocking*`.
   `*assumed*` is the agent's decision awaiting the user; there are no
   proposals — edits land directly and the tag is the review.
4. **Two readers, one fixture.** Go (`internal/platform/reqspec`) and the
   console (`apps/console` model/requirements.ts) read the files and are held to
   `packages/contracts/requirements/acme-expenses` (`expected.json`,
   `feature-lines.json`, `basis.json`). The save gate refuses an ID in two
   places, a reused retired ID, a story outside its feature's file, and a
   `Needs:` / `Applies to:` naming no live feature.
5. **The interview is per feature.** `/start` writes the product page, the
   product-wide page and a stub per feature; `/interview F<n>` fills one
   feature; `refine` is every later change.

## Consequences

- Designs (`stories` in design.json), acceptance tags (`@story-F2.3`), task
  issues ("Serves stories") and validation results cite stories by ID, so a
  rename or a move does not split history.
- Projects with a flat PRD are not migrated: they can no longer build.
