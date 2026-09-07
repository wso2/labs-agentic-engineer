# Developer guide — cutting a release

Releases are cut by dispatching the **Release** workflow
(`.github/workflows/release.yml`) by hand. Nothing releases on merge.

## Inputs

| Input | Meaning |
|---|---|
| `version` | No leading `v` — e.g. `0.6.0`. Becomes the image tag, the chart version, and the release tag. |
| `component` | `platform` (images + charts), `ctl` (the `aep` CLI binaries), or `all`. |

## What it produces

**`platform`** — one multi-arch image per entry in the `build-push-images`
matrix at `ghcr.io/wso2/aep/<name>:<version>` (and `:latest`); then the
`bootstrap` and `platform` Helm charts, version-stamped and pushed to
`oci://ghcr.io/wso2/aep/charts`; then a `platform/v<version>` GitHub release.

**`ctl`** — `aep` binaries for five GOOS/GOARCH pairs, attached to a
`ctl/v<version>` GitHub release.

Images are built for `linux/amd64` and `linux/arm64`. Builder stages that can be
architecture-independent are pinned to `$BUILDPLATFORM` so they run once,
natively, instead of under QEMU — read the note in `services/aep-api/Dockerfile`
before removing a pin. `remote-worker` is the notable exception: it installs a
per-arch Go toolchain, Ballerina, and Playwright browsers, so its arm64 half
genuinely is emulated and it is the slowest job in the release. One consequence:
chromium cannot run under QEMU, so the runner Dockerfile's browser smoke test
runs only on native builds. The release asserts the arm64 image's browser
launch nowhere; native arm64 builds (every Apple-silicon bring-up) do. The
emulated arm64 build itself is exercised before a release by the `Images`
workflow, which builds `remote-worker` for both platforms on its PR.

Layer cache lives in GHCR under `ghcr.io/wso2/aep/buildcache/<image>` rather than
the Actions cache, which is capped at 10 GB per repository and which CI's own
entries already fill. It is `mode=max`, so intermediate build stages are
published — **no build stage may hold a secret**.

## Recovering a failed release

**Re-run the failed jobs of the existing run rather than dispatching a fresh
one.** A re-run keeps the original inputs, rebuilds only what broke, and leaves
the images that already published alone.

```bash
gh run rerun <run-id> --repo wso2/labs-agentic-engineer --failed
```

Tags are overwritten, so re-running over a partially-published version is safe.

Note that each matrix leg moves its own `:latest` as it finishes, so a release
that fails partway can leave `latest` pointing at the new version for the images
that got through and the previous one for the rest. Completing the release
realigns them.

### When the failure is not ours

Two signatures mean GitHub Actions itself is degraded, not the build. Check
[githubstatus.com](https://www.githubstatus.com) before investigating:

- `Failed to resolve action download info. Error: Service Unavailable` — the
  service that resolves `uses:` references is down; the job dies before its
  first step.
- `The job was not acquired by Runner of type hosted even after multiple
  attempts` — no runner was ever assigned. The job shows zero steps and an empty
  runner name, having sat queued for up to a couple of hours.

Neither is fixable from this repository. Wait for the incident to clear, then
re-run the failed jobs.
