# #919: When a fixed issue closes, and how its thread hears progress

Research for ticket [#919](https://github.com/wso2/labs-agentic-engineer/issues/919), a child of map
[#913](https://github.com/wso2/labs-agentic-engineer/issues/913) (a generic sub-agent per area). The
issue sub-agent's lifecycle in that map is: hand to the coding agent, then PR, merge, deploy and close.
The settled rule in the map is "a closed issue's thread posts a short summary to the main chat, then is
removed".

Sources:

- Code at `upstream/main` (`da961409f`), unless a path says
  **[branch]**. **[branch]** means `claude/issue-aware-chat-agent-6983fc` (PR #897's branch).
- GitHub's own documentation, linked where it is used.

---

## Today, in one paragraph

The coding agent writes `Resolves #N` for each bug it finished ([`skills/aep/SKILL.md:303-317`]).
The platform squash-merges a PR only when that PR names an **armed** issue of the milestone with a
closing keyword, or uses `Advances`/`Validates` on the build issue or validation task
([`eventcore/policy.go:128-159`], [`eventcore/resolves.go:36`]). GitHub then closes the issue **at
merge**. The run's cycle then builds (`run/stage_build.go:54` `awaitBuilds`) and deploys
(`run/stage_deploy.go:78` `reconcileVersion`) to the project's write target, the root of its
deployment pipeline (ADR-0039), inside the same `runCycle` (`run/stage_boundary.go:602-646`).

So **an issue closes before its fix is deployed.** On the branch, `issues.closed` removes the issue's
thread ([branch] `app/app.go:1417`, `app/issue_agent_adapters.go:64-98`,
`spec/issue_threads.go:49`). The thread is therefore gone at merge, and it never hears "built" or
"deployed".

---

## Q1: Can a user-filed issue close after deploy instead of at merge?

### Facts

- **GitHub's keywords.** GitHub closes an issue on these nine keywords: `close/closes/closed`,
  `fix/fixes/fixed`, `resolve/resolves/resolved`. It closes the issue only when the PR "targets the
  repository's default branch". Keywords in a commit message also close the issue when the commit
  reaches the default branch. A manually linked PR does **not** close anything
  ([GitHub docs: linking a PR to an issue](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue)).
  The platform matches exactly that set, so "what we count" and "what GitHub closes" cannot diverge
  (`eventcore/resolves.go:25-36`).
- **Platform-owned closes already exist.** The words `Validates #N` and `Advances #N` are
  deliberately not GitHub keywords. The platform uses them where it must own an issue's lifecycle
  (`eventcore/resolves.go:50-94`). The validation task is closed by the platform with a comment
  (`run/ports.go:256-270` `CloseValidationIssue`). The reasoning there is "a closing keyword would put
  two owners on one issue" (`eventcore/policy.go:83-87`).
- **The merge gate.** Today, a closing keyword on an armed issue is the only way a coding PR that
  finishes a bug gets merged (`eventcore/policy.go:147-158`). A non-closing word for a bug would be
  declined (`policy.go:95-102`), unless the policy learns that word.
- **The working-set rule.** A run works open issues that carry `aep` and a kind label
  (`delivery/labels.go:231-241` `InDevWorkingSet`; the task-run variant follows it). If a merged issue
  stayed open and armed, it would still be in the working set. The boundary polls only **after**
  `runCycle` returns, and `runCycle` includes the deploy (`run/stage_boundary.go:405-470`).
  - On the green path, a close placed after `reconcileVersion` is in time.
  - On a failed deploy, the boundary mints a deploy-fix issue (`stage_boundary.go:1123`). The
    original issue, still open and armed, would be worked a second time.
- **Who records which issues a cycle finished.** `RunCycle.Resolves` (`delivery/run_cycle.go:117-124`)
  is filled from the policy's matched set at PR-open time (`eventcore/cycles.go:83-99`). After a
  merge, nothing else can tell which issues a cycle finished.
- **Where "the version serves it" becomes true.** `reconcileVersion` returns `cycleGreen` once every
  wave is ready (`run/stage_deploy.go:78-193`). The deploy environment's name is known on failure
  (`stage_deploy.go:506,524`).
- **How to close an issue.** REST `PATCH /issues/{n}` takes `state=closed` with
  `state_reason=completed|not_planned|duplicate`
  ([GitHub REST: update an issue](https://docs.github.com/en/rest/issues/issues#update-an-issue)).

### Options

**A. Keep the close at merge (status quo) and add a "deployed" note.**
- The platform comments, and optionally labels, the already-closed issue after `reconcileVersion`
  is green.
- Cost: none on the delivery side.
- But: the thread is removed at merge, so it never hears the deploy. To keep it, the thread's
  removal would have to key on a platform "delivered" event instead of `issues.closed`. That
  contradicts the map's settled "closed → summary → removed".

**B. A platform-owned close for issues that have a thread or a user source.**

1. The agent writes a non-closing word for the issue, for example `Delivers #N`, alongside
   `Advances` and `Validates`.
2. `decideAutoMerge` admits it on armed work. It is recorded on the cycle like `Resolves`.
3. At `pull_request.closed`, the platform:
   - unarms the issue, as `keepUnverifiedIssuesOpen` already does (`eventcore/unverified.go:48`);
   - stamps a marker such as `aep:merged`, so the issue leaves the working set;
   - comments "merged in #PR, deploying".
4. In `runCycle`, after `reconcileVersion` returns green, an activity closes the cycle's delivered
   issues as `completed` with a comment like "Fixed in #PR (sha), now serving in `<env>`".
5. A failed deploy leaves the issue open, marked and unarmed. The deploy-fix issue carries the
   repair, and a later green reconcile closes both.

Notes on B:
- Cost: one keyword, one policy branch, one activity, a skill line, and a sweep for
  merged-but-never-closed issues when a run fails or is cancelled.
- It is the same pattern the validation task already uses.
- It also removes the webhook-order race that `unverified.go:31-34` admits it does not handle (see Q3).

**C. Close at run settle instead of per cycle.**
- The platform closes every merged-and-marked issue when the run settles `succeeded`. For a task run
  that is the point when the version is serving.
- It is simpler than B, because there is only one close site.
- But a long multi-cycle run delays the close of an issue fixed early.

### Recommendation: B

- Scope: issues of kind `bug` whose source is the user, plus incidents. An absent source reads as
  `src/user` (`delivery/labels.go:87-105`).
- Platform-minted defects (`src/build`, `src/deploy`, `src/validation`) and the build issue keep
  GitHub's close at merge, because nothing needs to hear their deploy.
- The closing comment names the PR, the merge SHA and the environment. That gives the thread its last
  progress line, and gives the main-chat summary its content.
- One cost: a non-closing keyword does not appear in GitHub's "Development" sidebar. The `#N` mention
  in the PR body still puts a cross-reference on the issue's timeline, and the merge comment names the
  PR.

---

## Q2: Which signals could feed run, PR, merge and deploy events into the issue's thread, and what is missing?

### Existing signals

| Signal | Where | Per issue? |
|---|---|---|
| Issue to milestone to live run | `eventcore/ports.go:37-40` `LiveRunForMilestone`; adoption puts a bare issue in the deployed milestone (`eventcore/adopt.go`, upstream; **[branch]** `adopt.go:120-176`) | Derivable. An armed, open issue in a milestone that has a live run **is** that run's work from its next boundary (**[branch]** `adopt.go:163-174`). |
| Cycle record: PR number, URL, branch, draft, merge SHA, `Resolves`, `Advances`, merge verdict | `delivery/run_cycle.go:83-134`, written by `eventcore/cycles.go:42-150` | `Resolves` is per issue, but only once the PR is open. jsonb, no index (`run_cycle.go:124`). |
| `GET /builds/{tag}/runs` (runs with cycles) | `runread/handlers.go:54` | Per version, so the issue must be filtered from `resolves`. |
| Run progress SSE (`cycle` and `event` frames) | `runread/handlers.go:115`; `delivery/README.md:692-707` | Per run. Agent tool feed, not issue-scoped. |
| Version progress SSE | `runread/handlers.go:130`; `README.md:708-727` | Per version. |
| Run phase (planning, building, deploying, settling) | `run/stage_boundary.go:608,624` (`l.st.Phase`), query `QueryRunStatus` | Per run. Live only, through a Temporal query. |
| Run signals: `run-pr-merged`, `run-build-terminal`, `run-conflict` | `delivery/run_signals.go:30-45`; sent from `eventcore/events.go:328` | Wake-ups only ("a signal is a wake-up, never evidence", `run/stage_build.go:77`). |
| Webhooks: `pull_request.*`, `issues.*` | `eventcore/events.go:112-119`; **[branch]** `app/app.go:1411-1418` | `issues.closed` and `issues.reopened` are per issue. PR events carry the body, so the `Resolves` refs. |
| The agent's **status line**: one comment per state change on the issue it works | `skills/aep/SKILL.md:272-295` | **Per issue**, written by the coding agent. It is already the console's issue status (`README.md:757-761` `derivedStatus` plus the newest comment). aep-api registers no `issue_comment` handler; AE Studio does subscribe to it (`clients/aestudiotools/hooks.go:34-37`). |
| Task-log SSE `GET /tasks/{n}/log` | `execution/handlers.go:31`; `execution/doc.go` | Per issue, but **legacy**: it only surfaces provisioning-gate execution rows, not milestone cycles. Not a fit. |
| The branch's thread-outcome bridge | **[branch]** `spec/README.md` "The Issues outcome reaches the main chat" (`agent_turns.outcome` and `branchNotes`) | A pull model: notes are injected into the *next* turn. There is no platform-authored message into a thread. |

### What is missing

1. **An issue-to-run/cycle join before the PR exists.** It can be derived (issue's milestone, then
   `LiveRunForMilestone`) but is not exposed. From PR open onwards,
   `run_cycles.resolves @> '[N]'` answers it, but no read endpoint is keyed by issue.
2. **A platform event for "deployed".** `reconcileVersion` green is a workflow-internal return. No
   signal, webhook or row write says "issue N's fix is serving". (Q1's option B creates exactly this
   moment: the close comment.)
3. **A push path into a thread.** Every thread message today is a turn: a user message and an agent
   reply. Neither the platform nor a run can append "PR #12 merged" to `issue-<n>`. The branch's
   `branchNotes` only reaches the main agent on its next turn.
4. **A single per-issue progress read.** Its pieces exist: status line, cycle, run phase, and the
   close comment.

### Options

**A. Pull at turn time.**
- The area block assembled per turn (map #913) reads issue N's state: live run, phase, cycle PR,
  merge, close comment. It reads these from one new per-issue read-model call.
- No push. The thread learns progress when the user next speaks, or when the console polls.

**B. Platform notes into the thread.**
- A small "thread notice" write: a non-turn, platform-authored entry on `issue-<n>`. The console
  renders it, and the next turn's prompt receives it.
- It is fed by:
  - the PR webhook (opened or merged; refs from the body);
  - the run boundary (deploy green or failed, from a new activity);
  - the close.
- Idempotent per (issue, event kind, cycle).

**C. Mirror the GitHub issue comments.**
- An `issue_comment` handler copies the agent's status lines and the platform's merge, deploy and
  close comments into the thread.
- The agent already writes them, so there is one source of truth.
- But the thread then depends on the agent's comment discipline and on webhook order.

### Recommendation: B, with A as its read side

- Add one per-issue read: `GET /issues/{n}/progress`, or a field on the issue view. It is derived
  from the milestone's live run plus `run_cycles.resolves`.
- Add platform notices at four points:
  1. **picked up**: the boundary dispatches a cycle whose working set contains N;
  2. **PR opened**: from `noteCycleMergeDecision` when `Matched` contains N;
  3. **merged**: from `OnPullRequestClosed`;
  4. **deployed and closed**: from Q1's close activity.
- Each notice is written to GitHub (comment) and to the thread (notice). Then the thread still reads
  correctly if a webhook is lost, and GitHub stays the record.
- Do not stream the agent's tool feed into the thread. The run's own progress view already carries it.

---

## Q3: The `Confidence: high` gap

### Facts

- `unverifiedMerge` treats a merged PR as unverified unless **every** `Confidence:` line in the body
  says `high`. A body with none counts as unverified (`eventcore/policy.go:27-42`).
- On `pull_request.closed` that verdict reopens every **incident**-labelled issue the body resolves.
  It unarms the issue and leaves an "unverified fix" comment (`eventcore/events.go:319-322`,
  `eventcore/unverified.go:35-59`). The issue then reads as `unverified_fix`
  (`sourcecontrol/issue_recurrence.go:50-53,59`).
- **Nothing tells the coding agent about this convention.** `git grep -i "confidence:"` finds only
  `policy.go` and its test. Neither `skills/aep/SKILL.md` nor any runner text mentions `Confidence`,
  `incident` or `not_planned`. The skill's kind table has no incident row (`SKILL.md:91-100`). Yet
  the SRE runbook promises both behaviours ("Low-confidence coding-agent result" and "`not_planned`:
  the coding agent closes the issue",
  `docs/developer-guide/sre-handoff-runbook.md:231-243`). So **every incident fix ends as
  `unverified_fix`**.
- The declaration covers the whole PR body, not one issue. One PR that resolves two incidents cannot
  give them different confidence levels.
- The race is known and unhandled. GitHub does not order deliveries, so a keyword close that lands
  after the reopen leaves the issue closed as completed (`unverified.go:31-34`).
- Confidence is meant to be a declaration, "never a classification read from an issue's prose"
  (`policy.go:27-28`; introduced in `492da416c`).
- Skill constraint: PR-body mechanics must stay in the overlay-owned sections of `skills/aep/SKILL.md`,
  which is how `Resolves #` lines are placed today
  (`runners/remote-worker/src/lib/workflow_skill.test.ts:481-487,605`).
- Repo rule: skill edits go through the `writing-skills` loop (AGENTS.md).

### Options

**A. Teach the line as it is.**
- Add to "The record" (`SKILL.md:297-317`): for an issue labelled `incident`, add `Confidence: high`
  only when the fix was reproduced and verified, otherwise `Confidence: low`.
- Smallest change. Keeps the PR-wide granularity and the ordering race.

**B. Per-issue confidence, decided where the close is owned.**
- The agent writes the confidence next to each reference, for example `Delivers #N (confidence: high)`
  or a `Confidence #N: high` line.
- Combined with Q1's option B, the platform makes the decision at deploy-green: close as `completed`
  when the confidence is high, otherwise leave the issue open as `unverified_fix`.
- No reopen, no race, and the decision is per issue.

**C. Derive confidence from evidence instead of a declaration.**
- For example: the agent attached a reproduction that fails before the fix and passes after, or the
  incident does not recur within a window after the deploy.
- It is more honest, but it contradicts the declared design (`policy.go:27-28`) and needs new evidence
  plumbing.

### Recommendation

- **Now: A.** It is a skill-only fix that makes the existing gate reachable. Ship it through
  `writing-skills`. It should also teach the `not_planned` close the runbook already promises.
- **Then: B, together with Q1's platform-owned close.**
  - Same keyword, same activity, so both can land in one design.
  - It retires the reopen-after-merge path in `unverified.go` and its race.
- C stays out of scope unless incident verification becomes its own map item.

---

## Constraints for the spec (map #913)

- If the close stays at merge, the issue thread must not be removed at merge. Thread removal today
  fires on any `issues.closed` (**[branch]** `app/app.go:1417`).
  - Moving the close after deploy (Q1 B) keeps "closed, then summary, then removed" true **and**
    lets the thread hear the deploy first.
- An unverified incident goes closed (at merge), then reopened (`unverified.go:51`). On the branch
  that removes the thread twice and starts a fresh one (`issue_threads.go:27-38`). Q3 B avoids this.
- The issue sub-agent's lifecycle hooks map onto the four notices in Q2's recommendation. The
  "closed" step is the platform's deploy-green close, not GitHub's merge-time close.

[`skills/aep/SKILL.md:303-317`]: ../skills/aep/SKILL.md
[`eventcore/policy.go:128-159`]: ../services/aep-api/internal/delivery/eventcore/policy.go
[`eventcore/resolves.go:36`]: ../services/aep-api/internal/delivery/eventcore/resolves.go
