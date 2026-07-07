# AGENTS.md — packages/contracts/orchestration

The **workflow boundary contract** (Go). Single source of truth for the Temporal
signal/query names, task-queue name, workflow-ID builders, and the phase/status/
gate enums shared by `services/orchestrator` (executes workflows) and
`services/aep-api` (starts/signals/queries them).

## Rules
- **No Temporal SDK dependency** — pure constants + helpers, importable anywhere.
- Both services import these symbols; never hand-duplicate a signal/query string.
- Workflow-ID format encodes `org` for multi-tenant isolation + dedup.

Module: `github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration`
(a Go member of `go.work`, sibling to `example-server`).
