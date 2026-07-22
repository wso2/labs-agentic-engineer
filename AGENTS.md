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

## Development Practices
- Focus on writing maintainable code, clean code. 
- Keep files seperated based on responsibility.
- Proper Fix alawys, no hacks or workarounds unless explicitly specified.
- Dead code is gated. TS: `make deadcode-ts-check` (knip over `@aep/agents` +
  `@aep/playground`; `make deadcode-ts` for a report). Retain unwired infra / a
  deliberate test seam with a `@knipkeep <reason>` JSDoc tag; config + rationale
  in `knip.jsonc`. Go is gated per-module (`services/aep-api`, `//deadcode:keep`).

## PR Guidelines
- Make sure tests are enough to prove the change works as expected.
- Make sure to run /code-review before submitting a PR and then run tests again.
- Include proof of real execution in the PR Description (screenshots, test case from real payload, etc.)
- See if documentation(such as README, ADR, etc.) needs to be updated and update it accordingly.
- The documentation(including comments) should fit the overall project scope and should not be biased towards the specific PR. 

## Design docs

Each package keeps a `design/` folder: concise notes + ADRs written **after** a
feature ships (final state, not plans). Repo-wide ADRs/overview live in `docs/`.

Implementation plans can go to `docs/design/draft` but they should not be commited, once the feature is implemented, the relavant information should fit into package documentation and the draft should be deleted.

## More

`docs/architecture.md` (overview), `docs/decisions/` (ADRs), `docs/glossary.md`
(domain terms), `docs/developer-guide/` (setup/dev flow).
