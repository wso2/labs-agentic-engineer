# AEP — Architecture

> Repo-wide map. Each service's README is the source of truth for its own
> current architecture — start with [`aep-api`](../services/aep-api/README.md).

## Overview

AEP is a spec-driven, AI-enhanced SDLC platform built on OpenChoreo. It is a
polyglot (Go + TypeScript) monorepo organized around **shared contracts**: every
REST boundary is described by an OpenAPI document owned by its producing service,
and every consumer uses generated types — so an incompatible change is a
compile error, not a runtime surprise.

## Buckets

| Bucket | Contents | Deploys? |
|---|---|---|
| `apps/` | React webapps (Vite + Oxygen UI) | yes |
| `services/` | long-lived deployables (Go + TS) | yes |
| `runners/` | one-shot / job images | as jobs |
| `packages/` | shared libraries: `contracts`, `clients`, `ui`, `agent-stream`, `collab-doc`, `design-projection`, `excalidraw-dsl`, `progress-view`, `sse-cassette` | no |
| `skills/` | the authored skill library, seeded and reconciled into each org's own repo | no — delivered as content |
| `playground/` | local harness that runs the real agents against a plain directory (no cluster, no GitHub, no database) | no |
| `evals/` | on-demand evaluation suites for the platform's agents (`spec-agents`: per-section + chained evals over the real agents service; see its README) | no — never in CI |
| `deployments/` | canonical local setup (k3d + OpenChoreo; a legacy docker-compose path); resource types that ship a reference operator keep it under `resource-types/<type>/operator/` (e.g. `thunder-app-operator`) | n/a (operator subdirs: yes, in-cluster) |

## Data & contract ownership

- Contracts are hand-maintained and live in `packages/contracts`, one document per
  audience rather than one per service:
  - `api/v1/openapi.yaml` — the public BFF contract the console is generated from.
  - `api/internal/v1/openapi.yaml` — the service-to-service surface.
  - `workflows/v1/openapi.yaml` — the workflow/runner surface.
- Artifacts the agents produce are JSON Schema under `packages/contracts/schemas/`
  (`component-design`, `plan-task`, `update-task`), consumed by `@aep/agent-stream`
  and the design views.
- Generated clients/servers are never hand-edited. Whether they are committed
  differs by consumer: aep-api's contract codegen (`internal/gen/`, `internal/igen/`)
  and the OpenChoreo client are **committed**, with `make gen-api-check` as the CI
  freshness gate; the console's `apps/console/src/generated/` is gitignored and
  regenerated as a build prestep.

## Identity and gateways

The cluster runs **two identity tiers**, and both are platform infrastructure
rather than one product's. One **platform IdP** (`platform-idp`) is OpenChoreo's
configured issuer and backs platform sign-in; one **environment Thunder**
(`thunder-<org>-<env>`) per `(org, environment)` holds what belongs to what is
deployed there — generated apps' end-user identity, and a version's roles and
test users. Each environment also has its own API Platform gateway
(`api-platform-<org>-<env>`), whose only keymanager is that environment's
Thunder, so a managed API's authentication is terminated against the identity
its deployment actually uses.

Nothing derives an issuer from a name: each environment carries a **binding
record** naming its instance, and the operator, `aep-api`, the gateway installer
and the verifier all read it. Topology, publisher contract and lifecycle:
[`deployments/design/two-tier-thunder.md`](../deployments/design/two-tier-thunder.md);
decisions
[ADR-0027](decisions/ADR-0027-one-cluster-one-platform-idp.md),
[ADR-0028](decisions/ADR-0028-the-platform-idp-is-neutral-infrastructure.md),
[ADR-0029](decisions/ADR-0029-environment-identity-is-bound-by-a-record.md).

## Codegen pipeline

```
packages/contracts/**/openapi.yaml ──> openapi-typescript ──> TS client (generated/)
                                   └──> oapi-codegen ───────> Go StrictServerInterface (*.gen.go)
```

The build graph (`turbo` + `go.work`) wires every consumer's `build`/`typecheck`
behind `gen`, and CI runs `gen` + `git diff --exit-code` to catch staleness. See
`docs/decisions/ADR-0001-tooling-and-naming.md`.

## Service map

- [`aep-api`](../services/aep-api/README.md) — Go BFF + GitHub webhook receiver
  (git ops folded in); domain-oriented modules + vertical slices.
- `agents` — TS interactive spec agents (Vercel AI SDK).
- `collab` — TS Yjs collaboration server.
- `aep-mcp-server` — MCP surface for the SRE/RCA handoff.
- `runners/` (job image) — TS Claude Agent SDK one-shot pod; one Debian image serves
  both task kinds (ADR-0012).
- `console` (app) — React frontend.

## How a version gets built

A spec version is cut as a `v<N>` tag and executed as **one supervised run over
one GitHub milestone**: the run mints prose issues into it as its first phase, one coding agent
works the whole milestone per cycle — verifying every web application it builds
in a browser before committing it — its pull request auto-merges, the merge
fans out to a build per changed component, the supervisor then **reconciles the
version**: every component the design declares whose newest green build is not
what its binding pins is promoted, providers before consumers, and the run
settles when the working set is empty *and* every component is serving. The
decisions and their costs are
[ADR-0011](decisions/ADR-0011-milestone-is-the-unit-of-execution.md),
[ADR-0017](decisions/ADR-0017-the-platform-owns-deploy.md),
[ADR-0018](decisions/ADR-0018-planning-is-a-run-phase.md),
[ADR-0024](decisions/ADR-0024-cancel-reaches-the-planning-phase.md) and
[ADR-0026](decisions/ADR-0026-deploy-reconciles-the-version.md); the
mechanism is
[`internal/delivery/README.md`](../services/aep-api/internal/delivery/README.md).

**What the agent did** is a recording, not a tail. The platform reads each run
cycle's pod once, server-side, and appends its feed as `RunEvent` NDJSON to the
workspace volume; every console reads that file from a byte offset, so a reload
mid-run replays from the first event and a reload after the pod is reaped shows
the whole cycle. The recording is **observability, not ledger** — `run_cycles`
in Postgres stays the record of what happened to a version, and
`RunCycleView.recording` says what can actually be served, so a partial feed is
never presented as the whole of it —
[ADR-0027](decisions/ADR-0027-run-recordings-are-observability-not-ledger.md).

**Mock verification** is that browser step, and it sits inside the coding cycle
rather than after a deployment. Once a `web-application` builds clean the cycle
dispatches one more agent for it: it stands the app up in mock mode — MSW
answering `/api`, a substituted auth module, no cluster and no sibling service —
walks every screen the component's wireframes name, and repairs each failure the
moment it finds it, clearing the line by clicking it again rather than by the
edit compiling. It reports one verdict per user story. Running the walk before
the commit is what makes one pull request carry the build *and* its fixes. It is distinct
from validation, which judges a **deployed** version against live infrastructure
once the run has promoted it. The procedure lives in the skill library
(`skills/mock-verification`, `skills/react-webapp`) and runs inside the coding
session; the platform orchestrates none of it —
[ADR-0025](decisions/ADR-0025-a-web-application-is-verified-before-it-is-committed.md).
