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
| `PublisherTokenVerifier` | A per-org machine token (`aep-publisher-<org>`): the client is the audience with the prefix, and the org handle the dialect reads, declared outright, must equal the one in that audience; and the token is a machine's by the dialect. `StudioClientVerifier`, the MCP gate and the runner authorizer verify through it, so the same holds for each. |
| `StudioClientVerifier` | The same over the `ae-studio-` prefix, plus the client must be the one recorded for the org. |
| `RunnerAuthorizer` · `MCPGate` · issues MCP | The route gates over those verifiers. |
| `SREHandoffVerifier` | A static key, no IdP. |

## The token dialect (`auth/dialect`)

A **token dialect** is the claim names and rules one IdP kind uses to say who
the caller is. The kind is a setting, `PLATFORM_IDP_KIND` (empty is
`thunder`; an unknown value fails `config.Validate` at boot), and every
verifier reads the IdP's claims through `dialect.ForKind(kind)`. Two kinds
exist:

| Question | `thunder` | `asgardeo` |
|---|---|---|
| the org's handle (the OpenChoreo namespace) | `ouHandle`, else `ouName`, else `ouId` — the console mirrors this precedence. The fallback serves legacy user tokens only: the publisher and studio verifiers require the declared `ouHandle` (`Org.HandleDeclared`) | `org_handle`, with no fallback (every token carries it); declared when present |
| the org's id | `ouId` | `org_id` |
| the org's display name | `ouName` | `org_name` |
| is the token a machine's? | `grant_type == client_credentials`, any other grant a user's; a token without `grant_type` answers neither | `aut == APPLICATION` a machine's, `aut == APPLICATION_USER` a user's; a token without `aut`, or with any other value, answers neither |
| which client minted a machine token | `aud` (the same on every kind, so the verifiers read it directly) | `aud` (a single string; `sub`, `azp` and `client_id` hold it too) |

`dialect.Raw` holds the IdP-specific claims as decoded, every kind's side by
side, so one wire struct decodes a token of any kind and the dialect reads
only its own kind's. It is embedded in a verifier's wire struct beside
`jwt.RegisteredClaims`, so it never tags a registered claim name (a duplicate
tag at one depth makes encoding/json drop both fields). `dialect.Org` and
`dialect.Machine` are the two questions.

Fail closed: a user-only route refuses a token the dialect cannot place as a
user's or a machine's, rather than taking it for a user's, and a machine
route (`PublisherTokenVerifier`) refuses it rather than taking it for a
machine's. A `Dialect` of a
kind the build does not know (the zero value included) reads no org and
places no token, and a token of one kind read by the other's dialect names no
org.

### One org under the IdP's own handle

An Asgardeo tenant names its org by the tenant's handle, which is not the
org's OpenChoreo namespace. `Dialect.WithSingleOrg(idpHandle, platformHandle)`
(wired from `PLATFORM_IDP_ORG_HANDLE` and `PLATFORM_IDP_ORG_NAMESPACE`) maps
it: a token declaring exactly `idpHandle` reads as the org `platformHandle`,
and any other token (another org of the IdP, no handle claim) reads as no
org, so the tenant gate and the machine-token verifiers refuse it. Both
settings are required under `asgardeo` and refused under `thunder`
(`config.Validate`). The mapping is in the dialect because every consumer of
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
