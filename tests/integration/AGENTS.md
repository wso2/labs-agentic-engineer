# AGENTS.md — tests/integration

API integration tests (vitest) against the real cluster. Exercise service
boundaries without a browser.

**Status:** nothing here yet.

## Conventions

- Run against the cluster from `deployments/` (`make dev-env` once, `make
  dev-update` after each source edit) — no mocked infra.
- Suites share the cluster's database; there is no test-only reset endpoint (`TEST_MODE` is gone), so each suite creates and cleans up its own data.
- Assert against the generated contract types from `@aep/contracts`.
