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
| `ensure.go` | The build-time ensure: read `specs/design/security.json` at the tag, make every role and test user real, and return every account's login for the gate to publish. Three passes — classify, then accounts, then roles created complete with their members. |
| `catalog.go` | The design-time read: every role on the org's environment directory, with whether the platform created it. Backs the `list_roles` MCP tool. |
| `target.go` | `Scope` — the `(org, environment)` pair that names one directory — and the `TargetResolver` port that turns an org into a `Target`: that pair, the issuer, and a `Directory` already bound to it. |
| `repository.go` | `idp_roles`, `test_users`, `test_user_refs` — all three keyed by `Scope` — and the sealed password column. |
| `panel.go` | The Security panel's domain service: the live-state read (degrading to `directoryAvailable: false` rather than failing), and reveal / rotate / delete behind the org+project and ownership fences. |
| `rolespanel/` · `httpapi/` | The panel's HTTP slice and the aggregator the edge embeds. |

## Ports

| Port | Satisfied by | Mapped at |
|---|---|---|
| `TargetResolver` | the Environment's `aep.wso2.com/thunder-*` annotations (`clients/openchoreo`) + the admin credential at the binding's secret path (`platform/secrets`) | `app/identity_targets.go` |
| `Directory` | `clients/thundersvc`, one client per `(org, environment)` (the group + user half) | `app/identity_adapters.go` |
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
ensure returns) and `mcpdiscovery.RoleCatalogLister` (the design-time tool). Both
are mapped in `app/identity_adapters.go`, which is what lets this domain name no
client package and lets those domains name no entity of this one.

There is deliberately no port onto `validation.CredentialProvider`: a validation
agent reads a test user's login from the gate ticket the build published it in,
not from a platform callback. One published copy cannot disagree with itself.

## Invariants

**Roles and test users are SHARED within one `(org, environment)`, not
project-scoped.** That pair IS the identity provider. Two of an org's projects
naming the same role mean the same role, and a person who holds it holds it
everywhere **in that environment** — while the same name on another environment
is a different group, on a different directory, that shares nothing. `idp_roles`
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
  protected without a list to maintain.
- *The platform modifies only accounts it owns.* A username that exists with no
  `test_users` row is refused, never adopted: otherwise a design naming a real
  person would reset their password and hand it to a validation runner.

**Additive only.** Nothing here ever deletes a directory object on its own. A
role dropped from a design, a renamed one, a deleted project — the object stands,
and only `test_user_refs` changes. The panel deletes a TEST USER on request; it
offers no role delete, and there is no code here that can remove one. A role is
shared, outlives every project that names it, and may hold real members this
platform never created, so removing one is an operator action on the identity
provider.

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
