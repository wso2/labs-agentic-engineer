---
name: security-design
description: "Write specs/design/security.json when a design has sign-in, permissions, roles or test users — the permission catalog every API and screen is gated on."
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Security design

Write `specs/design/security.json` when the design has sign-in, permissions or
test users. It is the **permission catalog** for the project: every scope an
operation requires, every screen a role unlocks and every role the platform
provisions is authored here and referenced everywhere else. `openapi.yaml`
references handles from this file; it never defines them. A wireframe's screens
are gated by handles from this file.

There is exactly **one** control point. A caller's permissions are the
intersection of the roles their groups hold with the resource server the token
is asked for — group → role → granted permissions. Nothing else narrows: the
OAuth client's own scope allowlist is written for truthfulness and is **not a
gate** (the identity provider stores it, returns it, and ignores it), and an
unknown or ungranted handle is dropped from a token silently — no error, no
runtime signal. This file, and the gate that checks it, is the only defence
against a scope nobody can hold.

The platform provisions from it at Build (resource server, actions, project
roles, group bindings, test users) and the console renders it as the Security
page. `architecture` declares the `thunder-app` dependency on the SPA and each
protected API; this file says what the people who sign in through it may do.

---

## Build the catalog from the PRD's capabilities

Walk the PRD's capabilities, not its endpoints:

- **One resource per business object** the app protects — `claims`, `reports`.
  Each is owned by exactly **one** service component, named in `component`.
  Moving an endpoint between services later does not rename a scope.
- **One action per capability** — `submit`, `approve`, `reject`, `export`. The
  grain is the capability, not the operation and not read/write per service.
  Several operations may require the same handle; every operation requires
  exactly one.
- A handle is `<resource>:<action>`, unique in the project. Each segment is
  `[a-z][a-z0-9-]*`.

**Every action states its `ownership`, and it is required.**

| `ownership` | Means | The handler |
|---|---|---|
| `own` | the caller reaches their own rows | filters by the caller's identity (`X-User-Id`, or the mapping the description names) |
| `any` | the caller reaches every row | does not filter |

A capability that needs both is **two actions**, `read` (own) and `read-all`
(any). Without the split, a role holding a bare `read` lists everybody's rows
the first time a coding agent guesses — which is exactly how prose-only
permissions get implemented. The word is `ownership`, not "scope", because
scope already means the OAuth scope in this file.

`openid`, `profile`, `email`, `group` and `ou` are reserved OIDC scopes. They
ride every access token, so one of them as a handle would admit every signed-in
person in the organisation. They can never be a resource or an action.

## Reuse a group before you declare one

A **role** is the project's: it is created per project, its permission set is
replaced on every build, and its name is a PRD actor noun (`Approver`), unique
in the project, never a group name. A **group** is the organisation's: it is a
set of people, shared across projects, and the platform creates it but never
renames or deletes it.

The binding between the two is `assignTo`, and it is a **declared decision**,
not a name match. So reuse is deliberate:

**Call `list_groups` before you write a single `groups[]` entry.** It lists the
directory's groups with `memberCount`, `projects` (how many projects already
bind roles to it) and `platformCreated`. Reuse a name verbatim when the people
the role is for **already form that group** — `Finance` beats a fresh
`Approvers` that means the same people — and declare a new group in `groups[]`
only when no existing one is those people. A group already in the directory is
never redeclared.

The tool describes itself in the same words, and that description is the
contract between it and this skill:

> **`list_groups`** — Lists the directory groups in this organization's
> environment directory. Use it before writing `security.json`
> `roles[].assignTo`: reuse an existing group name when the people who should
> hold the role already form a group; otherwise declare a new name in `groups[]`
> (it is created at build). `memberCount` is the number of users in the group
> today; `projects` is how many projects already bind a role to it;
> `platformCreated` says whether this platform created it.

A group with `platformCreated: false` was made by hand — most notably
`Administrators`, which administers the platform itself. Bind to it only when it
genuinely is the population the role is for.

## Every PRD actor gets a role, and no role exists without an actor

Roles come from the PRD's Actors section. Define no role the PRD has no actor
for, and give every actor a row. Each role cites in `stories` the PRD story
numbers it serves — **at least one, and the build gate checks one direction
only**: every story a role cites must be a real PRD story, or the design and the
requirements have drifted. The reverse is not checked here — a story no role
names is not an error, because PRD coverage is carried by each component's
`design.json` `stories`, not by this file. So read the stories once more before
you finish and ask whether an actor-bearing story is really served by the role
you gave it.

## Screens

One `screens[]` row per screen a web application's wireframe draws, with
`requires` naming one handle, `null` for any signed-in user, or the literal
`"public"` for a screen shown before sign-in. A handle always contains a colon,
so the literal cannot collide.

Write the screen name as `wireframes.dsl` spells it; spaces and case are
ignored when the two are compared (`"My Claims"` matches `screen MyClaims`). A
screen the wireframe does not draw is refused. The reverse is **your** job: a
screen you leave out of `screens[]` is reachable by any signed-in user, which is
almost never what a gated app means.

## The two grant rules

Scopes are compared as whole strings, everywhere — the gateway, the service
middleware and the directory. Nothing implies anything: a token carrying
`claims:read-all` is refused by an operation that requires `claims:read`. Two
rules follow, and both are unconditional.

**(a) Every role that reaches a screen holds the scope of every operation that
screen calls.** Before writing a role's `grants`, open the `openapi.yaml` of the
component behind each screen that role unlocks and list the operations the
screen calls. Every one of those scopes goes in `grants`. In the worked example
below, `Approvals` is gated on `claims:approve`, but the queue it renders is
`GET /claims`, which requires `claims:read` — so `Approver` grants both.

**(b) A role granted `X:read-all` is also granted `X:read`.** The "all" handle
widens the *rows* an operation returns; it does not replace the *operation*'s own
handle. Both go in `grants`, and ownership then does its job: the same
`GET /claims` returns the caller's rows for an Employee and every row for an
Approver.

Miss either and the build gate refuses the design, naming the role, the screen
and the missing handle. Miss it on a plane where the gateway answers **401** for
every failure — no token, expired token, missing scope, all byte-identical — and
the symptom is not a tidy 403: the SPA cannot tell a missing scope from an
expired session, so it restarts sign-in and a correctly-provisioned user sits in
an **infinite sign-in loop** on the one screen their role exists for.

**One scope per operation is the invariant both rules protect.** Never propose
listing alternatives on an operation, and never ask for a handle to imply
another: the fix for a role that is one handle short is one more entry in
`grants`.

## Every admin-enrolment role gets a test user

A test user is an account that exists so a role's behaviour can be exercised —
the validation agent signs in as one to judge role-gated acceptance criteria.
The platform generates its password at Build, seals it, and publishes it in the
Roles gate ticket beside that role's granted scopes; that ticket is where the
validation agent reads its login. A test user is a **disposable account for
automated agents**, readable by anyone who can read the repository — never a
person's account.

Emit one per role with `enrolment: admin`, named `test-<role-slug>` (`Compliance
Admin` → `test-compliance-admin`), so the user sees them in Security and can
rename them before Build. **The platform supplies any you omit**, so a missing
test user is never a blocked build — but naming them yourself is what lets the
user recognise and change them. `roles` is a list: a user holding two roles is
how the design exercises somebody who both files and approves.

Self-service roles and `kind: service` roles get no test user.

A username the platform did not create (`jsmith`) is a refusal, not a password
reset: that role has no working login, and a real person's name lands in a
published ticket. Invent no password anywhere in the design; there is no
property at any level where one could go, and a write that adds one is rejected.

## The file

```json
{
  "version": 2,
  "permissions": [
    {
      "resource": "claims",
      "component": "expense-api",
      "description": "Expense claims and their approval",
      "actions": [
        { "handle": "read",     "ownership": "own", "description": "See own claims" },
        { "handle": "read-all", "ownership": "any", "description": "See every claim" },
        { "handle": "submit",   "ownership": "own", "description": "Create and send a claim" },
        { "handle": "approve",  "ownership": "any", "description": "Approve a submitted claim" },
        { "handle": "reject",   "ownership": "any", "description": "Reject a submitted claim" }
      ]
    },
    {
      "resource": "reports",
      "component": "expense-api",
      "actions": [
        { "handle": "read",   "ownership": "any", "description": "Monthly totals" },
        { "handle": "export", "ownership": "any", "description": "Download CSV" }
      ]
    }
  ],
  "groups": [
    { "name": "Employees", "description": "Everyone on payroll" }
  ],
  "roles": [
    {
      "name": "Employee",
      "description": "Submits and follows their own claims.",
      "stories": [1, 2, 6],
      "grants": ["claims:read", "claims:submit"],
      "assignTo": ["Employees"]
    },
    {
      "name": "Approver",
      "description": "Approves or rejects submitted claims and reads monthly reports.",
      "stories": [3, 4, 5, 7],
      "grants": ["claims:read", "claims:read-all", "claims:approve", "claims:reject", "reports:read"],
      "assignTo": ["Finance"],
      "assignableBy": ["Approver"]
    }
  ],
  "screens": [
    { "component": "expense-webapp", "screen": "My Claims",    "requires": "claims:read" },
    { "component": "expense-webapp", "screen": "Submit Claim", "requires": "claims:submit" },
    { "component": "expense-webapp", "screen": "Approvals",    "requires": "claims:approve" },
    { "component": "expense-webapp", "screen": "Reports",      "requires": "reports:read" }
  ],
  "testUsers": [
    { "username": "test-employee", "roles": ["Employee"] },
    { "username": "test-approver", "roles": ["Approver"] }
  ]
}
```

`Finance` is not in `groups[]`: `list_groups` returned it, so `Approver` is
assigned to the people who already are Finance. `Employees` is new to this
project and is declared. `Approver` grants `claims:read` **and**
`claims:read-all` — rule (b) — and it grants `claims:read` because `Approvals`
calls `GET /claims` — rule (a).

| Field | Rule |
|---|---|
| `version` | Always `2`. |
| `permissions[]` | The catalog. `resource` unique in the project; `component` names a `service` in the cell; at least one action; action handles unique within their resource (`claims:read` and `reports:read` legally coexist). |
| `permissions[].actions[].ownership` | **Required.** `own` or `any`, per the table above. |
| `groups[]` | Organisation groups this project introduces: `name`, `description`. Created if absent, never renamed or deleted. A group already in the directory is not redeclared. |
| `roles[].name` | A PRD actor noun, unique in the project (case-insensitively), never a group name. It becomes `<project>/<name>` on the directory. |
| `roles[].description` | What the role is for. Project-owned: the platform writes it on every build. |
| `roles[].stories` | PRD story numbers this role serves. At least one. |
| `roles[].grants` | Handles from `permissions[]`. At least one. A handle no role grants is a warning ("unreachable by any role"); a handle no operation and no screen requires is a warning ("declared, used nowhere"). |
| `roles[].assignTo` | Organisation groups the role is assigned to. Each must be declared in `groups[]` or exist in the directory (`list_groups`) — anything else is refused, so a typo cannot create a group. Required for `enrolment: admin` user roles; absent for self-service and service roles. |
| `roles[].enrolment` | Optional. `admin` (default) or `self-service`. See **self-service actors**. |
| `roles[].assignableBy` | Optional role names, validated against `roles[]`. Records who may hand this role out. |
| `roles[].kind` | Optional. `user` (default) or `service`. A service role is assigned to an application principal, never to a group, and gets no test user. |
| `screens[]` | `component`, `screen`, `requires`: one catalog handle, `null` for any signed-in user, or `"public"`. |
| `testUsers[].username` | Lowercase letters, digits, `.`, `_`, `-`. |
| `testUsers[].roles` | One or more declared `kind: user` roles. The account is enrolled in every `assignTo` group of every role listed. |

Nothing else goes in the file. There is no `coldStartRole`, no
`publicComponents`, no `thunder` block, no `grantedBy`: a component is protected
because it depends on sign-in, an operation is public because its `security` is
empty, a screen is public because `requires` is `"public"`, the OAuth client's
name and scope list are derived, and `assignableBy` records who hands a role
out.

## The signed-in baseline

There is no role a caller falls back to. A person whose groups hold no project
role gets a valid token with the OIDC scopes and **no permission scopes** — and
that is a designed state, not a hole:

- **Operations every signed-in person may call** are the baseline: an operation
  with no `security` override inherits the document default and needs a valid
  token and no scope. `GET /me` is the usual one. Nothing in this file declares
  them; `openapi.yaml` does, by saying nothing.
- **Screens** work the same way with `requires: null`.
- A caller who can reach **nothing** sees the SPA's `NoAccess` page — signed in,
  told which groups would help, given their username to quote — instead of an
  empty shell. The API agrees with it: the same scope is missing at both ends.

Inferring a role from the absence of one is fail-open, which is why the platform
does not do it. Real people are admin-created (somebody adds them to a group) or
self-registered.

## Self-service actors

An actor the organisation does not enrol — a Patient booking an appointment, a
customer opening an account — gets `"enrolment": "self-service"` and **no**
`assignTo`. The registration flow assigns the role at account creation, which is
the legitimate cold start and a concept every identity provider has. Such a role
gets no test user and no group binding.

Use it only where the PRD genuinely describes people who sign themselves up.
Everything else is `admin`.

## What the user has to know about a grant change

A refresh **narrows and never widens**. A grant removed from a role disappears
from the caller's token at the next silent renew; a grant **added** to a role —
or a person added to a group — does not appear until they sign out and sign in
again. Say so wherever the design explains an access change; it is the first
thing an operator hits after adding somebody to a group, and it looks exactly
like a broken deployment.

---

The `organization` skill's Security & compliance and Authentication defaults
apply before you invent policy — a filled org entry is the decision. Nothing
here creates anything: the platform creates the resource server, the roles, the
groups and the test users when the user clicks Build. `openapi-conventions` owns
how an operation names a handle, `wireframes` owns how a screen is gated by one,
`thunder-authentication` owns the build-time mechanics the coding agent
implements; this skill owns the decisions all three consume.
