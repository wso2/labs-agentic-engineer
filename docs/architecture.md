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
| `components/` | deployables arranged as they are deployed: `dataplane/ae-system-project/ae-studio/` is the per-org AE Studio; `controlplane/` and `dataplane/user-project/` are placeholders | yes (`ae-studio`: per org, by `aep-api`) |
| `runners/` | one-shot / job images | as jobs |
| `packages/` | shared libraries: `contracts`, `clients`, `ui`, `agent-stream`, `collab-doc`, `design-projection`, `excalidraw-dsl`, `progress-view`, `sse-cassette`, `platform-idp-auth`, `web-search`, `agent-eval`, `bal-library-tool` | no |
| `skills/` | the authored skill library, seeded and reconciled into each org's own repo | no — delivered as content |
| `playground/` | local harness that runs the real agents against a plain directory (no cluster, no GitHub, no database) | no |
| `evals/` | on-demand evaluation suites for the platform's agents (`spec-agents`: per-section + chained evals over the real design agent; see its README) | no — never in CI |
| `deployments/` | canonical local setup (k3d + OpenChoreo; a legacy docker-compose path); resource types that ship a reference operator keep it under `resource-types/<type>/operator/` (e.g. `thunder-app-operator`) | n/a (operator subdirs: yes, in-cluster) |

## Data & contract ownership

- Contracts are hand-maintained and live in `packages/contracts`, one document per
  audience rather than one per service:
  - `api/v1/openapi.yaml` — the public BFF contract the console is generated from.
  - `api/internal/v1/openapi.yaml` — the service-to-service contract.
  - `workflows/v1/openapi.yaml` — the workflow/runner contract.
  - `api/ae-studio-tools/{v1,internal/v1}/openapi.yaml` — the AE Studio tools
    container: `/v1` for the console, `/internal/v1` for `aep-api`.
  - `api/ae-design-agent/v1/openapi.yaml` — the design agent's console contract.
  - `sockets/ae-studio/{files,mcp,turn}/openapi.yaml` — one spec per Unix
    socket inside the AE Studio pod.
- Artifacts the agents produce are JSON Schema under `packages/contracts/schemas/`
  (`component-design`, `plan-task`, `update-task`), consumed by `@aep/agent-stream`
  and the design views.
- Both sides of every boundary listed above are generated from its spec, and
  generated clients/servers are never hand-edited. Whether they are committed
  differs by consumer: Go codegen is **committed** (aep-api's `internal/gen/`,
  `internal/igen/` and its `ae-studio-tools` client; `ae-studio-tools`'
  `internal/gen/`), and each Go module's `make gen-api-check` is its CI
  freshness gate; aep-api's OpenChoreo client is committed too, gated by
  `make gen-oc-client-check`. TypeScript `src/generated/` (console, design
  agent, collab) is gitignored and regenerated as a build prestep.
- An org secret's value lives only in the vault; Postgres keeps the name of
  the reference that holds it
  ([ADR-0047](decisions/ADR-0047-an-org-secrets-value-lives-only-in-vault.md)).

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

The org's AE Studio does not sit behind `aep-api`: each of its containers
verifies Platform IdP tokens itself, one gate per route group, and the only
claim it checks is the org. `aep-api` calls the pod as an install-wide AE-only
client naming the org; the pod calls `aep-api` back with its org's own clients
([ADR-0046](decisions/ADR-0046-ae-studio-checks-platform-idp-tokens-itself.md)).

## Design work: the organization's AE Studio

```
console ──/v1──────────────> ae-design-agent ┐ one pod per org: Resource `ae-studio`
        ──/v1/rooms (ws)───> ae-collab       │ of Project `ae-system`, in the org's
        ──/v1──────────────> ae-studio-tools │ dataplane namespace; one host each
aep-api ──/internal/v1─────> ae-studio-tools │
GitHub  ──/webhooks/github─> ae-studio-tools ┘ ──> aep-api (webhook-events)
inside the pod: Unix sockets files · mcp · turn · room, mounted per pair
```

Every turn, the live spec Room and every git and GitHub operation of an
organization run in that organization's AE Studio: one pod with three
containers, the design agent, the collaboration server and `ae-studio-tools`
(git, GitHub, the skills mirror, the webhook route). `aep-api` installs the
ResourceType per org and keeps the Resource current (a new release reaches an
org the next time someone opens its console); the pod holds the org's GitHub
token and webhook secret, and `aep-api` holds neither
([ADR-0045](decisions/ADR-0045-design-work-runs-in-the-organizations-ae-studio.md)).

The console calls the three hosts directly. `aep-api` reaches the pod only
through `ae-studio-tools` `/internal/v1`, for every git operation it needs.
GitHub delivers each repository's webhooks to the pod, which verifies the
signature and forwards the delivery to `aep-api`; `aep-api` persists it,
replays it and runs the sweeps
([ADR-0048](decisions/ADR-0048-github-delivers-each-repositorys-webhooks-to-ae-studio.md)).
Inside the pod the containers talk over Unix sockets, and the mount is the
gate. Resources, routes, env and converge:
[`ae-studio/design/README.md`](../components/dataplane/ae-system-project/ae-studio/design/README.md).

## Codegen pipeline

```
packages/contracts/**/openapi.yaml ──> openapi-typescript ──> TS client (generated/)
                                   └──> oapi-codegen ───────> Go StrictServerInterface (*.gen.go)
```

The build graph (`turbo` + `go.work`) wires every consumer's `build`/`typecheck`
behind `gen`, and CI runs `gen` + `git diff --exit-code` to catch staleness. See
`docs/decisions/ADR-0001-tooling-and-naming.md`.

## Service map

- [`aep-api`](../services/aep-api/README.md) — Go BFF; persists and replays
  webhook deliveries the org's AE Studio forwards; calls `ae-studio-tools` for
  every git operation; domain-oriented modules + vertical slices.
- [`ae-design-agent`](../components/dataplane/ae-system-project/ae-studio/ae-design-agent/AGENTS.md)
  — TS interactive spec agent (Vercel AI SDK), in the org's AE Studio.
- [`ae-collab`](../components/dataplane/ae-system-project/ae-studio/ae-collab/AGENTS.md)
  — TS Yjs collaboration server (the Room), in the org's AE Studio.
- [`ae-studio-tools`](../components/dataplane/ae-system-project/ae-studio/ae-studio-tools/AGENTS.md)
  — Go git, GitHub and webhook container, in the org's AE Studio.
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

**What the agent did** is read from wherever the cycle's log still is: the
pod's own log while the cycle's pod exists, the observer by the cycle's
Component UID once the pod is gone. The log is **observability, not ledger**:
`run_cycles` stays the record, and `RunCycleView.recording` says what can be
served ([ADR-0049](decisions/ADR-0049-a-finished-runs-feed-is-read-from-the-observer.md),
which supersedes
[ADR-0027](decisions/ADR-0027-run-recordings-are-observability-not-ledger.md)
on where a feed is kept).

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
