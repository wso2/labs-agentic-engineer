# ADR-0021: Builds is a version ledger, and the run story is one click down

- **Status:** Accepted
- **Date:** 2026-08-25 (grilling of the Build-and-Deploy design handoff,
  [#609](https://github.com/wso2/labs-agentic-engineer/issues/609))
- **Supersedes:** [ADR-0015](./ADR-0015-now-first-builds-page.md) in full. See
  *What happens to ADR-0013 and ADR-0014* below — ADR-0015's amendments to
  those two do **not** silently revert.

## Context

ADR-0015 made Builds now-first: the page lands on the newest version's live run,
with no list in between, because "what is happening right now, and how much is
left" is the question a reader arrives with. `BuildsPage.tsx` stated the
consequence plainly — *"There is no ledger list in between."*

That is true of a reader who arrives **during** a run. It is not true of the
reader who arrives between them, which is the common case: a project builds for
twenty minutes and is then read for days. That reader asks *what has this project
built, and how did each one go?* — and answered it by selecting a version from a
dropdown, reading it, remembering it, and selecting the next one.

## Decisions

1. **Builds is a version ledger — one row per version, newest first.** Version ·
   Milestone · Status · Duration · Started, dense, every row clickable. A
   live row tints and its status dot pulses, so the now-first reader still sees
   at a glance that something is moving and is one click from it.
   *Supersedes ADR-0015's premise and its title.*

2. **The run story moves to a build detail page, keyed by version tag**
   (`/projects/$projectName/builds/$tag`). Nothing about a run becomes
   unreachable; it stops being what the section's landing page is *for*.

3. **The build page is a summary card and three collapsible sections, not a
   stage rail.** The card carries Milestone, Started, Duration and the task
   breakdown; the sections are Tasks, Coding agent log, and Build logs.
   *Supersedes ADR-0015 §1 (the stage glance strip), §3 (the milestone panel
   beside the run) and §5 (the delivered banner). ADR-0015 §4 — "in progress" is
   derived, in two strengths — survives and is extended by §5 below.*

4. **Provisioning gates are task rows, not a stage.** A connection to configure
   and a feature to write are peers in one list, each with its own way out
   (*Configure in Resources* on a gate, *Log* on a running task). This is what
   replaces the rail's provisioning section and ADR-0015 §6's separate treatment
   of holds: a hold is a row that needs you, rendered like every other row that
   needs you.

5. **A task row's state is DERIVED, and the derivation is the decision.**
   `derivedStatus` is deliberately a two-value vocabulary — the issue is open, or
   it is closed — so the row's five states come from elsewhere, in this
   precedence: closed → `done`; `hold` or a non-empty `blockedBy` → `blocked`;
   a newest execution that is running and unfinished → `in_progress`; an
   execution that **ended** while the issue is still open → `in_review`;
   otherwise `pending`.

   `in_review` is the one worth defending: there is no ready-for-review field on
   the contract, and *"the agent finished and nothing merged"* is the only honest
   reading of that pair. The row's second line is the issue's newest comment
   ([#612](https://github.com/wso2/labs-agentic-engineer/issues/612)), flattened
   to its first non-empty line.

6. **The ledger adds NO contract surface. Every cell comes from a read the
   console already makes.** This is the decision that shapes the whole feature:

   | Cell | Source |
   |---|---|
   | Version, Status, Duration, Started | `BuildSummary` |
   | Milestone | `milestoneNumber` — the platform records no title |
   | "Deployed to development" | `ProjectStatus.deploy`, already polled by the layout |

   **There is no Tasks column, and the reason is a fact about the API rather
   than a preference.** An untagged `list-tasks` response cannot be attributed
   to versions: the server sets `lineage.specTag` only on a TAG-SCOPED read and
   leaves it empty when the query spans versions (`reads.go` — *"the version tag
   every returned issue belongs to (empty when the query spans versions)"*), and
   nothing else on a `TaskView` names its version. The only alternative is a
   tag-scoped read **per row**, each one GitHub-backed, which is precisely the
   cost the ledger exists to avoid. The per-version breakdown therefore lives on
   the build page, one click away, where the read is scoped to begin with.

   This was found by deploying and looking: against mocks the column filled,
   because the fixtures set `specTag` on every task regardless of which read
   returned it. Against the real API it was empty on every row.

7. **Only the version the deploy aggregate names may be described by where it
   reached.** The platform records ONE deployed version per project, so every
   other completed version says **Built**. Saying more would be a guess.

8. **`/builds/$tag` is a version; `/tasks/$issueNumber` is a task.** TanStack
   cannot carry two dynamic siblings, and `/builds/$issueNumber` was the task
   page. `builds.$issueNumber.tsx` is deleted, `builds.$tag.tsx` redirects a
   numeric segment to `/tasks/$issueNumber`, and `tasks.$issueNumber.tsx` stops
   redirecting and renders `TaskPage`.
   *Reverses the direction of [#185](https://github.com/wso2/labs-agentic-engineer/issues/185)'s
   redirect. Old `/builds/118` links keep working; the redirect now points the
   other way.*

## What this ADR does NOT cover

The design handoff this came from also drew a two-column **Deployments** board, a
deployment detail page, and per-component runtime logs. **None of that is here.**
It needs a deployment RECORD — every deployment across every environment,
including the failed and superseded ones — and no such record exists: OpenChoreo
`ReleaseBindings` are current state, overwritten on redeploy, and nothing in
Postgres or the activity feed stands in for them. Building it means a new table
and a writer that observes rollouts, which is a backend feature with a data-model
decision underneath, not console work. Deployments keeps the one-story rail
decided on [#401](https://github.com/wso2/labs-agentic-engineer/issues/401),
unchanged.

## What happens to ADR-0013 and ADR-0014

ADR-0015 amended ADR-0014 §4b, §9 and §10, and stood on top of ADR-0013's
amendments by ADR-0014. Superseding ADR-0015 does **not** revert those to their
pre-ADR-0015 wording — the flat `Issues` register (ADR-0014 §10) and run-wide
stage numbering (§4b) are not coming back, and this ADR does not reinstate them.

Concretely: **ADR-0013 and ADR-0014 are now historical.** Their surface — the
version-run card and its numbered rail — is not mounted by anything after this
ADR. ADR-0013 §5 (rows are not clickable; the one link a row carries is its
GitHub issue) is the one clause this ADR **does** overturn for the ledger: a
ledger row is clickable, because it now has somewhere of its own to go. A task
row keeps §5's spirit — its issue chip still links to GitHub, and only the title
navigates. ADR-0014 §9's fetch-on-demand policy survives as the log sections'
behaviour.

## Consequences

- **The stage rail leaves the product.** `RunSpine` and its satellites were
  already unmounted by ADR-0015; after this ADR nothing renders staged
  provisioning → coding → build → deploy anywhere. That is a real loss of a view,
  accepted deliberately: the information it carried is in the task list (gates as
  rows, decision 4) and the two log sections, and the rail's cost was making the
  reader scan six expanded stages to find the one that moved. `RunSpine`,
  `StageRow`, `RunGlanceStrip`, `RunNowPanel`, `RunHoldNotice` and
  `ProvisioningGates` remain in the tree, mounted by nothing.
- **The Milestone column shows a number, not a title.** `milestoneNumber` is what
  the ledger read carries; the title is on `MilestoneRunView`, which is
  tag-scoped and would cost a request per row. The lexicon's promise that the
  milestone "stays discoverable on the Builds page" is met by the number.
- **The commit and branch are not on the ledger.** Same reason. They remain on
  the build page's run story, which is tag-scoped by construction.
- **No BE handshake.** The feature changes no contract, which is what lets it
  merge on its own.
