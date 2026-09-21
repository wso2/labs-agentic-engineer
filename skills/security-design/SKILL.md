---
name: security-design
description: "Write specs/design/security.json when a design has sign-in, permissions, roles or test users — the permission catalog every API operation is gated on."
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Security design

Write `specs/design/security.json` when the design has sign-in, permissions or
test users. It is the **permission catalog** for the project: every scope an
operation requires and every role the platform provisions is authored here and
referenced everywhere else. `openapi.yaml` references handles from this file; it
never defines them. Nothing about screens is written here: a screen is gated by
the operation it loads, whose one handle is already in `openapi.yaml`
(`authorization-model` states the invariants this file lives under).

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

**An action says what a caller may do. Which rows it reaches is not in this
file.** Reach is the operation's **path** in `openapi.yaml`: an operation under
`/me/` reaches the caller's rows (or, through a relation noun, rows of theirs —
`/me/team/claims`); an operation anywhere else reaches every row. So a
capability that exists at two reaches is **two actions** guarding two
operations — `read` on `GET /me/claims` and `read-all` on `GET /claims` — and
nothing about either action says so; the paths do. `openapi-conventions` owns
the rule and the gate that refuses one handle guarding both sides of `/me/`.
There is no `ownership`, no `own`/`any`, and no widening: a role that needs both
reaches holds both handles, and nothing implies anything.

Name the every-row action so a reader can tell it from the caller's-rows one
(`read-all`, `manage`, `export`), and keep the description honest about it —
"every claim", not "claims". The Security page shows each handle's reach beside
it, read off the contract; a description that contradicts the path is the one
thing the page cannot catch.

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

The tool's own description says what each field means; it is the contract
between the tool and this skill, and it is not copied here — the two ship on
different clocks and a copy would go stale with nothing to catch it.

A group with `platformCreated: false` was made by hand. Bind to one only when it
genuinely is the population the role is for.

**Whether a new group may be introduced at all is the organisation's call, not
this skill's.** `organization`'s **Security & compliance** section holds it,
along with the groups this org prefers and the ones a role may never be assigned
to. Read it before you write `groups[]`: a filled line there is the decision.

## Every PRD actor gets a role, and no role exists without an actor

Roles come from the PRD's Actors section. Define no role the PRD has no actor
for, and give every actor a row. **When the actor noun is also the group's name**
— a PRD actor `Finance` whose people are the org's `Finance` group — the group
keeps the name and the role takes what the actor DOES here (`FinanceReviewer`),
naming the actor in its `description`. A role and a group cannot share a name,
and the group's is the organisation's to keep. Each role cites in `stories` the PRD story
numbers it serves — **at least one, and the build gate checks one direction
only**: every story a role cites must be a real PRD story, or the design and the
requirements have drifted. The reverse is not checked here — a story no role
names is not an error, because PRD coverage is carried by each component's
`design.json` `stories`, not by this file. So read the stories once more before
you finish and ask whether an actor-bearing story is really served by the role
you gave it.

## Grants follow the screens' loads

Scopes are compared as whole strings, everywhere — the gateway and the
directory. Nothing implies anything: a token carrying `claims:read-all` is
refused by an operation that requires `claims:read`.

A screen is reachable for whoever holds the scope of the operation it **loads**
— the list or detail call whose answer the screen renders on open. Nothing
about that is written here: the SPA reads it off `openapi.yaml`. What this file
decides is whether each role holds that scope. So before writing a role's
`grants`, walk the flow `wireframes.dsl` gives that role, open the
`openapi.yaml` of the component behind each screen in it, and grant the handle
of the operation each screen loads — at the reach the screen shows. An
Approvals queue that lists every claim loads `GET /claims`, so the Approver
holds `claims:read-all`; a My Claims page loads `GET /me/claims`, so the
Employee holds `claims:read`. Then grant the actions the screen's controls call.

No gate refuses a role that is one handle short at design time; the build's
mock walk does, opening each flow's entry screen as its role and naming the
screen, the operation and the handle it wanted. On a plane where the gateway
answers **401** for every failure — no token, expired token, missing scope, all
byte-identical — the same omission in a deployed app is not a tidy 403 either:
the SPA hides the screen, or a typed URL lands on `Forbidden`, and the fix is
one more entry in `grants`.

**One scope per operation is the invariant this guidance protects.** Never
propose listing alternatives on an operation, and never ask for a handle to
imply another.

## Every user role gets a test user

A test user is an account that exists so a role's behaviour can be exercised —
the validation agent signs in as one to judge role-gated acceptance criteria.
The platform generates its password at Build, seals it, and publishes it in the
Roles gate ticket beside that role's granted scopes; that ticket is where the
validation agent reads its login. A test user is a **disposable account for
automated agents**, readable by anyone who can read the repository — never a
person's account.

Emit one per `kind: user` role, whatever its enrolment, named `test-<role-slug>`
(`Compliance Admin` → `test-compliance-admin`), so the user sees them in
Security and can rename them before Build. **The platform supplies any you
omit**, so a missing test user is never a blocked build — but naming them
yourself is what lets the user recognise and change them. `roles` is a list: a
user holding two roles is how the design exercises somebody who both files and
approves.

**A self-service role gets one too** — `enrolment` says how real accounts
arrive, never whether the role can be exercised (`authorization-model`).
Only `kind: service` roles get none: their principal is an application.

A username the platform did not create (`jsmith`) is a refusal, not a password
reset: that role has no working login, and a real person's name lands in a
published ticket. Invent no password anywhere in the design; there is no
property at any level where one could go, and a write that adds one is rejected.

## The file

```json
{
  "version": 3,
  "permissions": [
    {
      "resource": "claims",
      "component": "expense-api",
      "description": "Expense claims and their approval",
      "actions": [
        { "handle": "read",     "description": "The caller's own claims" },
        { "handle": "read-all", "description": "Every claim" },
        { "handle": "submit",   "description": "Create and send a claim" },
        { "handle": "approve",  "description": "Approve a submitted claim" },
        { "handle": "reject",   "description": "Reject a submitted claim" }
      ]
    },
    {
      "resource": "reports",
      "component": "expense-api",
      "actions": [
        { "handle": "read",   "description": "Monthly totals" },
        { "handle": "export", "description": "Download CSV" }
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
  "testUsers": [
    { "username": "test-employee", "roles": ["Employee"] },
    { "username": "test-approver", "roles": ["Approver"] }
  ]
}
```

`Finance` is not in `groups[]`: `list_groups` returned it, so `Approver` is
assigned to the people who already are Finance. `Employees` is new to this
project and is declared. `Approver` grants `claims:read-all` because the
Approvals screen loads `GET /claims` — every claim — and `claims:read` because
an approver has claims of their own too; neither grant is implied by the other,
and the `openapi.yaml` beside this file is where `GET /me/claims` and
`GET /claims` say which rows each returns. No screen is named in this file: the
SPA gates each one on the operation it loads.

| Field | Rule |
|---|---|
| `version` | Always `3`. |
| `permissions[]` | The catalog. `resource` unique in the project; `component` names a `service` in the cell; at least one action; action handles unique within their resource (`claims:read` and `reports:read` legally coexist). |
| `groups[]` | Organisation groups this project introduces: `name`, `description`. Created if absent, never renamed or deleted. A group already in the directory is not redeclared. |
| `roles[].name` | A PRD actor noun, unique in the project (case-insensitively), never a group name. It becomes `<project>/<name>` on the directory. |
| `roles[].description` | What the role is for. Project-owned: the platform writes it on every build. |
| `roles[].stories` | PRD story numbers this role serves. At least one. |
| `roles[].grants` | Handles from `permissions[]`. At least one. A handle no role grants is a warning ("unreachable by any role"); a handle no operation requires is a warning ("declared, used nowhere"). |
| `roles[].assignTo` | Organisation groups the role is assigned to. Each must be declared in `groups[]` or exist in the directory (`list_groups`) — anything else is refused, so a typo cannot create a group. Required for `enrolment: admin` user roles; absent for self-service and service roles. |
| `roles[].enrolment` | Optional. `admin` (default) or `self-service`. See **self-service actors**. |
| `roles[].assignableBy` | Optional role names, validated against `roles[]`. Records who may hand this role out. |
| `roles[].kind` | Optional. `user` (default) or `service`. A service role is assigned to an application principal, never to a group, and gets no test user. |
| `testUsers[].username` | Lowercase letters, digits, `.`, `_`, `-`. |
| `testUsers[].roles` | One or more declared `kind: user` roles, of either enrolment. The account is enrolled in every `assignTo` group of every role listed; a role with no `assignTo` is bound to the account directly. |

**A role that reaches rows by path gets two test users, not one.** Where a role
grants a `/me/` operation, the thing worth proving is that one caller cannot see
another's rows — and a single account per role makes that unprovable everywhere
downstream: the mock walk, a wired run and validation alike can only ever check
the caller against themselves. A second account is what gives the isolation
something to fail against.

A self-service role is usually the `/me/` actor, and the platform supplies only
one login per role — so the pair is yours to author or isolation goes unproven.

Nothing else goes in the file. There is no `screens[]`, no `ownership`, no
`coldStartRole`, no `publicComponents`, no `thunder` block, no `grantedBy`:
which rows an operation reaches is its path, a component is protected because
it depends on sign-in, an operation is public because its `security` is empty,
a screen is gated by the operation it loads and is public when its flow carries
no `role`, the OAuth client's name and scope list are derived, and
`assignableBy` records who hands a role out. A document that carries
`screens[]` is refused with the key named.

## The signed-in baseline

There is no role a caller falls back to. A person whose groups hold no project
role gets a valid token with the OIDC scopes and **no permission scopes** — and
that is a designed state, not a hole:

- **Operations every signed-in person may call** are the baseline: an operation
  with no `security` override inherits the document default and needs a valid
  token and no scope. `GET /me` is the usual one. Nothing in this file declares
  them; `openapi.yaml` does, by saying nothing.
- **Screens** work the same way: a screen whose load operation is one of
  those, or that loads nothing, is every signed-in user's.
- A caller who can reach **nothing** sees the SPA's `NoAccess` page — signed in,
  told which groups would help, given their username to quote — instead of an
  empty shell. The API agrees with it: the same scope is missing at both ends.

Inferring a role from the absence of one is fail-open, which is why the platform
does not do it. Real people are admin-created (somebody adds them to a group) or
self-registered.

## Self-service actors

An actor the organisation does not enrol — a Patient booking an appointment, a
customer opening an account — gets `"enrolment": "self-service"` and **no**
`assignTo`. A registration flow assigns the role at account creation, which is
the legitimate cold start and a concept every identity provider has. Such a role
gets no group binding.

It still gets test users, two where the role reaches `/me/` rows.

Use it only where the PRD genuinely describes people who sign themselves up,
and only where `organization`'s **Security & compliance** section permits it.
Everything else is `admin`.

**The platform provisions no registration flow today**, so those test logins are
the role's only principals and no sign-up affordance appears on the sign-in
page. `authorization-model` invariant 11 has the consequence for criteria.

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
how an operation names a handle and which rows it reaches (its path),
`wireframes` owns which screens exist and which role's flow walks them,
`thunder-authentication` owns how the SPA gates each screen on the operation it
loads; this skill owns the decisions all three consume, and
`authorization-model` states the invariants they share.
