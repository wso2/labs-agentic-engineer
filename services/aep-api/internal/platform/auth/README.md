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
| `PublisherTokenVerifier` | A per-org machine token (`aep-publisher-<org>`): the client is the audience with the prefix, and the org handle the dialect reads, declared outright, must equal the one in that audience. |
| `StudioClientVerifier` | The same over the `ae-studio-` prefix, plus the client must be the one recorded for the org. |
| `RunnerAuthorizer` · `MCPGate` · issues MCP | The route gates over those verifiers. |
| `SREHandoffVerifier` | A static key, no IdP. |

## The token dialect (`auth/dialect`)

A **token dialect** is the claim names and rules one IdP kind uses to say who
the caller is. The kind is a setting, `PLATFORM_IDP_KIND` (empty is
`thunder`; an unknown value fails `config.Validate` at boot), and every
verifier reads the IdP's claims through `dialect.ForKind(kind)`. One kind
exists:

| Question | `thunder` |
|---|---|
| the org's handle (the OpenChoreo namespace) | `ouHandle`, else `ouName`, else `ouId` — the console mirrors this precedence. The fallback serves legacy user tokens only: the publisher and studio verifiers require the declared `ouHandle` (`Org.HandleDeclared`) |
| the org's id | `ouId` |
| the org's display name | `ouName` |
| is the token a machine's? | `grant_type == client_credentials`; a token without `grant_type` answers neither |
| which client minted a machine token | `aud` (the same on every kind, so the verifiers read it directly) |

`dialect.Raw` holds the IdP-specific claims as decoded; it is embedded in a
verifier's wire struct beside `jwt.RegisteredClaims`, so it never tags a
registered claim name (a duplicate tag at one depth makes encoding/json drop
both fields). `dialect.Org` and `dialect.Machine` are the two questions.

Fail closed: a user-only route refuses a token the dialect cannot place as a
user's or a machine's, rather than taking it for a user's.

## Invariants

- Consumers read `Claims.OrgHandle` / `OrgID` / `OrgName` and
  `TokenClaims` likewise; the claim names live in `dialect` alone.
- `ocauth` (the public overlay seam) keeps its `OuHandle` / `OuName` / `OuId`
  field names as its contract; it is fed from these projections.
- No unsigned-claim path: a nil JWKS cache refuses every token.
