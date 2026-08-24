# Distribution Plan

## Goal

Publish the tool to Ballerina Central so any user can install it with:

```bash
bal tool pull library
```

## Status

Pending — needs discussion with the Ballerina team regarding:
- Publishing under `ballerinax` org on Ballerina Central
- Whether the tool should be officially supported or community-maintained
- Versioning and release cadence

---

## How Ballerina tool distribution works

Ballerina tools are published as `.bala` packages to Ballerina Central. The tool JAR must be
bundled inside the `.bala` under `tool/libs/`.

```
<org>/<name>/<version>/<platform>/
├── Ballerina.toml
├── package.json
└── tool/libs/
    └── native-<version>.jar
```

Once published, users install with:
```bash
bal tool pull library
bal tool pull library:0.1.0    # specific version
```

And uninstall with:
```bash
bal tool remove library
```

---

## What needs to be done

The `ballerina/` subproject uses `io.ballerina.plugin` for bala packaging. All Ballerina.toml and
BalTool.toml files are generated from templates at build time — no manual edits needed.

### 1. Build and pack the bala

```bash
./gradlew clean build
```

No credentials to export: every dependency is on the Ballerina distribution's own classpath.

This runs `updateTomlFiles` (generates `ballerina/Ballerina.toml` and `ballerina/BalTool.toml`)
and `bal pack` inside the `ballerina/` subproject, producing the `.bala` artifact.

### 2. Push to Ballerina Central

```bash
bal login
cd ballerina && bal push
```

### 3. User installation (after publish)

```bash
bal tool pull library
bal library search kafka messaging
bal library overview ballerinax/kafka
```

---

## Local installation options

### Option A — `install-local.sh` (quick, no jballerina-tools download)

Builds the native JAR and installs directly into `~/.ballerina/repositories/local/bala`.
Fastest option for iterating on local changes.

```bash
./install-local.sh
```

### Option B — `make-dist.sh` (for a consumer that has to copy it)

Assembles `dist/` in the release zip's exact layout — jar, `Ballerina.toml`,
`VERSION`, and both installers — with no network and no jballerina-tools download.
This is the answer for anything that cannot `bal tool pull library`: a container
image, or another repository that vendors the tool.

```bash
./make-dist.sh
cd dist && ./install.sh     # on the target machine, in the target image
```

The install must happen **where the tool will run**. `install.sh` writes
`package.json` with the distribution the local `bal` reports, and `bal` rejects a
tool stamped newer than the distribution running it — so a bala tree built on one
machine and copied into an image with a different distribution can be refused.
The release workflow — still in the tool's upstream repository, which is where
releases are cut — stages the zip from this same script, so a published release
and a vendored copy are the same bytes.

### Option C — Gradle full build with local Central

Uses `io.ballerina.plugin` to produce the bala and install it via the local Central registry.
Requires downloading jballerina-tools (~400 MB, cached after first run).

```bash
./gradlew clean build -PpublishToLocalCentral=true
```

---

## Verification

```bash
# 1. Run tests
./gradlew :native:test

# 2. Build
./gradlew clean build

# 3. Install locally
./install-local.sh

# 4. Smoke test — see README.md "Verification" for the full protocol
bal library --help
bal library overview ballerinax/kafka
bal library type ballerina/http ClientRequestError --deps

# 5. Push to Central
cd ballerina && bal push

# 6. On a fresh machine
bal tool pull library
bal library search kafka messaging
bal library overview ballerinax/kafka
```

---

## Notes

- The tool JAR is ~250 KB and contains ONLY our own classes. `gson`, `picocli` and the CLI
  launcher are on the Ballerina distribution's runtime classpath (`bre/lib`), so they are declared
  `compileOnly` and nothing third-party is redistributed. HTTP is `java.net.http` from the JDK.
- Nothing needs to be bumped when anything upstream releases. The one coupling left is to the
  distribution's own versions of `picocli` and `gson`, so no code here may rely on a feature newer
  than what `bre/lib` ships.
- `ballerina/Ballerina.toml` and `ballerina/BalTool.toml` are generated — do not edit them directly;
  edit the templates in `build-config/resources/package/` instead
- The SPI entry at `META-INF/services/io.ballerina.cli.BLauncherCmd` wires `LibraryTool` as
  the `bal library` command handler
