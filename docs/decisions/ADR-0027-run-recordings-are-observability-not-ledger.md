# ADR-0027 — Run recordings are observability, not ledger

**Status:** Accepted · **Constrains:** the `/workspaces` volume
([`services/aep-api/design/shared-workspace-volume.md`](../../services/aep-api/design/shared-workspace-volume.md))
· **Exposed by:** `RunCycleView.recording` in `packages/contracts/api/v1/openapi.yaml`

**Why here and not in a package `design/` note.** A package note describes how one slice works. This
decision says what the platform PROMISES about a class of data — that a run's feed may be lost, that
`run_cycles` may not — and it is published on the wire for a different codebase to render. Two
consumers have to agree on it, so it lives where repo-wide decisions live.

## Context

A coding cycle's feed existed in exactly one place: the pod's stdout. Every viewer re-derived it —
each SSE connection polled the pod on its own two-second cursor, kept the newest 64 KiB and wrote
nothing down. Five losses were measured on that shape, and they are not edge cases; the fourth and
fifth happen on ordinary runs:

| loss | mechanism |
|---|---|
| a burst larger than the page between two polls | the read kept the last 64 KiB (`logPageBytes`) and dropped the rest |
| a finished run's whole history was its newest 200 events | the post-mortem read the observability archive capped at `defaultProgressLimit` |
| the pod exited between two polls | its last words — the runner's terminal `result` among them — were never read |
| cancel | deleting the Component makes the log unreadable from that instant |
| re-dispatch | a new pod whose `seq` restarts at 1 collides with the previous attempt's |

Closing them means writing the feed down, once, server-side. The only durable thing aep-api already
has beside Postgres is the `/workspaces` ReadWriteOnce volume — and every byte on it today is a
**rebuildable cache**: bare git mirrors, per-SHA snapshots, staging and trash. That property is load
bearing. It is why the mount can be evicted under disk pressure, why losing the PVC is an
availability trap rather than data loss, and why the reaper is free to delete almost anything on it
by age.

A recording is not rebuildable. A run's feed exists while its pod does and nowhere else; once the
Component is reclaimed there is no source to regenerate it from. Putting one on that volume changes
what the volume IS, and doing that silently would be the bug — a later operator, reading "content is
a rebuildable cache; blast radius is a cold start", would reason correctly from a premise that had
stopped being true.

## Decision

**The recording is observability. The `run_cycles` row is the ledger. Losing a recording costs a
feed, never a fact.**

Concretely, three commitments:

1. **Postgres stays the system of record for what happened to a version.** Outcome, verdict, pull
   request, merge SHA, agent reason, token spend and cost — every fact a decision is made on — are
   columns of `run_cycles` and `milestone_runs`. Nothing that a run's fate depends on is read out of
   the recording. Deleting `runs/` entirely leaves the platform's answers unchanged.
2. **The recording says how much of itself it is.** `RunCycleView.recording` is on the wire, with
   five states — `none | recording | complete | gaps | lost` — and the two empty ones are kept
   apart on purpose. `none` is "the platform has no record of this cycle's feed" (a cycle from
   before the recorder, or a boot with no volume); `lost` is "it had one and cannot serve it". They
   paint the same empty screen and are very different bugs. `gaps` is the honest middle: a record is
   being served that the platform KNOWS is incomplete, so a partial feed is never presented as the
   whole of it.
3. **`runs/` gets its own retention, and is never evicted for disk pressure.** Thirty days by age
   (`AEP_WORKSPACE_RECORDING_MAX_AGE`), with the existing per-org quota as the backstop — a seventh
   reaper pass, beside the passes that reclaim the cache. Quota/LRU eviction still walks `repos/`
   only. A cache may be thrown away to make room; the only copy of something may not.

## Consequences

**The volume is no longer uniformly a cache, and the docs say so.** `shared-workspace-volume.md`
carries the split explicitly. An operator restoring or resizing the mount now has one subtree where
"just let it rebuild" is wrong.

**A viewer never reads a pod.** The v2 run feed is served from the file: seek to the caller's byte
offset, hand back what has been appended, return the offset. The window is gone — a reload mid-run
replays from the first event, and a reload after the pod is reaped shows the whole cycle. Two
viewers cost two file reads.

**The 200-event post-mortem is retired.** The observability archive is still called, but only by the
recorder, to backfill a detected `seq` gap. It is no longer the ordinary source for a finished run's
history. (The v1 VERSION build-progress stream still derives per viewer and keeps its page cap; it
stitches many runs into one narrative and is not part of this cutover.)

**`recording: none` is a real state a console must render.** Every cycle dispatched before this
shipped has it, permanently, and a cycle in flight has it for up to one watcher tick (30 s) after
dispatch, until the recorder's first poll.

## Alternatives considered

**Rows in Postgres, keyed by `(cycle_id, attempt, seq)`.** This stays the contained fallback and is a
small change: the reader is already an interface, and the same recorder would write rows instead of
lines. It was not taken first because a 55-minute run is about 300 KB of append-only NDJSON that is
read sequentially from an offset and never queried by field — which is a file, not a table — and
because putting it in Postgres would put observability data in the same store as the ledger and
invite exactly the confusion this ADR exists to prevent.

**Ingesting from the runner over HTTP.** Rejected. Stdout stays the one transport, so a runner that
cannot reach the platform is not a runner whose work is invisible, and there is no ingest endpoint to
authenticate, rate-limit or lose events at.

**Treating the recording as a ledger and replicating it.** Rejected as scope: it would make the
single-node RWO limitation a data-durability problem rather than an availability one, which is a much
larger decision than making a feed replayable.

## Known limit, carried

kubelet rotates a container log at 10 MiB. A recorder that fell far enough behind a very loud
producer could ask for a window whose head has already rotated away. A 55-minute run wrote about
300 KB, so this is roughly thirty times off — and the `seq` gap detector is exactly what would name
it if it ever happened, as a `notice {code: gap}` at the point of the hole and a recording marked
`gaps`. It is not defended against beyond that.
