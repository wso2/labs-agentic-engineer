# ADR-0001: Issue-driven feature flow

- **Status:** Accepted
- **Date:** 2026-07-03
- **Context:** the original flow kept per-feature `feature.md` + `decisions.md`
  under `design/features/`. Grilled 2026-07-03: feature-local decisions go
  stale and mostly don't matter to other features, while AEP itself develops
  software from issues → coding agents — the console should dogfood that loop
  and get tracking/collaboration for free.

## Decision

- **A feature is a GitHub issue** (labels `console` + `feature`): the body is
  the feature doc; while the issue is open it is edited in place to match the
  current shape of the feature.
- **Nothing is built ungrilled**, and the grilling outcome is written down
  wherever the feature entered from:
  - **From an idea** — the interview runs *before* any issue exists and the
    issue is created from its outcome, its body carrying a **Decisions**
    section (decided / why / rejected). The issue is *born grilled*.
  - **From an issue someone opened by hand** — the interview runs on that
    issue and the outcome is posted as a **decisions comment**. The comment
    is the record: the body isn't rewritten to claim decisions its author
    didn't make, and the comment isn't edited afterwards (a reversal is a
    new comment). Nobody opens an issue merely to have somewhere to grill.
- **`/console-feature` is the entry point** for frontend feature work: pass
  it an idea or an existing issue number; it grills, records the outcome,
  and drives the build.
- **Closed issues are frozen history** — never edited when superseded.
- **ADRs in `design/decisions/` are the current truth.** A decision graduates
  from the issue (its Decisions section or its decisions comment) to an ADR
  when it (i) sets a convention other features must
  follow, (ii) changes the PRD, or (iii) rejects an approach someone would
  plausibly re-propose. Feature-local choices stay in the issue and may
  fossilize. A superseding ADR marks its predecessor `Superseded by ADR-NNNN`.
- **Lookup order for sessions:** ADRs first, then
  `gh issue list --label console` — console work requires `gh` auth.
- `design/features/` was deleted; its still-live content was distilled into
  ADR-0002…0004. The BE-handshake pattern (a separate BE issue carrying the
  request) is unchanged — though where the two halves meet moved from `main`
  to the feature branch in ADR-0018.

## Consequences

- The repo stays lean: guides + PRD + concise ADRs; workflow state lives where
  workflow tooling is (boards, labels, PR links).
- Sessions without `gh` access can't read feature history — accepted; ADRs
  carry everything that must survive.
- Issues filed by people who never ran the skill stay first-class: they come
  in the same door and get grilled before any build. The flip side is that
  "an issue exists" never means "the shape is settled" — a filled Decisions
  section or a decisions comment does, and that check is what the skill runs
  before it will build anything.
