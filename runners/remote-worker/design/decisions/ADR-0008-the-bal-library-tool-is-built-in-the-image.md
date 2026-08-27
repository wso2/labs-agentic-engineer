# ADR-0008 — The `bal library` tool is built by the image, not committed to it

Status: accepted · 2026-08-24

## Context

`bal library` is not a command on `PATH`. A Ballerina CLI tool installs into the local bala repository
under the user's home, registers itself in `~/.ballerina/.config/bal-tools.toml`, and `bal` dispatches the
`library` verb to it. It is not on Ballerina Central either, so `bal tool pull library` does not resolve
and the image has nothing to pull.

While the tool lived in a repository of its own, the image installed a distribution **checked in** at
`runners/remote-worker/vendor/bal-library-tool/` — a jar plus the two installers — because a fresh clone
of this repo had nothing to build. On 2026-08-24 the tool's source moved into this monorepo at
`packages/bal-library-tool`, which removed that premise. The copy stayed, and the seam it left is the
reason for this ADR.

**The refresh was a step you had to remember, and forgetting it was silent.** Shipping a tool change took
`make vendor-bal-library-tool` (JDK 21, runs `make-dist.sh`, replaces the directory) and *then*
`make build-runner FORCE=1`. Neither implied the other. `build-runner.sh` guarded the directory, but only
for presence:

```bash
if [ ! -f "$VENDORED_TOOL/install.sh" ] || [ -z "$(ls "$VENDORED_TOOL"/*.jar 2>/dev/null)" ]; then
```

That guard cannot see staleness, and the reason is the version. `gradle.properties` pins
`version=0.1.0-SNAPSHOT` and never moves it, so `make-dist.sh` names every build `native-0.1.0-SNAPSHOT.jar`.
A jar built today and a jar built in March have the same name, the same version stamp and nearly the same
size. Nothing in the repo compared them.

**CI made it worse rather than better.** The `bal-library-tool` job in `ci.yml` builds a jar and discards
it; `release.yml` builds the `remote-worker` image from the *committed* directory and never runs Gradle.
So a release could ship a months-old tool while both jobs passed green — and on this branch it would have:
the tool's views were rewritten and the vendored jar predated every line of it.

## Decision

**The image builds the tool.** A first stage compiles it from source, reached as a named build context —
the same mechanism `skills=` already uses, for the same reason: the source is outside this image's context.

```dockerfile
FROM --platform=$BUILDPLATFORM eclipse-temurin:21-jdk AS tool-build
COPY --from=bal-library-tool … .
RUN --mount=type=secret,id=packagePAT,required=true \
    --mount=type=cache,target=/root/.gradle,sharing=locked \
    packageUser=x packagePAT="$(cat /run/secrets/packagePAT)" ./make-dist.sh
```

There is no artifact to refresh, no ordering to remember, and no way to build this image against a tool
that is not this commit's. `make vendor-bal-library-tool` and its script are deleted.

**`--platform=$BUILDPLATFORM`, because the jar is architecture-independent.** This image is released for
`linux/amd64` and `linux/arm64`; unpinned, Gradle would run twice, the second time under QEMU, on an image
whose arm64 half is already measured at 28 minutes.

**`make-dist.sh` and not `gradlew :native:jar`.** It is the one place that decides what a distribution
contains, so a release zip and the image cannot disagree about it.

**The token is a secret mount, never an `ARG` or `ENV`.** `org.ballerinalang:ballerina-cli` is published
only to ballerina-platform's GitHub Packages — verified absent from Maven Central (404, no
`maven-metadata.xml`), `maven.wso2.org` and JitPack — so resolving it needs `read:packages`. In Actions
that is `secrets.GITHUB_TOKEN`, which is sufficient for a cross-org PUBLIC package read and is the
convention across WSO2 and ballerina-platform repos, so there is no bot user or org secret to provision.
The mount form is not a preference: `release.yml` builds this image with `cache-to mode=max`, which
publishes the builder stages to a public GHCR buildcache, so a token in a build arg or a layer would be
published with them. Verified after the change — the token appears in no layer, no image config and no
file, and `/run/secrets` does not exist in the built stage.

**The installer runs inside the image, as `aep`, rather than a bala tree being copied in.** `bal`
version-gates a tool: the bala's `package.json` records the distribution it was installed against, and
`bal` refuses one stamped **newer** than the distribution running it —

```
error: tool 'library:0.1.0-SNAPSHOT' is not compatible with the current
Ballerina distribution '2201.12.3'.
```

— measured directly, by installing into a synthetic `HOME` whose recorded distribution differed from the
installer's. So the stamp has to be written where the tool will run, by the `bal` that will run it, which
also ties the tool to this image's pinned distribution. `aep` and not root because the tool resolves out
of that user's own home and `bal` keeps writing there at task time — its central cache, and
`bal tool pull openapi` appending to the very `bal-tools.toml` this install writes. A root install would
leave the whole tree needing a recursive chown to keep both working.

**The install is smoke-tested.** `install.sh` writes a bala tree and a `bal-tools.toml` entry and exits 0
without asking `bal` whether the result loads. `bal library --help` after it turns a jar/version mismatch,
a tool stamped for the wrong distribution and a missing service registration into a failed build instead of
a failure inside a task pod.

## Consequences

- **A local `make build-runner` now needs a token with `read:packages`.** It previously needed nothing,
  and `setup-aep.sh` calls it, so this reaches local bring-up. `build-runner.sh` accepts `packagePAT`,
  `GITHUB_TOKEN` or `gh auth token`, and fails with the `gh auth refresh` command to run rather than
  letting Gradle report `Username must not be null!` from inside a build stage. This is the cost of the
  decision, taken deliberately over compiling against a distribution's bundled jars — an explicit version
  pin is reproducible where a `fileTree` over whatever is installed is not.
- **`vendor/` is gone, and with it the only committed binary in the repo.** The `LICENSE_MATCH` exclusion
  for `/vendor/` went too; it existed for this directory alone.
- **The playground's jar overlay reads the version from `gradle.properties`.** It read the deleted
  `vendor/…/VERSION`, and a missing file there returns `undefined`, which disables the overlay *silently* —
  every playground run would have used the baked jar with no error. `gradle.properties` is where
  `make-dist.sh` derives it, so the two now agree by construction.
- **`ci.yml` packages rather than just compiling.** `make-dist.sh` is the path release depends on, so it is
  the path CI exercises.
- **The `:ballerina` subproject is deleted.** It published the `.bala` to Central, nothing here consumed
  it, and its Gradle plugin resolves only from that same authenticated repository — so it failed a clean
  build at *configuration* time, before the classpath was even reached. Publishing to Central happens
  upstream. `maven-publish` and `net.researchgate.release` went with it: both were applied, neither was
  configured.
- **Host mode reports instead of repairing.** On a developer's machine `bal` resolves the tool out of
  their own `~/.ballerina`, so there is nothing to overlay. The playground harness and the eval preflight
  ask whether the working-tree jar was **built after** the installed one and name the gap; neither writes
  into someone's home. Deliberately mtime and not a byte comparison: a gradle jar is not
  byte-reproducible (the zip carries entry timestamps), so comparing content calls an unchanged rebuild
  stale and the advice becomes noise nobody reads.
- **The tool's edit-run loop is unchanged and now has a name:** `make bal-library-tool` builds the jar, the
  playground mounts it, no image rebuild.
