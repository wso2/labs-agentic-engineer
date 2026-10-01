# @aep/platform-idp-auth

The Platform IdP token check shared by the AE Studio pod's TypeScript
containers (`ae-design-agent`, `ae-collab`). The Go twin is
`ae-studio-tools/internal/auth`; both apply the same rules.

| Export | Does |
|---|---|
| `createVerifier({ issuer, jwksUrl })` | returns `verify(token, kinds)`. Checks the signature against the IdP's JWKS (cached 10 min, refetched on an unknown `kid`, 30 s cooldown), the exact `iss`, a required `exp` (5 s leeway) and the algorithm (RS256 or ES256). The token's `aud` must name one of `kinds`; a token matching no kind is refused. A `client_credentials` token never resolves to the `user` kind. |
| `userRule(claims, pod)` | the org-only user rule: `ouId` and `ouHandle` both equal the pod's org. |
| `problem(status, code, detail?)` | an `application/problem+json` body, the same shape ae-studio-tools writes. |
| `UnauthenticatedError` | every refusal from `verify`; map it to 401. |

Wiring errors throw a plain `Error` and fail closed: an empty issuer or JWKS
URL at construction, an empty kind list, a kind without audiences or an empty
audience at `verify`, an empty pod org in `userRule`. They surface as a 500,
never as a pass.

`sub` is optional in `PlatformClaims`: a `client_credentials` token need not
carry one.

## Known limitation

The Go twin accepts RS256 only; this package also accepts ES256.
