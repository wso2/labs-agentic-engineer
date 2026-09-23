# ADR-0033 — A screen's gate is the scope of the operation it loads

**Status:** Accepted · 2026-09-16
**Amends:** [ADR-0030](ADR-0030-scopes-are-the-authorization-authority-for-a-generated-app.md)
point 1 ("which operation every screen reaches" is no longer authored) and
[ADR-0031](ADR-0031-reach-is-the-path.md) point 6 (the screen reachability
rule is gone with the table it read). Every other point of both stands.

## Context

`security.json` version 3, as ADR-0031 left it, carried a `screens[]` table:
one row per screen a web application's wireframe draws, naming the handle a
caller had to hold to open it (`null` for any signed-in user, `"public"` for a
screen shown before sign-in). Two gates read it — the TypeScript write gate and
the Go build gate — and refused a design when a screen was drawn but not listed,
listed but not drawn, or gated on a handle whose holders could not call a read
of the resource the screen renders.

Building the first apps against it showed what the table was:

* **The third statement of one fact.** The wireframe DSL declares the screens
  and which role's flow walks each. `openapi.yaml` declares which handle guards
  each operation. `screens[]` restated the screen set a second time and the
  gate a third, and the two gates spent five messages, a name-normalisation
  rule (`"My Claims"` versus `MyClaims`) and a whole design step (step 7,
  "re-emit `security.json` once the wireframes exist") keeping the copies
  aligned.
* **The one array nothing at runtime consumed.** Thunder provisions from
  `permissions[]`, `roles[]`, `groups[]` and `testUsers[]`; the gateway is
  rendered from `openapi.yaml`. `screens[]` reached exactly one consumer, the
  SPA's generated `SCREENS` table, and the SPA already had a better source for
  the same answer.
* **A gate that could not be the authority.** Whether a caller may see a
  screen's data is decided by the gateway, per operation, and nothing the SPA
  does changes it. A screen gate is a courtesy: it keeps a user off a page
  whose first call would answer a bare 401 that the browser cannot tell from an
  expired session (ADR-0030 point 6). A courtesy that has its own authored
  handle can disagree with the operation it fronts, and then the user is turned
  away from a page the API would serve, or let onto one it will not.

## Decision

**`security.json` carries no `screens[]`. A screen is reachable when the token
holds the scope of the operation that LOADS it, and that operation's one scope
is already in `openapi.yaml`. Nothing about screens is authored anywhere.**

1. **The SPA reads a generated operations table.** One generator,
   `scripts/gen-authz.mjs`, reads the catalog and every contract under
   `specs/design/components/` and emits `src/authz/roles.gen.ts` (handles,
   roles, grants, groups), `src/authz/operations.gen.ts` (`"GET /me/claims"` →
   `public` | `signedIn` | `scope`) and `mock/authz/roles.gen.ts` (the mock
   personas, in declared order). The projection rules are the mock gateway's
   and the trait renderer's: one scope per operation, whole-string compare.
2. **The coding agent names each screen's load operation once**, in
   `src/authz/screens.ts`: `{ key, label, path, loads: OperationKey | null,
   public? }`, in rail order. `loads` is the operation whose answer the screen
   renders on open; a screen with no load call is any signed-in user's. The
   type is a string-literal union from the generated table, so a screen that
   names an operation the contract dropped fails the type check.
3. **The gate is one function.** `RequireOperation op=` around a route and
   `Can op=` around the item that reaches it, both answering
   `canCall(OPERATIONS[op], scopes, signedIn)`. `Forbidden` names the roles
   that would unlock the operation, read off the same tables. `NoAccess`
   replaces the shell when no screen is reachable.
4. **A screen shown before sign-in is one in a flow with no `role` line.** The
   DSL compiler already treats `role` as optional and `task-planning` already
   writes `Persona: any` for such a flow; that flow is the public journey and
   its screens are routed above the sign-in guard. No literal `"public"`
   anywhere.
5. **Version stays 3, redefined.** A v3 document with `screens[]` never
   shipped, so there is no v4: version 3 means no `ownership` and no
   `screens[]`, and a document carrying either is refused by the schema with
   the key named.
6. **The design gates keep the two coverage warnings and lose every screen
   rule.** "Declared, used nowhere" now means no operation requires the handle;
   "unreachable by any role" is unchanged. Whether a role reaches its flow's
   entry screen is proven where it can be seen: the mock walk under
   `?role=<name>` opens each flow's first screen as its role and reports a
   hidden screen with the operation and handle it wanted, not a bare 401.
7. **One reference skill states the model.** `authorization-model` holds the
   invariants every other skill was restating — one authority, whole-string
   compare, one scope per operation, reach is the path, 401 for everything and
   the browser's rule for it, screen gates derive from loads, refresh narrows,
   no groups claim — each with its measured reason and its ADR.
   `security-design`, `openapi-conventions`, `wireframes`, `react-webapp` and
   `thunder-authentication` point at it and say only what is theirs.
8. **`architecture` pins the auth skills.** A component that declares the auth
   dependency carries `thunder-authentication` in `skillsPinned`, and a service
   behind the gateway `api-management`, so the coding run's dispatch names
   them rather than hoping a description triggers a load.

## Alternatives considered

**Keep `screens[]` and fix the drift with more gate rules.** Every rule would
compare a copy against the source it was copied from. The source is one
`security` block per operation, and the SPA can read it directly.

**Move `requires` into the wireframe DSL, on the `screen` block.** One
restatement fewer, but still a handle typed by hand beside a picture, still a
gate rule comparing it against the contract, and the DSL is a drawing
language with no reason to know what a scope is.

**Derive the gate from the role that walks the flow.** A flow's `role` is a
persona for a reviewer, not an authorization statement: two roles walk shared
screens, and a user may hold both. Scopes compose; role names do not.

**A version 4.** Version 3 with `screens[]` existed on one branch for two
days and provisioned nothing. Bumping would make every gate carry a v3 reader
for a shape no document has, and a `v3_document` refusal sentence for authors
who never saw it.

## Consequences

**Design time loses a refusal and the build gains a proof.** The gate no
longer names "role X reaches screen Y and cannot read its resource"; the walk
does, as its role, on the real gate, with the operation named. A design whose
grants are one handle short is a red line in the walk's report, and the fix is
one entry in `grants`, exactly as before.

**`security-design` writes grants from the screens' loads.** The guidance
replaces the grant rule: before writing a role's `grants`, open the contract
behind each screen the role's flow walks and grant the operation each screen
loads. Design step 7 becomes a grants pass only, with no screen rows to
re-emit.

**The Security page draws no Screens block.** The console reads `permissions`,
`roles`, `groups` and `testUsers`, and the wireframes only to tell the reader
which flows exist. The `screen_*` findings are gone.

**All SPA authorization assets live in one tree and are copied with one
command**: `thunder-authentication/assets/app/` mirrors the app
(`scripts/gen-authz.mjs`, `src/authz/`, `mock/authz/`), so `cp -r` of that
directory is the whole install and the two `*.example.*` files remain the
patterns. The mock harness that is not about authorization (`plugin.ts`,
`browser.ts`) stays with `react-webapp`.

**A committed `src/authz/screens.ts` can go stale against the contract**, and
that is the type check's to catch: `gen` runs before `tsc` in `build`, the
union changes, and the stale `loads` is a compile error. A stale committed
`operations.gen.ts` is the same hazard the previous generator had and the same
cure: `gen` before `tsc`, always.
