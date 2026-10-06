# ADR-0044 — A finished run's feed is read from the observer

**Status:** Accepted · 2026-10-06
**Supersedes:** [ADR-0027](ADR-0027-run-recordings-are-observability-not-ledger.md)
for where a feed is kept. Its commitment 1 stands: Postgres is the system of
record for what happened to a version, and a feed is observability.
**Exposed by:** `RunCycleView.recording` in `packages/contracts/api/v1/openapi.yaml`
**Detail:** [`services/aep-api/internal/delivery/codingagent/design/cycle-status-and-logs.md`](../../services/aep-api/internal/delivery/codingagent/design/cycle-status-and-logs.md)

## Context

ADR-0027 made a coding cycle's feed replayable by recording it to files on
`aep-api`'s ReadWriteOnce workspaces volume. That volume was the last thing
pinning `aep-api` to one replica, and with git moved into each org's AE Studio
(ADR-0040) the recordings were the only data left on it. The observability
plane already indexes every coding pod's output, by Component, for as long as
its log retention.

The coding Job's lifecycle had two faults of its own. A finished Job deleted
by its TTL is re-created by OpenChoreo on the next reconcile and runs again.
Deleting the Component while its pod exists orphans the pod.

## Decision

**A live cycle's feed is read from its pod; a finished cycle's feed is read
from the observer by the cycle's Component UID. Nothing in the platform stores
a feed.**

1. **The cycle records its Component UID at dispatch.**
   `run_cycles.component_uid` is written at launch. Every observer read
   filters on it, so a later Component reusing the name, or another Component
   of the project, never lends the cycle its lines.

2. **Source follows the pod.** While a pod exists its log is read whole from
   the pod. Once no pod is left the observer answers: by component scope while
   the Component's binding resolves, by project scope plus `componentUid`
   once the Component is deleted. The project-scope phrase is `agentId`, which
   every runner line carries. `aep-api` reads the observer at `OBSERVER_URL`
   with the calling user's bearer, so the observer authorizes the person who
   asked (on Cloud through the observability proxy).

3. **One sequence across both sources.** The observer orders lines only to the
   second and has no cursor. A read pages by boundary-second replacement: each
   second is taken whole from one page, and a second fuller than a page is
   read from both ends and joined by line identity. Lines are sorted and
   deduped on the producer's `seq`, so a viewer that crosses the pod →
   observer switch sees no duplicate and no hole.

4. **The feed says what it is.** `RunCycleView.recording` is
   `live`, `kept`, `expired` or `unavailable`, derived from the cycle row alone:
   `live` while running, `kept` while the pod or the observer still holds it,
   `expired` past the observer's retention (`OBSERVER_LOG_RETENTION`, code
   default 72 h; the local chart sets 720 h) or for a cycle with no UID,
   `unavailable` when there is no observer or the last read failed. A hole
   inside a feed is a `notice` with `code: gap`.

5. **Suspend first, delete at settle.** The `coding-agent` ComponentType has a
   `suspend` environment config. `aep-api` suspends the cycle's binding when
   the watcher first sees the pod terminal, on cancel, and as a backstop on a
   closed cycle nobody suspended; a suspended Job that OpenChoreo re-creates
   is born suspended. A binding on a release cut before that schema cannot be
   suspended (`ErrSuspendUnsupported`), and its settle rests on the pod reads
   alone. The Job's TTL is 600 s (`CODING_AGENT_JOB_TTL`), so the
   finished pod goes about ten minutes after the Job completes. The settle
   sweep then deletes the Component, by the stored UID, once two "no pod"
   reads at least `CODING_AGENT_SETTLE_GRACE` (5 min) apart follow the
   suspend. Usage is captured from the pod's log while the pod exists, so "no
   pod" also means capture is done. The cycle's settle facts are
   `job_suspended_at`, `pod_gone_at`, `component_deleted_at`,
   `settle_checked_at` and `dispatched_at` (the current attempt's dispatch).

6. **The ledger is unchanged.** Outcome, verdict, pull request, merge SHA,
   reason, tokens and cost stay columns of `run_cycles` and `milestone_runs`.
   No decision reads the feed.

## Consequences

- `aep-api` has no volume: the workspaces PVC, the recording store, the
  recorder and its reaper are deleted.
- A run's history lasts as long as the observability plane's log retention
  (three days on Cloud by default), then reads `expired`. The ledger keeps the
  facts.
- A deployment without an observability plane shows finished feeds as
  `unavailable`, never as an empty stream.
- A finished cycle's Component is gone within minutes of settling, so it no
  longer holds a concurrency slot, and a TTL-deleted Job does not run again.
- The observer's indexing lag is covered by the settle grace and by reading
  the pod while it exists.

## Alternatives considered

- **Keep the recordings volume.** Rejected: a ReadWriteOnce volume that held
  only recordings, and kept `aep-api` at one replica.
- **A log volume in `ae-studio`.** Rejected for now: it needs the runner to
  push its feed and an ingest route to receive it, which belongs with a change
  to the coding runner.
