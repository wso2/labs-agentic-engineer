# ADR-0020 — The budget is in bytes, and an over-budget document degrades rather than paginating

Status: accepted · 2026-08-17

## Context

Size control was per SECTION and applied to resource functions only. `views/Overview` held two
constants — `MAX_INLINE_OPERATIONS` (100) and `MAX_INLINE_SIGNATURE_BYTES` (20,000) — and they were the
right sizes in the wrong scope:

- `ballerina/crypto` is all module-level functions, so neither guard applied and its `overview` reached
  **1,177 lines / 64,310 bytes**. The eval harness's own cap then truncated it and substituted a 2.2KB
  stub, so the agent received almost nothing and nothing said so.
- `type ballerina/http ClientConfiguration --deps` was **38 declarations, 505 lines, 24,183 bytes**,
  handed back whole with no bound at all.
- A count limit is wrong for name-addressed surfaces and right for path-addressed ones.
  `ballerinax/redis` declares 111 remote functions in 15KB and was measured as the single most
  productive lookup of the 2026-08-15 sweep — a count cap would have replaced the one document that
  worked — while 100 resource functions at gmail's ~480 bytes each is 48KB.

## Decision

**Three budgets, all in bytes, all document-wide and kind-blind.**

| Document | Budget |
|---|---|
| `overview` | bounded by CONSTRUCTION — it generates no signature (ADR-0017) |
| a container listing | 20,000 bytes |
| a filtered response (`-s`, or a selector) | 6,000 bytes |
| a `-r` closure | 20,000 bytes, breadth-first, with an omission list |
| `api` | unbounded; that is its definition |

Bytes rather than counts because one match is 200 bytes as a name and 900 as a resource signature:
github's `repos/{owner}/{repo}/actions/caches` is 723 bytes for three operations where
`googleapis.sheets` averages 210 across forty-three.

**Naming a container is not a filter on it.** `client ballerina/http Client` asked for that container
whole and gets the container budget; the narrower one applies once a selector or `-s` has cut it down.

**NEVER PAGINATE.** An over-budget listing is re-rendered at a coarser tier, and every tier quotes the
same shared renderer:

| Tier | Renders |
|---|---|
| full | one result: the declaration with its parameter documentation, plus the types its signature names |
| signature | the declaration and its first doc line, one per result |
| index | one line each — the name or the path, no signature |
| grouped | path roots, or camelCase prefix clusters, with counts |

A single result always takes the richest tier, which closes the defect where an exact one-of-many name
match printed the name back and forced a second call for the signature the caller had already
identified.

**Whatever collapsed says what it cost, and `--all` is the escape hatch.** `--all` ignores the budget,
is **hidden from `--help`**, and is offered only by the document that collapsed something — last, framed
as a last resort, with its byte figure. Hiding it is the point: a caller who meets it in `--help` reaches
for it before trying a selector, which is the behaviour the budgets exist to prevent.

## Consequences

- Measured tier placement on the corpus, asserted in `ViewsTest` so a budget change shows up as a change
  to that table: `sap` 15, `gmail` 33, `sheets` 44 and `redis` 113 signatures print in full; `slack` 175
  and `twilio` 200 fall to names; `github` 903 falls to path roots.
- The grouped tier requires a prefix of at least three characters, which is ADR-0019's `redis`
  measurement (`z` 20, `s` 14, `h` 14, `l` 10) turned from a recorded finding into a guard.
- A `-r` closure that hits the budget NAMES every type it dropped, because a name is a legal `type`
  argument and a count is not.
- Quoted readme code has no per-block cap and never will: the quickstart's budget selects WHOLE blocks
  and drops one that alone exceeds it, because half a snippet is a paraphrase with the compiler's half
  missing (ADR-0008).
