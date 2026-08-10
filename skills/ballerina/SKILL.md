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

- Read project structure on project-structure practices [project-structure.md](references/project-structure.md)

## Writing Ballerina Code

**File layout.** New code belongs in the file that fits its concern — `.bal` files and `Ballerina.toml` show the existing layout (see [project-structure.md](references/project-structure.md)).

**Finding an external connector or library.** For a package [code-rules.md](references/code-rules.md) already names, write the `import` and let `bal build` resolve it — `bal search` and `bal pull` are Central round-trips for something already decided. Reach for them only for a genuinely unknown connector. Where a `ballerinax/*` connector and a standalone `trigger.*` package cover the same events, use the connector — `trigger.*` is being superseded regardless of modified date.

`ballerinax/postgresql` pulls `ballerinax/cdc` and its Debezium jars transitively at every version; the jar-conflict warning that follows is expected and not yours to fix.

**Use `bal-library` over web search.** 

**Code rules.** [code-rules.md](references/code-rules.md) is the source of truth for how code is written — Make sure to load it before you write code or working with ballerina. [project-structure.md](references/project-structure.md) covers how the project is laid out and how packages and dependencies are managed (`Dependencies.toml`/`Ballerina.toml` are auto-managed, never hand-edited) — load it before adding files or packages, or touching a `.toml`. Write tests only when the user explicitly asks — then load [tests.md](references/tests.md).

**Code first, then build, then look things up — in that order.**

1. **Write the component**, As per the requirement, generate stubs (ie: openapi) when needed, implement as you required. 
2. **`bal build`** — Once done, use bal build to compile and get compilation errors. Fix every one and repeat until clean.
3. **Only now look up a signature**, To fix a compliation error resulted by a library error.

`bal build` is incremental and `target/` is its cache — **never delete `target/` to force a rebuild.** A build reporting no work to do is confirmation the last one still holds, not a failure to re-run.

Configurables are read at **runtime**, never at build time. Do not prefix `bal build` with a component's env vars, and do not re-run a build to check it works without them.

When the build does name a symbol, in this order:

1. [code-rules.md](references/code-rules.md) — `http` resources, `sql` queries, `time`.
3. Only beyond those, the package's own API. The `bal-library` command prints all of it — every client, function, type, service, and annotation — as one Ballerina syntax string:

   ```bash
   bal-library <org/name> [version] > /tmp/<name>-api.bal
   ```

   The version is optional — omitted, the latest published one is resolved; pass `--project-dir <component>` once a build has written `Dependencies.toml` and it reads the locked version instead. It answers whether or not a build has ever resolved the package.

   **Redirect it to a file; never pipe it to `head`.** The output is one file per package, tens of thousands of lines, ordered types → clients → services — so the first screen is always types and never the client you came for. `grep -n 'client class'` and `grep -n` the call you need, then `sed -n` those line ranges.

   A `// Special Agent Note:` at the end of a line names the package a type comes from — that is the `import` to add, and the `alias:Type` prefix on the line is how to write it.

   Exit codes: `2` means the arguments are wrong — the usual cause is a `:version` suffix on the package name, so drop it and pass the version as a second argument. `1` means Central could not answer; check the org and package spelling and run it once more. Never improvise a signature because a lookup failed — report the JSON it wrote to stderr instead.

4. **`--readme` when the question is "how is this used?" rather than "what is this called?"** — a wrong *shape* of call, an auth or config block you have no sample for, a connector you have never written against:

   ```bash
   bal-library <org/name> --readme > /tmp/<name>-readme.md
   ```

   This is the package's own guide and it leads with runnable samples — a couple of hundred lines against the API document's tens of thousands. It resolves the version exactly as step 3 does, so `--project-dir` applies here too, and it answers before any build has resolved the package.

Redirect both documents to a file rather than reading them off the pipe — whatever you read stays in context for the rest of the run, and the API document is far too large to spend that way. An error still unresolved after several attempts is worth reporting with its file and line instead of guessing again.

## Working with OpenAPI Specs

- Pull the tool once per environment: `bal tool pull openapi`
- Generate a service stub from a spec: `bal openapi -i oas.yaml --mode service`
- Generate a client from a spec: `bal openapi -i oas.yaml --mode client`

**The generated stub is the starting point — fill it using Edit, never delete it.** Three things it always needs:

- Resource bodies come out empty, You need to fill the implementation. Failing to fill will result a compiler error.
- The listener is generated as `new (9090, config = {host: "localhost"})` — **drop the config, leave `new (9090)`**. A container bound to localhost answers from inside and refuses every request from outside, so the deployed service is unreachable while looking healthy.
- Delete the `bal new` scaffold's `main.bal` once the service exists — a package implements a `main` OR a service, not both.

## Dockerfile

```dockerfile
FROM ballerina/ballerina:2201.13.5 
WORKDIR /src
COPY --chown=ballerina:troupe . .
ENTRYPOINT ["bal", "run"]
```

`--chown` is required, not stylistic: `COPY` always lands files as `root:root` regardless of the base image's `USER` directive, but the container runs as the non-root `ballerina` user. Without it, `bal run` fails writing `target/`/`Dependencies.toml` and the pod crash-loops.
