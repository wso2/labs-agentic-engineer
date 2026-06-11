# Local SM-API is an in-repo stub — one SM-API provider, local and cloud

## Status

accepted (2026-06-09)

## Context

The OC refactor docs and agent-platform's implementation prescribe an
**open-core split**: two binaries sharing one `app` package — an OSS binary
with an embedded `openbao` provider for local dev (in-process SecretReference
creation), and an enterprise binary with the `secret-manager-api` (SM-API)
provider for cloud (`WriteOnly`, SR-creation owned by the server). Agent-platform
runs **no** SM-API service locally.

App-factory's product requirement ("works locally as well as in the cloud")
puts more weight on **local↔cloud parity** than agent-platform does. A first
cut achieved parity by running the *real* cloud SM-API binary locally, built
from a sibling checkout of the **private** wso2cloud repo. But that build
context (`deployments/docker-compose.yml`) was the **only** dependency on a repo
outside this one in the entire local setup — and since lab-app-factory is
**public**, public users without that private checkout can't `docker compose up`
at all.

## Decision

Run a **single binary** that uses the **SM-API provider in both local and
cloud** — never the `openbao` provider. Locally, a self-contained **in-repo Go
stub** at `deployments/local-secret-manager-api/` reproduces the SM-API HTTP
contract the BFF client (`asdlc-service/clients/secretmanagersvc`) actually
calls: `POST /secrets`, `GET /secrets?labelSelector=` (used by `DeleteSecret`'s
ID resolution), `DELETE /secrets/{id}`. Full parity on the result — it writes
OpenBao KV-v2 at `secret/data/user-app-secrets/<ns>/<ref>` **and** creates the
OC `SecretReference` CR; the namespace is derived from the JWT `ouId` claim as
`wc-<first8>-<sha256[:8]>`, mirroring `OrgBaseNamespace`. `WriteOnly` +
`ManagesSecretReferences()=true` are preserved. Mirrors the existing
`deployments/local-cluster-gateway-proxy` stub precedent.

## Why

Identical code path local↔cloud catches integration bugs earlier — the BFF
composition root picks the SM-API provider everywhere, so local testing covers
the cloud server-authored-SecretReference path (which the openbao
locally-authored path would not). The public-repo "works locally with no
`wso2cloud` checkout" goal rules out building the real cloud binary. Two
deliberate local simplifications, both invisible downstream because the KV
result is identical (ESO / ExternalSecret reads the same path): the stub writes
OpenBao over plain HTTP with the root token (vs the real ESO + k8s-SA flow),
and decodes the JWT without signature verification (single-tenant local —
`ouId` only scopes the namespace).

## Consequences

- An in-repo service to maintain — the stub reproduces a contract owned by a
  private repo, by hand (the fork-drift risk the "real binary" approach
  avoided). **Mitigation:** the header comment in
  `deployments/local-secret-manager-api/main.go` cites the mirrored source
  files, and the coding-agent e2e exercises `CreateSecret` end-to-end.
- **Cloud is unchanged** — still the real sm-api.
- Local OpenBao remains, but as the **stub's KV backend**, not the agent's
  direct secret store. PR #37.

## Rejected alternatives

- **Two-binary open-core split (agent-platform's model)** — an embedded
  `openbao` provider for local. Less code, but weaker local↔cloud parity: the
  locally-authored-SR path is structurally different from the cloud
  server-authored path, undermining "local testing covers the cloud case."
- **Run the real cloud binary locally, built from the private wso2cloud repo**
  — the original approach; fails the public-repo goal (private build context,
  unbuildable for the public audience).
- **Publish a prebuilt image** — cloud's lives in private ECR, and publishing a
  binary built from private source needs policy sign-off.
- **Open-source the service** — needs relicensing.
