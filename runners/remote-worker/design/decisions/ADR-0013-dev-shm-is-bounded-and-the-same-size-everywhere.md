# ADR-0013 — `/dev/shm` is bounded, memory-backed, and the same size in every run path

**Status:** Accepted

This image runs a headless Chromium — `agent-browser` during a mock-verification
walk, playwright during a validation run — and Chromium mmaps its shared buffers
out of `/dev/shm`. On 64Mi of it Chromium does not degrade, it **aborts**: the
browser process dies mid-page and the CLI reports a closed CDP channel, which
reaches an agent as a browser that will not start rather than as a resource
problem.

64Mi is what a Kubernetes pod gets by default, because nothing mounts `/dev/shm`
and the container runtime supplies its own. Both of the other ways of running
this exact image already sized it — `--shm-size=1g` in
`runners/remote-worker/local/run-local.sh:141` and in the playground's
`docker run` (`playground/src/engine/coding-run.ts`) — so the cluster was the one
place the image behaved differently from where it is developed, and it was the
one place nobody could reproduce.

**All three now share one 1 GiB budget.** The Job mounts a `dshm` volume at
`/dev/shm` with `medium: Memory` and `sizeLimit: ${parameters.shmSize}`,
defaulting to `1Gi` and enum-bounded `64Mi | 256Mi | 512Mi | 1Gi | 2Gi`, in
`services/aep-api/internal/clients/openchoreo/coding_agent_component_type.go`
(the `shmSize` parameter and the `dshm` volume in the Job resource). That file
belongs to the BFF; this ADR records the runner-side fact — the image's browser
assumes a real `/dev/shm`, and every caller of the image is now responsible for
supplying one.

Two properties of that mount are easy to get wrong later, and both are the
reason it is written the way it is:

- **`medium: Memory` is a tmpfs charged to the container's cgroup.** What the
  browser puts in `/dev/shm` comes out of `memoryLimit` like any other
  allocation. It is not extra headroom, and `sizeLimit` is a ceiling on the
  tmpfs rather than a reservation. Read it beside the memory pins, not as an
  addition to them.
- **An unbounded memory-backed emptyDir is sized from the NODE's memory.** A pod
  that filled one would take the node down with it instead of being OOM-killed
  on its own. That is why `shmSize` is enum-bounded rather than free-form, and
  why raising it is a deliberate choice made against `memoryLimit`, not a knob
  to turn when a browser misbehaves.

## Rejected

- **`--disable-dev-shm-usage` in `AGENT_BROWSER_ARGS`.** The obvious cheap fix,
  and the one to explain rather than leave looking like an oversight. That
  variable is an **image ENV** (see the Dockerfile's `AGENT_BROWSER_ARGS` block
  and `runners/AGENTS.md`), so it applies to the local and playground paths too —
  where `/dev/shm` is already 1 GiB and healthy. Adding it would degrade the two
  paths that were fine in order to patch the one that was not, re-creating the
  cluster-versus-desktop asymmetry this ADR closes, pointing the other way. It
  also relocates Chromium's buffers to `/tmp`, which in the pod is a disk-backed
  emptyDir — trading a memory limit for I/O on the browser's hot path. A
  per-invocation `--args` would avoid the ENV problem and re-introduce the one
  the ENV exists to solve: a flag every browser command has to remember.
- **Raising `memoryLimit` instead.** It does not create a `/dev/shm`; the 64Mi
  default is fixed by the runtime and is not a share of the pod's memory.

## Consequences

- Reproducing a browser failure locally is now meaningful: the desktop and the
  cluster give Chromium the same shared memory, so a walk that works in the
  playground and not in a pod is a difference somewhere else.
- `shmSize` and `memoryLimit` move together. Someone raising `shmSize` to `2Gi`
  against the default `3Gi` limit has given a third of the pod's memory to a
  tmpfs, and the agent process, the JVM and the dev server share what is left.

Related: ADR-0007 (which chromium the CLI launches) and the Dockerfile's
`AGENT_BROWSER_ARGS` block (why it launches with `--no-sandbox`) are the other
two halves of "the image states what the browser environment is".
