# AGENTS.md — apps/console (`@aep/console`)

The agentic-first console. It was built beside the earlier console (as
`apps/console-next`) and replaced it; the earlier one is kept in
`apps/console-old` as source to port from. That folder is outside the pnpm
workspace: nothing builds, tests or ships it, and it goes once the last of its
features is ported. `design/console-old-gaps.md` lists what it has and this
app does not yet.

Every decision behind it lives on the wayfinder map "The agentic-first
console" (https://claude.ai/artifact/LhkAvt26fS5XVLniiVyu2F). Its build items
are Phase 6 (N1–N10) of the plan tracker
(https://claude.ai/artifact/JyqAbejCdT6gx2J7mkauLx). Read the map's ticket for
an area before changing it.

## Rules

- **Oxygen UI is the only component library.** Colours come from `aepTheme`
  (`@aep/ui-theme`, shared with the Oxygen prototype theme), the guided-shell
  design's palette over Oxygen's base. Never add raw MUI or another kit.
- **Build on MSW, approve, then wire.** Each screen is built against MSW
  handlers and fixtures (`src/mocks/`), approved on the running mock
  (`VITE_API_MODE=mock`), and only then wired to aep-api. The mock cannot show
  whether aep-api maps a new contract field onto the response, so the wiring
  step checks against the real API.
- **Depend only on the contract and existing packages.** Code this app needs
  from `apps/console-old` is copied in and owned here (auth was), not
  extracted into a new shared package. Nothing imports from `apps/console-old`.
- **Tests:** unit tests with Vitest (node; `// @vitest-environment jsdom` per
  file for components). The live end-to-end walk lives in `tests/e2e`.
- Request and response types come from the generated client
  (`src/generated/aep-api.d.ts`, from `pnpm gen`); never redefine them.
- **Adding a `@aep/*` dep whose `types` resolves to `./dist` means adding a
  `RUN pnpm --filter … build` line to the `Dockerfile`.** The list is
  hand-maintained, host builds hide the omission, and the image build fails
  with TS2307 plus a cascade of unrelated-looking type errors.

## Layout

- `src/auth/`: OIDC sign-in, the session and token handling, copied from the
  old console. `src/api/`: the `openapi-fetch` client and its 401 handler.
- `src/features/<feature>/{components,api}`: one folder per area. `shell` is
  the frame (rail, chat slot, main outlet) and the route→scope mapping the
  rail and chat read.
- `src/components/`: app-wide primitives copied from the old console
  (`ErrorBoundary`, `EmptyState`).
- `src/routes/`: TanStack file routes; `src/generated/` is codegen, gitignored.
- `src/mocks/`: MSW handlers and fixtures for mock mode. Dev-only; never in a
  production build.
- `Dockerfile`, `nginx.conf`, `docker-entrypoint.sh`: the image (`ghcr.io/wso2/aep/console`).
  nginx serves the SPA and forwards `/collab` and GitHub's callbacks; the
  entrypoint writes `env-config.js` (`window._env_`) from the pod's env.
