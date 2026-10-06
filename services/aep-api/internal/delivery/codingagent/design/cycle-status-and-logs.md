# Cycle status and logs

How the platform knows what a coding-agent run cycle did, where its output is
read from, and how its Job and Component are wound down. The decision is
[ADR-0044](../../../../../../docs/decisions/ADR-0044-a-finished-runs-feed-is-read-from-the-observer.md);
this note is the mechanics. How a cycle is dispatched and settled in
OpenChoreo is [oc-job-dispatch.md](oc-job-dispatch.md).

## Status comes from the Pod, never the binding

A cycle is an ephemeral OpenChoreo Component whose workload is a `batch/v1 Job`.
OpenChoreo registers **no health check for that kind**, so its ReleaseBinding
reports Ready — "completed successfully" — while the Job is still running, and
keeps reporting it after the Job has failed. Every classification therefore
reads the child **Pod** out of `GetReleaseBindingK8sResourceTree` and maps its
phase (`internal/delivery/codingagent/cycle_outcome.go`, a pure function).

| Pod | Outcome |
|---|---|
| absent, `Pending`, `Unknown` | pending — a node that stopped reporting is not a verdict |
| `Running` | running |
| `Succeeded` | the process ended; whether the WORK landed is the pull request's answer, delivered by webhook |
| `Failed` | the cycle is closed failed, with `DeadlineExceeded` reported as `timed_out` — unless the runner's settle says its model provider stopped it (below) |

Two rules bound the watcher's willingness to conclude anything: a **startup
grace** (10 minutes without a Running pod closes the cycle with a reason built
from the pod's waiting reason or its events, so an image-pull backoff, an
unschedulable pod and an unsynced secret are three different answers), and a
**sustained-404 rule** (three *consecutive* missing reads mean the workload is
gone; anything else — a 5xx, a timeout — is never evidence). Once a cycle's
Job is suspended or the cycle is closed, "no pod" is the expected state and
never a verdict.

The watcher closes a cycle only when it carries **no pull request**. A cycle that
opened one has landed side effects, so a pod exiting badly afterwards is not
evidence against it, and closing the row would fence out the very webhook that
completes the run.

## The lifecycle of a cycle's Job

| Moment | What happens | Recorded |
|---|---|---|
| Dispatch | The Component's UID is copied from the create reply (a 409 re-reads it) at launch. Every observer read filters on it. | `run_cycles.component_uid`, `dispatched_at` |
| First terminal pod | The watcher captures the run's usage from the pod's log, THEN suspends the Job binding. Idempotent: a stamped cycle is never asked again. | `job_suspended_at`; `codingagent.job_suspended {cause: terminal}` |
| Cancel | The cycle closes cancelled, THEN its Job binding is suspended; the runner gets its 30 s SIGTERM grace. Nothing is deleted. | `job_suspended_at`; `codingagent.job_suspended {cause: cancel}` |
| Nobody suspended a closed cycle | The settle sweep's backstop suspends it. | `codingagent.job_suspended {cause: backstop}` |
| Job finished | Kubernetes deletes the Job and its pod after `CODING_AGENT_JOB_TTL` (600 s). The Job OpenChoreo re-creates is born suspended and runs nothing. | |
| Settle | The Component is deleted by name once two "no pod" reads are far enough apart. | `pod_gone_at`, `component_deleted_at`, `settle_checked_at`; `codingagent.component_deleted` |

The settle's exact conditions (the binding still resolves; no pod of any
attempt; the Job held; a second no-pod read at least
`CODING_AGENT_SETTLE_GRACE` after the later of the first read and the
suspend), the backstop's rules, fair paging and the cancel fence are in
[oc-job-dispatch.md](oc-job-dispatch.md#settle-the-component-is-deleted-once-no-pod-is-left).
The settle waits on pod reads only, never on the observer: the grace covers
the observer's indexing lag. The watcher reads usage from the pod's log at
the first terminal pod, before it suspends, so by "no pod" the capture has
been attempted and none is possible any more. A failed usage write is
logged (`record cycle usage failed`) and not retried: the suspend goes ahead
and that cycle has no usage on its row.

A release cut before the `suspend` environmentConfig cannot be suspended
(`ErrSuspendUnsupported`, logged `codingagent.job_suspend_unsupported`); its
Job is left to its TTL and its settle rests on the pod reads alone.

## A cycle's feed is read, never stored

`CycleFeed` (`cycle_feed.go`) builds the v2 `RunEvent` feed of the RUN
progress stream on request. The platform writes no copy of agent output: the
`run_cycles` row is the system of record, and the feed is observability.

### Source follows the pod

- **While a pod exists** (the resource tree lists one, whatever the Job's
  state), its whole log is read through OpenChoreo, with no window. A
  suspended Job's finished pod still serves its own log, which is complete,
  while the index may lag; so the switch is on whether a pod exists, never on
  the Job's state.
- **Once no pod is left**, the observability plane at `OBSERVER_URL`
  answers, read with the calling user's bearer so the observer authorizes the
  person who asked: by COMPONENT scope while the Component's release binding
  resolves, by PROJECT scope once the settler has deleted it (a deleted name
  no longer resolves). Every read filters on the cycle's Component UID, so a
  later Component reusing the name, or another Component of the project,
  never lends the cycle its lines. A cycle with no UID has nothing to filter
  on: once its pod is gone its log is not served.
- **The window is the cycle's lifetime**: from 5 minutes before the cycle was
  created to 10 minutes after the latest of its close, its suspend and its
  pod's going (`cycleLogWindow`), or to now while none of those has happened.
- **Paging.** The observer orders lines only to the second and has no
  cursor. `clients/observability/cycle_logs.go` pages by boundary-second
  replacement, at most 20 pages a read. Each read logs `observer.read
  {cycle, componentUid, scope, lines, pages, linesMissing}`; a read that hit
  the page cap logs `observer.read_truncated`, and a second it could not read
  whole logs `observer.read_incomplete`.

Every source is read in the environment the cycle's Job was bound into
(`run_cycles.environment`), falling back to the project's write target for a
cycle that recorded none.

### One numbering for both sources

The console dedups on `(cycle, attempt, seq)`, so a viewer that crosses the
pod → observer switch sees no duplicate and no hole only if both sources give
the same line the same seq. The feed keeps the PRODUCER's number: a v2 line
keeps its own `seq`; a v1 line's seq `s` becomes `2s`, with its synthesised
`agent_started` at `2s-1`. Lines are sorted by seq and deduped on it, so a
line the index returned twice is served once.

A line with no producer seq (container bootstrap output, a stray library
write) is not in the v2 feed. The project-scope query admits only lines that
carry `agentId`, so the observer cannot return seq-less lines, and serving
them from the pod would make the two sources disagree. They stay in the v1
surface below.

### Attempts

A re-dispatch reuses the cycle's Component, so one log can hold several
pods. Lines group by pod name, ordered by each pod's first line; the newest
pod is the cycle's current attempt. A pod whose last line is older than the
current attempt's `dispatched_at` (less the watcher's 30 s clock skew) is the
previous attempt's leftover, the watcher's own rule stated on lines so both
sources apply it identically.

### Shared reads and the cursor

One read of a cycle serves every viewer for 2 s, so N viewers cost one
OpenChoreo or observer read per tick. The read runs detached from the viewer
that started it, bounded at 30 s. A failed read becomes a `logs unavailable`
notice on the feed and holds the cycle's state `unavailable` for 60 s.

The cursor is opaque (`f:<attempt>:<lastSeq>`). Anything else restarts at the
first attempt, because a duplicate event is deduped and a silently empty feed
is not. One call serves one attempt; the cursor rolls to the next attempt
once the current one has nothing new and a later one exists.

### What the feed says it is

`RunCycleView.recording` comes from the cycle row plus the outcome of this
cycle's last observer read (`CycleFeed.State`); no log is read to answer it.

| State | When |
|---|---|
| `live` | the cycle is open and its Job is not suspended |
| `expired` | the cycle has no Component UID, or it ended longer ago than `OBSERVER_LOG_RETENTION` (code default 72 h) |
| `unavailable` | a read of this cycle failed in the last 60 s, or there is no feed reader |
| `kept` | otherwise: the pod or the observer still holds it |

A cancelled cycle's feed ends with a platform-minted `run_settled`
(`outcome: cancelled`) at the attempt's last seq + 1, once no pod is left and
60 s have passed since the cycle ended, so the runner's last lines are in the
index before the settle takes a seq.

## The V1 surfaces still derive per viewer

`CycleProgress` (`agent_progress.go`) serves the VERSION build-progress
stream, which stitches many runs into one narrative, and the legacy
execution path. It reads, in order:

1. **Live** — the pod's log through OpenChoreo while the pod exists, keeping
   the newest 64 KiB (`logPageBytes`).
2. **Archive** — the observability plane by the cycle's Component UID, the
   same read as the v2 feed's (`readCycleObserver`).
3. **Unavailable** — a single synthetic marker when neither can answer. An
   empty stream and a lost log look identical to a reader and mean opposite
   things about the agent, so the platform never lets "gone" render as
   "silent".

Which source answers is resolved once (`resolveCycleLog`), and its page is
capped at `legacyProgressLimit` (200) with the head drop named on the feed.
Execution rows that predate the milestone model are still read from
`coding_agent_logs`; nothing writes new ones.

| Read | Shape | Source | Surface |
|---|---|---|---|
| `CycleEvents` | `gen.RunEvent` (v2) | the pod, then the observer | the RUN progress stream |
| `CycleProgress` | `contracts.ProgressEvent` (v1) | the pod, then the archive | the VERSION build-progress stream, and the legacy execution path |

The one thing taken out of a terminal pod's log by the WATCHER is the runner's
terminal line, in one pass (`terminalFromLog`). Its token usage is stamped onto
the cycle row — from v2's `run_settled` or v1's `result`, whichever the image
wrote, and always the LAST one because the runtime reports usage cumulatively
across a session. That is accounting, not logging, which is why the usage rides
on the reader's own `runnerLine` and never on a shape that reaches a console.

The same line says whether the run ended on its **model provider's limit**: the
runner stops a run whose provider has answered 429 for longer than a wait and
settles it with `code: provider_limit`, `host` and, when the provider stated
one, `resetAt` (`runners/remote-worker/src/lib/provider_limit.ts`). A failed pod
with that settle is not agent death. The watcher writes the run's failure record
(`model-provider-limit`, with the host and reset time the console's sentence
names) FIRST — the record only lands on a non-terminal run — and then closes the
cycle under `model_provider_limit`, which the supervisor settles BLOCKED on
without spending the re-dispatch budget (`delivery/provider_limit.go`). It also
logs one `model_provider_429 {source: runner}` line carrying the provider's own
words (`providerDetail`), which the platform neither stores nor shows.

## The feed is v2, whatever produced it

A run's feed is `RunEvent`, generated from `packages/contracts/api/v1/openapi.yaml`.
Two producers write the pod's stdout and both are read:

- a **v2 runner** emits `{"v":2,…}`, which IS a `RunEvent` and is passed through
  whole. An envelope naming a `kind` this build cannot render is wrapped rather
  than forwarded — `kind` is what every consumer switches on, so an unrenderable
  one would put a blank row on a user's feed.
- a **v1 runner** emits `{"schemaVersion":1,…}` and is LIFTED
  (`run_event_lift.go`).

### What the lift does

The lift is not a field rename. v1 attributed a line with `emitter` +
`emitterId`, which is a fact about the LINE; v2 attributes it to an agent that
STARTED and later SETTLED, which is a fact about the run. So the lift infers the
agents, and marks the inference:

| v1 | v2 |
|---|---|
| `emitterId` (absent ⇒ the lead) | `agentId` |
| first line carrying a new `emitterId` | a synthesised `agent_started` with `role: "inferred"`, `depth: 1` |
| `tool_result` whose `toolUseId` IS the `emitterId`, tool `Agent`/`Task` | `agent_settled` with the runtime's `status`, `durationMs`, `toolCount`, `linesAdded`, `linesRemoved` |
| `activity` | `agent_progress` (`phrase`) |
| `phase: agent_started` | `run_started` |
| any other `phase` | `agent_progress` (`phrase` = the phase name, the stable id a console labels) |
| `progress_item` | `work_item` (`source: criterion`, `itemId`, `itemStatus`) |
| `tool_use` / `tool_result` / `git_commit` / `git_push` / `gh_action` | the same kinds |
| `log`, and anything unrecognised | `notice` (`level`, `detail`) |
| `result` | `run_settled` (`outcome`, `error`, `usage`) |

`role: "inferred"` is load-bearing: a v1 runner announced no subagents, so a
reader has to be able to tell a tree this reader DEDUCED from one a runtime
declared. A console upserting agents by id repaints one row for an agent it
has already seen.

### Platform markers are notices

Everything the platform itself puts on a feed — the dark zone (pod scheduling,
image pull, container boot), a gap, a lost log — is a `notice` on the lead, on a
STABLE NEGATIVE seq, stamped with the instant the platform derived it:

- the negative seq is the marker's id: a client dedups on `(cycle, attempt, seq)`
  and a producer's seqs are positive, so the same state re-derived every read
  collapses to one row and a state TRANSITION shows exactly one new row. A gap
  starting at producer seq `n` is notice seq `-1000000 - n` (`seqGapBase`), so
  the same hole is the same row on every read and from either source;
- the timestamp is a REAL instant, passed in by whoever derived the marker — the
  read that observed the pod, or the clock of the line after a gap
  (`platformNotice`, `run_events.go`). `RunEvent.ts` is required and generates
  as a `time.Time`, so a marker without one would go out as
  `0001-01-01T00:00:00Z`, which a console reads as an agent 2026 years old.
  (`@aep/progress-view` refuses an implausible instant too.)
- `agentId: lead` because the field is required and any other value names a
  SPAWNED agent under the contract — a made-up id would have a console open a row
  for an agent that does not exist.

A pod that has not started is deliberately **not** an agent event. There is no
session, no turn and nothing to report a phrase about, so a scheduling delay is
never lifted into `agent_progress`.

The dark-zone notices carry a **code and, ordinarily, no prose**:
`runner_scheduling`, `runner_pulling_image`, `runner_image_pull_backoff`,
`runner_config_error`, `runner_unschedulable`, `runner_starting` — exactly the
stable ids `bootstrapState` has always held in its `name`. Wording lives in
`@aep/progress-view`, keyed off the code, so a consumer can relabel or translate
the dark zone without a platform release; a producer that shipped the sentence
would be a second place the copy could change. `detail` is filled only where the
code genuinely cannot say the whole thing: the scheduler's own first line on an
unschedulable pod (WHICH resource ran out), and the raw waiting reason on one
this build has never seen (bucketed under `runner_pulling_image`).

The markers that ARE a lost feed — a log that cannot be served, a detected
`seq` gap — carry `code: gap`. The one positive seq the platform mints is a
cancelled cycle's closing `run_settled`.
