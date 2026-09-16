# ADR-0027 — AE permissions ride the OAuth scope claim; a deny-by-default gate enforces them in aep-api

**Status:** Accepted

## Context

`aep-api` had no per-endpoint authorization. `tenantGate` resolved and bound the caller's
organization, and nothing after it asked what that member was allowed to do. Any authenticated
member of an org could call any operation the contract exposed.

The AE→OC RBAC bridge (`internal/authz`) does not close this. It is outbound-only: it maps AE roles
to OpenChoreo actions and provisions `AuthzRole`/`AuthzRoleBinding` CRs so **OpenChoreo** can
authorize calls the platform makes on a user's behalf. It says nothing about `aep-api`'s own
handlers. Meanwhile the console already shipped permission-gated UI built against a hardcoded
stand-in permission set — gating that a caller could bypass by calling the API directly, because
nothing on the server side agreed with it.

Terms: [CONTEXT.md](../../CONTEXT.md).

## Decision

**AE permission keys ride the standard OAuth `scope` claim.** A permission key (`ae:build`,
`ae:model-config`, …) is indistinguishable in shape from a scope string, so it arrives as a
space-delimited entry in `scope` alongside unrelated ones (`openid`, `profile`, …).
`auth.Claims.Permissions()` filters `scope` down to the entries matching a known `authz.Permission`
constant; that filtered set is the caller's permissions, full stop. The console derives its own view
of them the same way, from the access token it already holds (`permissionsFromScope`), so both sides
read one source and neither invents a second.

A Thunder-issued custom claim was considered and rejected: Thunder's user-attribute list appears
fixed rather than a generic claim-mapping facility (ADR-0022's "Thunder rejects custom attributes").
Reusing `scope` needs no IdP schema change — only resource-server and scope-grant configuration.

Two consequences of riding `scope` are not optional, and both were found the hard way against a live
cluster. Thunder narrows a requested scope to what the caller's role holds but never grants what was
not requested, so the console must request **every** `ae:*` scope or the omitted one can never be
granted to anyone. And an `ae:*` scope requested without an RFC 8707 `resource` indicator resolves
against the platform's default resource server, silently dropping the scope and rewriting the token's
audience — so the console sends `resource`, and `aep-api`'s `JWT_AUDIENCE` accepts that identifier.

**Enforcement is a deny-by-default gate**, `internal/edge/permission_gate.go`, sitting in the strict
middleware chain beside `tenantGate` and running after it (a claimless request deserves `tenantGate`'s
401, not a permission denial for holding none). Every contract operationID must appear in exactly one
of `operationPermissions` or `permissionGateCarveOuts`; `TestPermissionGateCoverage` fails the build
otherwise, and a mirror test rejects a carve-out naming an operation the contract no longer has. An
operation reaching the gate with no declared permission denies rather than passes. Today that is 65
gated operations against 23 carve-outs, each carve-out grouped by the reason it is one.

**Entry to a surface is gated on its view permission exactly.** A write permission authorizes
mutations; it never admits its holder to a page on its own. `ae:build` does not satisfy a row reading
`ae:build-view`, even though both built-in roles that hold one hold the other — the gate describes
what a permission *means*, not which roles happen to exist today. Where an OR does appear it spans
features (one surface reached from two places, each bringing its own permission), never a write
permission and its own view sibling.

**Two operations cannot be decided by a flat row, and say so in their own terms.**

`UpdateConfig` spans several permission domains through one request body, so
`updateConfigPermissions` inspects which sections the patch actually touches and requires *every*
touched section's permission — an AND, against the OR of a plain row. Its `idp` section is refused
outright rather than mapped: that section repoints the issuer the org's protected APIs pin JWT
validation to, no AE permission describes identity configuration, and nothing in the platform writes
it, so there is no grant to check and no caller to break. The permission arrives with the surface
that needs it.

The spec collaboration room cannot be settled at the gate at all. The gate sees a join; the edits
that follow arrive as Yjs updates on a socket it never inspects again, and `ApplyFiles` sees whose
token makes the request rather than whose edits are in the document. So `validate-collab-access`
answers `canWrite` alongside the identity, and `services/collab` acts on it — marking a viewer's
connection read-only, and keeping their token out of the commit path. Hiding the console's editing
controls is the same rule stated a third time, for the user's benefit rather than the system's.

## Alternatives considered

**Deriving permissions server-side from the existing `groups` claim.** Lower-risk — that claim is
proven live for project-scoped roles — and it needs no Thunder-side scope configuration. Not chosen:
reusing Thunder's native scope model was the explicit direction, and the scope-grant configuration it
requires is bounded and now done.

**A Thunder-issued `permissions` claim.** Rejected as above, and it would move the role→permission
mapping out of this repo into Thunder's configuration.

**Guessing a permission for every operation with no traced caller.** Rejected. A guessed permission
risks a real regression — a role that lacks the guess silently loses a flow that works today, with
nobody having decided that. Those operations are carved out explicitly and grouped by reason, so the
gap is visible and countable rather than either silently open or silently, wrongly, closed.

**Inventing a permission for `idp` so the section could be gated rather than refused.** Rejected for
the same reason in reverse: a permission key costs an entry in the catalog, the console's union, and
six Thunder scope lists that must then stay in step forever — a standing cost for a surface that does
not exist yet.

## Consequences

- `authz.Permission` is the single typed source of truth for the 14 AE permission keys, shared by the
  role catalog, the AE→OC action catalog, and the gate. The console mirrors the list by hand until
  the backend exposes it as a generated contract type.
- Two roles ship: `ae-admin` holds every permission; `ae-developer` holds the requirement-view,
  design and build pair. Org spend and incident reports stay admin-facing.
- The carve-out set is not yet a complete authorization boundary. Three operations are reached only
  by the SRE agent's MCP tools, where what "authorized" means for an agent rather than a person is
  undecided; the rest have no caller at all and carry a `TODO(authz)`.
- A caller who cannot write a config section can no longer read its audit fields either: `GET /config`
  is answered on either credential permission and then redacted per section, including
  `codingAgent`'s `updatedBy`, which exists precisely because that endpoint's permissions are coarse.
- `DirectOCStrategy` resolves a tokenless, unmarked OC call to the BFF's own M2M identity. The
  webhook and dispatch paths depend on it and `tenantGate` keeps console traffic away from it, but
  it is a fail-open and is logged as one, pending the audit that lets it become a refusal.
