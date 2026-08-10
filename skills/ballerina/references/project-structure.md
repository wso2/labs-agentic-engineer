# Ballerina Project Structure & Dependencies

How a Ballerina project is laid out on disk and how its packages and dependencies
are managed. For what goes *inside* a `.bal` file, see [code-rules.md](code-rules.md).

## File Layout

- Split code by concern across multiple `.bal` files rather than cramming everything into `main.bal` — files in a package share one module, so splitting is free; use submodules or packages for larger separation.
- Reuse a fitting existing file before adding a new one; name new files for their concern (`snake_case.bal`). Naming and granularity are your call, not a fixed scheme.
- Keep every `configurable` declaration in `config.bal` — one declaration per value, not scattered across other files.
- Do not create documentation markdown files.

## Dependency Management

- **Never hand-edit `Dependencies.toml`** — it is auto-managed by the build tool. Do not create or hand-modify it to manage dependencies; deleting it to force a clean re-resolution (then rebuilding) is a valid troubleshooting step.
- **Never edit `Ballerina.toml` to add dependencies** — add the `import` statement in the `.bal` file and run `bal build`; Ballerina resolves and downloads packages from Central automatically.
- Which imports a package actually needs — including the paired `.driver` package for SQL connectors — is an import rule; see [code-rules.md](code-rules.md).

## Workspace Projects

When working with a Ballerina workspace (root `Ballerina.toml` with a `[workspace]` section):

**Creating a new package:**
1. Create the package directory with a `Ballerina.toml` containing the `[package]` section (`name`, `org`, `version`).
2. Add the new package path to the `packages` array in the root workspace `Ballerina.toml`.
3. Create initial `.bal` files in the new package.

**Guidelines:**
- Always prefer modifying existing packages over creating new ones.
- The root workspace `Ballerina.toml` should only contain a `[workspace]` section.
- Do not modify existing package `Ballerina.toml` files for dependency management.

## Config.toml

- Never read `Config.toml` or `tests/Config.toml` directly — they may contain secrets.
- Providing values to configurables is a runtime task. Only do it before running or testing.
- If the user needs to supply values, list the configurable variable names in the summary.
