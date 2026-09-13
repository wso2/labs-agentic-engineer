# ADR-0030 — Scopes are the authorization authority for a generated app

**Status:** Accepted · 2026-09-13
**Supersedes the role half of:** [ADR-0022](ADR-0022-roles-and-test-users-are-shared-directory-objects.md),
whose group and test-account model stands unchanged.

## Context

[ADR-0022](ADR-0022-roles-and-test-users-are-shared-directory-objects.md) made a
design's roles and test users real on a directory, and stopped there. Nothing in
a running generated app consumed them: a token carried the holder's groups, the
gateway checked only that a token was present, and the service trusted whatever
reached it. A validation agent signing in as `Viewer` could call the approval
endpoint and get 200, so `security.json`'s matrix described an access control
the deployed system did not have — a document, not a control.

Three properties of the gateway this repo deploys (API Platform `gateway-1.2.2`,
jwt-auth policy `v1.3.0`) are load-bearing below, and all three were measured
rather than read:

* the policy compares scope strings WHOLE. `claims:read-all` does not admit a
  caller to an operation guarded on `claims:read`;
* a policy has ONE failure status. A missing token, an expired one and a valid
  one lacking the scope are the same answer on the wire;
* it emits no `WWW-Authenticate`, so that answer carries no reason either.

## Decision

**A generated app's authorization is decided by SCOPES: handles a project's own
permission catalog declares, granted to that project's roles, named one per
operation, and carried on a token minted for that project's resource server.**

**1. The catalog is the authority; groups only say who holds a role.**
`specs/design/security.json` declares a two-level catalog of `resource:action`
handles, each role's grants over it, and which operation every screen reaches.
A token's group claims say which roles the account holds; the handles on the
token are what anything checks. Nothing implies anything — whole-string compare
at the gateway — so a design must not be able to mean an implication the runtime
does not make: a role granting `x:read-all` without `x:read` is refused by name,
at authoring time.

**2. One scope per operation.** An operation's `security` is absent (it inherits
the document default), `[]` (public), or exactly ONE requirement object naming
`oauth2` with at most one scope. Two requirement objects, or two scopes in one,
are refused. "Which handle does this operation need" then has one answer, and
the write gate, the build gate, the gateway projection and the generated service
all read the block with the same reader, so none of them can disagree about it.

**3. The "all" handle widens the ROWS, it never replaces the operation.** An
action declares its `ownership` — `own` (the caller's rows) or `any` (every
row) — so a capability that needs both is two actions. A role that may read every
claim holds `claims:read-all` IN ADDITION to `claims:read`, the list operation
still names `claims:read`, and the one `GET /claims` returns the caller's rows
for the first role and every row for the second. Guarding the list operation on
`claims:read-all` instead would make one operation need a different handle per
caller, which no per-operation policy can express.

**4. A project owns a resource server, and its identifier is the token
audience.** It is derived from `(org, project)` —
`https://aep.wso2.com/orgs/<org>/projects/<project>` — and nothing else. The
identifier never resolves: no DNS record, no endpoint. The SPA sends it as the
RFC 8707 `resource` indicator, the directory binds it into the token's `aud`,
the gateway pins it, and the build's ensure creates the resource server and the
catalog under it. Derivation rather than lookup, because the parties that must
agree on the string never speak to each other, and a hostname would make one
project's audience differ between the local plane and a cloud one.

**5. The service enforces the same scope the gateway does.** The gateway is the
first check, not the only one. It maps the token's `scope` claim onto
`x-user-scopes`, and the generated service's middleware re-checks the
operation's own handle against that header before the handler runs. A trait that
failed to render, a route reached by another path inside the cell, or a policy
somebody relaxed is then a refusal instead of an open door. The header is the
gateway's to set: everything in front of the service strips an inbound one, the
SPA's own `/api` proxy included.

**6. The gateway answers 401 for every refusal; the browser resolves which kind
it was from its own token.** The wire cannot say — one status, no
`WWW-Authenticate`. So the SPA asks the token it is holding: still valid, and
the refusal is about SCOPE, which renders a Forbidden screen; absent or expired,
and it starts sign-in. Guessing from the status alone produces the failure this
decision exists to prevent — an infinite sign-in loop, entered by a signed-in
user who simply lacks a grant, with nothing in any log. Inside the cell, where
there is no browser to confuse, the service keeps the two apart properly: 401
with no identity, 403 with `WWW-Authenticate: insufficient_scope`.

**7. Roles bind to declared groups.** An admin-enrolled role names the org
groups that hold it in `assignTo`, and the build converges that binding; a
self-service role names none, because the app's own registration flow assigns it
per account; a service role names none, because its holder is an application
principal. `assignTo` is the only membership statement a design may make, so the
converge touches groups and only groups — a principal the platform did not put
on a role is never removed by one.

**8. Cold start is gone.** Version 1 served one account to a caller who asked
for credentials without naming a role. There is no such account and no such
field: every published login is a role's login, and an account that holds no
role at all is not created. A signed-in user who holds nothing meets the app's
own no-access state, which is a screen the design has to declare.

## Alternatives considered

**Groups as the authorization authority.** The token already carries them, so
the service could match group names and no catalog would be needed. Rejected:
group names are the ORG's namespace and are shared across projects, so every
generated app would re-derive what a role may do from a name it does not own,
and two projects reusing `Finance` would have to agree on the meaning. The
catalog is per project precisely so a handle means one thing.

**Several scopes on one operation (`anyOf`).** The pinned gateway does have
`scopes.anyOf`. Rejected anyway: with one failure status per policy, a caller
refused by a two-handle guard cannot be told which handle it lacked, and the
design gate's reachability check — "can this role reach the operation behind
this screen" — stops having a single answer to compare against.

**An implied hierarchy (`x:read-all` implies `x:read`).** It would live only in
the gate: the gateway compares whole strings, so the implication would be a rule
the runtime does not have, and the design would pass while the call 403s. The
refusal that names both handles costs the author one line and keeps the two
identical.

**Gateway-only enforcement.** It makes every authorization decision a property
of a trait rendered by a deploy, invisible in the component's own code and
unprovable by its own tests. A generated service is also reachable from inside
its cell without crossing the gateway.

**`403` or a `WWW-Authenticate` hint at the gateway.** Not available on the
pinned gateway, and inventing one by widening the policy would trade a correct
refusal for a nicer error.

**A per-environment audience.** The same version deployed to two environments
would carry two audiences, so a token minted against one would be refused by the
other for a reason no log names. The identifier is environment-independent; it
is the DIRECTORY that is per environment.

## Consequences

**A new grant needs a fresh sign-in.** A refresh narrows a token and never
widens it, so a scope added to a role reaches the token only after the holder
signs in again; one removed disappears at the next renew. Every published
credential block says so.

**A design can be refused for a reachability defect.** If a role can open a
screen whose operation it cannot call, the gate names the role, the screen and
the handle at authoring time — because at runtime it is a bare 401 the SPA
cannot tell from an expired session.

**The permission catalog is project-scoped, and so is the resource server.**
Both are converged by the project's build and removed by its delete, unlike the
groups and accounts of ADR-0022, which stay shared and additive.

**An API whose callers are services is not audience-pinned.** The platform does
not mint those tokens and cannot assert their audience, so pinning would 401
every call such an API serves today. Those APIs are gated on sign-in, not on the
project's `aud`.

**Every test account is a role's account.** A project whose design declares only
self-service or service roles publishes no logins at all, and the gate ticket
says so rather than publishing a credential that holds nothing.
