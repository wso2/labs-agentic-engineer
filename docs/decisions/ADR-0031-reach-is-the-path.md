# ADR-0031 — Which rows an operation reaches is its path

**Status:** Accepted · 2026-09-16 · **point 6's screen rule superseded** by
[ADR-0033](ADR-0033-screen-gates-derive-from-operations.md) the same day
(`security.json` carries no `screens[]`; a screen's gate is the scope of the
operation it loads, and no design gate compares screens against grants).
Version 3 stays version 3, redefined: no `ownership`, no `screens[]`.
**Supersedes point 3 of:** [ADR-0030](ADR-0030-scopes-are-the-authorization-authority-for-a-generated-app.md)
("the all handle widens the rows, it never replaces the operation"). Every
other point of ADR-0030 stands.

## Context

ADR-0030 gave each catalog action an `ownership` of `own` or `any`, so the
generated handler knew whether to filter rows to the caller, and it recognised
the idiom `read` (own) beside `read-all` (any) — the same operation, more rows
for a holder of the wider handle — by the string suffix `:read-all`. Three
behaviours keyed off that suffix: grant rule (b) ("a role granted `X:read-all`
also holds `X:read`"), the exemption of `X:read-all` from the "declared, used
nowhere" warning, and the handler's `if hasScope("X:read-all") { rows = all }`.

The first live design broke it. It needed a manager to see their direct
reports' claims — neither the caller's own rows nor every row — and wrote
`read-team` with `ownership: "own"` and the real rule in prose ("filtered by
the employee's managerId"). Nothing in the schema could say what it meant, none
of the three suffix-keyed behaviours applied to it, the console rendered it as
"own records", and the coding agent would have implemented whatever it inferred
from the sentence.

A survey of 22 authorization systems (`docs/design/draft/authz-schema-survey.md`)
found the two established shapes for row reach: a filter expression over the row
compared to the caller's identity (Hasura, Postgres RLS, Cedar, Firestore), or a
named relation walked as a graph (Zanzibar, OpenFGA, Oso). Both are runtime
evaluators that OR their rules at decision time. AEP has no evaluator: it has a
build gate, a code generator, and two enforcement points that know nothing about
rows — ThunderID, whose permissions are flat `resource:action` strings, and the
API gateway, which compares one scope per operation. Making either of them
row-aware was ruled out; they were not built for it.

## Decision

**Which rows an operation reaches is the operation's PATH in `openapi.yaml`.
`security.json` carries no row axis, and the generated backend holds no
authorization policy.**

1. **`/me/<resource>` reaches the caller's own rows.** The handler resolves
   them through the gateway assertion's `sub` and nothing the client sends; a
   row that is not the caller's does not exist in that collection, so the
   answer is 404, never 403. `POST /me/<resource>` stamps `sub` onto the row it
   creates.
2. **`/me/<relation>/<resource>` reaches rows of a relation of the caller's** —
   `/me/team/claims`, `/me/company/orders`. The relation is a noun the domain
   model has (`team` because `Employee.managerId` exists), never a role or a
   group name: `/me/team/claims`, not `/manager/claims`. Roles say *what* a
   caller may do; the path says *how far*; the data holds the relation.
3. **Every other path reaches every row.** A collection `GET /claims` returns
   every claim to whoever holds its handle, and nothing filters.
4. **One reach per handle.** A handle guards operations under `/me/` or outside
   it, never both. The `openapi.yaml` write gate and the build gate refuse a
   document that mixes them, naming both operations. A capability that exists
   at two reaches is two operations with two handles — `GET /me/claims` on
   `claims:read`, `GET /claims` on `claims:read-all` — and a role that needs
   both views holds both.
5. **Nothing widens.** There is no handler that turns one list into a bigger one
   on a scope, so `hasScope` decides no row question. The SPA calls the
   operation whose path is the screen's reach; the gateway is the whole of the
   authorization decision.
6. **`security.json` is version 3**: `actions[].ownership` is gone and a v2
   document is refused with one migration sentence. The reachability rule
   becomes *a role that reaches a screen must hold at least one handle that
   guards a safe operation on the resource the screen renders* — which one is
   the design's choice, and it is the reach the screen shows.
7. **The console derives reach; it never authors it.** The chip beside a handle
   on the Security page is read off the owning component's contract: "the
   caller's", "the caller's team", "every record", or no chip when the contract
   is not in the bundle.

## Alternatives considered

**A relation field on the action (`by: "owner"`, `by: "manager"`) plus a
declared `widens` edge.** The survey's own recommendation, and the strongest
precedent (Cerbos derived roles, Oso relations). Rejected because it keeps
authorization policy in the generated handler — a filter the generator has to
compile and the gate has to validate against the domain model — and because
the default question it raises (optional = every row, or required with an
explicit `all`) is the exact class of question a path does not have: `/claims`
versus `/me/claims` is a choice the author makes every time, with no omission
state.

**A third `ownership` value (`own | team | any`).** Cheap and wrong: "team" is
not a universal concept, the next design wants "department", and the enum
becomes the tier ladder every surveyed system avoids.

**Row policy in ThunderID or the gateway.** Zanzibar-family systems store
relations as tuples in the authorization service. ThunderID's permissions are
"structured strings derived from a hierarchy of resources and actions" with no
row qualifier, and the API gateway's jwt-auth policy compares one scope per
operation. Neither was designed for it, and the platform does not bend them.

**Keep `ownership` and add a warning for unfiltered reads.** Keeps the
suffix-keyed idiom that broke, and adds a warning that is expected on every
correct every-row list — noise, not a net.

## Consequences

**Every existing v2 design is refused until re-authored.** The write gate and
the build gate both answer a v2 document with the `v2_document` sentence; the
live `employees-submit-expense878` design is one such document. The
re-authoring is mechanical: drop `ownership`, set `version: 3`, and move each
caller's-rows operation under `/me/`.

**Contract surface grows.** A resource with both reaches has two list
endpoints, and a relation nests the path. For a generator that is cheap; for a
reader of the contract it is more lines. The operation `summary` is expected to
say the reach in words.

**Default-open outside `/me/`, by construction.** A collection `GET` at
`/claims` is every row. There is no field to forget — the author chose the path
— and the Security page shows "every record" beside the handle, but a designer
who writes `/claims` meaning "the caller's claims" has made an error nothing
mechanical catches. `openapi-conventions` carries the rule; the reviewer sees
the chip.

**Cross-caller business rules stay in the handler as domain logic.** "An
approver may not approve their own claim" is separation of duties, not reach;
every surveyed system leaves it to the application too.

**Silent narrowing.** Two holders of different handles get different
cardinalities from what a reader may think of as one list; counts and
pagination totals are per-operation. The two-operation shape makes this visible
in the contract rather than hidden in a handler branch.

**The service's own scope re-check (ADR-0030 point 5) was unchanged by this
decision** — it re-checked the operation's handle, not rows. That separate call
was made the same day:
[ADR-0032](ADR-0032-the-gateway-assertion-replaces-the-service-scope-recheck.md)
replaces the re-check with verification of the gateway's signed assertion.
