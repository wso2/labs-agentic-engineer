# identity — Roles & Test users

> **L2 · a domain.** Part of the [aep-api architecture](../../README.md).

The platform's record of the identity-provider objects it creates for **one
environment of one org**: the **Roles** a project's design declares, and the
**Test users** that exist so those roles' behaviour can be exercised. It owns the
build-time **ensure** that makes them real, the design-time **catalog** that lets
a design reuse one instead of minting a near-duplicate, and the **sealed
passwords** the validation agent signs in with — published to the build's roles
gate ticket, which is where that agent reads them.

There is no single directory. Every environment of every org has its own identity
provider — the environment tier, "T2" — and an account minted on one is rejected
by every other. So nothing here names a role or an account without also naming
the `(org, environment)` it belongs to: that pair is `Scope`, and it is a **key**,
not a filter.

```mermaid
flowchart LR
  API(["/api/v1"]) --> HTTP
  MCP(["/mcp"]) --> CAT
  BUILD[[dependencies · provisioning]] -->|roles gate| ENS
  ENS -->|logins, published on the gate ticket| BUILD
  subgraph identity
    HTTP["rolespanel — the Security panel's read + reveal/rotate/delete"]
    ENS["ensure — accounts, then roles; idempotent; no model"]
    CAT["catalog — the roles that already exist"]
    PANEL["store — idp_roles · test_users · test_user_refs"]
    HTTP --> PNL["panel — the fenced service"]
    PNL --> PANEL
    ENS --> PANEL
    CAT --> PANEL
    ENS --> TGT["target — WHICH (org, env) directory"]
    CAT --> TGT
    PNL --> TGT
  end
  ENS -->|security.json at the tag| SPEC[[spec]]
  TGT -->|binding + credential, per (org, env)| ADP[[app/identity_targets.go]]
  ADP -->|groups + users| IDP[[clients/thundersvc]]
  PANEL -->|AES-256-GCM| SEC[[platform/secrets · ColumnCipher]]
```

## Owns

| | |
|---|---|
| `ensure.go` | The build-time ensure: read `specs/design/security.json` at the tag and make everything it declares real — the org groups, the test accounts, the project's OAuth resource server and its permission catalog, and the `<project>/<Role>` roles with their group assignments — and return every account's login, its roles and its scopes for the gate to publish. Six passes; see **Passes** below. |
| `catalog.go` | The design-time read: every group on the org's environment directory, with whether the platform created it, how many members it has, and how many projects bind a role to it. Backs the `list_groups` MCP tool. |
| `target.go` | `Scope` — the `(org, environment)` pair that names one directory — and the `TargetResolver` port that turns an org into a `Target`: that pair, the issuer, and a `Directory` already bound to it. |
| `repository.go` | `idp_roles`, `test_users`, `test_user_refs` — all three keyed by `Scope` — and the sealed password column. |
| `panel.go` | The Security panel's domain service: the live-state read (degrading to `directoryAvailable: false` rather than failing), and reveal / rotate / delete behind the org+project and ownership fences. The read answers with TWO role lists — the shared org-group catalog and this project's OWN roles, with the groups each is assigned to, how many projects lean on those groups, and what each role grants — plus, per test login, every role it holds and the union of their scopes. |
| `teardown.go` | The project delete's counterpart to the ensure: remove this project's `<project>/<Role>` roles (assignments first), its resource server and the whole catalog under it, and its own rows. Shared objects — org groups, accounts — are never deleted, and every step is best-effort and reported rather than fatal. |
| `resource_server.go` | The two names a project's authorization objects are known by — `ResourceServerIdentifier` (the token's `aud`) and `RoleName`/`RoleNamePrefix` — derived from `(org, project)` alone, because the parties that agree on them never speak to each other. |
| `entities.go` | The stored rows — `IdPRole`, `TestUser`, `TestUserRef`, `IdPResourceServer`, `IdPRoleBinding` — and the `Scope` fences on them. |
| `rolespanel/` · `httpapi/` | The panel's HTTP slice and the aggregator the edge embeds. |

## Ports

| Port | Satisfied by | Mapped at |
|---|---|---|
| `TargetResolver` | the Environment's `aep.wso2.com/thunder-*` annotations (`clients/openchoreo`) + the admin credential at the binding's secret path (`platform/secrets`) | `app/identity_targets.go` |
| `Directory` | `clients/thundersvc`, one client per `(org, environment)` — groups and users, plus resource servers, resources, actions, roles and assignments | `app/identity_adapters.go` |
| `DesignReader` | `spec.ArtifactService.GetDesignAtTag` | `app/identity_adapters.go` |

`TargetResolver` has two methods split by whether they can fail. `Scope(orgID)`
is pure — it is the choice of environment alone, and the panel needs it to read
the platform's own rows for an environment whose directory is unreachable.
`Resolve(ctx, orgID)` performs the two network reads and returns the bound
`Directory`. It takes the **org** and not the environment on purpose: every build
deploys and validates in exactly one environment today, so that choice is made
once at the composition root rather than at each of the four call sites here.
When a run carries its own environment, `Resolve` grows a parameter and nothing
else about this design moves.

The adapter caches one client per `(org, environment)` and drops the entry when
the identity provider **rejects** its credential — a rotated admin secret — with
a ten-minute TTL as the backstop for a stale secret that fails at the token mint
and so never produces a rejected call. Anything that is not a rejection leaves
the entry alone.

Outbound, this domain is consumed through two ports declared elsewhere:
`provisioning.RolesEnsurer` (the build gate, which also publishes the logins the
ensure returns) and `mcpdiscovery.GroupCatalogLister` (the design-time tool). Both
are mapped in `app/identity_adapters.go`, which is what lets this domain name no
client package and lets those domains name no entity of this one.

There is deliberately no port onto `validation.CredentialProvider`: a validation
agent reads a test user's login from the gate ticket the build published it in,
not from a platform callback. One published copy cannot disagree with itself.

## Passes

`EnsureForTag` runs six passes against one `(org, environment)` directory, and
the order is load-bearing.

| # | Pass | What it does |
|---|---|---|
| 0 | Classify | Reads only. Each org group the design needs is settled as one the platform owns, one somebody else made, or one that is absent. An `assignTo` naming a group that neither `groups[]` declares nor the directory holds **fails the gate by name**, before any account is minted — creating it instead would mint an org-wide group nobody asked for and leave the role the design meant to reach unreachable. |
| 1 | Accounts | Test users, and the decision of **how** each will come to hold each of its roles: through a group the platform owns (enrolment, pass 2) or bound to the account itself (a user principal, pass 5). An account whose every role assigns to no group at all is not created — see *A test login always holds its roles* below. |
| 2 | Groups | Additive. Each absent declared group is created complete with its members; missing members are added to the ones the platform owns. Enrolment rides here, in the union of the `assignTo` groups of every role the account holds. |
| 3 | Resource server | Find by the derived identifier (`ResourceServerIdentifier`), create when absent, record `idp_resource_servers`. |
| 4 | Resources and actions | Converge the permission catalog to the tag. Creates first, then deletes leaf-first. Handles are immutable, so a rename is a delete plus a create; deleting an action cascades out of every role that granted it, so nothing has to strip permissions first — and nothing may cache a role's grants across a catalog edit. |
| 5 | Roles | Converge `<project>/<Role>` (one write per changed role, assignments survive it), converge each role's **group** assignments to `assignTo`, **add** the test-account principals pass 1 settled, unassign-then-delete the roles the tag no longer declares, rewrite `idp_role_bindings`. |

Pass 4 runs before pass 5 because the directory refuses a role granting a
permission no action derives. Pass 2 runs before pass 5 because a membership edit
mints a NEW group id, and the assignment has to name the current one.

There is **no allowlist pass**. The OAuth client's `scopes` list is written onto
the `thunder-app` CR by the provisioning overlay, not from here — and it enforces
nothing either way: ThunderID 1.0.0 stores the field, reads it back, and silently
drops an unknown or ungranted scope. The write gate on `security.json` is what
keeps a stale handle out.

## Invariants

**Groups and test users are SHARED within one `(org, environment)`, not
project-scoped.** That pair IS the identity provider. Two of an org's projects
naming the same group mean the same group — that reuse is the point of
`assignTo` — and a person who is in it is in it everywhere **in that
environment**, while the same name on another environment is a different group,
on a different directory, that shares nothing. (A *role* is the other way round:
it is project-owned and becomes `<project>/<name>` on the directory.) `idp_roles`
and `test_users` are keyed by `(org_id, environment, name | username)`;
`test_user_refs` carries the project on top of that, and every panel mutation
goes through it.

One consequence closes a disclosure that was open while a single IdP served the
cluster: an account exists on exactly one org's environment directory, so every
project that can reference it belongs to that org. The panel's "referencing
projects" list and the count behind the delete warning are now one query, not a
name-returning read plus a bare cross-org count.

**The published login names its issuer.** A username and password say nothing
about where to sign in once there is an identity provider per environment, so
`Result` carries the `Issuer` and the `Environment`, and the roles gate prints
both beside the credential table.

**A row here IS the ownership marker.** The identity provider rejects custom
attributes, so the platform cannot stamp "I made this" on the object itself. Two
rules follow, and they are the whole safety story:

- *The platform enrols members only into roles it created.* A group with no
  `idp_roles` row is somebody else's — `Administrators`, which
  `setup-aep.sh` binds to OpenChoreo's `admin` role, above all — and is left
  entirely untouched. A rule, not a denylist, so every hand-made group is
  protected without a list to maintain. The test account still gets the role
  (below); what it does not get is membership.
- *The platform modifies only accounts it owns.* A username that exists with no
  `test_users` row is refused, never adopted: otherwise a design naming a real
  person would reset their password and hand it to a validation runner.

**A test login always holds every role the ticket publishes it under.** There
are two ways it can, and pass 1 picks one per role:

- the role assigns to a group the platform owns → **enrolment**, the normal path;
- the role assigns only to groups somebody else made — the design's own
  `Approver → Finance (reused)` — → the role is bound to the **account** as a
  user principal. The group is not touched: no member is added, its id does not
  change. A `<project>/<Role>` is created and owned by this project and grants
  only what this project's catalog declares, so binding one to a disposable
  account carries none of the authority membership of a reused org group would.

A role that assigns to no group at all (self-service, service) is granted by
neither, and an account whose every role is of that shape is **not created** and
is reported in `UsersSkipped`: a standing credential for a login that holds
nothing is worse than no credential, because validation would sign in as it and
grade role-gated criteria against it.

The converge never REMOVES a user principal, the platform's own included. A test
account that is deleted and recreated gets a new directory id, so the principal
left behind names an account that no longer exists and grants nobody anything,
and the project teardown deletes the role outright. Removing them would mean the
converge deciding which user principals are the platform's — a second ownership
rule, for a stale row that is already harmless.

**Project-owned converge, shared additive.** The line runs through the middle of
this domain, and every delete question is answered by which side an object is on.
*Project-owned* — the resource server, its resources and actions, the
`<project>/<Role>` roles and the bindings recording them — is created by exactly
one project and carries its name, so the ensure converges the set on every build
(what the design dropped is deleted) and a project delete removes it outright
(`teardown.go`). *Shared* — org groups and accounts — keeps ADR-0022's additive
rules: nothing here ever removes one. A group two projects assign a role to is
exactly the intended reuse; un-assigning a role touches the binding, never the
principal. A role dropped from a design leaves the shared group standing and only
`test_user_refs` changes. The panel deletes a TEST USER on request; it offers no
group delete, and there is no code here that can remove one, because a group is
shared, outlives every project that names it, and may hold real members this
platform never created — removing one is an operator action on the identity
provider.

Two consequences of that line the ensure has to earn. **A rebuild of an unchanged
tag makes zero directory writes**: every `Ensure…` verb is contractually no-write
when the directory already matches, and that is not an optimisation — deleting an
action cascades out of every role that granted it, so a converge that churned the
catalog would briefly revoke every grant in the project on every build. And **the
assignment converge touches GROUPS only**: a user principal is somebody
self-service registration or an administrator put on the role, and an app
principal is a service identity, neither of which `security.json` can describe —
so reading "not in `assignTo`" as "remove" would silently revoke them all.

**The login is PUBLISHED, and only the ensure decides what goes out.** The gate
posts every referenced account's username and password as a comment on its
ticket, because the validation agent reads its login from there. Three rules keep that
honest: an account the ensure refused or skipped is never published (it would
name a real person beside a password the platform never set); a seal that will
not open publishes the row with an empty password for the ticket to call
unavailable, rather than failing the build or printing a blank; and `Summary()`,
which is the half of the result that reaches logs, never carries a password.

**No model is ever in the loop below the version tag.** A model authors
`security.json` and reads the catalog. Everything from the tag down — the gate, the
ensure, the directory writes, the sealing — is deterministic code. These calls
mint credentials, so that boundary is the single most important property here.

**The password is sealed because it cannot be read back.** `GET /users/{id}`
returns no password field, so a credential could otherwise be issued exactly once
and never served again — a rebuild would have nothing to publish for the accounts
it reused, and the panel nothing to reveal. The seal uses `secrets.ColumnCipher` under
`credential-encryption-key`, the same framing as `publisher_client_secret`, and
opens with `Open` rather than `OpenTolerant`: there is no migration window here,
so a decrypt failure must be an error, not a base64 blob handed out as a
password.

**Membership is written by delete-and-recreate.** The identity provider sets
group members only at group creation (`PUT /groups/{id}` accepts `members`,
answers 200, and ignores it). `thundersvc.AddGroupMembers` therefore reads the
current membership and recreates the group with the union, all under one
per-group-name lock — the read has to be inside the lock, or two concurrent
builds each write over the other's member. The group's id changes; nothing keys
on it, because the token claim and the authz bindings both use the NAME.

## See also

- [`ADR-0022`](../../../../docs/decisions/ADR-0022-roles-and-test-users-are-shared-directory-objects.md) — why the BFF writes the directory directly, and its 2026-09-05 amendment moving the sharing scope from the cluster to one `(org, environment)`.
- [`ADR-0028`](../../../../docs/decisions/ADR-0028-the-platform-idp-is-neutral-infrastructure.md) — the platform IdP is neutral infrastructure and holds no roles or test users.
- `deployments/scripts/setup-environment-thunder.sh` — provisions an environment's identity provider and writes the binding this domain resolves through.
- `skills/security-design/SKILL.md` — the agent-facing half: `security.json` and the reuse-first rule.
