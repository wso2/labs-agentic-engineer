# ADR-0039 — AE permissions ride the OAuth scope claim; a deny-by-default gate enforces them in aep-api

**Status:** Accepted

## Context

`aep-api` had no per-endpoint authorization. `tenantGate` resolved and bound the caller's
organization, and nothing after it asked what that member was allowed to do. Any authenticated
member of an org could call any operation the contract exposed.

The AE→OC permission mapping (`internal/authz`) does not close this. It points outward: it maps AE
roles to the OpenChoreo actions the platform's `AuthzRole`s grant, so **OpenChoreo** can authorize
calls the platform makes on a user's behalf. It says nothing about `aep-api`'s own
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
operation reaching the gate with no declared permission denies rather than passes. Today that is 74
gated operations against 19 carve-outs, each carve-out grouped by the reason it is one.

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
  role catalog, the AE→OC action catalog, and the gate. Two copies of the vocabulary exist outside
  Go and cannot import it — the console's `ALL_PERMISSIONS` (TypeScript, built by a Node-only image
  stage) and the chart's `console.thunder.scopes` (deployment configuration) — so each is reconciled
  by a test that reads the other side's file and names it in the failure:
  `TestConsoleUnionMatchesCatalog` and `TestChartConsoleScopesCoverCatalog`, alongside
  `TestChartAuthzRolesMatchCatalog` for the chart's AuthzRoles. Codegen was tried and removed: a Go
  binary emitting TypeScript bought one fewer hand-edit and cost a generated-and-committed artifact,
  a `go:generate` writing across modules, and a build that broke in the image but not on a laptop.
- The console's scope filter is deliberately by `ae:` PREFIX, not by membership of its own list. As
  an allowlist it withheld a grant the token really carried whenever the list was stale, closing
  every gate on that key for everyone while the BFF went on allowing the call. By prefix, a stale
  list costs the TYPE only: nobody can write a gate on a key the union lacks, which is a compile
  error rather than a silence.
- Two roles ship. `ae-admin` holds every permission. **`ae-developer` is the role that does the
  work on a project** — it states what is wanted, designs it, builds it, and watches what the build
  did — so it holds each of those as a write/view pair (requirement, design, build), plus the two
  reads that make the work legible: observability-view, because alerts and RCA reports are how a
  developer learns their own deployed code is failing, and resource-view, because a design that
  declares a dependency is unreadable without the catalogue it names.
- **The line is the project, not the organization.** What `ae-developer` lacks is everything that
  configures the org rather than a project: both credential permissions, skills, org spend, and
  resource-**config**. That last one is the role's only view without its write, and deliberately so
  — registering a resource for the whole org is an admin act that happens to be reachable from a
  project page.
- **An agent is an ordinary principal.** The SRE agent reaches aep-api through aep-mcp-server with a
  long-lived shared secret rather than a Thunder JWT, and `SREHandoffVerifier` used to mint claims
  with no scope at all — which made its three operations un-gateable, and so carved out of the gate
  entirely: a hole in front of the gate rather than a decision inside it. Those synthetic claims now
  declare `ae:build-view` and `ae:build`, exactly what filing, listing and promoting an issue need
  (each write starts a run), and the three operations are gated on that pair like any other. The
  agent holds what it needs, is refused everything else by the same deny-by-default rule as a person,
  and an MCP tool added later that needs more fails at the gate rather than inheriting a bypass.
  Permissions describe what an operation does, not who tends to call it — which is why this is the
  build pair and not `ae:observability-view`, though the caller is the SRE agent.
- The remaining carve-outs have no caller at all and carry a `TODO(authz)`, plus the four that run
  before a caller's permissions can be known or provisioned and `UpdateConfig`, which the gate
  decides from the patch body instead.
- A caller who cannot write a config section can no longer read its audit fields either: `GET /config`
  is answered on either credential permission and then redacted per section, including `agents`'
  `updatedBy`, which exists precisely because that endpoint's permissions are coarse. The org's model
  connection and the runtime that spends it are one setting under `ae:model-config` — ADR-0038 made
  the connection singular and ADR-0036 made the coding credential a subscription riding beside it, so
  splitting a permission between them would describe a boundary the config no longer has.
- `DirectOCStrategy` resolves a tokenless, unmarked OC call to the BFF's own M2M identity. The
  webhook and dispatch paths depend on it and `tenantGate` keeps console traffic away from it, but
  it is a fail-open and is logged as one, pending the audit that lets it become a refusal.
