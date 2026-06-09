# Run a local SM-API stub instead of an OpenBao-direct provider

## Status

superseded by [[adr-local-sm-api-in-repo-stub]] (2026-06-09) — see ADR-0004

## Context

The OC refactor docs (`10-target-architecture.md` §3.3, `future-phases.md`
WS3.3) and agent-platform's verified implementation prescribe an
**open-core split**: ship **two binaries** sharing one `app` package — the
OSS binary embeds an `openbao` provider for local dev (`StoreCapabilityReadWrite`,
in-process SR-creation via the OC client), and an enterprise overlay
binary embeds the `secret-manager-api` provider for cloud (`WriteOnly`,
SR-creation owned by the SM-API server). Agent-platform's local
docker-compose has **no** SM-API service; their local devs run the OSS
binary against local OpenBao.

## Decision

App-factory will **diverge** from agent-platform: run a single binary that
uses the SM-API provider in **both** local and cloud. Locally, a thin
SM-API-compatible stub service runs in docker-compose, backed by local
OpenBao for KV storage and the local OC API for SR creation. The stub
preserves the production contract (`WriteOnly`,
`ManagesSecretReferences()=true`).

## Why

Identical code path local-vs-cloud catches integration bugs earlier and
avoids the two-binary maintenance overhead. App-factory's user-facing
requirement ("works locally as well as in the cloud") puts more weight on
**local–cloud parity** than agent-platform does (their local story is
unmaintained: `Dockerfile.dev` is missing, the cloud binary has no working
local mode at all).

## Consequences

- Extra service to build and maintain (the local SM-API stub).
- The stub must keep up with whatever shape the deployed SM-API exposes;
  drift between them re-creates the bug class this decision is meant to
  prevent. Mitigation: the **stub is the cloud server itself**, built from
  `wso2cloud/backend/secret-manager-api/` (Go service with `Dockerfile` +
  `cmd/secret-manager-api/main.go`), just configured against local
  OpenBao + local ESO + local Thunder JWKS instead of cloud equivalents.
  Drift risk reduces to "pull and rebuild when wso2cloud team ships a
  new version" — no fork to maintain.
- The OC refactor's WS0.2 changes scope: porting the `openbao` provider
  is no longer required (the cloud SM-API provider is used in both
  environments). The seam still exists — but the local composition root
  picks the SM-API provider, not `openbao`.
- Local OpenBao is still needed, but as the **stub's KV backend**, not as
  the agent's direct secret store.

## Rejected alternatives

- **Match agent-platform exactly** — two binaries, openbao for local. Less
  code to maintain, but less local–cloud parity, and the OSS-equivalent
  binary's locally-authored SR path is structurally different from the
  cloud SM-API server-authored path, weakening the value of "local
  testing covers the cloud case."
