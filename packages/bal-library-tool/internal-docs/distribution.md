# Distribution

How the `bal library` tool reaches the machines that run it, and what is not possible today.

## Status

**Ballerina Central publishing is not wired.** The goal is still that any user can install with
`bal tool pull library`, but nothing here can produce that today, and the gap is structural rather
than a missing credential:

- The `:ballerina` subproject that packaged the `.bala` for Central was **deleted** — it applied
  `io.ballerina.plugin`, which resolves only from ballerina-platform's authenticated GitHub Packages
  and therefore failed a clean build at *configuration* time, before the classpath was reached.
  Nothing in this repo consumed the bala it produced. See
  `runners/remote-worker/design/decisions/ADR-0008-the-bal-library-tool-is-built-in-the-image.md`.
- The release workflow that once cut a zip lives in the tool's **previous upstream repository**, not
  here. It builds with `-PlsVersion=…` and expects a `.bala` under `ballerina/build`; neither exists
  in this tree, so tagging there against a synced source would fail at the build step.

So there is currently **no way to cut a release of this tool from anywhere**. That is fine for the
platform — the runner image compiles the tool from source, so it never needed a release — but it does
mean the "Install a Released Version" path in the README is gone, and reinstating it is a decision
someone has to take rather than a script someone has to run.

Open questions if Central publishing is revived:
- publishing under the `ballerinax` org on Ballerina Central
- whether the tool is officially supported or community-maintained
- versioning and release cadence — `gradle.properties` pins `version=0.1.0-SNAPSHOT` and nothing
  moves it, which is safe only because no artifact is addressed by that version today

## How Ballerina tool distribution works

Ballerina tools are published as `.bala` packages to Ballerina Central. The tool JAR must be bundled
inside the `.bala` under `tool/libs/`.

```
<org>/<name>/<version>/<platform>/
├── Ballerina.toml
├── package.json
└── tool/libs/
    └── native-<version>.jar
```

Once published, users would install with `bal tool pull library` and remove with
`bal tool remove library`. The local installers below write this same tree by hand, into the local
bala repository rather than a Central-backed one.

## The two install paths that DO work

Both derive the version from `gradle.properties`, which is the only place it is written.

### `install-local.sh` — the developer loop

Builds the native JAR and installs it into `~/.ballerina/repositories/local/bala`, registering
`[[tool]] id = "library"` with `repository = "local"` in `~/.ballerina/.config/bal-tools.toml`.

```bash
./install-local.sh
```

This is what host runs resolve — `make eval-bal` and `pnpm play <dir> code --host` execute the tool out of your
own home, so a working-tree jar is invisible to them until this script copies it in. It copies
without `-p`, deliberately: the installed jar's mtime is when it landed, which is what lets the
playground and the evals detect a stale install by comparing mtimes.

Note it `rm -rf`s every installed version of the tool first, so it will remove a copy installed any
other way.

### `make-dist.sh` — for a consumer that has to copy it

Assembles `dist/` — the jar, `Ballerina.toml`, `VERSION`, and both installers — with no network
beyond the dependency resolution the build itself needs.

```bash
./make-dist.sh
cd dist && ./install.sh     # on the target machine, in the target image
```

This is the answer for anything that cannot `bal tool pull library`: a container image, or another
repository that vendors the tool. It is the ONE place that decides what a distribution contains, so
the runner image and any future release zip cannot disagree about it — which is why the runner image
runs this script rather than `gradlew :native:jar`, and why CI runs it too (`ci.yml`, the
`bal-library-tool` job) instead of only compiling.

The install must happen **where the tool will run**. `install.sh` writes `package.json` with the
distribution the local `bal` reports, and `bal` rejects a tool stamped newer than the distribution
running it — so a bala tree built on one machine and copied into an image with a different
distribution can be refused. That is also why the image installs by running this script rather than
by copying a prebuilt bala tree in.

## How the platform ships it

The runner image builds the tool itself. `runners/remote-worker/Dockerfile`'s first stage reaches the
source through the `bal-library-tool` **named build context**, runs `make-dist.sh`, and the final
stage runs the resulting `install.sh` as the `aep` user, followed by `bal library --help` as a smoke
test. There is no artifact to refresh and no ordering to remember: a build cannot use a tool that is
not this commit's.

Every build path must pass both that named context and a `packagePAT` **secret** — never a build arg,
because the release workflow publishes builder stages to a public buildcache. The three paths are
`deployments/scripts/build-runner.sh`, `.github/workflows/release.yml`'s matrix row, and
`runners/remote-worker/local/run-local.sh`.

The playground does not rebuild the image for a tool change: it bind-mounts the working-tree jar over
the image's installed copy, aiming the mount with the version from `gradle.properties`.

## Verification

```bash
# 1. Test
./gradlew :native:test

# 2. Coverage floors (the same gate CI applies)
./gradlew :native:jacocoTestCoverageVerification

# 3. Package the distribution — the path the image depends on
./make-dist.sh

# 4. Install locally
./install-local.sh

# 5. Smoke test — see README.md "Verification" for the full protocol
bal library --help
bal library overview ballerinax/kafka
bal library type ballerina/http ClientRequestError --deps
```

## Notes

- The tool JAR contains ONLY our own classes. `gson`, `picocli` and the CLI launcher are on the
  Ballerina distribution's runtime classpath (`bre/lib`), so they are declared `compileOnly` and
  nothing third-party is redistributed. HTTP is `java.net.http` from the JDK.
- Building needs a GitHub token with `read:packages`, because `org.ballerinalang:ballerina-cli` is
  published only to ballerina-platform's GitHub Packages — absent from Maven Central,
  `maven.wso2.org` and JitPack. In Actions a repo-scoped `GITHUB_TOKEN` suffices, so there is no bot
  user or org secret to provision.
- Nothing needs to be bumped when anything upstream releases. The one coupling left is to the
  distribution's own versions of `picocli` and `gson`, so no code here may rely on a feature newer
  than what `bre/lib` ships.
- The SPI entry at `META-INF/services/io.ballerina.cli.BLauncherCmd` wires `LibraryTool` as the
  `bal library` command handler.
