---
name: ballerina
description: "Use this whenever you are working with ballerina code or editing .bal files — project layout, the bal library lookup flow, scope enforcement for a contract-backed service, and the bal build && bal test verify step."
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

## Scope enforcement on a contract-backed service

A service whose `design.json` sets `exposesAPI.auth` implements
`specs/design/components/<name>/openapi.yaml`, and that contract declares one
OAuth2 scope per protected operation. **`bal openapi` drops the `security`
block entirely** — the generated resource functions carry no scope information,
and no flag changes that. So the mapping is transcribed, and a shipped test
keeps it honest.

From the App Path:

```bash
cp "$AEP_SKILLS_DIR/ballerina/assets/scopes.bal" scopes.bal
mkdir -p tests
cp "$AEP_SKILLS_DIR/ballerina/assets/scope_table_drift_test.bal" tests/scope_table_drift_test.bal
cp ../specs/design/components/<name>/openapi.yaml openapi.yaml
```

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` next to this skill's
`SKILL.md` (the BFF mirrors that directory to `.claude/skills/ballerina/`).
`tests/scope_table_drift_test.bal` is verbatim — never edit it; it is the only
thing that catches a table row drifting from the contract.

The last `cp` is not optional. **`openapi.yaml` at the package root is the copy
the drift test reads** — `yaml:readFile("openapi.yaml")` resolves against the
package root, not against `specs/`, so without it `bal test` fails on a missing
file rather than on a wrong row. Re-copy it whenever the contract changes; it is
a build input, and it is committed with the package.

Three edits, and nothing else:

1. **`OPERATION_SCOPES` in `scopes.bal`** — one row per operation in the
   contract, between the marked comment lines. `segments` is the path split on
   `/` with every `{pathParam}` written as `"*"`; `scope` is the handle, `()`
   for an operation that inherits the document-level `security: [{oauth2: []}]`
   (signed in, no scope), or `PUBLIC` for `security: []`.
2. **The generated service becomes interceptable**:
   ```ballerina
   service http:InterceptableService / on ep0 {
       public function createInterceptors() returns ScopeInterceptor => new;
       // …generated resources, unchanged…
   }
   ```
3. **Nothing in `Ballerina.toml` or `Dependencies.toml`.** The drift test's
   `import ballerina/yaml` is the whole declaration: `bal build` resolves it and
   records it as `scope = "testOnly"` on its own, because it is imported only
   from `tests/`, so the YAML parser never ships in the runtime image. Hand-
   editing `Dependencies.toml` here is the same mistake as anywhere else.

**Read identity only from the injected headers, and read them the way they are
actually shaped.** `api-management` holds the full table; these four are what a
resource gets wrong:

| Rule | Why |
|---|---|
| **Never read `Authorization`.** | The gateway's jwt-auth filter **strips** it and re-presents the verified token as `X-Forwarded-Authorization`. `Authorization` reaches a resource only on a **public** operation — where any caller can set it — so it is never an authorization input. |
| **Test a header for *empty*, never for *missing*.** | A claim absent from the token still yields its header, set to `""`. The asset's `header()` helper collapses both cases to `""` for exactly this reason; a `is ()` check on a bound `string?` parameter is not the same test. |
| **`X-User-Scopes` is the authorization authority — and it carries the five OIDC scopes too** (`openid profile email group ou`). | Every signed-in user therefore holds five scopes, so "holds some scope" is never "is authorized". `hasScope` splits on whitespace and compares **whole strings**: `claims:read-all` is not `claims:read`, at this layer or at the gateway. |
| **`X-User-Groups` is JSON** (`["Finance"]`), and this stack does not read it. | Roles reach a service only as scopes. If anything ever needs the list it is `value:fromJsonString`, never a comma split. |

**What the interceptor is, and is not.** It converts *misconfiguration* into a
401/403: an operation accidentally left public, a stale trait, a local run with
no gateway in front. It does **not** stop a hostile pod in the same namespace —
that pod sets `X-User-Scopes` itself and is believed, because the forwarded
headers carry no proof of who set them. The boundary is the NetworkPolicy that
the workload's `visibility: internal` creates; this is the second line behind
it. Do not try to close that gap by validating a JWT in the service —
`http:ServiceConfig { auth: … }` is listener-side token validation and is the
wrong construct here.

**Widening inside a resource.** The generated resource already binds
`X-User-Scopes` as a header parameter, so no context plumbing is needed:

```ballerina
Claim[] rows = hasScope(xUserScopes, "claims:read-all")
    ? store.all()
    : store.byOwner(xUserId);
```

Never use `hasScope` as an operation's only check — the interceptor and the
contract own that.

A **single-row** read is the same rule at the other end: when the caller holds
only the `own` handle and the row belongs to someone else, return **404, not
403** — a caller who may not see a row may not learn it exists from a status
code either (`api-management` owns the rule; this is its Ballerina shape).

**Verify** a package with `scopes.bal` in it:

```bash
bal build && bal test
```

`bal test` is what runs `testScopeTableMatchesContract`; without it a table row
that disagrees with the contract ships silently.

## Dockerfile

When the user asks for a container image — load [dockerfile.md](references/dockerfile.md).
