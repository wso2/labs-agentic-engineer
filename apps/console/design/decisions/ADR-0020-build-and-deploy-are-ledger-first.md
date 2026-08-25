# ADR-0020: Build and Deploy are ledger-first — a version table, an environment board, and the run story one click down

- **Status:** Accepted
- **Date:** 2026-08-24 (grilling of the Build-and-Deploy design handoff,
  [#609](https://github.com/wso2/labs-agentic-engineer/issues/609))
- **Supersedes:** [ADR-0015](./ADR-0015-now-first-builds-page.md) in full. See
  *What happens to ADR-0013 and ADR-0014* below — ADR-0015's amendments to
  those two do **not** silently revert.
- **Reverses:** the one-story Deployments rail decided on
  [#401](https://github.com/wso2/labs-agentic-engineer/issues/401), which was an
  issue-level decision and never had an ADR.

## Context

ADR-0015 made Builds now-first: the page lands on the newest version's live run,
with no list in between, because "what is happening right now, and how much is
left" is the question a reader arrives with. `BuildsPage.tsx` states the
consequence plainly — *"There is no ledger list in between."*

That is true of a reader who arrives **during** a run. It is not true of the
reader who arrives between them, which is the common case: a project builds for
twenty minutes and is then read for days. That reader asks *what has this project
built, and how did each one go?* — and today answers it by selecting a version
from a dropdown, reading it, remembering it, and selecting the next one.

Deployments has the mirror-image problem for the mirror-image reason. #401 argued
Development and Production are consecutive stages of one path rather than parallel
places to diff, and rendered them as one top-to-bottom rail. That reads well for
*promote this version* and badly for *what is running where*, because a rail
answers the second question only by being read end to end. It also keeps no
record: a deployment that failed, or was superseded, leaves no trace in the
console at all.

Both pages, in other words, optimised the narrated single item and lost the
scannable set.

## Decisions

1. **Builds is a version ledger — one row per version, newest first.** Version ·
   Milestone · Status · Tasks · Duration · Started, dense, every row clickable. A
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
   derived, in two strengths — survives unchanged and keeps governing the task
   rows' copy.*

4. **Provisioning gates are task rows, not a stage.** A connection to configure
   and a feature to write are peers in one list, each with its own way out
   (*Configure in Resources* on a gate, *Log* on a running task). This is what
   replaces the rail's provisioning section and ADR-0015 §6's separate treatment
   of holds: a hold is a row that needs you, rendered like every other row that
   needs you.

5. **Deployments is a two-column environment board.** Development and Production
   side by side, each with its own state, and an all-environments table of every
   deployment beneath them. The promotion path is not lost — it lives inside the
   Development card, attached to the thing being promoted, with the same
   `PromoteDialog` and connection-values flow.
   *Reverses #401.*

6. **A deployment is an addressable thing.** `/projects/$projectName/deployments/
   $deploymentId` renders one deployment: its commit, its duration, its
   components, and each component's runtime log. Before this, no deployment could
   be opened and none but the current one was recorded.

7. **`/builds/$tag` is a version; `/tasks/$issueNumber` is a task.** TanStack
   cannot carry two dynamic siblings, and `/builds/$issueNumber` was the task
   page. `builds.$issueNumber.tsx` is deleted, `builds.$tag.tsx` redirects a
   numeric segment to `/tasks/$issueNumber`, and `tasks.$issueNumber.tsx` stops
   redirecting and renders `TaskPage`.
   *Reverses the direction of [#185](https://github.com/wso2/labs-agentic-engineer/issues/185)'s
   redirect. Old `/builds/118` links keep working; the redirect now points the
   other way.*

8. **Runtime logs are a bounded window, not a stream.** The window is a query
   parameter and one GET. Live tailing needs a streaming endpoint and a reconnect
   policy, and buys nothing the `Last 1h` picker doesn't already give — the
   picker *is* a window control. A future tail supersedes this clause, not the
   ADR.

## What happens to ADR-0013 and ADR-0014

ADR-0015 amended ADR-0014 §4b, §9 and §10, and stood on top of ADR-0013's
amendments by ADR-0014. Superseding ADR-0015 does **not** revert those to their
pre-ADR-0015 wording — the flat `Issues` register (ADR-0014 §10) and run-wide
stage numbering (§4b) are not coming back, and this ADR does not reinstate them.

Concretely: **ADR-0013 and ADR-0014 are now historical.** Their surface — the
version-run card and its numbered rail — is not mounted by anything after this
ADR. ADR-0013 §5 (rows are not clickable; the one link a row carries is its
GitHub issue) is the one clause this ADR **does** overturn: ledger rows and
deployment rows are clickable, because they now have somewhere of their own to
go. ADR-0014 §9's fetch-on-demand policy survives as the log sections' behaviour.

## Consequences

- **The stage rail leaves the product.** `RunSpine` and its satellites were
  already unmounted by ADR-0015; after this ADR nothing renders staged
  provisioning → coding → build → deploy anywhere. That is a real loss of a view,
  accepted deliberately: the information it carried is in the task list (gates as
  rows, decision 4) and the two log sections, and the rail's cost was making the
  reader scan six expanded stages to find the one that moved. `RunSpine`,
  `StageRow`, `RunGlanceStrip`, `RunNowPanel`, `RunHoldNotice` and
  `ProvisioningGates` should now be pruned rather than left as reference.
- **The ledger needs data the contract does not carry.** `BuildSummary` gains
  `milestoneTitle`, `commit`, `taskCounts` and `deployedTo`; the note that task
  counts are "deliberately absent, the console renders them from list-tasks"
  held for one version and does not hold for a table of them (it would be one
  fetch per row).
- **Deployments needs endpoints that do not exist.** A project-level deployment
  list and detail, and a component runtime-log read. Today's `Deployment` is
  per-component and carries no version, duration or validation.
- **This ADR ships ahead of its backend.** The frontend lands on the feature
  branch against typed mocks; `1c`/`1d` degrade to a note rather than an error
  card while the backend is in flight (see #609's *States to design*).
- Vocabulary is unchanged in meaning: a dispatch is a **build session**, the
  wrapper is a **run**, and the ledger's rows are **versions**. The new
  situation-naming status strings (`Running · Coding agent`, `Failed · Merge
  conflict`, `Deployed to dev`, `Superseded`) are added to `lexicon.md` in the
  same PR, per its amendment rule.
