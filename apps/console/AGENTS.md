# AGENTS.md — apps/console (`@aep/console`)

React SPA console for AEP. Vite + TypeScript + Oxygen UI, talking to the
`aep-api` BFF through the generated OpenAPI client.

> [!IMPORTANT]
> All UI in this project must be built with WSO2 Oxygen UI. For ANY React UI work —
> setup, components, pages, layouts, forms, tables, dialogs, theming — consult the
> `oxygen-ui` skill before writing or editing UI code, even when the request does not
> mention Oxygen UI by name.

> [!IMPORTANT]
> Frontend features go through the `console-feature` skill — it grills first,
> then drives the build. A feature request goes into that cycle, not straight
> into code, and an ungrilled issue gets grilled before it gets built.

**Read before working on any feature:**

- `PRD.md` — the living product picture: what exists, what's planned.
- `design/development-flow.md` — **the** spec for the feature cycle: every
  stage, every rule, the feature-issue template. Follow it; don't freestyle
  features.
- `design/design-system.md` — Oxygen UI conventions and which skills to use.
- `design/api-guidelines.md` — data fetching, error handling, user feedback,
  and the mock layer. Three rules are non-negotiable; the rest is judgment
  with a promotion path.

## Layout

- `features/<feature>/{components,hooks,api,routes}` + small shared `ui/`.
- `src/layouts/` — the app shell (`AppLayout`, per the oxygen-ui skill's
  canonical structure); pages render `PageContent > PageTitle > body`.
- `src/mocks/` — MSW handlers + fixtures, typed against `@aep/contracts`
  generated types. Dev-only; excluded from production builds.
- Request/response types come from the generated OpenAPI client — never
  redefined locally.
- Runtime config via `window._env_` (BFF-owned `env-config.js`).
- **Adding a `@aep/*` dep whose `types` resolves to `./dist` means adding a
  `RUN pnpm --filter … build` line to `apps/console/Dockerfile`.** The list is
  hand-maintained, host builds hide the omission, and the image build fails with
  TS2307 plus a cascade of unrelated-looking type errors. Rationale inline in
  the Dockerfile.

## Feature docs (issue-driven — ADR-0001)

- **A feature is a GitHub issue** (labels `console` + `feature`): the body is
  the feature doc, and nothing is built ungrilled. Requires `gh` auth.
- **`design/decisions/` ADRs are the current truth** — read them FIRST for
  context, then `gh issue list --repo wso2/labs-agentic-engineer --label
  console --label feature` (closed issues are frozen history, never edited;
  issues live upstream, not in forks).

Commands are the uniform verbs from the root `Makefile` (`make build`, etc.).
