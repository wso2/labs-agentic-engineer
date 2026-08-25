# bal-library-tool — System Design & Architecture

## Context

`bal library` reads a Ballerina package off **Ballerina Central's docs API** and answers eight addressed
questions about it. It talks to Central over HTTPS and to nothing else: no language server, no bundled
search index, no compiler invocation, no local package resolution.

The tool jar contains only our own classes — about 250KB. Everything it needs at run time (`gson`,
`picocli`, the CLI launcher) is already on the Ballerina distribution's classpath, so the dependencies
are `compileOnly` and no third-party code is redistributed.

---

## Commands

```bash
bal library find     <keywords...>                              # Central registry search
bal library overview <org/name>                        [-s <q>]  # a map of the package
bal library client   <org/name> [<Name|selector>...]   [-s <q>] [-r]
bal library class    <org/name> [<Name|member>...]     [-s <q>] [-r]
bal library funcs    <org/name> [<name|prefix*>...]    [-s <q>] [-r]
bal library type     <org/name> <Name>...              [-s <q>] [-r]
bal library guide    <org/name> [<n|title>] [--module <name>] [-s <q>]
bal library api      <org/name>

  --refresh · -h                          # every verb that reads a package
  --all                                   # hidden: ignores the byte budget, offered only in `## Next`
```

**Verb-first, and no implicit default.** A verb has no `/`, so a stale binary fails it against the
qualified-name pattern as `validation`, which is loud and names the verbs it does know. A verb placed
*after* the package used to land in the version slot and come back as `package-not-found` — which the
skill teaches means "retry"; there is no version slot any more, so a trailing verb is now an
unexpected positional.

**Three verbs for the callable surface, split by how a symbol is CALLED.** `client` addresses what is
reached with `->`, `class` what is reached with `.`, `funcs` what needs no receiver. The split is
DERIVED, not read off the payload: Central publishes no `isClient` key and its `clients` array is not
the callable surface — `ballerina/http` declares ten clients, two of which (`ClientObject`,
`StatusCodeClientObject`) it files as ordinary declarations, and for `ballerina/sql` the array is empty
while both of its clients live there. `symbols/Surface` is the one place that partition is computed.

**A wrong verb costs a line, not a call.** Any of the three answers for a symbol of another kind,
prepending one line that names the canonical verb; a name that is a member rather than a container
resolves and names its owner. Without that, a kind-specific verb would make every guess a wasted round
trip — which is what makes the split safe rather than merely tidier.

**Versions are internal.** There is no version syntax in the grammar and no document discloses the
resolution. `LibraryTool` walks up from the process's directory for a `Ballerina.toml`, and
`Dependencies.toml` beside it pins the version — including transitively, so a cross-package pointer
needs no version argument either.

---

## Architecture

```
                       argv
                        │
        ┌───────────────▼────────────────┐
        │  cli/LibraryTool               │  BLauncherCmd. THE ONLY class that reads the
        │  (the process wrapper)         │  environment or exits the process.
        └───────────────┬────────────────┘
                        │  argv + streams + cache, all injected
        ┌───────────────▼────────────────┐
        │  cli/Cli + cli/Commands        │  picocli grammar → one verb → one document.
        │  cli/Usage                     │  Returns an exit code; never calls System.exit.
        └───────────────┬────────────────┘
                        │
        ┌───────────────▼────────────────┐
        │  Loader.loadPackage            │  resolve a version, then read it ONCE.
        └───────┬────────────────┬───────┘
                │                │
   ┌────────────▼─────────┐   ┌──▼──────────────────────────┐
   │ central/CentralClient│   │ cache/DiskCache             │
   │ THE ADAPTER          │◄──┤ raw payload, keyed by        │
   │ retry · Retry-After  │   │ coordinates. Never throws,   │
   │ budget · 400-vs-404  │   │ never reports.               │
   │ offline fallback     │   └─────────────────────────────┘
   └────────────┬─────────┘
                │  untyped JSON
   ┌────────────▼─────────────────┐
   │ central/schema/{CentralDocs,  │  the ONE description of what Central sends.
   │                 Schema}       │  Read fields required; unknown keys stripped.
   └────────────┬─────────────────┘
                │  typed
   ┌────────────▼──────────────────────────────┐
   │ model/{FromCentral, Patches} → Library     │  the IR: sealed hierarchies, no flag bags.
   └────────────┬──────────────────────────────┘
                │
   ┌────────────▼────────────┐   ┌────────────────────────┐
   │ symbols/{Declarations,   │   │ render/{Signatures,     │
   │  Names, PathTree,        │   │  TypeDefs, Documents,   │
   │  Surface, Filter}        │   │  Report} the SHARED     │
   │  indexes + the partition │   │  renderer               │
   └────────────┬────────────┘   └───────────┬────────────┘
                │                             │
                └──────────┬──────────────────┘
   ┌───────────────────────▼──────────────────────────────┐
   │ views/{Find, Overview, Containers, TypeView, Guide}  │
   │ one function of a loaded package each                │
   │ views/Closure — the type walk both `-r` paths share  │
   └──────────────────────────────────────────────────────┘
```

Every arrow points one way and no stage mutates its input.

---

## The two registers

A document either **is** Ballerina or it **describes** a package. Blending the two produces things that
look like declarations and are not, which invites an agent to transcribe from a summary.

| Register | Documents | Rules, enforced by `RegisterTest` |
|---|---|---|
| Code | `type`, `api`, **and any `-r` response** | No fences of our own, no report marker, no Markdown tables. A `//` comment annotates a real declaration. |
| Report | `find`, `overview`, `client`, `class`, `funcs`, `guide` | Ballerina only inside ` ```ballerina ` fences. No bare `//`. Structure is headings. Opens with `<!-- bal library <verb> v1 -->`. |

The register is a property of the **document**, not of the verb (amending ADR-0008). A `-r` response is
nothing but declarations, so it is pasteable whole even when a report verb reached it — which means the
three container verbs each produce documents in both registers and each obeys the rules of the one it
is in.

`Report` is the only way a report document is built, which is why those rules hold for documents nobody
has written yet.

**Both registers state the document's own length on line one** — `<!-- bal library overview v1 · 42
lines -->` and `// ballerinax/github:6.0.0 · 535 lines`. Stamped by `Documents.withLength` at the single
point in `Cli` that every document passes through on its way to stdout, so a view added later inherits it
and the renderers stay untouched (which is why the committed `.bal` snapshots did not move when this
landed). It is the only defence the tool has against a caller's filter: piping was measured at 100% of
sessions and did not respond to prose, and a `| head -150` over a 535-line closure says nothing about the
385 lines it dropped. See ADR-0023.

**The split also decides what goes WHERE, not only how it is spelled.** A declaration in the code register is
something to copy, so anything that does not compile stays out of it and the gap gets named instead (ADR-0010):
a service template is written only for a type the listener's `attach` names, and a `configurable` — which is
module-private, so a caller cannot reference it — is NAMED by `api` in comments rather than declared by it. That
last one moved: `overview` used to carry it as a `Config.toml` fragment and stopped (ADR-0017), because that
section addressed a deployer rather than the reader writing a `.bal` file. `type` cannot reach a configurable at
all, so `api` is where the fact lives or nowhere does. A module-level `public final` variable goes the other way: it IS referenceable, so it is
a declaration and prints with the initialiser its own source writes.

---

## What each module hides

| Module | Depth |
|---|---|
| `central/CentralClient` | **The adapter.** The only module that can fail for reasons outside the process: retry policy, `Retry-After` in both legal forms, jittered backoff, a wall-clock budget, Central answering an unpublished package with 400 rather than 404, and the offline fallback. |
| `central/HttpTransport` | The seam every retry test drives. Transport trouble is a value, not an exception, so "503 then 200" is two records and no socket. |
| `central/schema/Schema` | The one place untyped JSON is touched. Collects EVERY mismatch before failing, because the person reading a drift failure is about to extend the schema. |
| `cache/DocsCache` | **The interface is the test surface.** `DocsCache.NULL` is what keeps every other test hermetic — no test can reach a developer's real `$HOME`. Two implementations make it a real seam. Nothing here may throw or report. |
| `model/FromCentral` | Deepest module in the tool: Central's flag-bag encoding is decided **once**, and nothing downstream ever sees `isResource`, `isAnonymousUnionType` or `inclusionType`. |
| `model/Patches` | Three per-package corrections, for names Central OMITS. Was eight; ADR-0007 sets the admission bar and five did not clear it. |
| `render/Report` | The report register, and the one place that decides how declarations are separated inside a fence — so `ballerinaBytes` is what a header sizes a block with, rather than a second derivation of the same number. |
| `render/Signatures` | The **shared** renderer. Views and `api` agree structurally because both call `renderMemberFunction`; `ViewsAgreeTest` asserts it because the cheap way to break it is for a view to hand-roll a line. `Detail` is the one axis on which they differ — the registers print a declaration's `# +` parameter rows and the compact views do not (ADR-0008). |
| `model/ModuleRef` | A foreign reference's three derivations, which one formatted string kept conflating: the import path (keyword segments quoted), the CLI coordinate (which rejects the quote), and whether an import is needed at all. The pre-declared langlib set is measured with the compiler, not inferred from `lang.*` (ADR-0009). |
| `symbols/PathTree` | Anchored, tolerant path matching. Anchoring is the correctness property: an unanchored match for `repos/{owner}/{repo}` on github returns nine operations rather than three — and a wildcard names every branch it also matched, because `*` takes the busiest one and `--all` promises completeness. `locate` is the ONE relaxation: a *trailing* segment is looked for under the prefix that matched, answered when there is exactly one and listed when there are several, so `repos/owner/repo/caches` reaches `.../actions/caches` without reintroducing suffix matching. |
| `symbols/Surface` | **The partition the three container verbs address**, by derived role rather than by Central's filing. Exhaustive and disjoint over every object a package declares, which `SurfaceTest` asserts in both directions — an object in neither verb is unreachable, one in both is a document that says two different things about one name. |
| `symbols/Filter` | `-s`, as a linear scan over the package already in memory: no index, no second cache tier. Two tiers of RESULT, though, and that is the design — a surface match is rendered and a documentation-only match is named. Measured on github, `upload` is 7 against 12 and `pagination` is 0 against 14, so rendering both buries the first set and dropping the second loses the query shape a caller uses when they know the capability and not the vocabulary. |
| `views/Closure` | `-r`, from a declaration OR a signature. Breadth-first and bounded at 20,000 bytes with an omission list, because a shallow field is likelier needed than a four-levels-deep one and `http:ClientConfiguration` was 24,183 bytes unbounded. Starting from a signature is what makes the common flow one call: github's caches DELETE names `*ActionsDeleteActionsCacheByKeyQueries`, whose fields ARE the call's named arguments. |
| `views/Containers` | One implementation behind `client`, `class` and `funcs`. Holds the resolution order (exact container → exact member in scope → another scope → substring member), the byte budgets and the tier ladder. The parser cannot be decided per verb: a client IS a class, so the selector grammar is read off what the resolved CONTAINER declares. |
| `Loader` | `loadPackage` is the only load, so no verb is cheap because it skipped work another verb does. |
| `cli/Cli` | argv → exit code, with streams, transport and cache injected so tests drive the real command. |
| `cli/LibraryTool` | The process wrapper — the only place that reads the environment or exits. |

---

## The contract

| | |
|---|---|
| stdout | the requested document, and nothing else — including the usage text, when `--help` is what was asked |
| stderr | on failure, one JSON object matching `Failure`, and nothing else |
| exit 0 | success, and stdout is **complete** |
| exit 1 | every failure, whatever went wrong |

**One failure code, and the `kind` is the branch** (ADR-0015). `upstream` and `timeout` are the two
worth re-running unchanged; `validation`, `package-not-found` and `symbol-not-found` need a different
command, which the `suggestion` names; `schema-drift` is for a human. None of them is a licence to
guess a signature.

`type` is **all-or-nothing** across names: if any one fails, stdout gets nothing, because "exit 0 means
stdout is complete" is what every redirecting caller relies on.

---

## The cache

```
<root>/v1/docs/<org>/<name>/<version>.json      mode 0600, no TTL
<root>/v1/latest/<org>/<name>.json              {"version":"6.0.0","atMs":…}
```

What is cached is the **raw payload**, not the IR and not the rendered string — the payload is not
derived from our code, so the coordinates are the whole key. Measured: `overview ballerinax/github`
goes from **8.0s cold to 1.2s warm**, which is what makes four addressed questions cheaper than one
big answer.

Location is a pure function of the environment (`cache/CacheLocation`), tried in order:

1. `BAL_LIBRARY_CACHE=off` — explicit opt-out
2. `BAL_LIBRARY_CACHE_DIR=<dir>` — explicit location, no fallback
3. `$XDG_CACHE_HOME/bal-library` when absolute
4. `~/.cache/bal-library` — the default
5. `<tmpdir>/bal-library-<user>`, mode 0700
6. disabled

**Any** problem with an entry is a miss, never a failure: missing, unreadable, truncated, not JSON,
rejected by the schema, or coordinates that do not match its own path. Each of those drops the entry
and uses the network, so a corrupt entry cannot produce a wrong document and heals on the next fetch.

Writes go to a per-process temp file and are then moved atomically. No lock and no single-flight: two
processes that miss the same package both fetch and both move, the content is equivalent, and no third
process can observe a partial file.

---

## Project structure

```
bal-library-tool/
├── build.gradle              ← root: plugins + allprojects repos
├── settings.gradle           ← includes ':native', and nothing else
├── gradle.properties         ← all versions
├── install-local.sh          ← local dev install
├── build-config/resources/package/{Ballerina,BalTool}.toml   ← templates
└── native/
    ├── build.gradle          ← Java subproject; every dependency is compileOnly
    └── src/
        ├── main/java/io/ballerina/library/
        │   ├── Result.java, Failure.java          ← sealed; failures are values
        │   ├── QualifiedName.java, Version.java   ← parser-only construction
        │   ├── Texts.java                         ← collation, byte length, counts
        │   ├── Loader.java, LoadedPackage.java
        │   ├── cache/{DocsCache,DiskCache,CacheLocation,Versions}.java
        │   ├── central/{CentralClient,HttpTransport,JdkHttpTransport,HttpOptions,
        │   │            Coordinates,DependenciesToml,SearchHit,Json}.java
        │   ├── central/schema/{CentralDocs,Schema}.java
        │   ├── model/{Library,TypeDef,Fn,Service,TypeRef,Param,ReturnDef,RecordField,
        │   │          ClientClass,FromCentral,Patches,Defaults,Pipeline,ModuleRef}.java
        │   ├── render/{Signatures,TypeDefs,Documents,Identifiers,Report}.java
        │   ├── symbols/{Declarations,Names,PathTree,Surface,Filter}.java
        │   ├── views/{Find,Overview,Containers,TypeView,Guide,Closure,
        │   │            Snippets,Readmes}.java
        │   └── cli/{LibraryTool,Cli,Commands,Usage,UsageRenderer}.java
        └── test/
            ├── java/io/ballerina/library/    ← 17 suites, 715 cases
            └── resources/
                ├── fixtures/*.json.gz        ← 13 recorded Central payloads
                ├── snapshots/                ← 13 .bal + 50 .md + keyspace.txt
                └── command-outputs/unix/     ← usage golden files
```

---

## Dependencies

Every dependency is `compileOnly` — all three are on the Ballerina distribution's runtime classpath
(`bre/lib`), so the jar bundles nothing:

| Dependency | Purpose |
|---|---|
| `org.ballerinalang:ballerina-cli` | `BLauncherCmd` interface |
| `info.picocli:picocli` | argument parsing and per-verb flag rejection |
| `com.google.code.gson:gson` | JSON parsing and emission |

HTTP is `java.net.http` from the JDK. No GitHub Packages credentials and no language server version
are needed to build.

The cost is version coupling to the distribution (`picocli 4.0.1` is from 2019), so nothing here may
rely on a feature newer than the version in `bre/lib`. One place that bites: picocli only gained
`setUnmatchedOptionsAllowedAsOptionParameters` in 4.4, so `Cli.rejectFlagShapedValues` does that check
by hand rather than adding a bundled dependency.

---

## Tests

`./gradlew :native:test` — 715 cases, offline, no network and no `$HOME` access.

| Suite | What it holds the line on |
|---|---|
| `CorpusTest` | **The one that pins the rendering.** Thirteen recorded payloads render byte-for-byte to thirteen committed `.bal` snapshots. Those snapshots are also the interface redesign's own gate: they did not move by a byte, which is what proves the renderer was never touched. |
| `ViewsAgreeTest` | **The one that makes the addressed verbs safe.** Every signature a container verb prints appears in the `api` snapshot verbatim, at every tier and under `--all`; `overview` generates none at all; every `type <Name>` body is `renderTypeDef` exactly; every offered path is reachable; every `-r` closure terminates, stays bounded and names what it dropped. |
| `PointersTest` | **The general form of "a pointer that cannot answer is worse than no pointer".** Extracts every `bal library` command from every document of every fixture and RUNS it through the real CLI, requiring exit 0. Three separate bugs of this shape had three separate assertions written after the fact; a new pointer cannot be added wrong. |
| `SurfaceTest` | That the three-way split is exhaustive and disjoint, and that a `client object` type Central files as an ordinary declaration is still addressed by `client`. |
| `ViewsTest` | The report snapshots, and the composition rules that decide their shape — what the quickstart quotes and in what order, where the tier ladder fires, that the errors the map names resolve through `type`. Also where path-tree ORDERING is pinned: a locale collator, not `String::compareTo`, which disagree on real github segments. |
| `RegisterTest` | The two-registers rule, mechanically, over every fixture and every verb. |
| `CliTest` | Parsing, streams and exit codes together, in-process against a recorded payload. |
| `CacheTest` | Every corruption mode falls through to the network silently; TTL boundaries; concurrency; the offline fallback. |
| `ClientTest` | Which failures are worth retrying, which are answers, and what each costs the caller. |
| `SymbolsTest` | Carries the **discovery corpus** — every lookup the nine recorded playground runs made, pinned with hit counts, so a zero-hit pin cannot masquerade as a working index. |
| `KeySpaceTest` | The payload's whole key space per fixture: the live drift detector for fields the reader does not read yet. |
| `PatchesTest` | Each correction pinned in BOTH directions — what it must change and what it must leave alone. |
| `LibraryToolTest` | The usage text as golden files, and that `bal` routes every verb. |

Snapshot escape hatches, both narrow and deliberate:

```bash
UPDATE_SNAPSHOTS=1 ./gradlew :native:test              # after an intentional rendering change
BAL_LIBRARY_UPDATE_KEYSPACE=1 ./gradlew :native:test   # after re-recording fixtures
```

`./gradlew :native:test` does **not** prove the tool is installed, that `bal` routes to it, that
arguments survive the launcher, or that exit codes reach the shell. Those are only observable through a
real `bal library` invocation — see **Verification** in `README.md`.

---

## Build & install

```bash
./install-local.sh        # build the jar and register it as a local bal tool
./gradlew :native:test    # the suite
./gradlew :native:jar     # just the jar
```
