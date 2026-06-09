# Local SM-API is an in-repo stub, not the cloud binary

## Status

accepted (2026-06-09); **supersedes** [[adr-local-sm-api-stub]] (ADR-0002).

## Context

[[adr-local-sm-api-stub]] decided the local sm-api would be **the cloud
server itself**, built from `wso2cloud/backend/secret-manager-api/` — its
whole drift-avoidance argument was "no fork to maintain." But that build
context (`deployments/docker-compose.yml` building from
`../../wso2cloud/backend/secret-manager-api`) is the **only**
`../../wso2cloud` dependency in the entire local setup, and `wso2cloud` is a
**private** repo while lab-app-factory is **public** — so public users can't
`docker compose up` at all.

## Decision

Replace the wso2cloud build dependency with a self-contained in-repo Go stub
at `deployments/local-secret-manager-api/`. It reproduces the sm-api HTTP
contract that the BFF client (`asdlc-service/clients/secretmanagersvc`)
actually calls: `POST /secrets`, `GET /secrets?labelSelector=` (used by
`DeleteSecret`'s ID resolution), `DELETE /secrets/{id}`. Full parity on the
result: it writes OpenBao KV-v2 at `secret/data/user-app-secrets/<ns>/<ref>`
**and** creates the OC `SecretReference` CR. The namespace is derived from
the JWT `ouId` claim as `wc-<first8>-<sha256[:8]>`, mirroring
`OrgBaseNamespace`. Mirrors the existing `deployments/local-cluster-gateway-proxy`
stub precedent.

## Why

The public-repo "works locally with no `wso2cloud` checkout" goal outweighs
ADR-0002's no-fork argument — the private dependency made ADR-0002 literally
unbuildable for the public audience. Two deliberate local simplifications,
both invisible downstream because the KV result is identical (ESO /
ExternalSecret reads the same path): the stub writes OpenBao over plain HTTP
with the root token (vs the real ESO + k8s-SA flow), and it decodes the JWT
without signature verification (single-tenant local — `ouId` only scopes the
namespace).

## Consequences

- Reinstates the exact fork-drift risk ADR-0002 avoided — the stub now
  reproduces a contract owned by a private repo, by hand. **Mitigation:** the
  header comment in `deployments/local-secret-manager-api/main.go` cites the
  mirrored source files; the existing coding-agent e2e exercises
  `CreateSecret` end-to-end.
- **Cloud is unchanged** — still the real sm-api.
- PR #37.

## Rejected alternatives

- **Keep building the real binary from wso2cloud** — fails the public-repo
  goal (private build context).
- **Publish a prebuilt image** — cloud's lives in private ECR, and publishing
  a binary built from private source needs policy sign-off.
- **Open-source the service** — needs relicensing.
