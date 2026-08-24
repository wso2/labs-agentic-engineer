# Console development flow

Issue-driven (ADR-0001): a feature lives in a **GitHub issue** until it
ships, and no issue enters the build without having been grilled. The repo
keeps only what must outlive the feature — the PRD, the guides, and concise
ADRs. Console work requires `gh` auth.

**Start every frontend feature with the `/console-feature` skill** — pass it
the idea in plain words, or the number of an issue that already exists. It
runs the grilling interview, gets the outcome written down, and then drives
the build with a checkpoint at each stage. This doc remains the spec it
follows.

Issues live in the **upstream** repo — pass
`--repo wso2/labs-agentic-engineer` to every `gh issue` command (working
clones may have a fork as `origin`).

```text
       ┌─ an idea ───▶ /console-feature <idea>   ──▶ grill ──▶ feature issue,
       │                                                       Decisions in the body
PRD.md ┤ (context)                                                 │
       │                                                           │
       └─ an issue ──▶ /console-feature <issue#> ──▶ grill ──▶ decisions comment
          opened by hand                                        on that issue
                       (either way: one issue, labels console + feature)
                                                           │
                                        durable? ──▶ ADR in design/decisions/
                                                           │
                                                           ▼
   contract diff ──▶ console gen ──▶ mocks ──▶ UI ──▶ feature branch + PR
                                                      (+ PRD entry)
                                                           │
                            feedback loop: review ⇄ push (on the PR)
                                                           │
                                              gate: validate in mock mode
                                                           │
      changed the contract? ──▶ BE handshake issue ──▶ backend PR lands on
                                (diff + branch name)      the feature branch
                                                           │
                                       gate: validate the whole feature on
                                             the local setup (real API)
                                                           │
                                                           ▼
              merge branch ──▶ main: feature shipped, its issues closed
```

## Steps

1. **Grill first — whichever end you start from.** Read `PRD.md` and the ADRs
   first: the feature must fit the product picture or explicitly change it.
   Then run `/grill-me` (the `/console-feature` skill does this for you).
   Every open question is decided by the developer in the interview, never
   answered on their behalf. There are two entry points, and they differ only
   in *where the outcome is written down*:

   - **From an idea** — nothing exists yet. Grill the raw idea, then create
     the issue from the outcome (step 2). The issue is **born grilled**.
   - **From an existing issue** — someone opened it by hand. Grill the issue
     as written, then post the outcome as a **decisions comment** on it:
     what was decided, why, what was rejected. The comment is the record —
     don't retro-fit it into a body someone else wrote. Never open an issue
     just to "have somewhere to grill"; that's the idea entry point.

2. **Open the feature issue** (idea entry point). `gh issue create` with
   labels `console` + `feature`; the body is the feature doc (template
   below), filled from the grilling outcome — including the **Decisions**
   section. The issue exists only once the shape of the feature is settled.

3. **Keep it current.** While the issue is **open**, edit the body in place
   so it always reflects the current shape of the feature, Decisions
   included. The issue body tracks the **end state**, not the history of
   getting there — superseded rationale is overwritten, and anything that
   must survive that becomes an ADR (step 4). Comments are the opposite:
   a decisions comment is a dated record and stays as posted; a later
   reversal is a **new** comment, not an edit of the old one.

4. **Graduate durable decisions to ADRs** (`design/decisions/ADR-NNNN-*.md`).
   A decision earns an ADR when it **(i)** sets a convention other features
   must follow, **(ii)** changes the PRD, or **(iii)** rejects an approach
   someone would plausibly re-propose. Feature-local choices stay with the
   issue — its Decisions section or its decisions comment. A superseding ADR
   marks the old one `Superseded by ADR-NNNN`.

5. **Build the frontend on mocks.** Contract diff in
   `packages/contracts/api/v1/openapi.yaml` — the spec is the **source of
   truth** for the API (see #76); editing it *is* the contract change — →
   `pnpm --filter @aep/console gen` (console-scoped; root `make gen`
   regenerates every Go module and is never needed for console work) → typed
   MSW mocks (cover the scenarios from the grilling decisions) → UI in mock
   mode (`VITE_API_MODE=mock`). Follow `design-system.md` and
   `api-guidelines.md`.

   Push it to the **feature branch** and open the PR against `main`. That
   branch is where the whole feature is assembled — frontend now, backend
   next — and it is the only thing that ever merges to `main`. Attach the
   feature's screenshots to the PR (drag-and-drop by a human — screenshots
   are never pushed to git branches), and post a comment on the feature
   issue linking the PR: from that point all feedback belongs on the PR, not
   the issue. Review comments loop back into this step (implement → verify →
   push).

   The PR also carries the **PRD update**: the feature's entry in the
   feature inventory, linking the issue and any ADRs, plus amendments to any
   PRD section the feature changes. A feature PR without the PRD update is
   incomplete.

   **Gate: validate the feature in mock mode** and get the developer's
   confirmation. A mock-validated frontend is what makes the next step
   concrete — until it exists there is nothing solid to ask the backend for.

6. **BE handshake.** Only if the feature changes the contract. New or
   changed `aep-api` behavior gets its own GitHub issue whose body is the
   request: **the contract diff already on the feature branch** (exercised
   by real UI code and typed mocks, so it is a proposal that has been
   proven to work, not a sketch), the rationale, a link to the feature
   issue, and **the feature branch name**.

   **The backend lands on the feature branch, not on `main`** — its PR
   targets the branch, so frontend and backend meet before either reaches
   `main` and neither can ship half a feature. Contract deltas discovered
   while implementing go back into the same branch and the same handshake
   issue.

7. **Ship — test it in the local setup, then merge.** Entry condition: the
   backend changes are on the feature branch (or the feature needed none).
   Bring up the local setup with the branch built into it
   (`docs/developer-guide/setup.md`) and test the feature against the real
   `aep-api` — **not** mock mode: mocks proved the frontend, and this is
   what proves the two halves agree. Walk it the way the issue's experience
   walkthrough describes it, and get the developer's confirmation that it
   holds.

   **Merging the feature branch to `main` ships the feature**, so the merge
   comes last and nothing follows it: the PRD entry is already in the PR.
   Put `Closes #<feature-issue>` in that PR's body — plus
   `Closes #<handshake-issue>` whenever the feature raised one, because
   closing keywords fire only when a PR merges into the default branch, so
   the backend's own PR into the feature branch can't close its issue. A
   **merged PR is frozen**: anything discovered afterward is a new issue
   referencing the original.

## Rules

- **`/console-feature` is the entry point** for frontend feature work — pass
  it an idea or an existing issue number; either way the grilling happens
  before any code.
- **The feature branch is the integration point.** Frontend and backend both
  land there and are validated together; only the branch merges to `main`.
  A feature never reaches `main` in halves.
- **The merge is the ship.** Validation happens on the local setup while the
  PR is still open; merging closes the issues via `Closes #<n>` and freezes
  the PR. Nothing about a feature is left to do after its merge.
- **Two validation gates, and they prove different things.** Mock mode
  proves the frontend and produces the contract proposal; the local setup
  proves frontend and backend agree. Neither substitutes for the other.
- **Closed issues and merged PRs are frozen history** — never edited or
  pushed to when later work supersedes them; post-merge requests become a
  new issue referencing the original. **ADRs are the current truth**;
  supersede explicitly.
- **The open issue is the in-flight tracker** — `gh issue list --repo
  wso2/labs-agentic-engineer --label console --label feature` is what's
  being built right now. The PRD records shipped features only.
- Lookup order for any session needing context: **ADRs first**, then
  `gh issue list --repo wso2/labs-agentic-engineer --label console --label
  feature` (and `--state closed` for history).
- No UI feature work without a **grilled** feature issue — born grilled, or
  grilled after the fact with a decisions comment. Bug fixes and polish are
  exempt.

## Feature issue body template

The shape every feature issue converges on. When the issue is created after
the interview, it's filled from the grilling outcome. An issue opened by hand
should still follow it — its **Decisions** section stays empty and the
grilling outcome arrives as a decisions comment instead (step 1).

```markdown
## Problem
<!-- What's broken or missing for the user. Not the solution. -->

## Users
<!-- Which PRD personas this serves. -->

## Experience walkthrough
<!-- The user's path, step by step. The grilled, decided version. -->

## Scope
**In:**
**Out (explicitly):**

## Decisions
<!-- The grilling outcome: what was decided, why, what was rejected. Empty
     for an issue opened by hand — its outcome is a decisions comment.
     Durable ones graduate to ADRs (flow step 4). -->

## Contract changes
<!-- aep-api endpoints needed (OpenAPI sketch), or "None". Becomes the BE
     handshake issue (flow step 6). -->

## States to design
<!-- Empty / loading / error / permission states. -->

## Open questions
<!-- Anything the grilling deliberately left open, with why. Empty is the
     normal case — open questions are what the interview closes. -->
```
