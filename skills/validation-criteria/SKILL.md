---
name: validation-criteria
description: Use when generating the validation criteria — write specs/validation/validation-criteria.json, the machine-readable acceptance oracle for the VALIDATION phase, from the requirement prose alone.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Generate `validation-criteria.json`

You are producing the **acceptance oracle** for the VALIDATION phase: a machine-readable list of
testable criteria derived from the requirement. Every downstream check (generated e2e tests, the
human sign-off checklist) is derived from this file, so **faithfulness to the
requirement matters more than volume** — a wrong or invented criterion silently corrupts every
downstream check.

This skill runs standalone (a turn asking only for criteria) or as the **final step of a
design-generation turn** — in both cases the requirement-only input rule below stands unchanged.

## Input — the requirement ONLY

Read `specs/requirements/prd.md` — the numbered User Stories are the spine,
with `specs/requirements/features/<slug>.md` for depth. **Do not read
the design bundle (design.cell, domain-model.md, flows/), `openapi.yaml`, or
any source code** to derive criteria — the
oracle must be independent of the work it will grade. Base every criterion
only on what the PRD says or necessarily implies, and cite each requirement's
story numbers so coverage is checkable: every story the PRD defines
appears in some entry's `stories`.

## Output — write exactly one file

Create `specs/validation/validation-criteria.json` (use `addFile`; if it already exists, replace its
contents). The file MUST be valid JSON conforming **exactly** to the schema below — no comments, no
trailing prose, no Markdown fences. Keep field order stable.

**Regeneration — keep ids stable.** When the file already exists in your workspace, keep each unchanged
criterion's `id` the same as in the existing file. Committed e2e specs are keyed by criterion id
(`tests/e2e/specs/<AC-ID>.spec.ts`), so renumbering an unchanged criterion would orphan its spec. Assign
a fresh id only to a genuinely new or reworded criterion.

```json
{
  "requirements": [
    {
      "id": "REQ-001",
      "statement": "Users can reset their password via email",
      "stories": [3, 8],
      "criteria": [
        { "id": "AC-001-a", "must": "A registered email receives a reset link", "method": "e2e" },
        { "id": "AC-001-b", "must": "The reset link expires after 1 hour", "method": "e2e" },
        { "id": "AC-001-c", "must": "The reset confirmation message is clear and actionable", "method": "manual" }
      ]
    }
  ]
}
```

### Field rules

| Field | Rule |
|---|---|
| `requirements[]` | One entry per distinct requirement parsed from the prose. |
| `requirements[].id` | `REQ-NNN`, assigned sequentially (`REQ-001`, `REQ-002`, …). |
| `requirements[].statement` | The requirement restated as one clear sentence. |
| `requirements[].stories` | The PRD story numbers this requirement traces to — every entry cites at least one; a story number that appears in no entry means the oracle misses that story. |
| `criteria[]` | The testable acceptance criteria for that requirement. **≥ 1 per requirement.** |
| `criteria[].id` | `AC-<req-number>-<letter>`, e.g. `AC-001-a`, `AC-001-b`. |
| `criteria[].must` | A single, **atomic**, verifiable assertion. No conjunctions — split "X and Y" into two criteria. |
| `criteria[].method` | Exactly one of `e2e` \| `manual` (see below). |

## Assigning `method`

- **`e2e`** — deterministically checkable by driving the app: a UI flow via a headless browser, or a
  deterministic API/CLI assertion for non-UI behavior. Prefer this whenever a stable pass/fail assertion
  is possible.
- **`manual`** — everything that cannot be checked by a deterministic e2e assertion: needs human
  judgment, exploratory interaction, or subjective evaluation (e.g. "the error message is clear and
  actionable", "the layout is usable on mobile"), or has external/physical/third-party side effects
  beyond an agent's reach.

When unsure, choose `e2e` if you can phrase a concrete deterministic assertion; otherwise `manual`.
Every criterion gets exactly one method.

## Discipline

- **Faithful, not inventive.** Every criterion must trace to something the requirement actually says or
  necessarily implies. Do NOT invent scope, endpoints, or features.
- **Atomic.** One independently checkable fact per criterion. Split compound requirements.
- **Deterministic.** The same requirement should yield substantially the same criteria each run.
- **Report ambiguity, don't fabricate.** If the requirement is ambiguous or missing acceptance detail,
  do NOT guess a criterion to fill the gap. Instead, **list the ambiguities and any assumptions you made
  in your reply to the user** — keep them OUT of the JSON file (the file stays exactly on-schema).

## Do not

- Do not add fields beyond the schema, comments, or any prose inside the JSON.
- Do not read the design bundle (design.cell, domain-model.md, flows/), `openapi.yaml`, or source code to derive criteria (requirement-only input).
- Do not generate e2e test files, a validation workflow, or anything else — this skill produces the
  single `specs/validation/validation-criteria.json` file, whether it runs standalone or as the final
  step of a design turn.
