---
name: security-design
description: Use when a design involves sign-in, roles, or permissions — writing the two halves of the security design, specs/design/roles.json (which roles this project uses, what each may do, and its test users) and specs/design/security.md (how a caller's role is resolved), and reusing existing platform roles instead of minting near-duplicates.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Security design

A project's security design is **two documents**, and nothing appears in both.

| | Holds | Read by |
|---|---|---|
| `specs/design/roles.json` | Which **roles** this project uses, what each may do **within this project**, and its **test users** | The coding agent, and the platform |
| `specs/design/security.md` | How a caller's role is **resolved** from a token, and the policy narrative | The coding agent |

The split is not tidiness. `roles.json` is the one design file the **platform acts
on**: when the user clicks Build, it creates every role and test user the file
declares on the platform identity provider — deterministically, with no model
involved. A role name in prose cannot be acted on, and two copies of a role name
can disagree with no tiebreak. So the structured file DECLARES every role and
test user, and the prose file declares none — it may name one while explaining
how a token resolves to it, but it never restates the list or the matrix.

Write both when the design has sign-in. Write neither when it does not — a
public-only design says so in `roles.json`'s `publicComponents` if it has any
protected surface at all, and otherwise skips both files.

---

# 1. `roles.json` — the roles and their test users

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
Its password is generated and published by the platform, in the provisioning
ticket the build opens, because that ticket is where the validation agent reads
its login from. So a test user is a **disposable account for automated agents**,
readable by anyone who can read the repository — never a person's account.

Emit one per role, named `test-<role-slug>` (`Compliance Admin` →
`test-compliance-admin`), so the user sees them in the Security panel and can
rename them before Build. **The platform supplies any you omit**, so a missing
test user is never a blocked build — but naming them yourself is what lets the
user recognise and change them.

**Never name a real person's username.** The platform refuses to touch an account
it did not create, so naming `jsmith` produces a refusal, not a password reset —
but it also produces a role with no working login, and it puts a real person's
username in a published ticket.

## The file

```json
{
  "version": 1,
  "coldStartRole": "Viewer",
  "publicComponents": ["expense-webapp"],
  "roles": [
    {
      "name": "Compliance Admin",
      "description": "Approves and audits submitted claims.",
      "stories": [3, 7],
      "grantedBy": "Compliance Admin",
      "permissions": [
        { "component": "expense-api",    "actions": ["approve claim", "read all claims"] },
        { "component": "expense-webapp", "screens": ["Approvals", "Audit log"] }
      ]
    }
  ],
  "testUsers": [
    { "username": "test-compliance-admin", "role": "Compliance Admin" }
  ]
}
```

| Field | Rule |
|---|---|
| `version` | Always `1`. |
| `coldStartRole` | The role a caller holds before anyone grants them one, or `null` when a caller with no role reaches nothing. Must name a declared role. See **the cold start** below. |
| `publicComponents` | Components that serve unauthenticated traffic. Absence of sign-in is a decision, so write it down rather than leaving it to be inferred from silence. |
| `roles[].name` | Verbatim — it becomes the identity-provider group name and reaches the app as a `groups` claim. Reuse a catalog name where one fits. |
| `roles[].description` | What the role is for. A **create-time seed only**: a shared role may already have been described by somebody else, and the platform never overwrites that. |
| `roles[].stories` | The PRD story numbers this role serves. At least one. |
| `roles[].grantedBy` | How a person comes to hold it: the name of the role that can grant it, or `first sign-in` for the cold-start role. |
| `roles[].permissions[]` | Per component: `actions` for a service, `screens` for a web application. At least one entry, and each entry grants at least one of the two. |
| `testUsers[].username` | Lowercase letters, digits, `.`, `_`, `-`. |
| `testUsers[].role` | Must match a declared `roles[].name`. |

**No secret ever appears in this file.** It is committed to git and pinned into
the version tag. A test user carries a username and a role and nothing else.
There is no field to put a password in, and a write that adds one is rejected.

The platform generates the password at Build, seals it, and publishes it in that
build's *Provision roles and test users* ticket — the validation agent reads its
login from there, and a person reads the same password from the **Security →
Roles & users** panel. Never invent, suggest or record a password anywhere in the
design.

## Answer the cold start

A matrix whose every role is granted by somebody else describes a system nobody
can enter. `coldStartRole` names the role a first-time caller holds. The default:
the PRD's least-privileged actor, so a fresh deployment is usable by whoever
signs in, and every role above it is granted by someone who already holds one —
`grantedBy` names who. Say so explicitly where the system needs a different
origin: an admin admits people, an import loads them, the first user becomes the
admin. `null` is a real answer, but only for a system where a caller with no role
genuinely reaches nothing.

---

# 2. `security.md` — how a role is resolved

The prose half. It carries the **mechanism and the narrative**: how a token
becomes a role, and why the access rules are what they are.

Sections, in order:

1. **Role resolution** — how each service maps a token to a role: the claim or
   lookup used, and what a caller holding no role reaches. Every privileged
   action denied by default.
2. **Public surfaces** — why the components `roles.json` lists as public are
   public, and what they expose.
3. **Notes** — anything a reader of the permissions needs that is not itself a
   permission: a rule the PRD implies, a constraint from the organization's
   security defaults, a decision that was close.

**Do not DECLARE what another document already declares.** The rule is about
second copies that can disagree, not about vocabulary:

- **Never** restate the permission matrix, the list of roles, the test users, the
  cold-start role, or a Thunder configuration block. `roles.json` and
  `design.json` own those, and a second copy goes stale silently. Say "the
  least-privileged role", not "Employee is the cold-start role".
- **Do** name a role where the mechanism is unintelligible without it —
  "membership in the `Finance Approver` group resolves to that role" is the
  mechanism, and writing it without the name makes it worse, not safer. Naming
  something while explaining how it is resolved is not declaring it.

Keep it at design altitude: which authority decides a caller's role, never how a
middleware implements it.

---

# Rules for both

- The organization skill's Security & compliance and Authentication defaults
  apply before you invent policy — a filled org entry is the decision.
- Nothing here creates anything. You are describing a design; the platform
  creates the roles and test users when the user clicks Build. Never write code
  or instructions that provision users, and never invent a password.
- `thunder-authentication` owns the build-time mechanics the coding agent
  implements; this skill owns the design decisions it consumes.
