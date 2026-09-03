# ADR-0019: The lexicon governs the console's user-facing copy

- **Status:** Accepted
- **Date:** 2026-08-19 (wayfinder map
  [#521](https://github.com/wso2/labs-agentic-engineer/issues/521); first drawn
  on by [#561](https://github.com/wso2/labs-agentic-engineer/issues/561))
- **Context:** the console's words were never chosen — they were inherited.
  `CONTEXT.md` and `docs/glossary.md` are precise and disciplined, and both are
  explicitly **internal** engineering languages (*spec bundle*, *milestone run*,
  *stage aggregate*, `org NS`, OpenBao). Nothing recorded what the product says
  to a **user**, so each feature reached into the domain glossary for whatever
  term sat nearest. That is how `Cut version v1 · git tag · Stories in scope ·
  Milestone` ended up on the most consequential click in the product: four
  internal terms surfacing together, never decided as copy.

  The domain model had already conceded the split without writing it down —
  `CONTEXT.md`'s **Milestone run** entry warns *"build (the console's 'build' is
  the click that starts a run, not the run)"*.

## Decision

**`apps/console/design/lexicon.md` is the authority for what the console says to
a user**, as `design-system.md` is for how it looks.

- **A feature draws its words from it.** Introducing a user-facing term means
  amending the lexicon **in the same PR**. A term absent from the lexicon is not
  yet a product word.
- **Two vocabularies, deliberately allowed to differ.** `CONTEXT.md` and
  `docs/glossary.md` say what terms *mean* to engineers; the lexicon says what
  the product *says*. The lexicon carries the mapping between them (e.g.
  *Architecture* ↔ `specs/design/design.cell`), so the divergence is recorded
  rather than rediscovered — and so nobody later "fixes" it in the wrong
  direction.
- **It governs agent prose too, not just component strings.** The user reads the
  agent's narration in the console, so it is product surface. Because a running
  agent cannot read a repo file, the rules reach it through a console-specific
  skill supplied by the caller — which is also why a local run (an agentic
  coding tool, where the user *is* standing in the repo) omits that skill and
  keeps repo paths.

## Consequences

- New user-facing copy is a lexicon amendment plus its implementation, not a
  judgement call made per feature.
- `CONTEXT.md` stays a domain glossary and must not accumulate UI copy — the
  domain-modeling discipline requires it stay "totally devoid of implementation
  details", which a copy deck plainly is.
- Retiring a word is a product change with a blast radius the lexicon makes
  visible. The first pass retired *published*, *draft*, *cut*, *git tag*,
  *milestone*, *stories in scope*, the `v1+` suffix, *solo session* and *AEP*.
- Vocabulary decisions no longer earn individual ADRs — the lexicon is their
  home, at a finer granularity than a decisions log. This ADR establishes the
  authority; it does not restate its contents.

## Alternatives rejected

- **Put user-facing copy in `CONTEXT.md`.** Wrong audience and wrong contract:
  it is a domain glossary, and the discipline that governs it forbids exactly
  this kind of content.
- **Leave copy to per-feature judgement** (the status quo). That is what
  produced the jargon: nobody was choosing, so each feature borrowed the nearest
  internal term.
- **Edit the shared flow skills to stop naming repo paths.** Repo paths are not
  wrong, they are wrong *in the console* — the same skills must run in a local
  agentic coding tool where those paths are exactly right. The vocabulary
  belongs to the surface, hence the console-specific skill.
