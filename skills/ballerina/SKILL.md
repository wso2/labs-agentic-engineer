---
name: ballerina
description: Use this whenever you are working with ballerina code or editing .bal files.
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

Starting from an OpenAPI contract instead of a blank package — load [openapi.md](references/openapi.md).

### bal library

**Run `bal library --help` before your first lookup, and follow the flow it describes.** It reads a
package off Central so you write signatures that exist rather than remembered ones.

- Run `bal library` unpiped. The CLI is designed to filter and give you the information you need
  with fewer calls. Narrow with the verb's own arguments and flags — a name, a path, `--client` —
  never with a shell filter. Instructions and workflow is there in the `bal library --help` output.
- A failed lookup is not a blocker: write from `code-rules.md` and let `bal build` name what is
  wrong. Report the failure's JSON rather than improvising a signature.
- When `bal build` rejects something a connector's guide told you to write, go back to the
  signature — the guide is prose and drifts, the signature is generated. That is the usual cause.
- `ballerina/*` is the standard library; every vendor or third-party connector is `ballerinax/*`. — `search` when you cannot name the package.
- Prefer a `ballerinax/*` connector over a `trigger.*` package covering the same events.

### bal build

- An `import` plus `bal build` resolves a package — no manual `Dependencies.toml` edit.
- `target/` is the incremental cache — leave it in place. A build that printed `Generating executable` is green; do not re-run `bal build` to confirm it.
- Configurables are read at runtime, so env variables or Config.toml is not needed to build.

## Dockerfile

When the user asks for a container image — load [dockerfile.md](references/dockerfile.md).
