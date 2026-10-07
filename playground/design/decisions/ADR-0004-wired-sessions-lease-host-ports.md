# ADR-0004: Wired sessions lease their host ports

**Status:** Accepted (2026-10-05)

## Context

ADR-0002 left ports to compose: each service listens on 9090 in its container
and only the host side is mapped. Each session picked its host ports by asking
`isPortAvailable` whether anything held them at that moment.

A probe answers only for that moment. A wired session binds its ports much
later: compose publishes a service's port after an image build that can take
minutes, and Vite binds seconds after it is spawned. In an evals sweep at
`--concurrency 2`, two sessions started together both probed 19090 as free,
both wrote it into their compose files, and the second `up` failed with "port
is already allocated". That was scored as the app's failure.

## Decision

**A session leases every host port it is assigned** (`engine/wire/ports.ts`).
The leases live in one registry per machine user,
`~/.aep-playground/ports/leases.json`, shared by every playground checkout and
every project. A port that another live session leases is not offered, even
before anything is bound to it. This also covers the gap while `r` on the
panel recreates a container. A session holds its leases until its teardown has
released the ports.

**Lease first, then probe.** `take` reserves the port in the registry and then
probes it, and gives it back if the probe finds it held. The probe still
catches holders that are not playground sessions. Probing inside the lock would
make every other session wait behind a socket timeout.

**A lease belongs to a process.** Each lease records its pid and a session id
(one TUI can run more than one session). The next writer drops every lease
whose pid is gone. This is the whole cleanup for a `wire` that was killed
before its teardown ran.

**The registry changes under a `link(2)` lock.** A writer writes its pid to a
temporary file and links it to `leases.lock`. `link(2)` fails atomically when
the lock exists, and the lock file already holds the holder's pid. The registry
itself is replaced by `rename(2)`, so a reader never sees half a file. The lock
is held only for a few file operations, never across a probe, a build or a
bind.

**An abandoned lock is removed.** A lock is abandoned when its holder is dead,
its content is not a valid pid, or it is older than 30 seconds. A writer that
cannot get the lock within 10 seconds fails with an error that names the lock
file.

## Consequences

- This supersedes ADR-0002's "ports are compose's problem" for host ports.
  Container ports are unchanged.
- Concurrent sessions, including evals sweeps, never get the same host port.
- Two sessions can both take over one abandoned lock only if its holder died
  inside the few-millisecond critical section and both read it at the same
  instant. The probe is the backstop for that case.
- The registry is machine state outside the project tree. Deleting it is safe
  when no `wire` is starting.
