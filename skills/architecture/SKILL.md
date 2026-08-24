---
name: architecture
description: Use when deriving or enriching a component's design — deciding the component decomposition, filling a scaffolded design.json (language, dependencies, description, pinned skills), or resolving/reconsidering any dependency.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Architecture

Component decomposition and per-component facts. The cell (`cell-design`
skill) declares WHAT exists; this skill owns deciding the decomposition and
ENRICHING each component's `design.json`.

## Scaffold first, enrich second

When the cell is saved, the platform scaffolds
`specs/design/components/<id>/design.json` for every deployable component —
the mechanical fields (name, type, version, buildpack, appPath, entrypoint, a
default exposure) plus a `"language": "TBD"` sentinel. Your job is the
JUDGMENT fields, and the build gate refuses a deployable component left
unenriched:

- **language** — **Language is decided in this order**: the organization skill's Tech stack
default first, then a language the requirements name, then the platform
default — **Ballerina** for backend services, TypeScript for web apps.
Replace the scaffold's `"language": "TBD"` sentinel with the decided value;
the build gate refuses a component whose language is still TBD. That choice is a fact
you record, not a preference you re-derive per component: write it as the
component's `language` and pin the matching stack skill in `skillsPinned`.
- **stories** — the PRD story numbers this component serves, as an integer
  array (e.g. `"stories": [1, 2, 4]`). Claim every story the component
  actually serves: the build gate refuses the tag while any PRD story is
  claimed by no component.
- **dependencies** — the playbook below.
- **description** — one paragraph: single responsibility, port/entrypoint
  expectations, and what it explicitly does NOT do.
- **skillsPinned** — After emitting or changing a component's design, record the skills that
component's build actually needs as a `skillsPinned` array **inside that
component's `specs/design/components/<name>/design.json`** — use the exact
catalog names, e.g. a Ballerina API service →
`["openapi-conventions", "ballerina"]` (a Go one → `["openapi-conventions",
"go"]`); a web-application → `["wireframes", "react-webapp"]`. Add
`"api-management"` to any service that sits behind the gateway, and
`"thunder-authentication"` to **both** sides of sign-in — the SPA *and* every
protected backend it calls, since that skill owns how each resolves the caller's
role. It is a JSON key on the component's design object, so include it when you
write that `design.json` (addFile/editFile) — do NOT put `skillsPinned` in
`design.md` frontmatter. Each component carries only the skills its own build
needs.

Writing the whole enriched file yourself (removeFile + addFile with every
field) is equally valid — the scaffold is a safety net, not a required
intermediate.

## Deriving components — deployment units the requirements justify

A component is one independently deployable unit, NOT a domain concept. The
right number comes from the requirements: for every component you must be
able to say "this deploys and evolves independently because <something the
requirements state>". Write that justification into the component's
`description`.

A requirement justifies a SEPARATE component when it shows:

- a distinct user-facing surface — e.g. an internal admin portal AND a
  customer-facing app with different users and lifecycles → two web-applications;
- a genuinely different runtime or scaling profile — e.g. an async
  worker/batch processor beside an interactive API, or a long-running
  AI/inference service;
- a technology the rest of the system doesn't share — e.g. a Python ML
  service beside a Ballerina API;
- an explicitly separate lifecycle or ownership stated in the requirements.

Do NOT split by:

- entity or domain concept — claims-service, users-service,
  receipts-service... is a domain model dressed as a topology; those are
  modules of ONE service;
- layer — auth, notifications, file storage as own services when they are
  modules of the API; a single frontend split into `ui-shell` + `ui-pages` when
  it is one bundle;
- infrastructure — api-gateway, database, queue, auth server are NEVER
  components; the platform provides them;
- periodic work — a nightly digest or a cleanup sweep is a background task the
  owning service starts, not a component of its own. Only a genuinely different
  runtime or scaling profile (above) justifies splitting one off.

When nothing above forces a split, a small system naturally lands at one
service + one web-application — that is an outcome of the rule, not a target. Name
components in kebab-case after their responsibility (`expense-api`,
`expense-webapp`, `report-worker`).

**Component `type` is a fixed vocabulary — use the EXACT string.** A backend is
`"service"`; a browser app is `"web-application"` (OpenChoreo's own term). Write
`"web-application"` verbatim — NOT `"webapp"`, `"web-app"`, or `"webApplication"`
(those are rejected, and a wrong value silently breaks the app's deployment and
runtime config). The `-webapp` in a component NAME is fine; the `type` is still
`"web-application"`. Other kinds the requirements imply (`"scheduled-task"`,
`"worker"`, …) are captured verbatim, but the platform installs component types
for `service` and `web-application` ONLY — anything else records intent and does
not deploy, so reach for one when the requirements truly force it, never as the
default home for periodic work.

## Per-component design.json

Each component's structured facts live in ONE JSON document (no markdown, no
frontmatter). The platform validates each write against this schema and rejects
violations:

```json
{
  "name": "expense-api",              // MUST equal the directory name
  "type": "service",                  // EXACT kind: "service" or "web-application" (NEVER "webapp"/"web-app"), or another the requirements imply ("scheduled-task", "worker", ...)
  "version": "0.1.0",                 // semantic version; 0.1.0 for a new component
  "language": "Ballerina",            // implementation language — "Ballerina" for a service unless the requirements say otherwise; "TypeScript" for a web-application
  "buildpack": "docker",              // always "docker"
  "appPath": "expense-api",           // repo-relative source dir — the component name
  "entrypoint": "deployment/service", // deploy entry — PAIRS with `type`: "deployment/service" for a service, "deployment/web-application" for a web-application
  "exposure": "internet",             // "internet" (public) | "intranet" (internal only)
  "dependencies": [ /* see below — every arrow in Interactions appears here */ ],
  "description": "One paragraph: single responsibility, port/entrypoint expectations, and what it explicitly does NOT do.",
  "endpoint": { "name": "http" } // optional; see below
}
```

`name`, `type`, `version`, `language`, `buildpack`, `appPath`, `entrypoint`,
`exposure`, `description`, and `dependencies` are required. To CHANGE a
design.json, re-emit the whole corrected file (removeFile + addFile) — never
patch JSON with anchored edits. On INVALID_JSON or SCHEMA_VIOLATION, fix what
the message lists and re-emit.

`endpoint` is optional: omit it and a service's endpoint takes the default name
`"http"`. Declare `{ "name": "<endpoint-name>" }` only when the endpoint must be
named otherwise — `name` is the single source of truth the coding agent copies
into `workload.yaml` and the managed-API gateway binds to. The port lives in
`workload.yaml`, not here.

**Platform-owned fields you never author**, in two kinds:

- **Preserved verbatim** where the platform has already written them:
  `exposesAPI`, `componentAgentInstructions`, and any dependency
  `status`/`reason`.
- **Recomputed and overwritten** on every save: a dependency's `wiring` object,
  and the component's `stories` array — the platform restamps it from the
  design.cell citations, so cite stories in the CELL, never here.
  The platform derives its `ref` and its env-var names from the dependency's name
  and its resource type's declared outputs, so anything you write there is
  discarded.

### dependencies — one entry per Interactions arrow

`dependencies` mirrors the Interactions section of design.md and the edges of
design.cell: every arrow appears here and vice versa — a mismatch is a defect.
Each entry is a `kind` plus a `name`, and you pick the kind by WHAT the target
is. The kind-only fields below are exhaustive: one of them on another kind is a
schema violation that both the zod write-gate and the Go fold gate reject.

### The four kinds

| `kind` | The target is | `name` comes from | Kind-only fields | Discover with |
|---|---|---|---|---|
| `component` | a SIBLING in this design that this component CALLS | the sibling's own name | — | this design |
| `org-service` | a service ANOTHER project publishes for cross-project use | the provider's exact name, **copied verbatim** | — | `list_org_endpoints`, then `list_org_component_endpoints` |
| `platform-resource` | a backing resource the platform provisions (database, cache, IDP) | **your choice** — it becomes the env-var prefix | `resourceType` (a registered type), `parameters` | `list_platform_resource_types` |
| `external` | a system OUTSIDE the platform (a SaaS API, a legacy service) | a registered resource's exact name, else your choice | `style` (`rest-api`\|`sdk`), then `specPath` or `package`; `config`; `candidates` | `list_external_resources` + `get_external_resource_schema`, else `web_search` |

**Discover before you invent.** Call that last column's tool before authoring the
entry, and take the name and schema from what it returns rather than from the
requirement's wording — a registered resource described as "transactional email
delivery" is the right reuse for an "email" need even when its name (`sendgrid`)
doesn't echo the requirement. When nothing the catalog returns fills the role,
leave the dependency unresolved rather than forcing a fit: a name that resolves
to nothing is worse than an absent one.

```json
"dependencies": [
  { "kind": "component", "name": "expense-api" },
  { "kind": "platform-resource", "name": "orders-db", "resourceType": "postgres-cnpg" },
  { "kind": "external", "name": "stripe", "style": "sdk", "package": "npm:stripe@^14",
    "config": [ { "key": "STRIPE_API_KEY", "secret": true, "description": "Your Stripe secret API key" } ] },
  { "kind": "external", "name": "github", "style": "rest-api",
    "description": "GitHub REST API for issues + PRs." }
]
```

The `github` entry is unresolved on purpose: `style: "rest-api"` with no
`specPath` computes `unresolved`/`needs-spec` — expected, not an error to fix.

#### Reading a provider's real contract

Once you have an `org-service` provider's name, call
`list_org_component_endpoints` and base that dependency's `description` on the
operations its contract actually exposes:

| `spec.availability` | Where the contract is |
|---|---|
| `inline` | `spec.inlineContent` IS the OpenAPI document — read it directly |
| `repo` | the row's `owner`/`repo`/`subdir`/`branch` locate the source: `search_remote_git_code` under that `subdir`, then `get_remote_git_file_contents` |
| `none` | nothing resolvable — say so plainly in the `description`, never invent a shape |

### Traps

- **Never the reverse edge.** Declare a dependency ONLY on the caller, naming the
  callee: a web-app depends on the API it calls; the API does not depend on the
  web-app. A component this one doesn't call is not a dependency of it — don't
  list one "for reference".
- **A role is not a name.** The requirement says "the organization's directory
  service"; the provider is usually called something else (`employee-service`).
  Look it up — a name coined from the role words matches no provider and
  hard-fails the build.
- **A `platform-resource`'s `name` becomes the env-var prefix** for every one of
  its outputs (`orders-db` → `ORDERS_DB_HOST`, and for a SPA
  `window._env_.ORDERS_DB_*`), so pick a clear one: renaming it later renames the
  component's whole config surface.
- **Secret-bearing dependencies belong on a `service`, not a
  `web-application`.** A web-application ships to the browser, so anything it
  holds is visible in dev tools — and *every* output of a `platform-resource` it
  declares is emitted into `window._env_`, so a database dependency on a SPA
  publishes its password. The secure default: attach the dependency to a backend
  `service` and give the web-app a `component` edge to it, so the service proxies
  and the SPA never sees the credential. A web-application may declare an
  `external` dependency directly only when NONE of its `config` keys need
  `secret: true` — a genuinely public API, or one the END USER authenticates with
  their own in-browser OAuth. The one exception is `thunder-app` (next bullet):
  its outputs are public OIDC client config by design. The schema does not reject
  a secret on a web-application, so apply this as the architect's judgment call,
  not a rule to route around.
- **`thunder-app` is how sign-in happens, and nothing else provisions it.** When
  the spec implies users sign in, declare it on BOTH the SPA and each protected
  service under the SAME dependency `name` — that shared name is what ties
  sign-in to token-carrying API calls. With no such dependency the SPA deploys
  unable to sign in. For `thunder-app` only, proposing the `scopes` parameter is
  allowed (default `openid profile email`); every other resource type keeps the
  no-invented-parameters rule, and `redirectUris` are platform-managed — never
  propose them. `thunder-authentication` owns the full rule.

### Resolving an `external` dependency

`external` is the one kind with real-world discovery to do — the SaaS or legacy
system lives in no catalog you can look up directly. Work it in order:

1. **Reuse first**, via the table's tool column. A registered resource whose
   description fits resolves from the registry regardless of
   `style`/`specPath`/`package`. Don't re-discover what the org already has.
2. **`web_search` for candidates** when nothing registered fits. Stop at the
   options actually worth presenting — often 2–3 genuine contenders, sometimes
   one when a real signal already points to it.
3. **Classify each candidate's style.** `rest-api` when the component calls
   specific HTTP endpoints; `sdk` when it codes against a vendor SDK/library —
   the candidate's own docs make it obvious ("REST API reference" vs "install our
   SDK").
4. **Resolve the contract.** A `rest-api` needs a `specPath`: prefer a URL you
   discovered — confirm it is a real OpenAPI document with `fetch_openapi_spec`
   (it fetches and validates, stores nothing), then set `specPath` to that URL. If
   the user hands you a spec file, or the API is private/undocumented, `addFile`
   it to
   `specs/design/components/<component>/dependencies/<dep-name>.openapi.yaml` and
   point `specPath` at that repo-relative path. With NO `specPath` the dep stays
   `needs-spec` and the build gate asks the user for one. Don't hand-author a
   whole spec — the coding agent researches the API. An `sdk` needs `package`
   instead: one ecosystem-prefixed identifier (`npm:`, `go:`, `pypi:`), version
   inline but optional.
5. **Derive `config` keys** from the contract — a `rest-api`'s
   `components.securitySchemes`, an `sdk`'s auth documentation.
6. **Emit the outcome**, never a `status`/`reason`:
   - **A real SIGNAL points to one option → emit it resolved**, with `style` +
     (`package` or `specPath`) and `config`. A signal is one of: the requirement
     names or implies the vendor, a registered resource fits, an org or platform
     skill mandates it, or a concrete technical reason forces it. "This one is
     popular" is not a signal — it is a guess dressed as a resolution, and it
     belongs in `candidates`.
   - **No signal and 2+ viable equivalents → emit `candidates`** — 2 or more,
     never one. This is the EXPECTED outcome for a genuinely-choosable dependency
     (transactional email: SendGrid/Resend/Postmark); don't force a pick the
     requirements don't justify. One option fully known resolves outright, one
     only partly known is a partial dep — leave what you know on the dependency
     itself and let the missing field compute the reason. Each candidate carries
     its own `style` and a lean `package`; the dependency's own
     `style`/`package`/`specPath` stay unset until one is pinned.
   - **You can't identify the system at all** → a style-less entry: `name` plus a
     `description` saying what is missing and what the user must supply.
7. **On pin** (a chat turn collapses `candidates` to one): REMOVE `candidates`
   entirely — a one-item array is a schema violation — and set the chosen
   option's `style` and `package`/`specPath`.

### Config-key conventions

`config` is the env-var schema the consuming component codes against. Use
`SCREAMING_SNAKE_CASE` keys and keep them minimal — only what the component
reads. `secret` is opt-in: set `"secret": true` ONLY for credentials (they route
through the secret path), and OMIT it entirely otherwise. Give each key a
`description` saying what the value is and where the user finds it
(`{ "key": "STRIPE_API_KEY", "secret": true, "description": "Your Stripe secret
API key" }`) — the Build dependency drawer shows it under the field. For a
NON-secret key whose sensible default you can infer, add `defaultValue` and the
drawer pre-fills it (`{ "key": "AWS_REGION", "defaultValue": "us-east-1" }`).
NEVER set `defaultValue` on a secret — a credential has no default to invent. An
`external` dependency should almost always carry at least one key: the
value-collection gate needs something to collect.

#### How the platform derives status/reason

You never author `status`/`reason`. The platform computes them at read time from
which fields are present, first match wins:

1. `candidates` present (2+) → `ambiguous`
2. `name` matches a registered external resource → `resolved` (registry reuse,
   regardless of `style`)
3. `style` absent → `unresolved`/`needs-input`
4. `style: "rest-api"` with no `specPath` → `unresolved`/`needs-spec`
5. `style: "sdk"` with no `package` → `unresolved`/`needs-input`
6. otherwise → `resolved`

`component` and `platform-resource` are always `resolved` here. An `org-service`
resolves on catalog visibility, and is `blocked`/`access-required` when the
provider exists but this project cannot see it. The old `needsSpec` boolean is
REMOVED from the schema — a draft carrying it fails the write-gate; migrate
`needsSpec: true` to `style: "rest-api"`.

### Narrating the design turn

The design-generate turn runs in the chat panel, so your turn text is what the
user watches live. **Narrate each dependency decision in one plain-prose line as
you settle it**, before moving to the next:

- resolved → `✓ <capability>: using <choice>`
- candidates → `<capability>: options are A / B / C — tell me which (I'll
  continue meanwhile)`
- needs-input → `<capability>: I couldn't identify the system — tell me which +
  how it authenticates`

Never block the design on an ambiguous or unresolved dependency — print the line
and keep emitting the rest; the user replies in the same chat to steer it, now or
later. Then **close with three parts and nothing more**: one line per component
(name, type, one-clause role); a **"Needs your input"** block listing ONLY the
dependencies still ambiguous or unresolved, each with the single thing you need;
and a one-line pointer to `specs/design/`. The narration already carried the
play-by-play, so a file-by-file recap would only bury the user's next action.

### Resolving or reconsidering a named dependency on request

A later chat turn may point you at a single dependency by name — "resolve the
`email` dependency on `notification-service`", "reconsider the `stripe`
dependency on `billing-api`". It carries no dependency JSON and no playbook by
design: read that entry from the component's `design.json` (it is in the turn's
snapshot) and act on its current state.

- **Ambiguous — it already carries `candidates`.** The user clicked to CHOOSE, so
  hand them the choice: each option with a one-line distinction, plus that they
  may name another. Pin the one they name — the same signal rule as discovery, so
  with no signal the choice stays theirs — then remove `candidates` per step 7.
- **Unresolved.** Apply that kind's row in the table above.
- **Already resolved — reconsider.** Present fresh alternatives as `candidates`,
  or repin to the one the user names.

Edit ONLY that one dependency's entry: re-emit the component's whole
`design.json` (never a patch) with every other field and dependency carried over
exactly as they were.

### Descriptions, and the per-component artifacts

Every dependency carries a one-line `description`: what the target is and how
this component uses it. Source it per kind — an `external`'s says which
endpoints/SDK and which auth scheme; an `org-service`'s says the specific
operations it calls from the discovered contract, or plainly that no contract was
resolvable, never a guess; a `platform-resource`'s says what it stores. The
console shows it in the dependency drawer and the coding agent relies on it to
integrate correctly.

One component per directory. Every `web-application` gets a `wireframes.dsl`
(`wireframes` governs it); every `service` gets an `openapi.yaml`
(`openapi-conventions` governs it), emitted after design.md's ER model.
Other kinds (scheduled tasks, workers, …) carry no extra artifact yet — capture
their behaviour fully in `description` and `dependencies`.
