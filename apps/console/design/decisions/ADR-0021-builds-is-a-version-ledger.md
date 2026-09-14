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
   precedence: closed **or a recorded merge SHA** → `merged`; `hold` or a
   non-empty `blockedBy` → `blocked`; a newest execution that is running and
   unfinished → `in_progress`; a claiming cycle **with a pull request that has
   not merged** → `pr_sent`; a claiming cycle still open without one →
   `in_progress`; otherwise `pending`.

   **A row reports its pull request, not itself.** One build session dispatches
   ONE pull request claiming a SET of issues (`resolves`), so its state lands on
   every row in that set — three rows reading `PR sent` at once are three issues
   on one pull request, which is the platform's actual unit of work.

   Two corrections this took, both from deployed data:

   - **Claims come from EVERY cycle of EVERY run, not from the open cycle.** A
     cycle ends the moment its pull request settles, so reading only the open one
     threw the answer away at exactly the moment it became final: a task whose
     work had merged, or whose pull request was sent and refused, fell back to
     `Pending` unless GitHub had also closed its issue. And a version is often
     worked by several runs — a `task` run reworking what a `dev` run delivered —
     so the newest run alone does not hold its history.
   - **`merged` reads the SHA as well as the closed issue.** The two can disagree
     for a moment, in one direction: the merge lands before GitHub's close event
     does, and for that stretch the row said `PR sent` about merged work.

   The row's second line is the issue's newest comment
   ([#612](https://github.com/wso2/labs-agentic-engineer/issues/612)), flattened
   to its first non-empty line.

   **The row is not a link, and the list reads ascending.** The title used to
   open the per-task detail page; that page is not a destination this surface
   sends anyone to any more, and the row already carries what it led with. The
   `#N` chip remains, and it goes to the issue on GitHub — which is also why
   nothing was lost. Order is by issue number ascending, the order the milestone
   was planned in: `list-tasks` promises none, so GitHub's newest-first default
   showed through and the gates the platform files first sat at the bottom.

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

9. **A running duration needs a clock, and the Deployments link needs merged
   work.** Two things the summary card got wrong, both worth writing down
   because the obvious implementation of each is the wrong one.

   The Duration counts against `Date.now()` until the build ends — but polling
   alone never made it move. React-query's structural sharing hands back the
   *same* `BuildSummary` object when the payload has not changed, and a running
   build's payload does not change between its own state transitions, so no
   refetch ever caused a re-render and the number sat frozen at first paint.
   `useTicker` supplies the second. It is keyed on `isDurationOpen` — the
   absence of `completedAt` — and NOT on `isLedgerLive`, because the absence of
   an end stamp is exactly the condition under which the number is being
   measured against now. A build that has left `in_progress` without an end
   stamp is still counting, and keying on the status would freeze it. "and
   counting" follows the same condition, for the same reason.

   **One ticker per page, not per component.** The clock started inside the
   summary card, so its forced re-render reached that subtree only — and the
   task rows, a sibling away, format their own elapsed time against `Date.now()`
   with nothing driving them: rows sat at `0m 24s` for two minutes while the
   card above them counted. The interval therefore lives on the build page,
   above both, and runs while *either* is counting — an open duration, or a task
   row still measuring. Two intervals for one page would also drift, and a row
   reading a second behind the card is a page arguing with itself.

   **"Go to Deployments" appears only once the version's work has merged.** A
   version reaches an environment as its work merges, so before that the board
   has nothing to say about it and the link could only disappoint — it sat one
   line above a note reading *"v5 deploys as its tasks merge"*, contradicting
   it. `isDeployable` is a build cycle carrying a `mergeSha`, or the deploy
   aggregate already naming this version.

   The signal matters more than the gate. The obvious one — count the tasks
   whose `derivedStatus` is `merged` — is WRONG, and deploying proved it inside
   a minute: that field is the two-value vocabulary of §5, so a cancelled run
   whose issues were closed without a pull request ever opening (`prNumber` 0,
   no merge SHA) read as fully merged while nothing had landed in the repo.
   `mergeSha` is the platform's only record of a merge, and the contract says
   why there is no second one: *"a merge is recorded by the merge SHA, so a
   second spelling of it could disagree with the first"*. Validation cycles are
   excluded — a validation cycle's SHA names the commit it judged.

   And it asks EVERY run of the version, not the newest one. Deploying caught
   that too: a version whose coding cycle merged pull request #15 was later
   reworked by a `task` run that opened no cycle at all, so reading `current`
   made merged code look unmerged. A merge is a permanent fact about the
   repository; a later run cannot take it back.

10. **Two sections were reading the wrong thing about the run, and both said
    nothing was happening when something was.**

    **The "streaming" chip is the AGENT's state, not the build's.** It read
    `isLedgerLive`, and a run stays `in_progress` through everything that
    happens after its agent stops — the merge, the component builds, the
    deployment — so the chip kept promising a live stream long after there was
    nothing left to stream. `isAgentStreaming` asks the only question the chip
    is for: does the newest, non-terminal run have a build cycle still open?

    **Build logs must ask about the cycle that MERGED.** The section was handed
    `cycles.at(-1)` — the newest cycle of the newest delivery run — and
    `list-cycle-builds` answers empty for any cycle without a merge SHA ("a
    cycle whose pull request has not merged has nothing to have built"). The
    newest cycle is routinely a validation cycle or a retry that never merged,
    and on the local stack the merge was one run further back still, so the
    section sat on "No component builds were produced for this version"
    permanently. `mergedCycle` walks the version's runs newest-first for the
    newest cycle carrying a SHA — the same scope `isDeployable` needs, and for
    the same reason.

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
