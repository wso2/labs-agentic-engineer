# AGENTS.md — apps/

React webapps (Vite + Oxygen UI). One package per app; `apps/<name>` →
`@aep/<name>`.

**Status:** `apps/console/` (`@aep/console`) is scaffolded — Vite + React +
Oxygen UI + TanStack Router/Query + openapi-fetch + MSW. Read
`apps/console/AGENTS.md` before console work. `apps/tryit/` (`@aep/tryit`) is
the platform's test app — a static SPA the console opens to sign in as a
project's test user and talk to a deployed agent; read `apps/tryit/AGENTS.md`.

## Conventions

- Feature folders: `features/<feature>/{components,hooks,api,routes}` + a small
  shared `ui/`.
- Request/response types come from `@aep/contracts` — never redefined locally.
- Runtime config via `window._env_` (BFF-owned `env-config.js`); no build-time env.

Commands are the uniform verbs from the root `Makefile` (`make build`, etc.).
