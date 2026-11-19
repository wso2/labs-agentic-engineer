# @aep/platform-idp-auth

The Platform IdP token check shared by the AE Studio pod's TypeScript
containers (`ae-design-agent`, `ae-collab`). The Go twin is
`ae-studio-tools/internal/auth`; both apply the same rules.

| Export | Does |
|---|---|
| `createVerifier({ issuer, jwksUrl, claims })` | returns `verify(token, kinds)`. Checks the signature against the IdP's JWKS (cached 10 min, refetched on an unknown `kid`, 30 s cooldown), the exact `iss`, a required `exp` (5 s leeway) and the algorithm (RS256 or ES256). The token must carry a `kid`, and its `aud` must name one of `kinds`; a token matching no kind is refused. The IdP's own claims are read through the token dialect of `kind` (`thunder` when unset): a `user` token needs a non-empty `sub` and is a user's by the dialect; an `ae-studio` token is a machine's by the dialect whose `client_id` is one of that kind's audiences; a token the dialect cannot place as either is refused. The result is a union on `kind`: `user` carries `UserClaims` (`sub: string`); every claims object carries `org` (the dialect's `OrgIdentity`: `handle`, `id`, `name`, `handleDeclared`). |
| `readIdpClaims(env)` | reads the pod's `AE_IDP_CLAIMS_*` settings into `{ claims, problems }`: the names and values of the IdP's own claims, and a `missing KEY` line for each mandatory one that is absent. There are no defaults: the pod knows no IdP. |
| `userRule(userClaims, pod)` | the org-only user rule: the org's `id` and its `handle` both equal the pod's org. |
| `problem(status, code, detail?)` | an `application/problem+json` body, the same shape ae-studio-tools writes (`detail` always present, `""` by default). |
| `UnauthenticatedError` | every refusal from `verify`; map it to 401. |

Wiring errors throw a plain `Error` and fail closed: an empty issuer or JWKS
URL at construction, an empty kind list, a kind without audiences or an empty
audience at `verify`, an empty pod org in `userRule`. They surface as a 500,
never as a pass.

`sub` is optional in `PlatformClaims` (the `ae-studio` kind): a machine
token need not carry one.

## The token dialect

A token dialect is how one IdP's tokens say two things no standard defines:
which org the caller belongs to, and whether the token was issued to a
machine or to a user. It is configuration: `createVerifier`'s `claims`
(`IdpClaims`) names the claims and values, and nothing here knows an IdP.

| `IdpClaims` field | Env key | What it names |
|---|---|---|
| `orgHandle` | `AE_IDP_CLAIMS_ORG_HANDLE` | the claim that carries the handle the IdP knows the org by. Required |
| `orgId` | `AE_IDP_CLAIMS_ORG_ID` | the claim that carries the IdP's own id for the org. Required |
| `orgName` | `AE_IDP_CLAIMS_ORG_NAME` | the claim that carries a display name. Optional (`""`) |
| `callerTypeClaim` | `AE_IDP_CLAIMS_CALLER_TYPE_CLAIM` | the claim that says who the token was issued to. Required |
| `callerTypeMachine` | `AE_IDP_CLAIMS_CALLER_TYPE_MACHINE` | its value that means a machine. Required |
| `callerTypeUser` | `AE_IDP_CLAIMS_CALLER_TYPE_USER` | its value that means a user. Optional: set, any third value is placed as neither; `""`, any value other than the machine's is a user's |

Each is a top-level text claim; a claim of another type is not read. Nothing
stands in for the org's handle. A token with no caller type claim is placed
as neither a user's nor a machine's, and `verify` refuses it. Claims a
standard names (`sub`, `aud`, `iss`, `exp`, `client_id`, `name`, `email`) are
read by those names.

`src/dialect.ts` is the twin of ae-studio-tools' `internal/auth/dialect` and
aep-api's `internal/platform/auth/dialect`. All of them, and the console, run
the sample tokens in `test/fixtures/idp-claims`: for each IdP AE ships claim
values for (ThunderID, Asgardeo), token payloads and how each must be read.
Change a rule in one reader and that file fails the others.

## Known limitation

The Go twin accepts RS256 only; this package also accepts ES256.
