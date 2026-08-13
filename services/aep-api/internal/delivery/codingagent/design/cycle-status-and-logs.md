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

## Logs are read, never stored

The platform stores no agent logs. Three sources answer, in order:

1. **Live** — `GetReleaseBindingK8sResourceLogs` while the Component exists.
2. **Archive** — the observability plane's `POST /api/v1/logs/query`, component
   scope, for as long as the Component is retained. The observer indexes on the
   component CR, which is why retention deletes Components lazily instead of at
   the moment a cycle ends. It has no cursor, so reads page by advancing a time
   window with `limit: 1000`.
3. **Unavailable** — a single synthetic `logs_unavailable` progress line when
   the Component has been reclaimed or no observability plane is configured. An
   empty stream and a lost log look identical to a reader and mean opposite
   things about the agent, so the platform never lets "gone" render as "silent".
4. **Truncated** — when a finished cycle's archive exceeds one progress page
   (200 events), the response leads with a `logs_truncated` phase line naming
   the window, then the newest lines. Older output is not paged into the UI yet.

The one thing taken out of a terminal pod's log is the runner's token-usage
line, stamped onto the cycle row. That is accounting, not logging.
