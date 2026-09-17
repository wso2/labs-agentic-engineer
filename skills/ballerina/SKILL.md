---
name: ballerina
description: "Use this whenever you are working with ballerina code or editing .bal files — project layout, the bal library lookup flow, verifying the gateway's signed assertion on a contract-backed service, and the bal build && bal test verify step."
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Ballerina

**Load [code-rules.md](references/code-rules.md) before writing any `.bal` code.** This contains best practices, syntaxes and common patterns for Ballerina code.

## Development workflow

### Creating a New Project

When the user asks to create a new project, service, or program from scratch:

```bash
bal new <project-name>   # scaffolds main.bal + Ballerina.toml
cd <project-name>
```
- Read [project-structure.md](references/project-structure.md) for how a Ballerina project is laid out.

Write tests only when the user asks — then load [tests.md](references/tests.md).

### Working with OpenAPI Specifications

**Load [openapi.md](references/openapi.md) before writing a service or a client that has
an OpenAPI document** — one this component implements, or one it calls. It carries the
`bal openapi` invocation for each, and where the generated module has to land.

### bal library

**Run `bal library --help` before your first lookup, and follow the flow it describes.** It reads a
package off Central so you write signatures that exist rather than remembered ones. `--help` is the
grammar — the verbs, their flags, the session to walk; each document prints the rules about its own
contents. What follows is what you carry into every lookup.

#### Working with library

##### Preferences
- `ballerina/*` is the standard library; every vendor or third-party connector is `ballerinax/*`. 
- Prefer a `ballerinax/*` connector over a `trigger.*` package covering the same
events.
- Balerina has libraries created for working with standards, search before creating your own implementation.
- A trailing `// Special Agent Note:` mentions important informations, follow them. eg: Other package references.
- **Line one states the document's own length** If it says `· 535 lines` and you filtered using head | tail for 150 which mean the other 385 are gone and the answer can be among them: re-run that call unfiltered before writing anything from it. The library cli is designed to be self contained. You can't judge without the whole output.
- -r flag only covers dependant types in the own package. For the external package references, you might need to invoke `bal library type` if you need them.
- Tool may return ## Next section: Those are suggestions for your next look up navigation tool. Use them as hints.
- Tool might return errors, Handle accoringly based on the error message.

### bal build

- An `import` plus `bal build` resolves a package — no manual `Dependencies.toml` edit.
- `target/` is the incremental cache — leave it in place. A build that printed `Generating executable` is green; do not re-run `bal build` to confirm it.
- Configurables are read at runtime, so env variables or Config.toml is not needed to build.
- **`bal build` does NOT run tests.** Any package with a `tests/` directory
  verifies with `bal build && bal test` — a green build says nothing about a
  test that guards a correctness invariant.

## Who the caller is: the gateway's signed assertion

A service whose `design.json` sets `exposesAPI.auth` implements
`specs/design/components/<name>/openapi.yaml`.

**The gateway has already done the authorization.** It validated the caller's
token and checked the scope each operation declares in that same contract; a
request that failed either never reached this process. So this service holds
**no operation → scope table**. That `bal openapi` drops the contract's
`security` block no longer matters: there is nothing to transcribe, and nothing
to keep in sync.

What the gateway cannot do is prove to your code that a request *came through
it* — any pod that can open a socket to this service can send whatever headers
it likes. So it signs a JWT of the caller it authenticated and puts it on the
upstream request, and this service verifies that signature against one
certificate the platform publishes per environment.

From the App Path:

```bash
cp "$AEP_SKILLS_DIR/ballerina/assets/gateway_assertion.bal" gateway_assertion.bal
```

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` next to this skill's
`SKILL.md` (the BFF mirrors that directory to `.claude/skills/ballerina/`). The
file is **verbatim — there is no line to edit**.

Two edits, and nothing else:

1. **The generated service becomes interceptable**:
   ```ballerina
   service http:InterceptableService / on ep0 {
       public function createInterceptors() returns AssertionInterceptor => new;
       // …generated resources, unchanged…
   }
   ```
2. **A resource that needs an identity takes `http:RequestContext ctx`** and
   calls `requireGatewayCaller(ctx)`, returning its `http:Unauthorized` as-is.
   Match on the side the model stores — `userId` for a directory id,
   `requireCallerUsername(caller)` for a login name:
   ```ballerina
   // GET /me/claims — the caller's rows, resolved through the assertion and
   // nothing the client sent. `ownerId` here stores a directory id.
   resource function get me/claims(http:RequestContext ctx) returns Claim[]|http:Unauthorized {
       GatewayCaller|http:Unauthorized caller = requireGatewayCaller(ctx);
       if caller is http:Unauthorized {
           return caller;
       }
       return store.byOwner(caller.userId);
   }
   // Same shape when the model stores a login name instead — the only change
   // is which side of the comparison the assertion supplies.
   //   string|http:InternalServerError name = requireCallerUsername(caller);
   //   if name is http:InternalServerError { return name; }
   //   return store.byManager(name);
   // GET /claims — every row; a different operation, guarded by claims:read-all
   resource function get claims() returns Claim[] {
       return store.all();
   }
   ```

Nothing goes in `Ballerina.toml` or `Dependencies.toml`: `ballerina/crypto`,
`ballerina/jwt` and `ballerina/os` are distribution modules that `bal build`
resolves from the imports.

**Read identity from the verified caller, never from a header.**

| Rule | Why |
|---|---|
| **`requireGatewayCaller(ctx)`** in any resource that needs an identity. | The assertion is the only statement about the caller that a forged request cannot make. |
| **Match the caller on the side the model stores** — `userId` for a directory id, `requireCallerUsername(caller)` for a login name. | A UUID compared to a name matches nothing and answers `200 []` — "you own nothing", not "who are you". An identity failure is never an empty list. |
| **Never bind `X-User-Id`, `X-User-Scopes`, `X-User-Groups` or `Authorization`** as resource header parameters. | The first three are unsigned — the gateway sets them, and so can anyone else who reaches this pod directly. `Authorization` is stripped. |
| **A `security: []` resource reads NO identity at all** and takes no `ctx`. | The gateway serves a public operation with no assertion, so there is no caller to read there. |
| **Compare a scope with the asset's `hasScope`.** | It compares whole handles. `string:includes` is the trap — it matches `claims:read` inside `claims:read-all`. |
| **Never `http:ServiceConfig { auth: … }`.** | That is listener-side token validation against the identity provider — the coupling the assertion exists to remove. |

`hasScope` is never an operation's only check. Whether the operation may be
called at all was settled at the gateway, from the contract.

A **single-row** read is the same rule at the other end: when the caller holds
only the `own` handle and the row belongs to someone else, return **404, not
403** — a caller who may not see a row may not learn it exists from a status
code either (`api-management` owns the rule; this is its Ballerina shape).

**Verify** a package with `gateway_assertion.bal` in it:

```bash
bal build && bal test
```

Write the tests against a **throwaway RSA keypair**: mint an assertion with the
private half and export `GATEWAY_ASSERTION_CERTIFICATE` from its self-signed
certificate, so nothing in the test talks to a gateway. Cover four cases — a
valid assertion is accepted; one signed by a *different* key is a 401; one whose
payload was edited after signing is a 401 (never an anonymous caller); and a
`security: []` resource answers 200 with no assertion at all.

## Dockerfile

When the user asks for a container image — load [dockerfile.md](references/dockerfile.md).
