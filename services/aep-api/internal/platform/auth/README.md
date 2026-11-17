# platform/auth — inbound identity

> **Kernel package.** Part of the [aep-api architecture](../../../README.md).

Verifies every token aep-api is handed and says who the caller is. Nothing
outside this package reads a claim by the platform IdP's own name for it.

## Pieces

| Piece | Role |
|---|---|
| `jwtassertion.Authenticator` | RS256 JWT verification against a JWKS cache: issuer and audience lists, the RFC 9728 challenge, and on a user-only route the machine-token refusal. Decodes a token into `WireClaims`, reads it through the dialect into `TokenClaims`. |
| `JWTMiddleware` | The `/api/v1` user edge: `Authenticator` with `UserTokensOnly`, projected into `Claims` (`Subject`, `ClientID`, `OrgHandle`, `OrgID`, `OrgName`, display claims). |
| `EnsureOrgMiddleware` | Verifies the token's `OrgHandle` names an OpenChoreo namespace and warms the org row; passes through when the token carries no org. |
| `PublisherTokenVerifier` | A per-org machine token (`aep-publisher-<org>`): the client is the audience with the prefix, and the org handle the dialect reads, which the token must carry, must equal the one in that audience; and the token is a machine's by the dialect. `StudioClientVerifier`, the MCP gate and the runner authorizer verify through it, so the same holds for each. |
| `StudioClientVerifier` | The same over the `ae-studio-` prefix, plus the client must be the one recorded for the org. |
| `RunnerAuthorizer` · `MCPGate` · issues MCP | The route gates over those verifiers. |
| `SREHandoffVerifier` | A static key, no IdP. |

## The token dialect (`auth/dialect`)

A **token dialect** is how one IdP's tokens say two things no standard
defines: which org the caller belongs to, and whether the token was issued to
a machine or to a user. It is configuration, not code: the names and values
are settings (`PLATFORM_IDP_CLAIMS_*`, from the chart's `idp.claims`), read
once at boot into `dialect.Claims`, and every verifier reads the IdP's own
claims through `dialect.New(claims)`. aep-api knows no IdP. The four
mandatory settings have no default: `config.Validate` refuses an install that
leaves one out.

| Setting | What it names |
|---|---|
| `PLATFORM_IDP_CLAIMS_ORG_HANDLE` | the claim that carries the handle the IdP knows the caller's org by. Required |
| `PLATFORM_IDP_CLAIMS_ORG_ID` | the claim that carries the IdP's own id for that org, a UUID. Required |
| `PLATFORM_IDP_CLAIMS_ORG_NAME` | the claim that carries a display name for the org. Optional |
| `PLATFORM_IDP_CLAIMS_CALLER_TYPE_CLAIM` | the claim that says who the token was issued to. Required |
| `PLATFORM_IDP_CLAIMS_CALLER_TYPE_MACHINE` | its value that means a machine. Required |
| `PLATFORM_IDP_CLAIMS_CALLER_TYPE_USER` | its value that means a user. Optional: set, only this value and the machine's are accepted and any third is refused; unset, any value other than the machine's is a user's |

For example, ThunderID's are `ouHandle`, `ouId`, `ouName`, `grant_type`,
`client_credentials` and no user value (it has several user grants); an
Asgardeo organization's are `org_handle`, `org_id`, `org_name`, `aut`,
`APPLICATION` and `APPLICATION_USER`. `aectl` ships both as presets
(`tools/aectl/internal/idpclaims`).

- **Top-level text claims only.** A claim of any other JSON type, or a nested
  one, is not read: the IdP is configured to emit a flat text claim.
- **Nothing stands in for the org's handle.** It comes from the one handle
  claim or the token names no org.
- **Standard claims are not the dialect's.** `sub`, `aud`, `iss`, `exp`,
  `client_id` (RFC 9068), `scope`, `name` and `email` are read by their
  standard names on every IdP; `aud` says which client minted a machine
  token.

`dialect.Raw` is a token's whole payload. A verifier's wire struct keeps it
beside its typed standard claims (`dialect.Decode` in its `UnmarshalJSON`),
and `dialect.Org` and `dialect.Machine`, the two questions, read the IdP's
own claims out of it by their configured names.

Fail closed: a user-only route refuses a token the dialect cannot place as a
user's or a machine's, rather than taking it for a user's, and a machine
route (`PublisherTokenVerifier`) refuses it rather than taking it for a
machine's. The zero `Dialect` reads no org and places no token, and a token
whose claims carry other names than the configured ones names no org.

The four readers of the platform IdP's tokens (this package, the AE Studio
pod's `ae-studio-tools/internal/auth/dialect`, `packages/platform-idp-auth`
and the console's `auth/claims.ts`) are tested against the same sample
tokens, `packages/platform-idp-auth/test/fixtures/idp-claims`, so they cannot
drift apart; `dialect.ThunderClaimsForTest` and `AsgardeoClaimsForTest` give
other tests those claim names.

### One org under the IdP's own handle

An install serves one org. The IdP names it by a handle of its own (a
ThunderID organization unit's handle, an Asgardeo organization's handle),
which need not be the org's OpenChoreo namespace.
`Dialect.WithSingleOrg(idpHandle, platformHandle)` (wired from
`PLATFORM_IDP_ORG_HANDLE` and `PLATFORM_IDP_ORG_NAMESPACE`) maps it: a token
declaring exactly `idpHandle` reads as the org `platformHandle`, and any
other token (another org of the IdP, no handle claim) reads as no org, so the
tenant gate and the machine-token verifiers refuse it. Both settings are
required on every install (`config.Validate`), and the mapping is always
applied. It is in the dialect because every consumer of
the handle goes through it: the user edge, the org lookup, and the publisher
and studio verifiers, whose clients are named after the namespace.

Only aep-api maps. The org's pod is given the IdP's handle as its
`AE_ORG_HANDLE` (`aestudio` `Params.Org.Handle`), so its dialect twins
compare a token's handle as it is, and its Room names match the ones the
console builds from the same token.

## Invariants

- Consumers read `Claims.OrgHandle` / `OrgID` / `OrgName` and
  `TokenClaims` likewise; the claim names live in `dialect` alone.
- `ocauth` (the public overlay seam) keeps its `OuHandle` / `OuName` / `OuId`
  field names as its contract; it is fed from these projections.
- No unsigned-claim path: a nil JWKS cache refuses every token.
