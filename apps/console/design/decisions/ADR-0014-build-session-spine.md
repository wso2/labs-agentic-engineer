# ADR-0014: A run is ONE numbered flow — provisioning, then every build session's stages

- **Status:** Historical as of
  [ADR-0021](./ADR-0021-builds-is-a-version-ledger.md) — nothing renders
  the numbered rail this ADR designed. §9's fetch-on-demand policy survives as
  the build page's log-section behaviour. Previously: Accepted
- **Date:** 2026-07-28
- **Supersedes in part:** [ADR-0013](./ADR-0013-version-run-surface.md)
  decisions 3, 4b, 6 and 9.
- **Context:** ADR-0013's run card drew a cycle as a facts row — `branch`,
  `PR #4`, `merge 0f7a4786` as three grey monospace values — followed by a build
  list. Three structural problems fell out of that shape:

  1. **No time axis inside a cycle.** Everything between "the agent pushed" and
     "builds exist" was drawn nowhere, so the handover from agent to platform was
     invisible: the pull request read as a third fact, not as an event.
  2. **The platform is the actor for half a cycle and had no narration
     channel.** `decideAutoMerge`'s verdict was a debug log; the fan-out's
     unmatched paths a warn log. Neither reached the user.
  3. **Nothing existed until after it happened.** The build block returned
     `null` before a merge SHA, so builds appeared from nowhere — and four
     genuine stalls (a draft pull request, a declined merge, a refused merge, a
     twice-red component) looked identical to a session still thinking.

## Decision

**A run renders as ONE NUMBERED FLOW, and every applicable stage exists from the
moment the run does.** A stage that has not been reached says what it waits for;
a stage that went wrong says so in the platform's own recorded words.

```
1  Provisioning     platform     the connections this version needs
2  Coding agent     runner       the runner Job, up to the pull request it opens
3  Pull request     agent        the handover from agent to platform
4  Merge            platform     the auto-merge policy's decision
5  Builds           platform     the fan-out one merge causes
6  Deployment       cluster      a green build deploys itself
   ── build session 2 · fix ──
7  Coding agent     …            a re-entry, counting on through the same flow
```

1. **The console calls a cycle a "build session".** Copy only — the model, the
   wire, the routes and the budgets stay `cycle`. The bare word *session* is
   never used for one: that is the spec-collaboration Room's sense. One glossary
   line carries the mapping.

2. **Provisioning is a RUN-LEVEL stage, first on the rail**, and only when the
   milestone holds gates. This is what the platform's own dispatch predicate
   says: `OpenProvision == 0 && OpenDevWork() > 0`, so an open gate holds
   *every* session, not the first one. Nesting it inside session 1 would have no
   home for a gate minted mid-run and would force a fix session to either repeat
   the stage or pretend the gate was absent.

3. **The gate hold moves out of the run's notice and into that stage.** "Why is
   nothing moving" is best answered where movement stopped, and per gate rather
   than per run: each connection carries who is acting on it — the platform
   standing it up, a failed provisioning run, or the one case only a human can
   release. `RunHoldNotice` keeps exactly the holds that name no connection:
   `planning`, an empty milestone, and the unbounded park between sessions.
   ADR-0013 decision 4b's three-way split survives intact; only its *location*
   changed, and only for the gate cases.

4. **Each build session is five stages with a NAMED ACTOR** — coding agent
   (runner), pull request (agent), merge (platform), builds (platform),
   deployment (cluster). Naming the actor is what stops a platform wait reading
   as a hung agent. The derivation is pure (`lib/sessionSpine.ts`), so the
   vocabulary is unit-tested without rendering.

4b. **One rail, one ordering, numbered straight through.** Every stage on the
   run carries its step number, and a later build session keeps counting (7, 8,
   …) under a boundary label naming it. There is no nesting: an earlier shape
   made each session a collapsible card holding its own five-dot sub-spine,
   which put two orderings on one screen and asked the reader to hold both. A
   run with ONE session shows no boundary at all — it is simply the flow. The
   loop stays legible because a re-entry is labelled, not because it is boxed;
   pretending a fix session is a fresh start would hide the very thing a reader
   is trying to understand. Step numbers are assigned from the session COUNT
   alone (`SESSION_STAGE_COUNT`, asserted against `sessionStages`), so the rail
   numbers itself before any cluster-derived fact has arrived.

5. **The agent log lives inside the Coding agent stage** — taller while the
   agent runs, shorter once it exits, never removed. ADR-0013 decision 9's
   stream contract is unchanged: one SSE stream for the run, cycles upserted by
   id, lines deduped by `cycleId:seq`, only a terminal run settling it. What
   changed is that the log is now one stage's evidence rather than the whole
   section's body.

6. **Issues appear on the provisioning and coding-agent stages ONLY.** Past the
   point where the set stops changing, the same chips again read as duplication
   rather than progression — and the merge's matched set *is* the coding stage's
   set. Each chip keeps ADR-0013 decision 5's rule: durable facts only (Open /
   Done) and a link, never mid-run liveness on an issue row. Attribution is
   carried by the group's caption instead.

7. **A build session's issue set comes from the recorded `resolves`** — the merge
   policy's matched set, persisted on the cycle row where `decideAutoMerge`
   already computes it. Before a pull request exists the console falls back to
   the milestone's open agent work and **says so in the caption**; a settled
   session that recorded no matched set claims nothing rather than guessing from
   what is open now, which would attribute a later session's work to an earlier
   one.

8. **Deployment is a statement plus navigation, with no cluster read.** A
   `Deployment` carries no commit, image or revision, so no rollout can be
   attributed to the merge that caused it — chips here would show a later
   session's rollout under an earlier session. What the stage owns is the
   consequence (components carry auto-deploy, so a green build deploys itself)
   and the way to the deployments board, which does know what is running. This
   replaces the "cycle verdict" idea outright.

9. **Nothing collapses; the agent LOG is the only thing still fetched on
   demand.** Every stage is on screen, so every stage's facts are fetched: one
   cluster query per *merged* session, which stops polling the moment its builds
   complete — a settled session costs one read and never asks again. That is the
   price of the flat rail, and it buys away a whole class of confusion: with no
   unread stage there is no "not read" state, so `StageState` has no member for
   it and `buildsStage` cannot claim a merge produced nothing.

   The run feed is different, because attaching replays *every* session's
   history. So it opens automatically while the run is LIVE — it is what the
   user came to watch, and the log must not vanish the moment the agent opens
   its pull request and the run moves on — and on a settled run it waits behind
   one "Show agent log" button. One flag per run, because there is one stream
   per run. ADR-0013 decision 9's property (a finished version opens no
   connection until asked) therefore survives the loss of the accordion.

10. **The flat `Issues` section stays** as the version's register. The rail is
    narrative; the register is the complete, sortable, linkable list — and the
    only home for work no session has claimed yet (`on_hold`, or awaiting
    dispatch). ADR-0013 decisions 5 and 7 stand; decision 6's "the issue list
    says nothing about gates" is now the other way round: the register lists
    gates as record, and the *stage* is where the hold is explained.

## Consequences

- `RunCycleView` gains `resolves[]`, `prDraft`, `prUrl`, `mergeVerdict`
  (`declined | refused`) and `mergeReason`. Every one is a fact the event plane
  already had in hand and discarded; none needs a new read. The verdict is
  overwritten on every decision, so a declined pull request that later merges
  does not keep a stale verdict — and a merge SHA outranks it regardless.
- `OnPullRequest` now records the cycle's pull request **before** returning on a
  draft. Nothing else about draft handling changed (the policy still never runs
  on one, and `ready_for_review` still brings it back), but a session parked
  behind a draft is no longer indistinguishable from one whose agent never
  opened a pull request.
- There is deliberately no field for the recovery issue a red build or a
  conflict mints: it surfaces as the NEXT session's `resolves`, and by the time
  `mintFixIssue` runs the cycle is closed — recording it there would have to
  break the repository's "a closed cycle is never rewritten" fence.
- `runHold` loses its gate branches and its `gates` field; `holdFromGates` is
  gone, replaced by `lib/provisioning.ts`. `gateDrive` is unchanged and is still
  the only honest source for who is driving a gate.
- `cycleLabel` becomes `buildSessionLabel`; `CycleSections` becomes `RunSpine`,
  which owns the whole rail — the provisioning stage and every session's stages
  — so step numbers have one author. `ProvisioningSection` becomes
  `ProvisioningGates`: the gate rows and the way out, hung under a `StageRow`
  that `RunSpine` renders. `CycleBuilds` no longer fetches — the session owns the
  read and hands it down, so the rows and the stage above them cannot disagree.
- `StageStrip` and `sessionSummary` are gone with the collapse they summarised;
  a summary of a sequence that is always on screen summarises nothing. The
  per-session re-dispatch budget moves from a chip in the session header onto
  the Coding agent stage's `fact`, which is the stage it is a fact about.
- The Deployment stage is the first thing on this page to link onward by router
  navigation rather than by an external issue link.
- A stage's `fact` can be a LINK (`factHref`), which is how the Pull request
  stage's `#N` reaches the pull request: the reference a reader wants to open is
  the string already on screen, so it needs no row of its own. The href is
  always a URL the platform recorded (`prUrl`, learned from `pull_request`'s
  `html_url`) — never one the console assembles from a repo URL and a number,
  which is what the Validation page used to do and why its link 404'd whenever
  the project's clone URL carried a `.git` suffix. No recorded URL, no link: the
  fact still renders, as text, so a cycle from before the platform kept the URL
  reads exactly as it did.
