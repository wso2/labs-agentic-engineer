---
name: ballerina
description: Everything related to working with ballerina langauge.
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Ballerina

**Load [code-rules.md](references/code-rules.md) before writing any `.bal` code.** This contains best practices, syntaxes and common patterns for Ballerina code.

## Development workflow

### Creating a New Project

When the user asks to create a new project, service, or program from scratch:

```bash
bal new <project-name>   # scaffolds main.bal + Ballerina.toml
cd <project-name>
```
- Read [project-structure.md](references/project-structure.md) for how a Ballerina project is laid out.

Write tests only when the user asks — then load [tests.md](references/tests.md).

### bal library

Reads a package's real API off Central. It is the source for a signature; web search is not.
Ask it a question by name or path — never `grep` or `sed` its output.

One chain, in order — each output names the argument for the next:

```bash
bal library overview ballerinax/github                     # Markdown: clients + every signature, module functions, errors, then the guide
bal library ops ballerinax/github repos                    # Markdown: the signatures callable at `repos`, and its child segments with counts
bal library ops ballerinax/github 'repos/*/*' --sigs       # Markdown: every signature at or under a path; `*` is a wildcard segment
bal library type ballerinax/github FullRepository          # Ballerina: the return type off that signature, whole
bal library type ballerinax/sap Client                     # Ballerina: the client class itself, when the constructor is the question
bal library type ballerina/http ClientRequestError --deps  # Ballerina: + the distinct chain and Detail record — how to branch on an error
bal library api ballerinax/github                          # Ballerina: every declaration — large, last resort, and say why
```

Every lookup names its verb: `overview` is the one to start with, and a bare `bal library
ballerinax/github` is exit 2 rather than a guess at which document you meant.

Above 100 operations the overview prints a path tree instead of the signatures (`repos 421   orgs 200   user 93 …`), and that tree is what supplies `ops` its path. `type` takes several names at once.

- `--client <Name>` — pick a client; `ops` requires it when a package declares more than one.
- `--version <version>` — read that version rather than Central's latest. Works on every verb that reads a package.
- `--project-dir <component>` — pin the version `Dependencies.toml` resolved instead of Central's latest; pass it on every lookup after a build.

Notes:

- Take a type's name from a **signature**, never from the guide — `github`'s readme returns `Repository` where the operation returns `FullRepository`. The overview says so explicitly when it can: a guide naming something the package does not declare is flagged above the guide, with the closest declared names.
- The overview's `## Configurables` section is what a **deployment** sets in `Config.toml`; those names are module-private, so a signature default naming one is set there, never passed as an argument.
- The payload is cached on disk and outlives the run, so four addressed questions cost less than one `api`. Add `--refresh` only when a name should exist and does not — a package published since the copy was taken.
- A trailing `// Special Agent Note:` names the **module** a type comes from: that clause **is** the import — write `import ` and the module exactly as printed, apostrophes included (`ballerinax/'client.config` is a real one; a segment that is a Ballerina keyword has to be quoted) — then use the `alias:Type` prefix it shows. **No note means no import.** A prefixed name with no note is pre-declared (`int:Signed32`, `xml:Element`, `string:Char`); importing it is a compiler error, not a precaution.
- `--deps` ends with the cross-package edges and, for each, the exact follow-up **including its version** — run that line rather than composing a coordinate. It is the only way to reach a module that is not its own package: `ballerinax/aws.auth` has no registry entry, so `bal library type ballerinax/aws.auth AuthConfig --version 1.0.1` works and the versionless form is exit 1.
- `*` matches one branch and the header names the ones it did not take (`| Also matched | … (4), not included here |`). If that row is present, the answer is short by that many — ask for the named path too before concluding an operation does not exist.
- Exit 2 means the arguments are wrong (`:version` suffix, a flag the verb does not take, unresolved name, ambiguous client); exit 1 means Central could not answer — run it once more.
- Every failure writes one JSON object with a `suggestion` to stderr; report that JSON rather than improvising a signature.
- A failed lookup is not a blocker: write from `code-rules.md` and let `bal build` name what is wrong.
- Prefer a `ballerinax/*` connector over a `trigger.*` package covering the same events.
- An `import` plus `bal build` resolves a package; `bal library search <keywords...>` is what names a genuinely unknown connector, with each hit's pull count beside it.

### bal build

- `target/` is the incremental cache — leave it in place; "no work to do" confirms the last build holds.
- Configurables are read at runtime, so env variables or Config.toml is not needed to build.

### bal openapi

```bash
bal tool pull openapi                          # once per environment
bal openapi -i oas.yaml --mode service         # generate service from openapi spec
bal openapi -i oas.yaml --mode client          # generate client from openapi spec
```

- The stub is the starting point: fill every empty resource body with Edit — an unfilled body is a compile error.
- Change the generated `new (9090, config = {host: "localhost"})` to `new (9090)` — localhost binding leaves the deployed container unreachable while it looks healthy.
- Delete the `bal new` scaffold's `main.bal` once the service exists.

### Dockerfile

```dockerfile
FROM ballerina/ballerina:2201.13.5
WORKDIR /src
COPY --chown=ballerina:troupe . .
ENTRYPOINT ["bal", "run"]
```

`--chown` is required: `COPY` lands files as `root:root`, the container runs as `ballerina`, and without it `bal run` cannot write `target/` and the pod crash-loops.
