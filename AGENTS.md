# AGENTS.md — AEP (root)

Agentic Engineer Platform: a polyglot (Go + TypeScript) monorepo. Spec-driven
SDLC platform built on OpenChoreo. 

## Uniform commands (single entry point: the root `Makefile`)

| Verb | Command | Does |
|---|---|---|
| install | `make install` | pnpm install + `go work sync` |
| build | `make build` | turbo build (TS) + `go build` (go.work) — runs `gen` first |
| dev | `make dev` | start dev servers |
| test | `make test` | turbo test + `go test` |
| lint | `make lint` | eslint + golangci-lint |
| typecheck | `make typecheck` | `tsc` + `go vet` |
| license-check | `make license-check` | fail if any source lacks the Apache header |
| deadcode (TS) | `make deadcode-ts-check` | knip over `@aep/agents` + `@aep/playground`; fails on any finding (`make deadcode-ts` reports without failing) |

## Coding Practices
- Focus on writing maintainable code, clean testable code. 
- Keep architecture, modules, files separated based on responsibility.
- Proper Fixes always, Propose design changes for better maintainability after exploring and if you are confident.
- no hacks or workarounds unless explicitly specified.
- Dead code is gated, in TS by `make deadcode-ts-check` and per Go module by
  that module's `make deadcode-check` (`services/aep-api`). Keep unwired
  infrastructure or a deliberate test seam only with a reason attached: a
  `@knipkeep <reason>` JSDoc tag in TS, `//deadcode:keep` in Go. What each
  gate covers and why is in `knip.jsonc` and `services/aep-api/Makefile`.

## Design docs

Each package keeps a `design/` folder: concise notes + ADRs written **after** a
feature ships (final state, not plans). Repo-wide ADRs/overview live in `docs/`.

Implementation plans can go to `docs/design/draft` but they should not be commited, once the feature is implemented, the relavant information should fit into package documentation and the draft should be deleted.

## More

`docs/architecture.md` (overview), `docs/decisions/` (ADRs), `docs/glossary.md`
(domain terms), `docs/developer-guide/` (setup/dev flow).
