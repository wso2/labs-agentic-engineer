---
name: security-design
description: Reuse catalog roles when a design has sign-in, permissions, or test users — write specs/design/security.json.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Security design

Write `specs/design/security.json` when the design has sign-in, permissions,
or test users. A public-only design records that in `publicComponents` if
anything is still protected; otherwise skip the file.

The platform provisions from it (Roles gate / ensure: roles and test users;
thunder-app create: `thunder`). The coding agent matches `roles[].name`,
permissions, `coldStartRole`, and `publicComponents`. `architecture` still
declares the `thunder-app` dependency on the SPA and each protected API; this
file authors the Thunder client those components share.

---

## Reuse before you invent

Roles are **shared**, not project-scoped: their scope is the identity provider's
scope, so two projects naming the same role mean the same role, and a person who
holds it holds it everywhere.

**Call `list_roles` before you author a single `roles[]` entry.** Take a matching
row's `name` verbatim. Pick by `description`, not by which name echoes the
requirement's wording — `Compliance Admin` already in the catalog beats a fresh
`Compliance Officer` that means the same job. This is the same reuse-first rule
the `architecture` skill applies to external resources and platform resource
types.

A row with `platformCreated: false` is a group somebody made by hand — most
notably `Administrators`, which administers the platform itself. You may reuse
the name if it genuinely is the role your PRD's actor describes, but the platform
will not put a test user in it, so prefer a role of your own.

## Every PRD Actor gets a role, and no role exists without an Actor

Roles come from the PRD's Actors section. Define no role the PRD has no actor
for, and give every actor a row. Cite the stories each role serves.

## Every role gets a test user

A test user is an account that exists so a role's behaviour can be exercised —
the validation agent signs in as one to judge role-gated acceptance criteria.
The platform generates its password at Build, seals it, and publishes it in the
Roles gate ticket — that ticket is where the validation agent reads its login.
A test user is a **disposable account for automated agents**, readable by anyone
who can read the repository — never a person's account.

Emit one per role, named `test-<role-slug>` (`Compliance Admin` →
`test-compliance-admin`), so the user sees them in Security and can rename them
before Build. **The platform supplies any you omit**, so a missing test user is
never a blocked build — but naming them yourself is what lets the user recognise
and change them.

A username the platform did not create (`jsmith`) is a refusal, not a password
reset: that role has no working login, and a real person's name lands in a
published ticket. Invent no password anywhere in the design; a write that adds
one is rejected.

## The file

```json
{
  "version": 1,
  "coldStartRole": "Viewer",
  "publicComponents": ["expense-webapp"],
  "roles": [
    {
      "name": "Viewer",
      "description": "Reads own claims.",
      "stories": [1],
      "grantedBy": "first sign-in",
      "permissions": [
        { "component": "expense-api", "actions": ["read own claims"] }
      ]
    },
    {
      "name": "Compliance Admin",
      "description": "Approves submitted claims.",
      "stories": [3, 7],
      "grantedBy": "Compliance Admin",
      "permissions": [
        { "component": "expense-api", "actions": ["approve claim"] },
        { "component": "expense-webapp", "screens": ["Approvals"] }
      ]
    }
  ],
  "testUsers": [
    { "username": "test-viewer", "role": "Viewer" },
    { "username": "test-compliance-admin", "role": "Compliance Admin" }
  ],
  "thunder": {
    "name": "Expense Tracker",
    "type": "browser",
    "scopes": "openid profile email group ou"
  }
}
```

| Field | Rule |
|---|---|
| `version` | Always `1`. |
| `coldStartRole` | The role a caller holds when their identity-provider groups match no declared role — a real person's first sign-in — or `null` when such a caller reaches nothing. When non-null, it must name a declared role. See **the cold start** below. |
| `publicComponents` | Components that serve unauthenticated traffic. Absence of sign-in is a decision, so write it down rather than leaving it to be inferred from silence. |
| `roles[].name` | Verbatim — it becomes the identity-provider group name and reaches the app as a `groups` claim. Reuse a catalog name where one fits. |
| `roles[].description` | What the role is for. A **create-time seed only**: a shared role may already have been described by somebody else, and the platform never overwrites that. |
| `roles[].stories` | The PRD story numbers this role serves. At least one. |
| `roles[].grantedBy` | How a person comes to hold it: the name of the role that can grant it, or `first sign-in` for the cold-start role — the one a caller holds with no matching group, granted by nobody. |
| `roles[].permissions[]` | Per component: `actions` for a service, `screens` for a web application. At least one entry, and each entry grants at least one of the two. |
| `testUsers[].username` | Lowercase letters, digits, `.`, `_`, `-`. Username and role only. |
| `testUsers[].role` | Must match a declared `roles[].name`. |
| `thunder.name` | 1–100 characters. Thunder `name` on the wire — not the `design.json` dependency name, not `clientId`. |
| `thunder.type` | Always `"browser"`. |
| `thunder.scopes` | Always `"openid profile email group ou"` (or a longer set that still includes `group` and `ou`). |

`thunder` is `name`, `type`, and `scopes`. The platform owns client identity,
redirect URIs, and grants.

## Answer the cold start

A matrix whose every role is granted by somebody else describes a system nobody
can enter. `coldStartRole` names the role a caller holds while their
identity-provider groups match no declared role.

**It is for real people, not the test accounts.** The platform enrols every test
account in its role's group, so those always match; this is the first sign-in of
somebody the org never enrolled. The default is the PRD's least-privileged
actor, so a fresh deployment is usable by whoever arrives, and every role above
it is granted by someone who already holds one — `grantedBy` names who. Say so
explicitly where the system needs a different origin: an admin admits people, an
import loads them, the first user becomes the admin. `null` is a real answer,
but only for a system where such a caller genuinely reaches nothing — the app
then answers them 403, which nobody can clear from inside the app.

It has a second effect worth knowing when you choose it: the test account for
this role is the one the platform serves when a caller asks for credentials
without naming a role.

---

The organization skill's Security & compliance and Authentication defaults
apply before you invent policy — a filled org entry is the decision. Nothing
here creates anything: the platform creates the roles and test users when the
user clicks Build. `thunder-authentication` owns the build-time mechanics the
coding agent implements; this skill owns the design decisions it consumes.
