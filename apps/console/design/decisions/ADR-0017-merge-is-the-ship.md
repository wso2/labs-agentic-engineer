# ADR-0017: The merge is the ship

- **Status:** Accepted
- **Date:** 2026-08-17
- **Context:** the flow left its last stage half-specified. Validation was
  "defined per-feature by the developer for now", and Ship was a sequence
  that ran *after* the merge — validate, move the PRD line, close the issue.
  In practice it never ran. The PRD's *In flight* section had accumulated
  ~20 entries whose PRs had merged long before (the Deployments page landed
  its line in PR #395 itself, commit `fd8fdee9`, and it was still sitting
  there), while the inventory table it was supposed to drain into stopped at
  six rows on 2026-07-28. A step that is always skipped isn't a step — the
  bookkeeping has to happen where work already happens.

## Decision

- **Validation is a local run against the real API.** Bring up the local
  setup and run the console against `aep-api` rather than MSW: mocks proved
  the UI, and validation is what proves the contract. Walk the feature as
  the issue's experience walkthrough describes it; the developer's
  confirmation is the gate.
- **Merging the PR ships the feature and closes the issue** (`Closes #<n>`
  in the PR body). Validation therefore happens while the PR is still open,
  and **nothing follows the merge** — the state after it is the shipped
  state.
- **The PRD update rides in the feature PR**: the inventory entry plus
  amendments to any section the feature changes. The record merges with the
  code that earns it.
- **The PRD's *In flight* section is deleted.** Open `console` + `feature`
  issues are the in-flight tracker; the PRD records shipped features only.

## Consequences

- A feature can no longer be shipped-but-unrecorded, the failure this ADR
  was written from: the entry is a reviewable part of the diff, and there is
  no post-merge step to forget.
- "What's being built right now" needs `gh` — the same cost ADR-0001 already
  accepted, and consistent with its principle that workflow state lives
  where workflow tooling is.
- The BE handshake must be **implemented** before merge, not merely agreed:
  validation runs against a real API. This tightens the old "agreement
  before ship" — mock-mode FE work still proceeds on an unagreed contract.
  ADR-0018 gives that requirement a location: the backend lands on the
  feature branch, so the merge to `main` carries both halves at once.
- The stranded *In flight* entries were folded into the inventory as-is, and
  the six dated table rows kept below them under their own sub-heading. No
  ship dates were invented for the folded entries; the two formats can
  converge the next time someone touches them.
