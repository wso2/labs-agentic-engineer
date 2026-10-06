# ADR-0041 — AE Studio checks Platform IdP tokens itself

**Status:** Accepted · 2026-10-06
**Related:** [ADR-0040](ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)
(the org's AE Studio) ·
[ADR-0028](ADR-0028-the-platform-idp-is-neutral-infrastructure.md) (the
Platform IdP)
**Detail:** [`services/aep-api/design/route-groups.md`](../../services/aep-api/design/route-groups.md)
(`aep-api`'s gates) ·
[`services/aep-api/design/git-boundary.md`](../../services/aep-api/design/git-boundary.md)
(`aep-api` → pod) ·
[`ae-studio-tools/design/route-groups.md`](../../components/dataplane/ae-system-project/ae-studio/ae-studio-tools/design/route-groups.md)
(the pod's gates)

## Context

With design work in the org's own pod (ADR-0040), the console calls that pod
directly: turn streams, the Room and file reads go browser → pod, not through
`aep-api`. `aep-api` calls the pod too, for git and GitHub work, and the pod
calls `aep-api` back for a project's repository, usage records and webhook
deliveries. Each of those paths needs a verifier and an answer to "which org".
The pod runs in the org's dataplane, so a token it accepts must be one only
that org's callers can hold.

## Decision

**Every container verifies Platform IdP tokens itself, one gate per route
group, and the org is the only claim it checks.**

1. **`/v1`: a user's token, org claim only.** A Platform IdP user JWT whose
   audience is the console's and whose org claim is the pod's org. The rule is
   one function per container: `userRule` in `packages/platform-idp-auth` for
   `ae-design-agent` and `ae-collab`, `ae-studio-tools`' own verifier
   (`internal/auth/verify.go`) in Go. It is the same org rule `aep-api`'s
   `/api/v1` applies in `edge/tenant_gate.go`. When scope claims arrive, each
   container changes in that one function.

2. **`/internal/v1` on `ae-studio-tools`: the AE-only client.** A
   client-credentials token of one install-wide client that carries no org
   claim, plus `X-Impersonate-Org` naming the pod's org (its Platform IdP OU
   id, the pod's `AE_ORG_ID`). Locally the client is
   `ae-studio-internal-client`, which `aep-api` reads from
   `AE_STUDIO_INTERNAL_CLIENT_ID` / `AE_STUDIO_INTERNAL_CLIENT_SECRET`; Cloud
   provisions its own client (APP_FACTORY_BFF_TO_AE_STUDIO) under the same
   env names. A user token never clears this group, and the AE-only token
   never clears `/v1`.

3. **Sockets are gated by mount; the webhook route by HMAC.** The
   containers' Unix sockets sit on emptyDirs mounted only into the two
   containers that talk over each, so any process that can reach a socket is
   trusted by it. `/webhooks/github` verifies GitHub's signature with the
   org's webhook secret (ADR-0043).

4. **`aep-api` never forwards a user's token to the pod.** Every `aep-api` →
   pod call is AE-only M2M on `/internal/v1`. A user's request to `aep-api`
   is authorized by `aep-api`, which then acts as itself.

5. **The pod holds two `aep-api` credentials, each used for one route group.**
   - The org's `ae-studio-<org>` client (org secret `ae-studio-client`)
     opens the `ae-studio/` route group of `/internal/v1`: the project repository and skills
     lookups, dependency completion, turn usage and the webhook forward.
     `aep-api` binds the org recorded for that client.
   - The org's publisher client (org secret `ae-publisher-client`, the
     pod's `AE_PUBLISHER_CLIENT_ID`) is what the pod uses for
     `/internal/v1/mcp`.

   The invariant: a publisher token opens `mcp` and the org's own `runs/`
   ops (fenced to cycles of its org) and nothing else, and every
   `ae-studio/` op accepts only the `ae-studio-<org>` client, so each token
   is 401 on the other's group. The pod's publisher token therefore also
   clears its own org's `runs/` ops (`runner-validation-context`, gated by
   `runnerCredential` in `services/aep-api/internal/edge/internal.go`),
   though the pod never calls them. Whether to narrow the pod's publisher
   client to `mcp` alone is an open decision.

6. **The issuer is checked exactly; the key set may live elsewhere.** Each
   container takes the issuer (`AE_IDP_ISSUER`) and the key-set URL
   (`AE_IDP_JWKS_URL`) separately. Locally the token's `iss` is a URL whose
   host is loopback inside a pod, so the key set is fetched from an in-cluster
   URL. A key set that cannot be fetched answers "retry", never "denied".

## Consequences

- The console calls the pod with the same token it sends `aep-api`, and the
  org boundary holds without `aep-api` on the path.
- Four verifiers implement one rule (`aep-api`, two TypeScript containers
  through one package, one Go container). A change to the rule is a change in
  each of them.
- The AE-only client is install-wide: whoever holds its secret can act as any
  org's `aep-api`. It lives only in `aep-api`'s environment.

## Alternatives considered

- **`aep-api` as the only verifier, with a shared secret to the pod.**
  Rejected: the console calls the pod directly, so the pod must verify the
  user's token itself.
- **Scope claims now.** Rejected for now: the Platform IdP's scope work is not
  merged. The org rule lives in one function per container, so a scope branch
  lands there later.
