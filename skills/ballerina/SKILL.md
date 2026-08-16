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

### bal library

**Run `bal library --help` before your first lookup, and follow the flow it describes.** It reads a
package off Central so you write signatures that exist rather than remembered ones.

- Run `bal library` unpiped. The CLI is designed to filter and give you the information you need
  with fewer calls. Narrow with flags only. Instructions and workflow is there in the
  `bal library --help` output.
- A failed lookup is not a blocker: write from `code-rules.md` and let `bal build` name what is
  wrong. Report the failure's JSON rather than improvising a signature.
- When `bal build` rejects something a connector's guide told you to write, go back to the
  signature — the guide is prose and drifts, the signature is generated. That is the usual cause.
- Prefer a `ballerinax/*` connector over a `trigger.*` package covering the same events.

### bal build

- An `import` plus `bal build` resolves a package — no manual `Dependencies.toml` edit.
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

## Dockerfile

Follow the exact multi stage dockerfile below.

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
