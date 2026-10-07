# ADR-0040 — A design goes out of date per feature

**Status:** Accepted · 2026-10-01
**Related:** [ADR-0039](ADR-0039-requirements-are-features-with-stable-ids.md),
[ADR-0041](ADR-0041-a-build-is-a-selection-of-features.md)

## Context

A design was out of date when anything in the requirements changed after it,
so an edit to one feature blocked building every other. With per-feature
requirements, a design can be judged per feature.

## Decision

1. **A feature's basis is what its design reads**: its file's lines by their
   words (IDs kept; sources, clauses and closing tags dropped), then the words
   of each P item that reaches it (`reqspec.Basis`). Confirming an assumed line
   or adding a source changes no basis; editing the product page changes none.
2. **A design run records what it covered in the line that started it**
   (`/design F1 F2`; a bare `/design` covers every feature designable at the
   commit it read). Nothing new is stored: the turn store already keeps each
   turn's instruction and base commit.
3. **A feature is out of date when its basis now differs from its basis at the
   commit of the newest run that designed it.** A feature no run covered is
   undesigned, not out of date. The build gate refuses only a build that
   carries an out-of-date feature (`FEATURE_NOT_BUILDABLE`).
4. **The console works out the same thing live** (`designWork.ts`) from the
   room's documents and `designedFrom` (`GET /spec/state`, each designed
   feature's basis at design time). Both readings are held to `basis.json`.
5. **Acceptance criteria are per feature** (`specs/validation/acceptance/F<n>-…
   .feature`, rules tagged `@story-F<n>.<m>`), and the gate checks only the
   features the version carries.

## Consequences

- Editing Approvals leaves Submit expenses buildable.
- A design turn can be scoped (`/design F2`) to bring one feature up to date.
