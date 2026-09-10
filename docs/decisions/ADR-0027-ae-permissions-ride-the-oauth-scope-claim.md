# ADR-0027 — AE permissions ride the OAuth scope claim; a deny-by-default gate enforces them in aep-api

Before this, aep-api had no per-endpoint authorization at all: `tenantGate`
resolves and binds the caller's org, but nothing checked what an org member
was allowed to do once bound. The AE→OC RBAC bridge (#743, `internal/authz`)
is outbound-only — it maps AE roles to OpenChoreo actions and provisions OC
`AuthzRole`/`AuthzRoleBinding` CRs so **OpenChoreo** can authorize calls the
platform makes on a user's behalf. It does nothing to protect aep-api's own
handlers. Meanwhile the console already ships permission-gated UI (`ae:build`,
`ae:model-config`, …, `apps/console/src/auth/permissions.ts`) built against a
hardcoded stand-in permission set, with an explicit comment that it is waiting
on a real backend source. Terms: [CONTEXT.md](../../CONTEXT.md).

## Decision

**AE permission keys ride the standard OAuth `scope` claim aep-api already
parses — no new JWT claim.** A permission key (`ae:build`, `ae:model-config`,
…) is indistinguishable in shape from an OAuth scope string, so it arrives as
a space-delimited entry in `scope` alongside unrelated scopes (`openid`,
`system`, …). `auth.Claims.Permissions()` (`internal/platform/auth/jwt.go`)
filters `scope` down to the entries matching a known `authz.Permission`
constant (`internal/authz/role_permissions_catalog.go`) — that filtered set is
the caller's permissions, full stop.

A new Thunder-issued custom claim was considered and rejected: Thunder's
user-attribute list appears fixed, not a generic claim-mapping facility — see
ADR-0022's "Thunder rejects custom attributes (400 USR-1019)". Reusing the
scope claim needs no IdP-side schema change, only a resource-server / scope
grant configuration, which is assumed already in place (out of scope for this
decision — see Non-goals).

**Enforcement is a deny-by-default gate, `internal/edge/permission_gate.go`,
mirroring `tenant_gate.go`'s structure exactly.** `permissionGate` sits in the
strict middleware chain beside `tenantGate`. Every contract operationID must
appear in exactly one of:

- `operationPermissions` — the permission(s) that satisfy it. A multi-entry
  requirement is OR'd (holding any one suffices), mirroring the console's
  `useHasAnyPermission` for a page readable by either a write or a view-only
  permission.
- `permissionGateCarveOuts` — an explicit "no permission required" entry,
  grouped and commented by reason (pre-org bootstrap, the authz domain's own
  onboarding endpoints, no traced frontend caller, …).

`TestPermissionGateCoverage` enforces the split exhaustively: an operation in
neither map, or in both, fails the build. A new contract operation can
therefore never ship silently ungated — the same "forgetting to gate is not
representable" posture `tenantGate` already established.

**`UpdateConfig` is a field-aware special case.** One operationID
(`PATCH /config`) spans three permission domains through the same request
body — `gitProvider` needs `ae:github-config`, `llm`/`codingLlm` need
`ae:model-config` — so a flat operationID→permission entry cannot express it.
`updateConfigPermissions` inspects which `patch.Field[T]` sections are `Sent`
and requires every touched section's permission (AND, not the OR semantics of
a plain `operationPermissions` entry). Its `idp` section has no permission
assigned yet — no console UI writes it today, so an idp-only patch is
presently unrestricted.

**Non-goals**, deliberately out of scope for this decision:

- Configuring Thunder's resource server / per-role scope grants.
- Provisioning any real user into `ae-admin` (or any AE role) in Thunder —
  nothing in the platform does this today, so every gated operation 403s for
  every real (non-mock) user until a separate workstream lands it.
- Any frontend change — `AuthGuard.tsx`'s hardcoded permission stand-in is
  untouched; it does not read from this gate or from the JWT's real scope.
- Expanding `role_permissions_catalog.go` / `oc_permissions_catalog.go` beyond
  the single `ae-admin` role and the placeholder OC-action mappings — tracked
  separately per #743.

## Alternatives considered

**Deriving permissions server-side from the existing `groups` claim +
`role_permissions_catalog.go`.** Lower-risk (the `groups` claim is proven live
today for project-scoped roles) and needs no Thunder-side scope-grant
configuration. Not chosen: reusing Thunder's native scope model was the
explicit direction, on the basis that the necessary Thunder-side
configuration is a separate, already-assumed-complete workstream.

**A new Thunder-issued `permissions` claim.** Rejected — see Decision. Would
also duplicate the role→permission mapping outside this repo, in Thunder's
own config, rather than reusing `role_permissions_catalog.go`.

**Guessing a permission for every operation with no frontend evidence.**
Rejected for the roughly 50 operations (mostly reads) with no traced console
caller and no permission-shaped name. Assigning a guessed permission risks a
real functional regression — a real user's role might not include the guess,
silently breaking a flow that works today with no one having decided that on
purpose. Carved out explicitly instead, grouped and reasoned in
`permission_gate.go`, so the gap is visible and trackable rather than either
silently open or silently (and wrongly) closed.

## Consequences

- `authz.Permission` (`internal/authz/role_permissions_catalog.go`) is now the
  single typed source of truth for the 7 AE permission keys, shared by the
  role→permission catalog, the AE→OC action catalog (`oc_permissions_catalog.go`),
  and this gate — no more bare string literals drifting between packages.
- 32 of 87 contract operations carry a real permission requirement; 55 are
  explicit carve-outs. That carve-out set is not yet a complete authorization
  boundary — it includes ~37 unaudited reads/streams under one blanket note,
  and a handful of mutations (`TriggerBuild`, `RevalidateBuild`,
  `UpdateComponentConfig`, `ApplyFiles`) individually flagged `TODO(authz)`
  because they write state with no traced caller to justify a carve-out
  beyond "nothing calls this yet."
- `RevealTestUserPassword` is deliberately stricter server-side (`ae:build`)
  than the console's current button gate (`ae:build` or `ae:build-view`,
  either satisfies) — the frontend gap reads as unintentional, not a design
  choice this decision preserves.
- Until a real user can be provisioned into `ae-admin`, this gate is
  correctness-tested but not yet exercised end-to-end against a live Thunder
  token — that gap is the Non-goals' "role provisioning" item, tracked
  separately.
