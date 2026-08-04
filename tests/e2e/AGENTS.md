# AGENTS.md — tests/e2e

End-to-end browser tests (Playwright) against the real local stack. Every test
maps to a user-facing flow in [`apps/console/PRD.md`](../../apps/console/PRD.md).

**Status:** nothing here yet.

## Conventions

- Run against the cluster from `deployments/` — no mocked infra.
- One spec per scenario; keep selectors resilient (roles/labels over CSS).
- Verify flows manually with the `playwright-cli` skill before writing the spec.
