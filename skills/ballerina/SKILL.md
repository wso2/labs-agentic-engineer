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

### Working with OpenAPI Specifications

- Prioritize generating services and clients from OAS specifications as instructed in [openapi.md](references/openapi.md) 

### bal library

**Run `bal library --help` before your first lookup, and follow the flow it describes.** It reads a
package off Central so you write signatures that exist rather than remembered ones. `--help` is the
grammar — the verbs, their flags, the session to walk; each document prints the rules about its own
contents. What follows is what you carry into every lookup.

#### Working with library

##### Preferences
- `ballerina/*` is the standard library; every vendor or third-party connector is `ballerinax/*`. 
- Prefer a `ballerinax/*` connector over a `trigger.*` package covering the same
events.
- Balerina has libraries created for working with standards, search before creating your own implementation.
- A trailing `// Special Agent Note:` mentions important informations, follow them. eg: Other package references.
- **Line one states the document's own length** If it says `· 535 lines` and you filtered using head | tail for 150 which mean the other 385 are gone and the answer can be among them: re-run that call unfiltered before writing anything from it. The library cli is designed to be self contained. You can't judge without the whole output.
- -r flag only covers dependant types in the own package. For the external package references, you might need to invoke `bal library type` if you need them.
- Tool may return ## Next section: Those are suggestions for your next look up navigation tool. Use them as hints.
- Tool might return errors, Handle accoringly based on the error message.

### bal build

- An `import` plus `bal build` resolves a package — no manual `Dependencies.toml` edit.
- `target/` is the incremental cache — leave it in place. A build that printed `Generating executable` is green; do not re-run `bal build` to confirm it.
- Configurables are read at runtime, so env variables or Config.toml is not needed to build.

## Dockerfile

When the user asks for a container image — load [dockerfile.md](references/dockerfile.md).
