# `bal library` tool — vendored distribution

**Generated. Do not edit by hand.** Version `0.1.0-SNAPSHOT`, assembled by the tool's own
`make-dist.sh` and copied here by `deployments/scripts/vendor-bal-library-tool.sh`.

```bash
make vendor-bal-library-tool     # refresh this directory
make build-runner FORCE=1        # then rebuild the image that installs it
```

The runner image installs this with `install.sh`, the tool's own offline installer,
so the bala's `package.json` records the distribution of the `bal` in the image —
`bal` refuses a tool stamped with a newer distribution than the one running it,
which is why a prebuilt bala tree is not what gets checked in.

Why a copy and not a dependency: the tool lives in its own repository and is not on
Ballerina Central, so there is nothing for a fresh clone to build and nothing for the
image to pull. See
[ADR-0006](../../design/decisions/ADR-0006-the-bal-library-tool-is-vendored.md).
