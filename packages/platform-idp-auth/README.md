# @aep/platform-idp-auth

The Platform IdP token check shared by the AE Studio pod's TypeScript
containers (`ae-design-agent`, `ae-collab`). The Go twin is
`ae-studio-tools/internal/auth`; both apply the same rules.

| Export | Does |
|---|---|
| `createVerifier({ issuer, jwksUrl, kind? })` | returns `verify(token, kinds)`. Checks the signature against the IdP's JWKS (cached 10 min, refetched on an unknown `kid`, 30 s cooldown), the exact `iss`, a required `exp` (5 s leeway) and the algorithm (RS256 or ES256). The token must carry a `kid`, and its `aud` must name one of `kinds`; a token matching no kind is refused. The IdP's own claims are read through the token dialect of `kind` (`thunder` when unset): a `user` token needs a non-empty `sub` and is a user's by the dialect; an `ae-studio` token is a machine's by the dialect whose `client_id` is one of that kind's audiences; a token the dialect cannot place as either is refused. The result is a union on `kind`: `user` carries `UserClaims` (`sub: string`); every claims object carries `org` (the dialect's `OrgIdentity`: `handle`, `id`, `name`, `handleDeclared`). |
| `parseDialectKind(value)` | reads `AE_IDP_KIND`: empty is `thunder`; a kind this build does not know throws, so a pod fails at boot rather than running on another kind's rules. |
| `userRule(userClaims, pod)` | the org-only user rule: the org's `id` and its declared `handle` both equal the pod's org (a handle the dialect filled from a fallback claim does not count). |
| `problem(status, code, detail?)` | an `application/problem+json` body, the same shape ae-studio-tools writes (`detail` always present, `""` by default). |
| `UnauthenticatedError` | every refusal from `verify`; map it to 401. |

Wiring errors throw a plain `Error` and fail closed: an empty issuer or JWKS
URL at construction, an empty kind list, a kind without audiences or an empty
audience at `verify`, an empty pod org in `userRule`. They surface as a 500,
never as a pass.

`sub` is optional in `PlatformClaims` (the `ae-studio` kind): a machine
token need not carry one.

## The token dialect

A token dialect is the claim names and rules one IdP kind uses to say who
the caller is. One kind exists, `thunder`: the org is `ouHandle` (else
`ouName`, else `ouId`; only `ouHandle` counts as declared), its id `ouId`,
its name `ouName`; a machine's token has `grant_type` `client_credentials`,
a user's any other grant, and a token with no `grant_type` is placed as
neither. `src/dialect.ts` is the twin of ae-studio-tools'
`internal/auth/dialect` and aep-api's `internal/platform/auth/dialect`.

## Known limitation

The Go twin accepts RS256 only; this package also accepts ES256.
