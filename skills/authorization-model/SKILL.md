---
name: authorization-model
description: "Read before deciding who may call what in a generated app — authoring or reviewing security.json, an openapi.yaml security block, a screen gate, a 401 handler, or a /me/ path. States the platform's authorization invariants once, with the reason each was measured."
metadata:
  aep:
    kind: platform
---

# The authorization model

One page, so the design skills and the coding skills stop restating it. Each
rule names the measurement or the incident behind it and the ADR that holds the
argument. Nothing here is a procedure: `security-design`, `openapi-conventions`,
`wireframes`, `thunder-authentication`, `react-webapp` and `api-management` say
what to write; this page says what must stay true whatever they write.

## The invariants

1. **Scopes are the one authority.** A caller may do what the access token's
   `scope` says, and nothing else decides — not a groups claim, not a role
   name, not a header. A handle is `<resource>:<action>` from
   `specs/design/security.json`; a role grants handles; a group says who holds
   a role. *Why:* group names are the organisation's namespace, shared across
   projects; a handle means one thing in one project. ADR-0030.

2. **Whole-string compare, everywhere.** `claims:read-all` does not satisfy
   `claims:read`. A role that needs both holds both, and nothing implies
   anything. *Why:* measured on the pinned gateway (`gateway-1.2.2`, jwt-auth
   `v1.3.0`); an implication would live only in a gate the runtime does not
   have. ADR-0030.

3. **One scope per operation.** An operation's `security` is absent (any
   signed-in caller), `[]` (public), or one requirement naming `oauth2` with at
   most one scope. Never two requirements, never two scopes. *Why:* the policy
   has one failure status, so a two-handle guard cannot say which handle was
   missing; and "which handle does this operation need" must have one answer
   for every reader of the block. ADR-0030.

4. **Reach is the path.** `/me/<resource>` is the caller's rows, resolved
   through the gateway assertion; `/me/<relation>/<resource>` is a relation's;
   anything else is every row. A handle guards operations on one side of
   `/me/`, never both. A capability at two reaches is two operations with two
   handles. Nothing widens on a scope, and a row that is not the caller's is
   404, never 403. ADR-0031.

   **A reach declares the field it matches on, and the assertion must carry
   that identifier.** `sub` is a directory UUID; a model that names its person
   by login name or email must be matched on that instead. The design says
   which field before any code is written. A UUID compared to a name matches
   nothing for every caller, and an identity that will not resolve is an error,
   never an empty result.

5. **The gateway is the whole "may this be called" decision.** It is rendered
   per operation from the same `security` block the gates read. The service
   holds no scope table; it verifies the gateway's RS256-signed
   `x-jwt-assertion` once at its edge and reads the caller from it. No
   assertion continues with no caller, an unverifiable one is 401, and the
   `x-user-*` headers are read by nothing. No generated service answers 403.
   *Why:* the headers are unsigned and settable by anything that reaches the
   pod; the assertion is not. ADR-0032.

6. **The gateway answers a bare 401 for every refusal**, with no
   `WWW-Authenticate` — no token, an expired one, and a valid one lacking the
   scope are byte-identical on the wire. The browser resolves which it was from
   its own token: 401 with a still-valid token is **Forbidden**; 401 with an
   absent or expired token is **sign in**, at most once per page load. *Why:*
   `if (401) signIn()` put a correctly provisioned user in an endless sign-in
   loop on the one screen their role exists for. ADR-0030 point 6.

7. **A screen's gate is the scope of the operation it loads.** Nothing about
   screens is authored in `security.json`. The SPA gates each route with
   `RequireOperation op=` and each rail item with `Can op=`, both reading the
   generated operations table; the coding agent names each screen's load
   operation once, in `src/authz/screens.ts`. A screen in a flow with no
   `role` line is public and is routed above the sign-in guard. *Why:* the
   screen table was a third statement of the contract, consumed by nothing at
   runtime, and a gate that can disagree with the operation it fronts turns a
   user away from a page the API would serve. ADR-0033.

8. **`NoAccess` replaces the shell; `Forbidden` sits inside it.** A caller who
   can reach no screen gets the message alone, naming the groups to ask for; a
   caller who holds other scopes keeps the rail and lands on `/forbidden`
   inside it. *Why:* a rail with no items around "you have no access" tells the
   user less than the message alone, and a real run drew exactly that.

9. **A refresh narrows and never widens.** A grant removed from a role
   disappears at the next silent renew; a grant added appears only after a
   full sign-out and sign-in. Say so wherever a design or a UI explains an
   access change. *Why:* RFC 6749 §6, and it is the first thing an operator
   hits after adding somebody to a group.

10. **No default role, no cold start.** A signed-in user whose groups hold no
    project role has a valid token with the OIDC scopes and no handle. That is
    a designed state: the operations with no `security` override serve them,
    and the SPA shows `NoAccess`. Inferring a role from the absence of one is
    fail-open. ADR-0030 point 8.

11. **A screen shows only what its role can reach.** Every field a screen
    displays is carried by the operation it loads, under a handle that screen's
    role holds. A criterion naming a field no permitted operation returns is a
    design contradiction, not a coding task: put the field on the projection
    the screen already reads, never widen the role to reach a second one.
    *Why:* otherwise the screen renders an id where a name belongs, and no code
    can fix it.

## What this rules out, by name

- A `groups` read anywhere in generated code, for any purpose.
- A scope check in a service handler or a mock handler; a `hasScope` that
  chooses rows.
- `anyOf` scopes, a second requirement object, or a `bearerAuth` scheme on a
  generated contract.
- A `WWW-Authenticate` read, a 403 from a generated service, or a 403 from the
  mock gateway.
- `ownership`, `screens[]`, `coldStartRole`, `publicComponents` or a `thunder`
  block in `security.json`; a handle retyped as a string literal in JSX.
- A role name, group name or administrator hardcoded in `NoAccess` or
  `Forbidden`; both read the generated tables.

## Where each half is decided

| Question | Decided in | Read by |
|---|---|---|
| which handles exist, which roles hold them, who holds a role | `security.json` (`security-design`) | Thunder at Build; `gen-authz.mjs` |
| which handle an operation needs | `openapi.yaml` `security` (`openapi-conventions`) | the gateway trait; the build gate; `gen-authz.mjs`; the mock gateway |
| which rows an operation reaches | the operation's path (`openapi-conventions`) | the generated handler |
| which screens exist and which role's flow walks them | `wireframes.dsl` (`wireframes`) | the walk (`mock-verification`) |
| which operation a screen loads | `src/authz/screens.ts` (`thunder-authentication`) | `RequireOperation`, `Can`, the rail, the landing redirect |
