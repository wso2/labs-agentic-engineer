# aep-api: route groups and gates

One rule shapes `aep-api`'s request boundary: **one spec per audience, one
prefix per caller, one gate per prefix.**

- The **spec** says who reads the contract: a person on the console
  (`packages/contracts/api/v1/openapi.yaml`) or a machine
  (`packages/contracts/api/internal/v1/openapi.yaml`). The internal spec's
  tags name the caller: `Runner`, `AE Studio`. The SRE handoff's raw
  JSON-RPC route (`/internal/v1/sre-handoff/mcp`) has no spec entry.
- The **prefix** says which caller. Each prefix is a **route group**. The
  word is "route group", never "surface": in this repo a **Surface** is the
  reader of a turn's narration (`CONTEXT.md`).
- The **gate** says which credential opens the group, and binds the org from
  that verified credential, never from the request.

The mount table is `routes()` in `internal/edge/routes.go`; the internal gate
table is `internalOpGates` in `internal/edge/internal.go`. The pod side of
the same boundary is
[`ae-studio-tools/design/route-groups.md`](../../../components/dataplane/ae-system-project/ae-studio/ae-studio-tools/design/route-groups.md);
how `aep-api` calls the pod is [git-boundary.md](git-boundary.md).

## Gate table

| Route group | Caller | Credential | Gate |
|---|---|---|---|
| `/healthz`, `/readyz` | kubelet | none | none. `/readyz` answers 200 once the server is up: `aep-api` holds no local state to wait on |
| `/api/v1` | the console | a Platform IdP user JWT | JWT (`JWT_ISSUER`, `JWT_AUDIENCE`, keys from `JWKS_URL`), then `EnsureOrgMiddleware`, then the deny-by-default tenant gate (`edge/tenant_gate.go`) on every generated op. The gate enforces unless `TENANT_GATE_MODE=log`, which only logs its verdicts (any other value enforces). No other credential has a way in |
| `/internal/v1/runs/{cycleId}/…` | the coding and validation runners | the org's publisher client token | the cycle fence: the token's org must own the cycle the path names (`auth.RunnerAuthorizer`) |
| `/internal/v1/ae-studio/…` | the org's AE Studio tools pod | the org's `ae-studio-<org>` client token | the token must be the client recorded for that org; binds that org; no cycle (`auth.StudioClientVerifier`) |
| `/internal/v1/mcp` | the coding runner and the AE Studio tools pod | the org's publisher client token (runner) or its `ae-studio-<org>` client token (pod) | its own gate, `auth.MCPGate`: the publisher token as on `runs/`, the `ae-studio-<org>` token as on `ae-studio/` (the client recorded for that org); binds the org that verifier answers. Any refusal is the same 401; an unreadable recorded client is 503 |
| `/internal/v1/sre-handoff/mcp` | the OpenChoreo SRE agent | the install-time SRE handoff key (`SRE_HANDOFF_TOKEN`) | its own gate, `auth.SREHandoffVerifier` (bearer compared in constant time; no org). Each tool call names its org (`namespace`), which `sourcecontrol/issues/sre_mcp.go` verifies against the observer's alerts of the last hour before it acts, and binds the incident context. Mounted only when the key is set; boot refuses a key without `OBSERVER_URL` and the service credential. The same verifier turns auto-RCA on. See [sre-handoff.md](sre-handoff.md) |

Each credential opens its own group only. A publisher token never clears
`ae-studio/` or `sre-handoff/mcp`. The SRE handoff key opens
`sre-handoff/mcp` only. An `ae-studio-<org>` token never clears a runner op or
`sre-handoff/mcp`. `mcp` is the one group two credentials open. A user JWT and `aep-api`'s own
AE-only client clear no internal group.

The coding runner and the validation runner hold the same org's publisher
client, so on `runs/` the prefix names the caller but does not prove it. The
gate is what enforces: the org and the cycle fence. The AE Studio tools pod
holds only its `ae-studio-<org>` client.

## How the internal gate runs

`/internal/v1/` is one mount (`newInternalV1Handler`). In order:

1. **Body cap** (`capInternalBody`): 1 MiB per request,
   `ingest-webhook-event` 25 MiB (GitHub's payload maximum). It looks the
   route up once and stores the matched operation for the next two steps. A
   declared length over the cap is 413 before anything reads the body.
2. **Gate** (`internalGate`): authenticates the matched operation's caller by
   its `internalOpGates` entry. **Deny by default**: an operation with no
   entry is 401, so a new internal op needs its credential in the table
   first. `TestInternalGate_CoversEverySpecOperation` pins the table to the
   spec both ways.
3. **Validator** (`internalValidator`): kin-openapi against the embedded
   internal spec, only for an authenticated request, so an anonymous caller
   never gets a schema-detail 400 or a body parse.
4. **Backstop** (`requireInternalGate`): a generated op that reaches the
   strict handler without the gate's verdict is 401.

`call-mcp-tool` is declared in the internal spec but excluded from
generation, so `POST /internal/v1/mcp` is a route miss for the gate and the
validator: its body is capped, not schema-validated, and its own gate
verifies the caller. Any other path no row names is 404.

The internal spec is not advertised by the gateway, but the path is
reachable through the console's `/aep-api-service/` route, which is why
nothing on it parses a body before the gate.
