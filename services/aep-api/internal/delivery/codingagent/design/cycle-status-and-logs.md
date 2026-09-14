# Cycle status and logs

How the platform knows what a coding-agent run cycle did, and where its output
is read from. Describes the shipped end state.

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
| `Failed` | the cycle is closed failed, with `DeadlineExceeded` reported as `timed_out` |

Two rules bound the watcher's willingness to conclude anything: a **startup
grace** (10 minutes without a Running pod closes the cycle with a reason built
from the pod's waiting reason or its events, so an image-pull backoff, an
unschedulable pod and an unsynced secret are three different answers), and a
**sustained-404 rule** (three *consecutive* missing reads mean the workload is
gone; anything else — a 5xx, a timeout — is never evidence).

The watcher closes a cycle only when it carries **no pull request**. A cycle that
opened one has landed side effects, so a pod exiting badly afterwards is not
evidence against it, and closing the row would fence out the very webhook that
completes the run.

## The platform RECORDS a cycle's feed, and serves viewers from the recording

A cycle's feed used to be derived per viewer: every SSE connection tailed the
pod on its own two-second cursor, kept the newest 64 KiB and wrote nothing. That
lost output five measured ways. The recorder closes all five, and the shape of
it is one server-side reader per cycle writing an append-only file that every
viewer then reads.

| Loss | Old mechanism | The recorder's answer |
|---|---|---|
| a burst larger than one page between two polls | the read kept the last 64 KiB (`logPageBytes`) | reads with a time cursor and **no byte cut**; dedupes by `seq` |
| a finished run's history was its newest 200 events | the archive read, capped at `legacyProgressLimit` | viewers read the recording, never the pod or the archive — **the window is gone** |
| the pod exited between two polls | nothing read after the last tick | a terminal pod phase triggers **one final full read** |
| cancel deletes the Component, so its log is unreadable from that instant | nothing | the recording closes with a runner-less `run_settled {outcome: cancelled}`, state `gaps` |
| a re-dispatch is a new pod whose `seq` restarts at 1 | seqs collided in one stream | **one file per attempt**; frames carry `attempt` |

### Lifecycle

The **cycle watcher owns discovery**, not the clock: every dispatched cycle it
sees on its 30 s tick is handed to `CycleRecorder.Ensure`, and each cycle's
session then paces itself — **1 s while its pod is `Running`, the watcher's 30 s
otherwise**. Live feel without hammering a log that is not growing.

A session ends on a terminal pod phase **plus one final full read** (the
OpenChoreo log API still serves the pod while the Component exists), and closes
the recording `complete` — or `gaps` if anything was known lost. A process
restart leaves it OPEN: a restart re-`Begin`s the same attempt and resumes from
the cursor persisted in `state.json`, so nothing is written twice.

### The read window is measured from the DATA, and every poll is bounded

Each incremental read asks from an **absolute instant**: the pod-clock timestamp
of the newest line already ingested (`cursor.lastLineTs`), less a 3 s overlap for
the log API's whole-second granularity. The recorder's binding name is resolved
**once per session** — it is fixed for the attempt — and re-resolved only when a
read reports it gone, and the absolute instant is converted into the API's coarse
`sinceSeconds` **in the breath before the log call**, never earlier.

All three are one measured fix. The window used to be measured from the
platform's own clock, stamped when the previous read RETURNED, and converted
before three sequential OpenChoreo round trips (binding list → resource tree →
pod logs) consumed it. Both halves assert something untrue: that the answer
described the instant the call returned, and that no time passes between choosing
a window and applying one. One `logs` handler took 13.22 s and one poll spent
6.4 s in front of a 5-second window, so three times in one run the next window
began AFTER lines the recorder had never read — and because the cursor only ever
moves forward, nothing asked for them again. Anchored on the data, a 13-second
stall makes the next window 13 seconds wider, which costs a re-read that dedupe
throws away.

Each poll's OpenChoreo calls carry a **30 s deadline** (the client has none of
its own, so a hung call used to block the session loop silently, leaving a
recording that simply stopped growing and not one warning to say why). A poll
that fails leaves the cursor where it was, so the next one asks for the same
window — widened by however long the failure took.

The runner makes **no network call** for any of this. Its stdout is still the
one transport, which is what keeps a runner that cannot reach the platform from
being a runner whose work is invisible.

**A `run_settled` does not close the feed** — the pod's terminal phase does. The
runner's last event is a statement about its own session, not about the
container's stdout, and the container keeps writing after it: a package manager's
exit notice, a shutdown hook, a crash tail from whatever was still running.
Those lines are recorded like any other, because stopping at `run_settled` would
throw away the output most likely to explain a run that ended badly. The cost is
that a reader cannot assume `run_settled` is the last line — it is the line that
says how the run ENDED, and `state.json`'s `closed` is the one that says nothing
more is coming.

### On disk

```text
<workspaceRoot>/runs/<orgId>/<cycleId>/events.<attempt>.ndjson   one v2 RunEvent per line
<workspaceRoot>/runs/<orgId>/<cycleId>/state.json                what can be served
```

`state.json` carries the state, the newest attempt, the cycle's event and byte
counts, and the recorder's own cursor. `events` counts ROWS across attempts while
`cursor.lastSeq` is one attempt's highest position, so the two need not be equal
— the dark-zone markers are counted and hold no position. What they must never
do is disagree about whether an event exists. The cursor holds the producer seq
and the two pod-clock timestamps a resumed session needs: `proseTs` (the last
seq-LESS line recorded, which is how those are deduped) and `lastLineTs` (the
newest line of ANY kind ingested, which is what the next read window is measured
from). The state machine:

```text
(no directory) ─── none
      │ Begin
      ▼
  recording ──── terminal pod + final read ───▶ complete
      │  │
      │  └─────── a seq gap / a dropped event ──▶ gaps   (sticky, does NOT close)
      │                                              │
      │  cancel, or the Component vanishing ─────────┘ (closed)
      ▼
   lost   ← the directory is there and the events are not
```

`gaps` is sticky: a `complete` written over it keeps the gaps, because a partial
feed presented as the whole of it is the one thing the state exists to prevent.
`none` and `lost` are deliberately never collapsed — they paint the same empty
screen and are very different bugs.

**Dedupe and gap detection run in the PRODUCER's numbering** — the `seq` on the
raw envelope, which is a different sequence from the one written to the file. It
is the only numbering that can answer "did the producer write something we never
saw": the recorded seq counts what the platform wrote down, so a hole in it is
invisible by construction, and one line can lift to two events (an inferred
`agent_started` and the line that revealed it), which would make a detector
reading recorded seqs report a missing event between every pair. A seq-less line
(container bootstrap output, a stray library write, a subprocess writing straight
to fd 1, a crash tail) has no producer numbering at all, so its cursor is the
kubelet's own monotonic timestamp.

A detected gap is repaired from two sources, in order: the **live pod**, re-read
with an explicit window reaching back to the last line ingested (while the
Component exists its whole log is still served — the incident that motivated this
was diagnosed by fetching a complete `1..478` after the fact, so a hole in the
recording is usually a hole in what the platform ASKED FOR), and then the
**observability archive** for what the pod can no longer give back — a stretch
the kubelet has rotated away, or a Component already deleted. That is the
archive's **only remaining job**. At most one repair per page, and a source that
errors or answers empty is logged at WARN: the incident left no trace at all
because the only log line on this path sat after an early return that both cases
took.

What is still missing becomes a `notice {code: gap}` written INTO the recording
at the point the events went missing — a reader has to be able to see WHERE the
hole is — and the recording is marked `gaps`.

The notice is worded **by recoverability**, because the wording is a claim the
platform has to be able to stand behind. Mid-run it says *"… N event(s) of this
run have not been captured yet"*: the pod is running, its log is readable, and
the recorder is about to ask for that stretch again. The unrecoverable sentence
belongs to the one moment it is true — the recording closing as `gaps`, which
writes a final row saying nothing can recover it now. `@aep/progress-view` owns
the label for `code: gap` ("events are missing from this feed") and the producer
owns the specifics; neither may assert a loss the other has not established.

The **dark zone** (pod scheduling, image pull, container boot) is recorded too,
one row per state rather than one per poll: a viewer that reads only the file
would otherwise see nothing at all for the slowest part of the flow.

### Reading it back

`CycleEvents(cycle, cursor)` seeks to the caller's byte offset, returns what has
been appended since, the ATTEMPT those events came from, and the next cursor.
The cursor is OPAQUE (`r:<attempt>:<offset>`) — anything unrecognised restarts
from the beginning, because a duplicate event is deduped by
`(cycle, attempt, seq)` and a silently empty feed is not. One call serves one
attempt; the cursor rolls to the next attempt once the current one is exhausted,
which is what lets the console show attempts as sequential crews under one cycle.

An offset is only ever advanced past a newline, so a reader that catches a
partial append re-reads that line whole instead of parsing half an event. That
single rule is what lets a live viewer follow a file the recorder is writing.

The attempt on the frame comes from the READER, not the cycle row: a row names
the attempt in flight while a replay may still be walking the previous one.

### Retention, and what the volume now is

`runs/` is the one subtree of `/workspaces` that is **not** a rebuildable cache
— a run's feed existed while its pod did and nowhere else. That is
[ADR-0027](../../../../../../docs/decisions/ADR-0027-run-recordings-are-observability-not-ledger.md):
the recording is **observability, not ledger**. The `run_cycles` row in Postgres
stays the system of record for what happened to the version, and the contract
gained `RunCycleView.recording` so a console can say "no recording" honestly.
Losing a recording costs a feed, never a fact. (Postgres remains the contained
fallback: the reader is already an interface and the same recorder could write
rows keyed by `(cycle_id, attempt, seq)`.)

Retention is a seventh reaper pass (`internal/platform/gitfs/reaper`): **30 days
by age** (`AEP_WORKSPACE_RECORDING_MAX_AGE`), with the existing per-org quota as
the backstop, oldest first. Quota/LRU eviction under disk pressure still walks
`repos/` only — a cache may be thrown away to make room, the only copy of
something may not. A per-cycle size cap exists
(`AEP_WORKSPACE_RECORDING_MAX_BYTES`) and is **off by default**; a run that trips
it records a notice and carries on without recording, because stopping an agent
to protect a log would be the wrong way round.

**Known limit, carried:** kubelet rotates a container log at 10 MiB. A 55-minute
run wrote about 300 KB, so this is roughly thirty times off — and the gap
detector is what would name it.

## The V1 surfaces still derive per viewer

`CycleProgress` is unchanged and reads the same three sources, in order:

1. **Live** — `GetReleaseBindingK8sResourceLogs` while the Component exists.
2. **Archive** — the observability plane's `POST /api/v1/logs/query`, component
   scope, for as long as the Component is retained. The observer indexes on the
   component CR, which is why retention deletes Components lazily instead of at
   the moment a cycle ends. It has no cursor, so reads page by advancing a time
   window with `limit: 1000`.
3. **Unavailable** — a single synthetic marker when the Component has been
   reclaimed or no observability plane is configured. An empty stream and a lost
   log look identical to a reader and mean opposite things about the agent, so
   the platform never lets "gone" render as "silent".

Which of the three can still answer is resolved once (`resolveCycleLog`), and
its page is capped at `legacyProgressLimit` (200) with the head drop named on
the feed. It survives for the VERSION build-progress stream, which stitches many
runs into one narrative and is not part of the run feed's cutover, and for the
legacy execution path.

| Read | Shape | Source | Surface |
|---|---|---|---|
| `CycleEvents` | `gen.RunEvent` (v2) | the platform's recording | the RUN progress stream |
| `CycleProgress` | `contracts.ProgressEvent` (v1) | the pod, then the archive | the VERSION build-progress stream, and the legacy execution path |

The one thing taken out of a terminal pod's log by the WATCHER is the runner's
token-usage line, stamped onto the cycle row — from v2's `run_settled` or v1's
`result`, whichever the image wrote, and always the LAST one because the runtime
reports usage cumulatively across a session. That is accounting, not logging,
which is why the usage rides on the reader's own `runnerLine` and never on a
shape that reaches a console.

## The feed is v2, whatever produced it

A run's feed is `RunEvent`, generated from `packages/contracts/api/v1/openapi.yaml`.
Two producers write the pod's stdout and both are read:

- a **v2 runner** emits `{"v":2,…}`, which IS a `RunEvent` and is passed through
  whole. An envelope naming a `kind` this build cannot render is wrapped rather
  than forwarded — `kind` is what every consumer switches on, so an unrenderable
  one would put a blank row on a user's feed.
- a **v1 runner** emits `{"schemaVersion":1,…}` and is LIFTED
  (`run_event_lift.go`).
- anything else — bootstrap stdout, a stray library write — is wrapped as a
  `notice` rather than dropped, so the feed stays continuous.

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
declared. Announcements are per PAGE, not per run — a live tail is a sliding
64KiB window, so a reader that attaches mid-run re-announces the agents it can
see rather than pretending it watched them start, and a console upserting agents
by id repaints one row instead of adding one.

**The recorder numbers the feed; the lift does not.** `(attempt, seq)` is the
dedup key every consumer is told to use, and the producer's numbering cannot
serve as it: a third of the lines on this stream carry no envelope at all, so
they lifted to `seq: 0` — all of them, which made them ONE event to anybody
deduping by seq. A measured run ended with five `npm notice` lines written by the
`npx` shim after the runner's process had settled, and the console rendered one
of them; the same path carries stack traces, compiler errors and crash tails, so
a multi-line diagnostic arrived as its first line.

So the lift returns events with no seq at all and the recorder stamps each one as
it appends (`recordingSession.number`) — the event's POSITION IN THE FILE, which
is exactly what the contract says `seq` is. It is stable across re-reads because
the recorder never lifts a line twice: an enveloped line is skipped once its seq
is at or below `cursor.producerSeq`, a seq-less one once its pod clock is at or
below `cursor.proseTs`, and both cursors are persisted beside the events, so a
restart and the final full re-read resume the numbering rather than restart it.
A recording that watched a run from its first line therefore numbers every runner
event exactly as the runner did, right up to the first seq-less line.

### Platform markers are notices

Everything the platform itself puts on a feed — the dark zone (pod scheduling,
image pull, container boot), a truncation, a lost log — is a `notice` on the
lead, on a STABLE NEGATIVE seq, stamped with the instant the platform derived it:

- the negative seq is the marker's id: a client dedups on `(cycle, attempt, seq)`
  and a producer's seqs are positive, so the same state re-derived every 2s
  collapses to one row and a state TRANSITION shows exactly one new row;
- the timestamp is a REAL instant, passed in by whoever derived the marker — the
  read that observed the pod, or the clock of the line that revealed a gap. These
  markers once carried none, on the reasoning that ordering is `seq`'s job (it
  is) so a marker needs no clock of its own. But `ts` is required by the contract
  and generates as a `time.Time`, so "none" was never what went on the wire:
  Go's zero value marshalled as `0001-01-01T00:00:00Z`, and because these
  notices belong to the LEAD and the dark-zone one heads very nearly every
  recording, a console subtracting it from its clock showed the lead agent 2026
  years old and drew its timeline lane across the whole axis. A field a producer
  cannot fill honestly has to be absent; this one cannot be absent, so it is
  filled honestly. (`@aep/progress-view` refuses an implausible instant too — a
  recording written before this fix still holds zero-stamped markers.)
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

The markers that ARE a lost feed — an unservable recording, a detected `seq`
gap, the size cap — carry `code: gap`.

One exception to the stable-negative-seq rule, and it is deliberate: a notice the
RECORDER writes at a point in the run (a gap, the size cap) takes the next free
POSITIVE seq and stays where it happened. Those are one-time facts about a
position in the feed, not a state re-derived every poll.
