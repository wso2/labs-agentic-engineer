# ADR-0001 — Read Ballerina Central directly instead of wrapping the language server

**Status:** Accepted

## Context

`bal library` began as a thin wrapper around the Ballerina Language Server's copilot library
functionality. Two verbs, `search` and `get`, each delegating one call to `CopilotLibraryManager`. Our
own code was three classes totalling 7,487 bytes of class files; everything of substance came from the
LS fat jar.

That shape had three problems, and none of them was fixable inside it.

**It did not work.** `bal library get ballerina/http` logged SEVERE `no such column: pmt.packageInfo`,
`f.optional` and `a.type_constraint` from `ServiceDatabaseManager` — the bundled index schema disagreed
with the bundled query code. The failure was in the dependency, not in the wrapper.

**It redistributed third-party code.** The tool jar was 24,929,770 bytes, of which 41.9 MB uncompressed
was two SQLite indexes (`search-index.sqlite` 20.5 MB, `central-index.sqlite` 21.4 MB) plus LS classes
and a SQLite JDBC driver with native libraries for five platforms. Publishing that raises a licensing
question before it raises a technical one.

**It could not be pinned.** The LS version was unpinned, so a build silently picked up whichever
language server had released most recently — which could change the bundled classes and the index data
underneath us.

Meanwhile a separate reader existed that answered the same questions off **Central's docs API**, in
TypeScript, with a nine-package recorded fixture corpus and byte-exact snapshots.

## Decision

Keep `bal-library-tool`'s shape as a `bal` tool and replace its entire implementation: stop talking to
the language server, start talking to Central.

The port is a change of language and delivery vehicle, **not a change of contract**. The recorded
payloads and the Ballerina they render to are language-agnostic by construction — a recorded HTTP
response and its rendering — so they came across as the oracle rather than as expectations rewritten to
match whatever the new code produced.

### Feasibility was measured, not assumed

Eight questions had to be answered before this was worth starting. Each was run against `bal` 2201.13.2:

| Question | Result |
|---|---|
| Can a `bal` tool set process exit codes? | `System.exit(2)` inside `execute()` reaches the shell as 2 |
| Does `bal` swallow the flag vocabulary? | All ten arguments of a realistic invocation arrive raw |
| Are stdout and stderr separable? | Yes; the document redirects cleanly |
| Is there heap for a 12.4 MB payload? | `bal` launches `-Xms256m -Xmx2048m`; peak used was 188 MB |
| How slow is JSON at that size in Java? | gunzip 58 ms, parse 119 ms, streamed 176 ms |
| What does a tool invocation cost before any work? | 0.42–0.54 s, about 0.3 s above a Node binary |
| Is Central reachable and does it serve what we need? | registry 200 in 1.74 s, docs 200 in 1.95 s |
| Does Central serve search? | Yes: `registry/search-packages?q=` |

The invocation overhead is the only cost, and it is noise against the 4.9–6.6 s per-invocation cost the
cache exists to remove. It buys the tool being `bal library` rather than a second binary to install.

## Consequences

**The jar went from 24,929,770 bytes to about 250 KB**, containing only our own classes. `gson`,
`picocli` and the CLI launcher are already on the distribution's runtime classpath, so they are
`compileOnly`; HTTP is `java.net.http` from the JDK. Nothing third-party is redistributed, and there are
no native libraries.

**The build needs no credentials and no upstream version.** No GitHub Packages token, no language server
release to resolve, no `lsVersion` to pin. `./gradlew :native:test` runs offline.

**Two dependencies became one, and it is a network.** The tool now fails for reasons outside the process,
which is why failures are values (`Result`/`Failure`) and why `central/CentralClient` is the only module
that can produce them. Retry policy, `Retry-After`, a wall-clock budget, Central answering an unpublished
package with 400 rather than 404, and an offline fallback all live there and nowhere else.

**Version coupling moved to the distribution.** `picocli 4.0.1` is from 2019, so nothing here may rely on
a feature newer than what `bre/lib` ships. One place this bites: picocli only gained
`setUnmatchedOptionsAllowedAsOptionParameters` in 4.4, so `Cli` checks for a flag-shaped option value by
hand rather than adding a bundled dependency.

**The capability grew rather than shrank.** `search` survived the migration because Central serves it;
`get` was dropped because it was `api`/`overview` with JSON instead of Ballerina, and a third register
would need its own justification. Three addressed verbs — `overview`, `ops`, `type` — are new here.

### What proves it

`CorpusTest`: nine recorded payloads render byte-for-byte to nine committed `.bal` snapshots that the
TypeScript reader produced. Beyond the corpus, `api` output was diffed live against that reader for six
packages **not** in the corpus — `ballerinax/rabbitmq`, `ballerina/websocket`, `ballerinax/twilio` (389
KB), `ballerina/sql`, `ballerinax/redis`, `ballerina/grpc` — and was byte-identical in every case, as
were the `overview` and `ops` documents once the command name and the then run-order-dependent `Source`
line were normalised. That line has since been dropped — see the cache bullet in `design/README.md` —
so the documents are now byte-stable across a cache hit and a fetch without any normalising.
