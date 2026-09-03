# ADR-0024 — Cancel reaches the planning phase, stops the work, and says so

**Status:** Accepted · **Related:**
[ADR-0018](ADR-0018-planning-is-a-run-phase.md) (planning is a run phase — this
revises two of its statements),
[ADR-0020](ADR-0020-a-run-species-is-a-workflow.md),
[ADR-0023](ADR-0023-external-dependency-values-are-a-deploy-gate.md) ·
**Complemented by** PR #681, which refuses an unknown `resourceType` at the build
click and classifies permanent provision faults — the front line this ADR's
bounded retry now backstops

## Context

ADR-0018 moved planning inside the run workflow: the click admits the row and
starts the supervisor, and the run mints its own gates and plans its own
milestone as `phase = planning`, before the cycle loop. Everything that ADR set
out to fix, it fixed — planning became durable, retryable and visible in history.

It left one thing unstated, and a live build found it.

Cancel in this platform is a **signal**, not a Temporal workflow cancellation,
and deliberately so: the run has to settle its own row and close its own issues
on the ordinary code path, which a cancelled context cannot do. The loop reads
that signal at every cycle boundary. But the planning phase runs *before* the
first boundary, so for its whole duration a run could not see a cancel at all.

That was survivable while planning was two quick activities. It stopped being
survivable because `ProvisionGates` inherited Temporal's **default unbounded
retry policy**, and provisioning has answers that repeating cannot change. A
project's design declared a platform resource whose `resourceType` named a
`ClusterResourceType` nobody had installed. OpenChoreo's `Resource` reported
`ResourceTypeNotFound` immediately and permanently; AEP saw only
`WaitForReleaseChange` timing out after 60s and read it as a blip.

The result was not a failed build. It was a loop:

- 11 attempts over 22 minutes, still going when a human intervened;
- **33 gate issues minted** into one milestone, because the gate mint dedupes
  against *open* gates and the same activity closes the ready ones itself — so
  every attempt found none open and minted three more;
- **six delivered `run-cancel` signals** sitting unread in the workflow's
  channel, the console answering 202 to each;
- ended only by `temporal workflow terminate`, which left the row in `planning`
  with no workflow behind it — a state the reconcile sweep deliberately skips, so
  every later build on that project was refused by the build mutex.

Three defects, one incident: a phase blind to cancel, an unbounded retry on a
permanent answer, and a mint whose idempotence key could not see its own writes.
A fourth, latent, was reachable already and made likely by fixing the second.

## Decision

**1. Every phase has a safe point, including the planning bookend.**

`fillMilestone` no longer blocks on `Future.Get`. Each of its two activities runs
under a `workflow.WithCancel` child context, raced against the cancel channel in
a selector (`loop.awaitInterruptibly`) — the idiom the dispatch and build stages
already used. On cancel the run settles `cancelled` on the ordinary path: it
closes the work it had in flight, stamps `aep:cancelled`, closes the milestone,
writes its row. The signal design of ADR-0018's era is untouched; the planning
phase simply joins it.

The phase asks three times, and the first is the run **row**, before a single
gate is minted. The cancel surface swallows a failed signal delivery so a dead
engine cannot wedge the console, so the row is the only reading that survives a
cancel whose signal never arrived — and planning is the longest stretch a run has
to be wrong about.

**The cancel case is registered first in the selector.** When the activity
finished in the same workflow task the cancel landed in, the cancel wins. The
alternative reports the activity's own outcome for a run a person had already
stopped — and when that outcome is a failure, the version settles `plan-failed`,
which is both the wrong terminal reason and the wrong story on the issues.

**2. The interruption reaches the work, not just the waiting.**

Cancelling the activity context frees the *workflow* at once, but Temporal
delivers an activity cancellation only in the response to a **heartbeat** — an
activity that never beats runs its wait out unaware. So the two waiting
activities heartbeat (`heartbeating`, `HeartbeatTimeout` on their options), and
the body receives the activity's context unchanged: the SDK cancels that context
itself, and `WaitForReleaseChange` already selects on `ctx.Done()`. Nothing
downstream needed new plumbing.

**3. `ProvisionGates` retries are bounded — three attempts, then the version
fails.**

It is the one activity in the package that does not get the unbounded default,
and `plan-failed` is the terminal reason the phase already wrote. Three rather
than one because the same call is how a genuine GitHub or OpenChoreo blip shows
up, and failing a version on the first hiccup would trade a rare runaway for a
common false failure. The spacing is set explicitly
(`gateActivityRetryInterval`, 10s doubling) rather than left to Temporal's 1s
default: a fault that fails FAST would otherwise burn all three attempts in
about three seconds, which is the same false failure arrived at from the other
direction. Its `StartToCloseTimeout` rises to 5 minutes: the activity is not one
round trip but a sequence of per-resource waits, and a `StartToClose` expiry
would report "timeout" instead of the provisioning failure a reader actually
needs.

**It is the LAST guard, not the first, and that changed while this was being
written.** PR #681 landed the two sharper ones: the build click now refuses a
design naming a `resourceType` the cluster does not have (409, no tag cut, no
workflow started), and a permanent provision fault that does reach the activity
comes back non-retryable (`provisionErr`) and fails on attempt one with the
provisioner's own message. Both work by NAMING a fault. The bound is what covers
the rest — the permanent mode nobody has met yet, which under the unbounded
default is not a failed build but an invisible forever-loop. Layered rather than
alternative: an answer we recognise is asked once, a blip is asked three times,
and an answer we do not recognise still stops.

**4. The gate mint dedupes on (version, dependency) in every state.**

Bounding the retries stops the runaway, but it does not make the mint idempotent —
and its callers have always assumed it was. Both narrowings were state-based: the
Go-side lookup skipped anything not open, and `CreateIssueRequest.DedupeKey` is
resolved host-side against open issues too. That is invisible until something
closes a gate and then runs the mint again, which is exactly what a retried
`ProvisionGates` does, because `settleReadyGates` closes the gate of every
dependency that is already Ready.

Widening the lookup to every state is only safe with a **version label** on the
gate (`aep:gate-version/<tag>`), and that is why it exists. Without it, "a gate for
`orders-db` in any state" matches the closed gate the previous version left behind
— supersede closes them — and v2's build would decline to mint its own gates,
leaving its run waiting on a hold that does not exist. Every version re-derives its
own gates, so the identity is (version, dependency), never dependency alone. The
`DedupeKey` has always said so; the state narrowing did not.

`provisionGatesForVersion` reads the gates once and answers two separate
questions: which dependencies have an **open** gate (a live hold, and the number
the build path threads into provisioning), and which have a gate for **this
version in any state** (suppresses the mint, and nothing else). Collapsing them
would either resurrect the duplicates or admit a provision run against a closed
issue nothing derives from. An open gate still suppresses the mint whatever
version filed it, and whether or not it carries the label — so a gate minted by an
older binary behaves exactly as it did before.

The **roles gate** needed the same treatment and had further to go: it had no
Go-side dedupe at all, resting entirely on the open-scoped `DedupeKey`, and it is
closed by the same call that files it. A duplicate there is worse than
bookkeeping — the ticket is the channel the test users' logins are published on
(ADR-0022), so each of the incident's eleven attempts republished a set of
passwords into a new issue. `existingRolesGate` now finds this version's ticket in
any state and reuses its number. A read failure mints, deliberately: a duplicate
ticket is noise a human can close, while wrongly believing one exists would leave
accounts provisioned and their logins published nowhere.

The host's `DedupeKey` resolution is left alone. Open-scoped is correct for its
other users — `DedupeKeyFix(component, sha)` and friends should file a fresh issue
when the old one was closed and the problem recurs — so the fix belongs in the
gate path that wanted version identity, not in the shared key.

**5. A cancelled version says so — in both aggregates that report one.**

The fixes above made cancel reachable from the planning phase, which turned a
cancelled version from a rarity into the ordinary outcome of the button. That
exposed a display defect old enough to predate all of it: two independent
aggregates folded `RunStateCancelled` into `failed`.

- `build.statusFromRunState` → `BuildSummary.status`, read by the version ledger
  and the build page header.
- `projects.buildStageStatus` → `BuildStage.status`, read by the toolbar's
  project badge.

Both now carry their own `cancelled` value, and both had to move together: they
are read off the same run row by surfaces a reader sees at once, so fixing one
alone produced a page whose header said Cancelled while its toolbar said "Build
failed" two centimetres away. A failure is the platform reporting it could not
deliver an increment; a cancel is a person deciding not to. Folding them told
whoever pressed the button that something had broken.

`blocked` still reads as failed in both, deliberately. The version did not ship
and a human must supply something before it can, which is what "failed, and here
is the reason" says — and nobody chose it, which is the whole difference from a
cancel.

**Widening a response enum breaks older clients, and this one did.** With only the
BFF deployed, the build page showed **"Unknown"** — worse than the "Failed" being
fixed — because the console serves a pre-built bundle whose `ledgerStatus` had no
case for the new value and fell through to its default. The two deploy as separate
units, so the skew window exists however carefully they are rolled: **ship the
console with, or before, the BFF.** The default branch now renders the raw status
humanised rather than "Unknown", so the next value added degrades to something a
reader can act on. Only the console consumes either enum, so the blast radius is
one client.

**6. `Rebuild` is answered by the milestone, not inferred from the spec.**

ADR-0018 and the code beneath it treated "the spec did not change" as evidence
that the milestone was already filled, and had the run skip its planning turn on
that basis. It is not evidence. A run that died in its planning phase —
`plan-failed`, and now a cancel — leaves the milestone holding its gates and no
work at all. Skipping the plan there hands the loop an empty working set, which
`onEmptyWorkingSet` reads as "planning produced nothing to work" and settles the
version **succeeded** having built none of it.

So the click asks the milestone. `build.reopenIncrement` already lists its issues
to reopen the marked set; it now lists them in every state and reports whether
any `development` issue is among them. `Rebuild` is set only when the spec is
unchanged **and** that answer is yes. A read failure answers "not filled": the
wrong "filled" settles an unbuilt version as delivered, silently, while the wrong
"not filled" spends one LLM turn that dedupes onto the milestone's own titles and
mints nothing.

This defect predates the incident and was reachable through `plan-failed` alone.
Bounding the retries is what turns it from rare into likely, so it is fixed here
rather than filed.

## Consequences

- The cancel button works in the phase people are most likely to press it in, and
  it stops the work rather than abandoning it.
- A version with an unsatisfiable dependency now **fails** in about three minutes
  with the provisioner's own message as its reason, instead of looping.
- The duplicate-gate mint is fixed, not merely bounded: a retry over a version
  whose gates it already filed now files nothing, and the same holds for a worker
  restart or a fresh execution recovering a run — the cases the mint's own
  docstring already promised were safe.
- Gates carry a third label. `aep:gate-version/<tag>` joins `provision` and
  `aep:dep/<slug>` on the two gates that are per-version; the visibility and
  publish gates are keyed per project and deliberately do not get it.
- Two contract enums gained a value, so the console and the BFF are no longer
  independently deployable for this change. That is the cost of saying the true
  thing; the humanised fallback is what keeps a future addition from repeating it.
- Two of ADR-0018's statements are revised. Its "re-offering a `planning` row
  would settle an unplanned version as delivered" reasoning stands, but the
  underlying hazard now has a second guard that does not depend on the sweep
  abstaining. And its reading of the rebuild branch — the spec status as the whole
  decision — is superseded by decision 4 above.
- A run can now settle `cancelled` from `planning`, a transition the read model
  did not previously see. It needs nothing new: `cancelled` is already terminal
  and already closes the milestone for a dev run.
- The `resourceType` a design asks for is now validated against the cluster's
  installed `ClusterResourceType`s, which this ADR had listed as the open gap and
  the higher-value fix. PR #681 closed it at the click: the build is refused with
  a 409 naming the unknown type and listing the installed ones, so no version is
  cut and no run is started. This ADR's fixes are what the version behind that
  gate falls back on — a fault the catalog check cannot see (it is fail-open when
  platform resources are disabled) still fails in about half a minute rather than
  looping.

## Alternatives rejected

- **Make cancel a Temporal workflow cancellation, as the button's name suggests.**
  A cancelled context cannot run activities, and the cancel path is entirely
  activities — close the in-flight issues, stamp them, close the milestone, settle
  the row. Without those the reconcile sweep restarts the run within a tick.
  Reaching them would mean rebuilding every settle path on
  `workflow.NewDisconnectedContext`, against a design decision that is still
  correct.
- **Make the retry per-dependency instead of fixing the mint.** Provisioning is
  already per-dependency internally — a failure becomes a `ProvisionFailure` and
  the batch continues — and what flattens it is that Temporal's unit of retry is
  the ACTIVITY. Scoping the retry finer would hide this symptom while leaving the
  mint non-idempotent for every other path that re-runs it. It would also not be
  free: it means per-dependency activities or child workflows. One real cost does
  survive the chosen fix — `provisionResource` re-applies the OpenChoreo Resource
  for healthy dependencies on every attempt, with no already-Ready short-circuit
  — and that is an optimisation worth making on its own terms, not a correctness
  fix.
- **Classify `ResourceTypeNotFound` as permanent INSTEAD of bounding the retries.**
  Rejected as a replacement, not as an idea — and it has since shipped as the
  complement it should be. PR #681 taught the OpenChoreo client to read the
  Resource's status conditions (it previously flattened the condition to a
  timeout), so the fault is now named and fails on its first attempt, with a
  message naming the missing type. What that does not do is cover the modes
  nobody has named, which is the one thing a bound does; the two are layered in
  decision 3 above rather than chosen between.
- **A `planned_at` column to record how far planning got.** ADR-0018 rejected the
  same column for the same reason: schema plus a second writer to a row the
  supervisor owns, for a fact the milestone already holds.
- **Have the cancel surface terminate the workflow when the signal finds a run in
  `planning`.** Trades the blind phase for a zombie row — exactly the state the
  incident's manual terminate produced, and one nothing heals.
