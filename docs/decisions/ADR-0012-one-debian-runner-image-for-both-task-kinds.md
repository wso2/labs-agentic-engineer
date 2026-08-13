# ADR-0012 — One Debian runner image serves both task kinds

The remote-worker runner dispatches two task kinds, `implementation` and
`validation` (`AEP_TASK_KIND`), from the same one-shot pod entry point
(`runners/remote-worker/src/oneshot.ts`). Validation authors and executes e2e
specs against the deployed system, which requires Playwright's browser
binaries; Playwright's browsers are glibc-linked and refuse to run on a musl
base, so an Alpine image can never host them — a slimmer, Alpine-based image
for the implementation kind was only possible by giving the two kinds
different images, with the executor selecting between them per kind.

## Decision

**One Debian image, `runners/remote-worker/Dockerfile`, serves both task
kinds.** It bases on `node:22-bookworm-slim` and layers:

- **Go, from a pinned tarball** (`GO_VERSION` build arg), not a distro
  package — the retired Alpine image's Go came from an unpinned `apk add`, so
  the toolchain the agent had verified its build against could silently
  drift under it between rebuilds.
- **Playwright plus a baked Chromium**, pinned as a pair via
  `PLAYWRIGHT_VERSION`: `AEP_PLAYWRIGHT_VERSION` is the contract the
  `aep-validation` skill pins `tests/e2e/package.json` to, so the browsers
  baked into the image always match the version the specs run with, and a
  validation run never downloads a browser at task time.
- **git / gh / curl / bash / python3**, for workspace provisioning, the git
  credential helper, the `gh` wrapper, and the agent's Bash tool — plus
  node/npm from the base image for the agent's compile-level build
  verification (`go build`, `tsc --noEmit`, lockfile resolution) before it
  opens a PR.

The image carries **no container build tooling of any kind**. Dockerfiles the
agent writes or edits are verified only by the post-merge OpenChoreo build,
never inside this pod — an in-pod container-build gate was designed and
prototyped, then dropped: it needed rootless podman with `--isolation=chroot`
plus seccomp and AppArmor unconfined, inside the same pod that already holds
the Anthropic key, the GitHub token, and the publisher secret (ADR-0011
records this from the execution side — a red post-merge build mints a fix
issue and the run's loop handles it).

Because both kinds now resolve to the same image, the executor's per-kind
image selection is retired along with `AGENT_RUNNER_IMAGE`'s old default: the
config loader reads it as an optional string defaulting to empty, and local
dev builds `aep-runner:dev` (`make build-runner`, wired to
`deployments/scripts/build-runner.sh`) rather than pulling a pinned tag from a
personal registry.

## Consequences

- **The implementation kind now carries a payload it never uses.** It ships
  the full Playwright/Chromium toolchain to do compile-level verification
  only. The coding kind's image grows from 1.34 GB to 4.11 GB; total bytes
  shipped across both kinds drop from 5.45 GB (two distinct images) to
  4.11 GB (one image), but every pod — implementation or validation — now
  pulls the larger image.
- **+** One image to build, ship, and import instead of two kept in lockstep
  across five places. `runners/AGENTS.md` records the constraint this
  buys freedom from: do not reintroduce a second, slimmer image without
  moving the release matrix, the Helm values and template, compose, and
  `AGENT_RUNNER_IMAGE` back apart to follow it.
- **+** Removed a supply-chain edge as part of the same collapse:
  `AGENT_RUNNER_IMAGE` no longer defaults to a personal Docker Hub image.

Related: ADR-0011 (no in-pod container build gate, and why), and
[`codingagent/design/oc-job-dispatch.md`](../../services/aep-api/internal/delivery/codingagent/design/oc-job-dispatch.md),
which records where `AGENT_RUNNER_IMAGE` is read and how `AEP_TASK_KIND` and the
deadline differ between the two kinds on one image.
