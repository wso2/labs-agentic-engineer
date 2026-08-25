# Ballerina Library Tool

A Ballerina CLI tool that reads a package off **Ballerina Central** and answers eight addressed
questions about it. It exists so an AI copilot can learn a package's real signatures instead of
guessing them — and so a human can too.

```bash
bal library --help          # what the tool is, what it can be asked, and how to walk it
bal library client --help   # one verb: its flags, and what its own reader needs to know
```

**The tool documents itself, and there is one copy of each thing it says.** The lists inside `--help`
are rendered from the picocli model rather than written twice (ADR-0012); a flag is described on the
verb that accepts it, and a rule about something a document prints is printed by that document, beside
the thing it is about (ADR-0013). The root text stops at the verb list: it answers *what can I ask,
and how*, while the discipline a caller carries into a lookup — a `// Special Agent Note:` is the
import, a `## Next` block is a pointer, a failure `kind` says whether to retry — belongs to the agent
skill that is in context when it applies (ADR-0022, which narrows ADR-0011). This file deliberately
does not restate the grammar — a second copy on a different release clock is exactly what those
decisions removed.

What follows is what `--help` has no room for: why the tool is shaped this way, and how to build,
iterate on and verify it.

## Why addressed rather than grepped

`ballerinax/github`'s API document is 927KB and 22,829 lines. Reading it by hand costs an agent
several turns and usually ends in a wrong extent. The addressed verbs answer by name or by path
instead, and the numbers are why each one exists:

| | measured |
|---|---|
| `overview` is a bounded MAP | it generates no signature at all, so its size is a property of the design rather than of the package — the eleven-package corpus is **732 lines**, against 2,426 when it carried signatures and 4,168 when it carried the readme too. A byte cap would still have let `ballerina/crypto` emit 20,000 bytes before degrading |
| the map is ordered to survive a pipe | **80% of recorded lookups were piped**, so it leads with facts, quickstart and navigation — a `head -100` reaches `## Next` in 11 packages of 11, against 0 of 11 before (ADR-0017) |
| `guide` is a verb, not a section | the readme was **44% of the entry document and 29% of it was account setup**. A code-only extract was considered and rejected: `googleapis.sheets`' readme is 178 lines with 4 code blocks, so it would discard 85% of it — including the note that `deleteSpreadsheet` needs the Drive API enabled, which no signature implies |
| `client` walks a path tree | github's **903 operations reduce to 36 top-level segments in 445 bytes**, and each level names the next command |
| `client` also addresses names | a remote function has no path, so the same slot takes a name filter — twilio's **200 operations are an index of names, not 62,063 bytes of signatures** |
| three verbs, not one | `client`, `class` and `funcs` split the callable surface by how it is CALLED. Central publishes no `isClient` key, so the split is derived from the grammar: `ballerina/http` has ten clients, two of which Central files as ordinary declarations |
| `-r` stops at the package edge | `http:ConnectionConfig` has a local closure of one and **fifteen** external edges; following them would hide a five-second fetch inside an answer the caller expects to be warm |
| `-r` is bounded and names what it drops | `http:ClientConfiguration` was 38 declarations, 505 lines and 24,183 bytes handed back whole |
| `find` demotes unadopted packages | measured, Central ranks a **one-pull** package fourth for `http client` |
| the payload cache | **8.0s cold, 1.2s warm** on `ballerinax/github` — what makes four precise questions cheaper than one big answer |

Five behaviours worth knowing because no signature shows them:

- **Line one states the document's own length**, in both registers. Piping was measured at 100% of
  sessions and did not respond to being asked not to — and the reason turned out not to be about this
  tool at all: every session that piped had piped a genuinely noisy command (`bal openapi`, `bal tool
  pull`) moments earlier, and the `| head` arrived on `bal library --help` before a byte of any
  document had been seen. A `| head -150` over one github operation's 535-line closure discards 72% and
  ends mid-record; the length makes that arithmetic instead of a guess (ADR-0023).

- **Paths match from the first segment.** An unanchored match for `repos/{owner}/{repo}` would return
  nine operations rather than three, mixing in two unrelated subtrees about team access. The one
  relaxation is a *trailing* segment: `repos/owner/repo/caches` is answered at
  `repos/{owner}/{repo}/actions/caches` when that is the only match, and listed rather than chosen
  when it is not.
- **A wrong guess costs a line, not a round trip.** A verb given another kind's symbol still answers,
  prepending one line naming the canonical verb; a name that is a member rather than a container
  resolves and names its owner. The same tolerance covers spelling: a path segment answers to
  `{owner}`, `owner` and `[string owner]`, to its escaped form (`code\-scanning`, `chat\.postMessage`,
  `'import`) as well as its readable one, and `new` addresses the constructor Ballerina spells `init` —
  measured, that guess cost a round trip in two separate sweeps. These are **input** aliases only: the
  document always prints what the package declares, so a genuinely declared `new` (github's
  `codespaces/'new`) outranks the alias.
- **Versions are never an argument.** The tool walks up from the process's directory for a
  `Ballerina.toml` and reads the version its `Dependencies.toml` locks, so a lookup and a `bal build`
  see the same one. No document discloses the resolution.
- **An unusable cache directory is never a failure** — no byte on stderr, no non-zero exit. Cache
  trouble is not the caller's problem, and failing there would send an agent into the argument-error
  advice in a loop it can never escape.

## Installing It

The tool is **not on Ballerina Central**, so `bal tool pull library` does not resolve it, and this
repository publishes no release of it either — the source moved here on 2026-08-24 and the release
half stayed with the tool's previous upstream, where it no longer builds against this tree. Treat the
zip as a layout rather than as a download: `./make-dist.sh` assembles `dist/` in exactly that shape.

There are three ways it reaches a machine, and which one you want depends on what runs it.

| You are | After a tool change, run | What it does |
|---|---|---|
| developing the tool, or running anything on the **host** (`make eval-bal`, `pnpm play <dir> code --host`) | `./install-local.sh` | Builds the jar and installs it into your `~/.ballerina`, which is where a host run resolves `bal library` from. |
| running the **playground** in docker | `make bal-library-tool` | Builds the jar only. The run bind-mounts it over the image's installed copy, so there is no install and no image rebuild. |
| running a **dispatched / cluster** task | `make build-runner FORCE=1` | Rebuilds the runner image, whose first stage compiles the tool from source and installs it. The build is skipped when the tag already exists, so `FORCE=1` is the part that matters. |

To put a built distribution on a machine that cannot build one — a container image, or a repository
that vendors the tool — run `./make-dist.sh` to assemble `dist/`, copy that directory to the target,
and run the installer beside it there.

**macOS / Linux:**
```bash
./make-dist.sh                 # on a machine with JDK 21 — produces dist/
cd dist && ./install.sh        # on the target
```

**Windows (PowerShell):**
```powershell
# `make-dist.sh` is bash, so dist/ is produced elsewhere and copied here.
Set-Location dist
.\install.ps1
```

Both installers are fully offline and only need `bal` on your `PATH`. The install has to happen
**where the tool will run**: `install.sh` stamps the bala's `package.json` with the distribution the
adjacent `bal` reports, and `bal` refuses a tool stamped newer than the distribution running it — so
a bala tree built on one machine and copied into an image with a different distribution can be
rejected. See `internal-docs/distribution.md`.

## Building from the Source

### Prerequisites

OpenJDK 21 ([Adopt OpenJDK](https://adoptopenjdk.net/) or any other OpenJDK distribution). Set
`JAVA_HOME` to the directory you installed it into.

**And a GitHub token with `read:packages`, exported as `packagePAT`.** This is not optional and it is
worth saying plainly, because this file used to claim the opposite. `org.ballerinalang:ballerina-cli`
— the one dependency that is not on Maven Central — is published *only* to ballerina-platform's GitHub
Packages, which requires authentication even for a public read. Without it Gradle fails with
`Username must not be null!`.

```bash
gh auth refresh -h github.com -s read:packages   # if you use gh
export packageUser="$(gh api /user -q .login)"
export packagePAT="$(gh auth token)"
```

In GitHub Actions no secret has to be provisioned: `packagePAT: ${{ secrets.GITHUB_TOKEN }}` is
sufficient for that cross-org public read, and is the convention across WSO2 and ballerina-platform
repos. The runner image's build stage takes the same token as a BuildKit secret.

The whole dependency is one interface, `io.ballerina.cli.BLauncherCmd`, which `LibraryTool` implements
and `bal` discovers through `META-INF/services`. Everything else — gson, picocli — is on Central.

### Build

```bash
./gradlew :native:jar      # the tool jar (~370KB, our classes only)
./gradlew :native:test     # the suite — 715 cases, offline
./install-local.sh         # build and register as a local bal tool
./make-dist.sh             # the offline distribution: what a release zip and the runner image both use
```

There is no `bala` packaging step. The `:ballerina` subproject that published one to Central was
deleted: nothing here consumed it, and its Gradle plugin resolves only from that same authenticated
repository, so it failed a clean build at *configuration* time before the classpath was even reached.
Both installers build the bala tree by hand.

### Shipping it to something that cannot pull it

Until the tool is on Ballerina Central there is no `bal tool pull library`, so a
consumer — a container image, another repository — has to **copy** it.
`./make-dist.sh` assembles exactly what a release zip carries:

```
dist/  native-<version>.jar  Ballerina.toml  VERSION  install.sh  install.ps1
```

Drop that directory anywhere and run `install.sh`; it needs nothing but `bal` on
`PATH` and touches nothing outside `~/.ballerina`. An unzipped release works the
same way, because it is the same output.

Install it **where the tool will run**, and do not ship a prebuilt bala tree
instead. `install.sh` stamps the bala's `package.json` with the distribution the
`bal` beside it reports, and `bal` refuses a tool stamped newer than the
distribution running it:

```
error: tool 'library:0.1.0-SNAPSHOT' is not compatible with the current
Ballerina distribution '2201.12.3'.
```

## Development Iteration Flow

The inner loop is: change the Java source, rebuild the jar, drop it in place, run `bal library`. The
tool resolves out of the local bala repository, so no Central interaction is involved in the install
itself.

### Apply a change

```bash
./gradlew :native:jar
cp native/build/libs/native-0.1.0-SNAPSHOT.jar \
   ~/.ballerina/repositories/local/bala/ballerinax/tool_library/0.1.0-SNAPSHOT/any/tool/libs/
```

The next `bal library` invocation picks up the new jar. There is no re-registration step and no cache
to clear. The version in both paths is `version` from `gradle.properties`.

After changing `Ballerina.toml`, `BalTool.toml`, the tool id or the package version, do a full
reinstall instead, so the metadata and the `bal-tools.toml` registration are rewritten:

```bash
./install-local.sh
```

### Test

```bash
./gradlew :native:test
```

The suite is offline and hermetic: no network, and no test can reach your real `~/.cache`. When a
rendering change is intentional, regenerate the snapshots and review the diff:

```bash
UPDATE_SNAPSHOTS=1 ./gradlew :native:test              # the report snapshots + usage text
BAL_LIBRARY_UPDATE_KEYSPACE=1 ./gradlew :native:test   # after re-recording the fixtures
```

The 13 `.bal` snapshots have no update switch on purpose. They are the oracle.

It runs at two scales, and the difference matters when you change how something renders:

| | asks | answers |
|---|---|---|
| **the corpus** — `CorpusTest`, `ViewsTest`, `ViewsAgreeTest` | 13 recorded packages, whole documents | *did anything move?* |
| **the constructs** — `constructs/ConstructTest` | one synthetic payload per Ballerina syntax dimension | *which construct moved, and is it now right?* |

A change to how closed records render fails one construct case by name; the corpus reports it as
several thousand lines of snapshot diff across four packages. Both are wanted — the corpus is the
only thing that covers a real package's whole surface, and the constructs are the only thing that
covers a construct no recorded package happens to use. `constructs/Constructs.java` is the table;
read it as a list of claims about the language.

### Coverage

```bash
./gradlew :native:jacocoTestReport      # native/build/reports/jacoco/test/html/index.html
./gradlew :native:check                 # tests + the coverage floor
```

The floor is 80% instruction / 70% branch, set below what the suite reaches so it fails on a
regression and never on an untouched gap. Treat the report as a map of what nothing executes, not as
a score: the suite reaches 93% of instructions, and the known fidelity defects are almost all on
covered lines. `Schema.java`'s reading a class as a bare name runs for every fixture and is why
class declarations come out empty. Coverage cannot see a wrong answer, only an unreached one — which
is the job the construct table does.

## Verification

`./gradlew :native:test` proves the pipeline, and CI runs it plus the coverage floors on every pull
request (`.github/workflows/ci.yml`). It does **not** prove the tool is installed, that `bal` routes to
it, that arguments survive `bal`'s own launcher, that exit codes reach the shell, or that stdout is
clean enough to redirect. Those are only observable through a real `bal library` invocation, so run
this after any change to the CLI.

### Routing and help

```bash
bal library --help                      # usage naming every verb, on stdout; exit 0
bal library                             # same as --help; exit 0
bal library overview --help             # overview's own flags; exit 0
bal library nonsense                    # names every verb; exit 1, one JSON object on stderr
bal library ballerinax/github           # exit 1, suggesting `bal library overview ballerinax/github`
```

### Coordinates the document prints must run

Every command a document hands back is part of the answer, so each of these is checked by RUNNING what
the previous one printed:

`PointersTest` does this offline over every fixture — it extracts every `bal library` command from
every document and re-runs it — so these are the cases it cannot reach: cross-package edges, and the
launcher.

```bash
bal library type ballerinax/sap TargetType -r               # footer names the edge with its version
bal library type ballerina/http Response -r                 # …and that command works verbatim
bal library type ballerinax/aws.s3 ConnectionConfig -r       # a module that is not its own package
bal library type ballerinax/aws.auth AuthConfig              # …readable on its own; the version is resolved
                                                             #    through ballerinax/aws, which contains it
bal library client ballerinax/googleapis.gmail | grep -c 'resource function'   # 32, not 0
bal library client ballerinax/github Client 'repos/*/*' | head -12   # names the branch `*` did not take
bal library client ballerinax/github Client repos/owner/repo/caches  # located one level deeper
bal library type ballerinax/sap Client                       # the client resolves by name
```

### Each verb, live against Central

```bash
bal library find     kafka messaging
bal library overview ballerinax/kafka
bal library overview ballerina/http -s cookie
bal library client   ballerinax/github Client repos
bal library client   ballerinax/twilio 'create*'
bal library client   ballerinax/github Client delete repos/{owner}/{repo}/actions/caches -r
bal library class    ballerina/http Cookie
bal library funcs    ballerina/uuid
bal library type     ballerina/http ClientRequestError -r
bal library guide    ballerinax/googleapis.sheets 2
bal library api      ballerinax/sap
```

Each must exit 0 and print its document to stdout. Two are worth reading rather than just checking:
`type ... -r` must show the `Detail` record and the `distinct` chain — the lookup eight of nine
recorded agent runs came for — and the `caches` line must print the `*ActionsDeleteActionsCacheByKeyQueries`
parameter together with that record's own declaration, which is what makes the call writable in one
call rather than two.

### The stream contract

```bash
bal library overview ballerinax/kafka > /tmp/doc.md 2>/tmp/err.txt
test -s /tmp/doc.md && test ! -s /tmp/err.txt   # document on stdout, nothing on stderr

bal library type ballerina/http NoSuchType 2>/tmp/err.json 1>/tmp/out.txt
test ! -s /tmp/out.txt                          # all-or-nothing: stdout empty on failure
python3 -c 'import json; d=json.load(open("/tmp/err.json")); print(d["kind"], len(d["candidates"]))'
```

### Exit codes, asserted not eyeballed

```bash
check() { local want="$1"; shift; bal library "$@" >/dev/null 2>&1; local got=$?
  [ "$got" = "$want" ] && echo "ok   $* -> $got" || echo "FAIL $* -> $got want $want"; }

check 0 overview ballerinax/kafka
check 0 guide ballerinax/kafka
check 0 client ballerina/http                # several clients is a roster, not a failure
check 0 funcs ballerinax/kafka               # an empty scope is a fact, not a failure
check 0 client ballerinax/github Client zzz  # a selector that matches nothing is answered
check 1 overview no-such-org/no-such-pkg
check 1 overview ballerina/http:2.16.6       # version suffix in the name
check 1 overview ballerina/http -r           # -r belongs to the four verbs that print declarations
check 1 client ballerina/http 2.16.6         # versions are not arguments
check 1 type ballerina/http NoSuchType
check 1 type ballerinax/kafka                # neither a name nor -s
check 1 guide ballerinax/kafka --module nope
check 1 nonsense
```

### Packages outside the corpus

`CorpusTest` covers the thirteen corpus packages offline and deterministically. What it cannot cover is
a payload shape nothing has snapshotted, so a spot check against Central is worth a minute — a
collation or a byte-length mistake moves bytes without breaking anything a unit test asserts.

`ballerina/sql` is the one to keep in this list whatever else changes: its clients are declared
`public type X client object`, so Central files them as ordinary declarations and a reader that trusted
the `clients` array would report a database package as having none.

```bash
for pkg in ballerinax/rabbitmq ballerina/websocket ballerina/sql ballerinax/redis; do
  for verb in overview client class funcs guide api; do
    bal library $verb "$pkg" >/dev/null 2>&1 && echo "ok   $verb $pkg" || echo "FAIL $verb $pkg"
  done
done
```

### The cache, and that it is silent

The cache no longer speaks in `--help` (ADR-0013), so its state is read from timings and from
`DocsCache.describe()` rather than from a status line.

```bash
rm -rf ~/.cache/bal-library
time bal library overview ballerinax/github     # cold
time bal library overview ballerinax/github     # warm — expect a large drop
BAL_LIBRARY_CACHE=off bal library overview ballerinax/kafka                  # still exit 0
BAL_LIBRARY_CACHE_DIR=/dev/null/nope bal library overview ballerinax/kafka   # still exit 0, silent
```

An unusable cache directory must never be a failure: failing there would send an agent into the
argument-error advice in a loop it can never escape.

## Design notes

`internal-docs/system-design.md` describes the architecture, what each module hides, and the two
document registers.

## Contributing to Ballerina

As an open-source project, Ballerina welcomes contributions from the community.

You can also check for
[open issues](https://github.com/wso2/labs-agentic-engineer/issues) that interest you. We look
forward to receiving your contributions.

For more information, go to the [contribution guidelines](https://github.com/ballerina-platform/ballerina-lang/blob/master/CONTRIBUTING.md).

## Code of Conduct

All contributors are encouraged to read the [Ballerina Code of Conduct](https://ballerina.io/code-of-conduct).

## Useful Links

* Chat live with us via our [Discord server](https://discord.gg/ballerinalang).
* Post all technical questions on Stack Overflow with the [#ballerina](https://stackoverflow.com/questions/tagged/ballerina) tag.
