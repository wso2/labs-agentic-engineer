# ADR-0013: The Builds page is one version's run story, and the run state is the only liveness

- **Status:** Accepted; decisions 3, 4b, 6 and 9 superseded in part by
  [ADR-0014](./ADR-0014-build-session-spine.md), which turns the run card into
  one rail of staged sections
- **Date:** 2026-07-27 (issue-driven execution,
  [#286](https://github.com/wso2/labs-agentic-engineer/issues/286) under the
  wayfinder map [#272](https://github.com/wso2/labs-agentic-engineer/issues/272))
- **Context:** the backend stopped executing a version task by task and started
  executing it as **one supervised run over one GitHub milestone**
  ([`docs/decisions/ADR-0011`](../../../../docs/decisions/ADR-0011-milestone-is-the-unit-of-execution.md)).
  The console's Builds page was a build-summary card over a flat task list whose
  chips came from a ten-value derived-status algebra, and whose poll-stop asked
  "is any task still active?". That algebra is gone; `derivedStatus` now says
  only whether a GitHub issue is open or closed. Everything the old page
  animated, it can no longer honestly know.

## Decision

**The Builds page shows ONE version's story, latest by default.** Navigating
there while a run is live lands directly on that run with its feed open — there
is no ledger list in between.

1. **The version ledger lives on the Builds page, as that page's own version
   picker.** A build is a version is a tag is a milestone, and choosing which
   version to look at is a control on the surface that shows one — not on the
   overview, whose stage cards are a read-only summary that link onward and
   nothing more. The picker writes `?tag=v<N>`; an unknown tag falls back to
   newest rather than erroring.

2. **The run state is the page's single liveness driver.** `useBuildRuns` polls
   at 5s while the newest run is non-terminal and stops when it settles; the
   GitHub-backed issue list is handed that same flag and polls only while it is
   true, plus **one** fetch on the live→settled edge (the merge that closes the
   last issue can land in the same instant the run turns terminal). Nothing
   polls on a task's status any more.

3. **Loop position renders from the cycle timeline, never from a stored phase.**
   *(Superseded in part by ADR-0014: the timeline is now a rail of staged
   sections, and a cycle's learned facts are attached to the stage that learned
   them rather than shown as a row of em-dashes. The rule this clause exists for
   — position is read from the cycles, never from a stored phase — is unchanged,
   and so are the budget counters and the terminal-reason sentences.)*
   One row per dispatch, oldest first, each showing its kind, its per-cycle
   re-dispatch count, and the branch / PR / merge SHA the platform **learned
   from webhooks** — an empty column is the fact "that cycle's agent has not got
   there yet", rendered as an em-dash rather than hidden. Beside it, the run's
   budget counters, with a spent one in red: it is usually the counter that
   explains the terminal reason printed above it. Terminal reasons render as
   sentences, because each names exactly one failure class.

4. **Cancel is prominent on a waiting run.** `waiting` is written only when a
   run actually parks, and cancel is the only expiry its unbounded wait has — so
   a parked run gets a filled warning button and a notice saying that cancelling
   abandons the increment. On a running run cancel is still there, just not
   shouted. On a terminal run, and while `planning`, it is absent: cancel is a
   signal to the supervisor, and during the plan window there is no supervisor
   yet to receive it — the button would return 202 and do nothing. A 503 says
   *nothing was cancelled*.

4b. **One hold notice on the run card answers "why is nothing moving", and it
   distinguishes three different answers.** *(Superseded in part by ADR-0014:
   the three-way split stands, but the two GATE answers moved onto the
   provisioning stage, which says it per connection. The notice keeps the holds
   that name no connection.)* `planning` is the platform writing
   the milestone (gates minted, issues planned in) — bounded work in progress,
   shown in the info tone with a spinner and nothing asked of the user. A
   `waiting` run with an open gate is held on a human. A `waiting` run with an
   empty working set is the loop declining to call a version delivered that was
   never planned. Each has a different thing to do about it, so each gets its
   own words; the notice is a soft-tinted panel in the tone's own colour
   (StatusChip's vocabulary), never a filled `Alert` — two of those stacked
   turned an ordinary build into a page that read as an incident.

5. **Issue rows carry durable facts only** — the GitHub state (Open / Done) and
   the issue link. They are **not clickable**: the run's story is the timeline
   and the feed above them, and the issue itself lives on GitHub. Mid-run
   liveness never comes from a row. (Agent replies on row expand are a later
   enhancement: lazy on-expand fetch, gracefully polled, never realtime.)

6. **A gate is part of the run's hold notice, not a row.** *(Superseded by
   ADR-0014 decisions 2, 3 and 10: a gate is now the first STAGE on the run's
   rail — still not a competing warning, but named per connection with who is
   acting on it, and listed in the issue register as part of the version's
   record. A resolved gate no longer disappears: it is how the version came to
   exist.)* While any
   `provision` gate issue in the milestone is open the run dispatches nothing, so
   the gate is the *reason nothing is moving*, not one item among many — which
   makes it the run card's business (decision 4b), not the issue list's. The
   notice names each held dependency and deep-links the spec view's connection
   drawer via `?connections=open`. A resolved gate disappears entirely — it
   holds nothing and was never work. The issue list deliberately says nothing
   about gates: announcing the hold in both places put two warnings on one page
   competing to explain one fact.

7. **Bare human issues sit in a separate Ledger section.** They joined the
   milestone but are never worked and never stall settle, so listing them
   alongside agent work would misrepresent both.

8. **Validation renders on the deployment surface, because the deployment is
   what is being validated.** The Validation page keeps its role — the authored
   criteria joined with the runner's committed report — but re-keys to the run's
   **verdict**, read from `/builds/{tag}/runs`; there is no validation endpoint.
   Its live log is the run feed filtered to the validation cycle. The deployments
   surface names the outcome ("validation passed") instead of naming the artifact,
   because the verdict is finally a fact the platform holds. The validation issue
   never appears in the issue list.
   *Amended by ADR-0016: the verdict alone is not what either surface renders (§1),
   and the entry chip this decision put in the Dev environment card is gone — the
   rail's Validation stage carries the outcome, with the counts, a sentence and its
   own link to the report.*

9. **The run feed is one SSE stream, rendered as an accordion by cycle.**
   *(The stream contract is unchanged. Per ADR-0014 the log renders inside the
   Coding agent STAGE of its build session rather than as the section's body.)* One
   section per cycle record (upserted by id), the newest expanded; every line
   attributed to its cycle, and stamped `subagent` only when a Task subagent
   produced it — an unstamped line *is* the main agent, which is the contract's
   own rule and keeps the feed quiet. Only a terminal run settles the stream, so
   "ended" is a fact about the run, not about the connection: an EOF without
   `[DONE]` is a dropped connection to reattach, and reconnects are idempotent
   (cycles upsert by id, lines dedup by `cycleId:seq`).

9b. **A run feed is one execution; the VERSION's own feed is a second stream, and
   the page holds one of them at a time.** A version is worked by several runs —
   the spec build, then whatever repaired or re-judged it — so the run card's feed
   is a chapter, not the story. `stream-build-progress`
   (`/builds/{tag}/progress`) is the story: the same frames plus the run each
   cycle belongs to, in chronological run order, rendered as one timeline with the
   run KIND as the section marker (`runKindLabel`, the same words the card and the
   history rows use). Cycle numbering stays RUN-relative, exactly as the wire
   carries it, so a box is named the same wherever it is read and the heading above
   is what tells two "Cycle 1"s apart. It sits behind a disclosure on the Builds
   page: each connection costs the server a ticking derive plus a pod-log read per
   cycle, and a page that opened both unasked would pay twice to show the same
   newest cycle twice — so the feed is UNMOUNTED until the reader asks, which is
   what closes the connection. Its "ended" means something weaker than a run's: the
   server settles it whenever **no run on the milestone is live**, which is a
   resting state and not a verdict on the version, so the frame carries `reason:
   no_live_run` rather than a state and the feed reattaches when the 5s run-list
   poll shows a new run.

## Consequences

- `ProjectStatus.build.tasks` is removed from the contract rather than left
  reading zero, and the overview's build stage is count-free ("building", not
  "0/0 done"). Counts live where the issue list is already paid for.
- The overview stops calling `list-tasks` altogether — it was an unconditional
  GitHub-backed poll on an idle page. The component cards lose their
  task-derived status chip with it: an issue no longer names a component, so the
  roll-up had no input left. What is running is the deployments board's job.
- `deploy.validationIssue` / `deploy.validationUrl` leave the contract; the
  validation PR link comes from the validation **cycle record**.
- The per-issue page (`builds/$issueNumber`) survives but is reachable by URL
  only. After the flip an agent's log belongs to a **cycle**, not an issue; what
  still has a per-issue log is an issue the *platform* ran something for — a
  provisioning gate.
- Mid-run gate resolution through the connection drawer is **not yet closed**:
  the drawer's Continue re-submits the build, which the spec-run mutex 409s
  while a run is live. The deep link takes the user to the right place and the
  error is legible, but a first-class mid-run resolve is follow-up work.
- Tests follow the house convention — `vi.mock` at the hook boundary, jsdom via
  a per-file pragma, fixtures as local factories typed off the generated
  contract. MSW stays the **dev-time** worker; the new endpoints (including the
  progress SSE) have mock handlers so every state above is reachable with
  `VITE_API_MODE=mock`.
