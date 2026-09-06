# thunder-app identity-claim contract

Every OAuth app the operator provisions carries the SAME identity claims, so a
signed-in user's end-user token is role-aware. This is a platform-wide contract
hardcoded in the operator (`internal/thunder/client.go`), NOT a per-app CR field.
Its authoritative copy is the platform console's own registration,
`deployments/single-cluster/thunder-resources/87-aep-console-app.yaml`, and
`internal/thunder/identity_contract_test.go` asserts the two still agree.
Companion to `thunder-application-reconciler.md` and ADR-0006 (why auth is a
platform resource).

## What the operator emits

On BOTH create and update — so an app provisioned before this contract existed
self-heals on its next reconcile rather than staying bare — `EnsureApplication`
sets on the OAuth app:

- the identity attributes `given_name, family_name, username, groups, email,
  name, ouId, ouName, ouHandle` on **both** tokens — at two different paths:

  | token | path |
  |---|---|
  | id token | `token.idToken.userAttributes` |
  | access token | `token.accessToken.userConfig.attributes` |
- `scopeClaims`: `group → [groups]`, `ou → [ouId, ouName, ouHandle]`,
  `profile → […]`, `email → […]`.
- top-level `allowedUserTypes: [Person]` (lets org users authenticate).

The `thunder-app` `ClusterResourceType`'s `scopes` default is
`openid profile email group ou` (was `openid profile email`) — the SPA requests
those, and the operator's `scopeClaims` release the matching claims. The claim
set is a shared constant (`identityUserAttributes`, `tokenClaimConfig`,
`scopeClaimConfig`, `allowedUserTypes`) used by both `createApp` and `updateApp`.

## The access token's attributes live under `userConfig`

ThunderID 1.0.0 takes the access token's user attributes at
`token.accessToken.userConfig.attributes` — nested, and keyed `attributes`, not
`userAttributes`. The id token keeps the flat `userAttributes`. The asymmetry is
the server's; `userConfig` mirrors the `clientConfig` an m2m app uses for the
same purpose (`services/aep-api/internal/clients/thundersvc/client.go`).

Every other shape is accepted with **200 and silently discarded**. Measured
against the live server on 2026-09-06:

| sent | kept |
|---|---|
| `accessToken.userConfig.attributes` | ✅ |
| `accessToken.userConfig.userAttributes` | dropped |
| `accessToken.userConfig.claims` | dropped |
| `accessToken.userAttributes` | dropped |
| `accessToken.validityPeriod` | dropped, replaced by the server default (3600) |

The operator sent the flat shape until 2026-09-06. The result was an app whose
**id token carried every claim and whose access token carried none**, which is
close to the worst possible failure mode: the SPA signs in, reads its role from
the id_token and renders the right UI, then every call it makes with the access
token reaches the gateway, whose `groups → X-User-Groups` mapping injects
nothing, and the generated API answers `403 "caller has no recognized role"`.
Nothing between the write and the 403 mentions a claim.

### Why the write is read back

`EnsureApplication` GETs the application after every create and update and fails
when the identity attributes are not there (`verifyIdentityClaims`). Since
ThunderID does not reject an unrecognised token config, a write that returns 200
is not evidence that anything was stored — the only evidence is reading it back.
A failure surfaces as a ThunderApplication that refuses to go ready, with the
missing attributes named, instead of a 403 in a deployed application hours
later. Confidential (m2m) apps skip the check: no user, no user attributes.

### Why a test compares this file's shape with the console's

The correct shape was already in the repo, in the console's bootstrap document,
before the operator was written. It never reached the operator because nothing
compared them, and the operator's own tests used a fake server that echoed back
whatever it was sent — which can only ever confirm that the package agrees with
itself. Two changes close that:

- the fake now models the real server's *silent drop*
  (`storeTokenConfig` in `client_test.go`), so a wrong shape fails the suite;
- `identity_contract_test.go` reads the console document off disk and asserts
  the operator's paths, attributes and scope claims match it.

Reverting `tokenClaimConfig` to the old shape now fails four tests with a
message naming the attributes that vanished.

Note that `deployments/dev-thunder-setup/bootstrap/60-aep-console.yaml`
legitimately still carries the flat shape: that stack pins ThunderID 0.47.0,
which takes it. The shapes differ **by server version**, so do not align them
without checking which version each targets.

## Why the id_token + the group/ou scopes (the decision)

The SPA reads roles from `user.profile.groups` — i.e. the **id_token**. Verified
empirically on Thunder 0.34 by driving the full auth-code+PKCE flow for a real
org user:

| requested scope | id_token `groups` | access_token `groups` |
|---|---|---|
| `openid profile email` | absent | present |
| `openid profile email group ou` | present | present |

So `groups` reaches the id_token ONLY when the `group` scope is granted (given
the `scopeClaims` map + `idToken.userAttributes` above). That is why `group` and
`ou` are in the default scope set — omit them and a role-aware SPA sees no role
(the original "Unknown role" bug).

**Chosen over** having the SPA decode the **access token** for groups: that
token is opaque to a public client by design, and decoding it in every app (plus
teaching the skill to) is worse than standard-OIDC id_token claims. See the
`thunder-authentication` skill for the SPA side.

**Chosen over** making the claim set a per-app CR / resourcetype parameter:
identity claims are a platform contract that shouldn't vary per app; a per-app
knob only invites the "forgot `groups` → no role" misconfig this fixes. `scopes`
stays the one genuinely variable knob (already a resourcetype parameter), and it
is inert for apps that don't check roles — the extra claims simply go unread.

## Scope

Single-cluster (v1 local) only today: the `thunder-app` operator is not part of
the cloud deployment (`deployments-v2/wso2cloud-deployment` has no
`ThunderApplication`). When end-user auth is promoted there, this contract
travels with the operator source.
