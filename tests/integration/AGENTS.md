# AGENTS.md — tests/integration

API integration tests (vitest) against the real cluster. Exercise service
boundaries without a browser.

**Status:** nothing here yet.

## Conventions

- Run against the cluster from `deployments/` (`make dev-env` once, `make
  dev-update` after each source edit) — no mocked infra.
- DB resets between suites via the test-only reset endpoint (`TEST_MODE=true`).
- Assert against the generated contract types from `@aep/contracts`.
