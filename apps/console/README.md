# Console

The web frontend of the Agentic Engineer Platform: a React + TypeScript
single-page app (Vite + Oxygen UI) talking to the `aep-api` BFF through a
generated OpenAPI client. See [`PRD.md`](PRD.md) for what it does and for
whom.

## Quickstart

```bash
make install                                   # pnpm install + go work sync (repo root)
make gen                                       # generated API types + route tree
VITE_API_MODE=mock pnpm --filter @aep/console dev   # http://localhost:8090, no backend needed
```

Mock scenarios: `localStorage.setItem('aep:mock:projects', 'empty' | 'some' | 'error')`.
Against a real BFF: `pnpm --filter @aep/console dev` (proxies `/aep-api-service`
to `API_PROXY_TARGET`, default `http://localhost:9090`).

## How development works

Console features are built in **Claude Code sessions** following a fixed,
issue-driven cycle: grilled first, built on mocks, integrated with the
backend on one feature branch, then tested locally and merged. Don't
freestyle a feature — start every one with the `/console-feature` skill,
which drives that cycle and pauses at each stage. Pass it an idea, or the
number of an issue that already exists:

```bash
/console-feature I want the project list to show each project's environments
/console-feature 42
```

The cycle itself — every stage, rule, and the feature-issue template — is
specified in [`design/development-flow.md`](design/development-flow.md).

## Docs map

Start at the top, go down as needed:

| Doc | What it answers |
|---|---|
| [`PRD.md`](PRD.md) | What the console does today (shipped features only) |
| [`design/development-flow.md`](design/development-flow.md) | How a feature goes from idea to shipped |
| [`design/design-system.md`](design/design-system.md) | How things should look; which skills to use |
| [`design/api-guidelines.md`](design/api-guidelines.md) | How to call the BFF; the mock layer; error handling |
| [`design/decisions/`](design/decisions/) | ADRs — the durable conventions and why (feature history lives in GitHub issues) |
| [`AGENTS.md`](AGENTS.md) | Entry point for AI sessions (same docs, terser) |

## Commands

The uniform verbs from the root `Makefile`: `make install`, `make build`,
`make dev`, `make test`, `make lint`, `make typecheck`.
