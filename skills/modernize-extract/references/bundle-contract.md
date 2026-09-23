# Requirements bundle contract

The tarball AEP imports is a **single top-level directory** of **flat** files
(no subdirectories). Allowed extensions: `.md`, `.excalidraw`, `.dsl`. The
importer refuses nested paths — AEP's requirements save gate only sees flat
entries under `specs/requirements/`.

When packing from this skill, the top-level directory is the contents of
`.aep/requirements/` (files at the root of the archive's one directory).

## Size

- Soft target: ~8 KiB per document.
- Soft warning (importer): decompressed total above 64 KiB.
- Hard refuse (importer): decompressed total above 256 KiB.

Every admitted `.md` is inlined into every design turn with no truncation.
Size is a correctness requirement.

## Files

### `prd.md` (required)

Exactly the `prd-contract` template. Minimum:

```markdown
# <project name> — PRD

## Problem Statement
…

## Solution
…

## Actors
- <actor>: …

## User Stories
1. As a <actor>, I want <feature>, so that <benefit>.
2. …

## Product Decisions
…

## Out of Scope
…

## Open Questions
…

## Further Notes
…
```

Rules that the importer and build gate both enforce:

- `## User Stories` must contain numbered lines matching `N. As a …` (N ≥ 1).
  An empty story list is `MISSING_USER_STORIES`.
- Product altitude only — no languages, frameworks, folder layouts, or
  component names.
- Skip-valve assumptions carry the `*assumed*` tag in Product Decisions (or
  wherever they land).
- Omit `## Further Notes` when empty.

### `domain-model.md` (recommended)

Entities the product cares about: fields, relations, invariants. No storage
engine, no ORM, no table DDL unless a constraint is product-visible (e.g.
"invoice numbers are unique per org").

Suggested shape:

```markdown
# Domain model

## Entities
### <Entity>
- fields: …
- invariants: …

## Relations
- <Entity> *—* <Entity>: …
```

This feeds `design.md`'s `## Domain model (ER)` and thence API schemas.
Inventing an ER model when this file exists is a defect in `/design`.

### `business-rules.md` (recommended)

Load-bearing logic as testable rules. Each rule cites provenance
`path:line` (or a short path range) in the legacy tree so a human can verify.

**Provenance is a citation for a human, not a pointer AE can follow.** AE
never has the legacy repository — only this bundle. `path:line` exists so
someone auditing the extraction later can open the legacy tree and check the
rule was transcribed right; it is never a substitute for the rule's own
content. Every rule's `When`/`Then` (and `Logic`, below) must stand on its
own as the complete behaviour — a rule with provenance but no real content is
a defect, not a shorthand.

```markdown
# Business rules

## <Rule name>
- When: …
- Then: …
- Provenance: `src/billing/pricing.go:120-145`
```

**When "When/Then" would lose behaviour** — a multi-branch calculation, a
precedence or tie-break rule, a state machine, rounding, or an edge case that
only shows up a few branches deep — add a `Logic` step to that same rule,
pseudocode by default:

````markdown
## <Rule name>
- When: …
- Then: …
- Provenance: `src/billing/pricing.go:120-145`
- Logic:
  ```
  1. start from the base rate for the plan
  2. if usage > tier_2_threshold: apply tier_2_rate to the excess only
  3. if the account is on a legacy grandfather plan: cap the total at
     grandfather_cap regardless of the above
  4. round to 2 decimals, half-up
  ```
````

Pseudocode is language-neutral by design: numbered steps, no framework
calls, no types — exactly the behaviour AE needs to reproduce, nothing about
how the legacy app happens to implement it. Reach for a verbatim excerpt of
the function itself — never the file or its surrounding wiring — only when
pseudocode would still lose precision the rebuild needs (exact arithmetic, a
specific library call whose behaviour matters, a regex). Keep every block to
the branches that actually change behaviour; it counts against the size
budget below, and a restatement of the whole function pads without adding
fidelity.

Verbatim fenced blocks — lookup tables, rate tables, formulas, or a `Logic`
excerpt as above — are the **only** exception to "no source in the bundle."
Label every one of them:

> Reference values/logic from the legacy app — not a design constraint on
> implementation shape.

### `integrations.md` (recommended)

External systems the product depends on, named by **capability**, not by
the legacy binding:

```markdown
# Integrations

- Transactional email — receipts and password reset
- Identity provider — employee SSO
- …
```

Binding to a concrete provider happens at design time inside AEP.

## Packing

From the legacy repo after the skill finishes:

```bash
cd .aep && tar czf requirements-bundle.tar.gz requirements
```

The archive must contain exactly one top-level directory whose children are
the flat files above (or pack so the single top-level dir is `requirements/`
with those children). No `SKILL.md`, no nested `features/` tree, no source.
