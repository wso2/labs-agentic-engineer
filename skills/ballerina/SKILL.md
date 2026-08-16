---
name: ballerina
description: How to build a Ballerina service on the platform — network-native service composition with a built-in `http:Listener`, OpenAPI-first codegen (`bal openapi`), and Ballerina Central for connectors/libraries. Apply when a component's `language` is Ballerina. For a Go service, use `go` instead.
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Ballerina

## Creating a New Project

When the user asks to create a new project, service, or program from scratch:

```bash
bal new <project-name>   # scaffolds main.bal + Ballerina.toml
cd <project-name>
```

- For a workspace (multiple packages in one repo), see workspace rules in [code-rules.md](references/code-rules.md)

## Writing Ballerina Code

**File layout.** New code belongs in the file that fits its concern — `.bal` files and `Ballerina.toml` show the existing layout (see [code-rules.md](references/code-rules.md)).

**Finding an external connector or library.** For a package [code-rules.md](references/code-rules.md) already names, write the `import` and let `bal build` resolve it — `bal search` and `bal pull` are Central round-trips for something already decided. Reach for them only for a genuinely unknown connector. Where a `ballerinax/*` connector and a standalone `trigger.*` package cover the same events, use the connector — `trigger.*` is being superseded regardless of modified date.

`ballerinax/postgresql` pulls `ballerinax/cdc` and its Debezium jars transitively at every version; the jar-conflict warning that follows is expected and not yours to fix.

**Never fetch Ballerina documentation from the web.** `central.ballerina.io` and `lib.ballerina.io` spend a network round-trip on prose about a module whose README and exact source are already on this machine.

**Code rules.** [code-rules.md](references/code-rules.md) is the source of truth for how code is written and structured, including dependency management (`Dependencies.toml`/`Ballerina.toml` are auto-managed, never hand-edited) — check code against it as it's written.

**Code first, then build, then look things up — in that order.**

1. **Write the component**, As per the requirement, generate stubs (ie: openapi) when needed, implement as you required. 
2. **`bal build`** — Once done, use bal build to compile and get compilation errors. Fix every one and repeat until clean.
3. **Only now look up a signature**, To fix a compliation error resulted by a library error.

`bal build` is incremental and `target/` is its cache — **never delete `target/` to force a rebuild.** A build reporting no work to do is confirmation the last one still holds, not a failure to re-run.

Configurables are read at **runtime**, never at build time. Do not prefix `bal build` with a component's env vars, and do not re-run a build to check it works without them.

When the build does name a symbol, in this order:

1. [code-rules.md](references/code-rules.md) — `http` resources, `sql` queries, `time`.
2. [langlib-reference.md](references/langlib-reference.md) — the `lang.*` libraries (string, array, map, json, regexp, query expressions).
3. Only beyond those, the package itself — **`docs/README.md` before `modules/`**: the README is the package's own guide and leads with usage samples, the modules hold the exact signatures. A stub README — some are a paragraph — is a dead end; go straight to the module. Both sit under `$(bal home)/repo/bala/<org>/<name>/<version>/<any|java21>/` for `ballerina/*`; `ballerinax/*` lands under `~/.ballerina/repositories/central.ballerina.io/bala/` in the same layout — **but only once a `bal build` has resolved it.** Before that the tree does not exist at all, so `ls` fails rather than returning empty.

Derive the distribution root with `bal home`, never a hardcoded version; both roots are read-only. Grep for the declaration rather than paging the file — a `grep` that lands in a 31KB `types.bal` carries that much context for the rest of the run. An error still unresolved after several attempts is worth reporting with its file and line instead of guessing again.

## Working with OpenAPI Specs

- Pull the tool once per environment: `bal tool pull openapi`
- Generate a service stub from a spec: `bal openapi -i oas.yaml --mode service`
- Generate a client from a spec: `bal openapi -i oas.yaml --mode client`

**The generated stub is the starting point — fill it using Edit, never delete it.** Three things it always needs:

- Resource bodies come out empty, You need to fill the implementation. Failing to fill will result a compiler error.
- The listener is generated as `new (9090, config = {host: "localhost"})` — **drop the config, leave `new (9090)`**. A container bound to localhost answers from inside and refuses every request from outside, so the deployed service is unreachable while looking healthy.
- Delete the `bal new` scaffold's `main.bal` once the service exists — a package implements a `main` OR a service, not both.

## Dockerfile

`Dockerfile` — multi-stage, pinned Ballerina builder, JRE runtime:

```dockerfile
FROM ballerina/ballerina:2201.13.5 AS builder
WORKDIR /src
COPY --chown=ballerina:troupe . .
RUN bal build && mv target/bin/*.jar /tmp/service.jar

FROM eclipse-temurin:21-jre
WORKDIR /app
COPY --from=builder /tmp/service.jar /app/service.jar
EXPOSE 9090
ENTRYPOINT ["java", "-jar", "/app/service.jar"]
```

`--chown` is required, not stylistic: `COPY` always lands files as `root:root` regardless of the base image's `USER` directive, but the builder runs as the non-root `ballerina` user. Without it, `bal build` cannot write `target/`.

The dataplane root filesystem is read-only. `bal run` writes `/.ballerina` and `target/` at start, which crash-loops the pod (`FileSystemException: /.ballerina: Read-only file system`). The runtime entrypoint is the jar the builder produced. The jar is moved to `/tmp/service.jar` so the runtime `COPY` has a stable path regardless of the package name.
