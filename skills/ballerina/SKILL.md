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

- Read [project-structure.md](references/project-structure.md) for how a Ballerina project is laid out.

## Writing Ballerina Code

**File layout.** New code belongs in the file that fits its concern — `.bal` files and `Ballerina.toml` show the existing layout (see [project-structure.md](references/project-structure.md)).

**Finding an external connector or library.** For a package [code-rules.md](references/code-rules.md) already names, write the `import` and let `bal build` resolve it — `bal search` and `bal pull` are Central round-trips for something already decided. Reach for them only for a genuinely unknown connector. Where a `ballerinax/*` connector and a standalone `trigger.*` package cover the same events, use the connector — `trigger.*` is being superseded regardless of modified date.

`ballerinax/postgresql` pulls `ballerinax/cdc` and its Debezium jars transitively at every version; the jar-conflict warning that follows is expected and not yours to fix.

**Use `bal-library` over web search.** It reads the package off Ballerina Central and answers four different questions; the grammar and the ladder are below.

**Code rules.** [code-rules.md](references/code-rules.md) is the source of truth for how code is written — Make sure to load it before you write code or working with ballerina. [project-structure.md](references/project-structure.md) covers how the project is laid out and how packages and dependencies are managed (`Dependencies.toml`/`Ballerina.toml` are auto-managed, never hand-edited) — load it before adding files or packages, or touching a `.toml`. Write tests only when the user explicitly asks — then load [tests.md](references/tests.md).

### Read the package before you call it

**Once, up front, for each third-party connector `code-rules.md` does not name:**

```bash
bal-library <org/name>
```

That is the overview: the package's own guide, every client's constructor and function signatures, its module-level functions, and its error declarations. About 7KB for a 903-operation connector. It is the answer to "how is this used" as well as "what is it called", and it is cheap because the payload is cached for the rest of the run.

`code-rules.md` already names `http`, `sql`, `time`, `log` and `os`, and the langlibs are the language itself (see [langlib.md](references/langlib.md)) — none of them is a package Central has anything to add about, so **do not look them up.** Local workspace packages and `generated/` submodules are not on Central at all.

If the client's operations were replaced by a path tree — which happens above 100 of them — navigate it:

```bash
bal-library ops <org/name> <path>
```

**The pre-read is not a precondition.** If it fails — the package is not on Central, the registry is having a bad minute — write the code from `code-rules.md` and let `bal build` name what you got wrong. The ban on guessing applies to a signature you were shown a failure for, not to starting work.

### Then write, build, and look up what the build names

1. **Write the component.** Generate stubs (e.g. openapi) when needed.
2. **`bal build`.** Fix every compilation error and repeat until clean.
3. **Look up only what the build named**, using the table below.

`bal build` is incremental and `target/` is its cache — **never delete `target/` to force a rebuild.** A build reporting no work to do is confirmation the last one still holds, not a failure to re-run.

Configurables are read at **runtime**, never at build time. Do not prefix `bal build` with a component's env vars, and do not re-run a build to check it works without them.

| When the question is | Run |
|---|---|
| `http` resources or param binding · `sql` queries and return types · `time` · imports and `.driver` pairing · listeners and event services · configurables · `log` | **[code-rules.md](references/code-rules.md). Do not run `bal-library`.** |
| anything `lang.*` — a conversion, an array/string/map/number operation | **[langlib.md](references/langlib.md).** It is part of the language, not a package on Central. |
| "what exactly is this declaration?" | `bal-library type <org/name> <Name>` |
| "what does this type contain, all the way down?" | `bal-library type <org/name> <Name> --deps` |
| "how do I branch on this error?" | `bal-library type <org/name> <ErrorName> --deps` |
| "what operations are under this path?" | `bal-library ops <org/name> <path>` |
| "what is the exact signature of everything under this path?" | `bal-library ops <org/name> '<path>' --sigs` |
| nothing above answered | `bal-library api <org/name> > /tmp/<name>-api.bal`, and say why |

**Pass `--project-dir <component>` on every post-build lookup.** Once a build has written `Dependencies.toml`, that is the version the component actually compiles against — without it you get Central's latest and may write a field that does not exist, with nothing in the output to say why.

**Take a type's name from a signature, never from the guide.** `github`'s own readme example returns `Repository` while the operation returns `FullRepository`, and eleven records in that package carry a `stargazersCount` — four of them optional. Reading a field off the wrong record compiles into a nil-handling bug.

A `// Special Agent Note:` at the end of a line names the package a type comes from — that is the `import` to add, and the `alias:Type` prefix on the line is how to write it.

Exit codes: `2` means the arguments are wrong — a `:version` suffix on the package name, an unknown flag, a declaration name that does not resolve (the failure lists candidates), or a package with more than one client where `ops` needs `--client`. `1` means Central could not answer; run it once more. Every failure writes one JSON object to stderr with a `suggestion`. **Never improvise a signature because a lookup failed** — report that JSON instead.

`ops` and `type` print a few hundred bytes to a few KB and are meant to be read directly. Only `api` needs redirecting to a file: it is tens of thousands of lines and whatever you read stays in context for the rest of the run. An error still unresolved after several attempts is worth reporting with its file and line instead of guessing again.

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
