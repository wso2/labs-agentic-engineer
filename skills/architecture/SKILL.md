---
name: architecture
description: "Reuse org catalog resources when deriving or enriching a component's design — deciding the component decomposition, filling a scaffolded design.json (language, dependencies, description, pinned skills), or resolving/reconsidering any dependency."
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
"go"]`); a web-application → `["wireframes", "react-webapp", <design system>]`,
where `<design system>` is the skill name in the **UI design system** section of
the Organization defaults block in your instructions. A web app's UI toolkit is
a settled organization decision, so pin it on **every** web-application rather
than leaving it to a description to trigger — and read the name from that
section every time instead of remembering one, because it differs per
organization. If that section is absent or empty, pin only the two stack skills.
Never substitute a design system the organization defaults do not name.

**Pin the design system; do not consult it.** A design system is built against,
not designed with — it is the coding run's to load, and its theming is a settled
organization decision that no design-time question reopens. Never ask the user
about colors, themes, or look and feel. **Pin the auth skills the same way,
never leave them to a description:** `"thunder-authentication"` on **both**
sides of sign-in — every component that declares the `thunder-app` dependency,
the SPA *and* each protected backend it calls — and `"api-management"` on every
service that sits behind the gateway. The first owns the SPA's sign-in, its
screen gates and the backend's verification of the gateway's assertion; the
second owns the gateway's contract. It is a JSON key on the component's design
object, so include it when you write that `design.json` (addFile/editFile) —
`design.json` is its only home.
Each component carries only the skills its own build needs.

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
  "dependencies": [ /* see below — every dependency edge touching this component appears here */ ],
  "description": "One paragraph: single responsibility, port/entrypoint expectations, and what it explicitly does NOT do.",
  "stories": [1, 2, 4],               // PRD story numbers THIS component serves — the build gate refuses the tag while any story is claimed by nobody
  "skillsPinned": ["openapi-conventions", "ballerina"], // the skills this component's build needs — see the field above
  "endpoint": { "name": "http" } // optional; see below
}
```

`name`, `type`, `version`, `language`, `buildpack`, `appPath`, `entrypoint`,
`exposure`, `description`, `dependencies`, `stories` and `skillsPinned` are
required — `stories` and `skillsPinned` are as required as the rest, and a
component missing either fails the build gate, so emit them in the SAME write
rather than as a follow-up edit. To CHANGE a
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
- **Recomputed and overwritten** on every save: a dependency's `wiring` object.
  The platform derives its `ref` and its env-var names from the dependency's name
  and its resource type's declared outputs, so anything you write there is
  discarded. `stories` is NOT in this class — it is yours to author, per the
  **stories** field above.

### dependencies — one entry per design.cell edge

`dependencies` mirrors the DEPENDENCY edges of design.cell: every arrow
between this component and another node appears here and vice versa — a
mismatch is a defect. The `north ->` / `west ->` gateway arrows are
**exposure**, not dependencies: they live in this component's `exposure`
field and never become entries here. The platform enforces the membership
half as you write: a dependency whose `name` is not a node the cell declares
is refused (`UNKNOWN_DEPENDENCY`) with the cell statement to add. A resource
you discover here — a database, a cache — is a cell node too: `editFile`
`specs/design/design.cell` to add `component <name> as "…" database` inside
the cell before the design.json that depends on it, or the diagram will not
draw what the build provisions.
Each entry is a `kind` plus a `name`, and you pick the kind by WHAT the target
is. The kind-only fields below are exhaustive: one of them on another kind is a
schema violation that both the zod write-gate and the Go fold gate reject.

### The four kinds

| `kind` | The target is | `name` comes from | Kind-only fields | Discover with |
|---|---|---|---|---|
| `component` | a SIBLING in this design that this component CALLS | the sibling's own name | — | this design |
| `org-service` | a service ANOTHER project publishes for cross-project use | the provider's exact name, **copied verbatim** | — | `list_org_endpoints`, then `list_org_component_endpoints` |
| `platform-resource` | a backing resource the platform provisions (database, cache, IDP) | **your choice** — it becomes the env-var prefix | `resourceType` (a registered type), `parameters` | `list_platform_resource_types` |
| `external` | a system OUTSIDE the platform (a SaaS API, a legacy service) | a **Registered External resource**'s exact name, else a **new** name for a **Project External resource** | none on the component — the definition is the dependency's own file (below) | `list_external_resources` + `get_external_resource_schema`, else `web_search` |

**Reuse.** Call that last column's tool before authoring the entry, and take
the name and schema from the matching row rather than from the requirement's
wording — a **Registered External resource** described as "transactional email
delivery" is the right reuse for an "email" need even when its name (`sendgrid`)
doesn't echo the requirement. When several rows could fill the role, the
org-level one wins (a Registered External over a new Project External name; an
org-service over a sibling you would otherwise add; a listed cluster resource
type for `resourceType`). This step is done when every `external`,
`org-service`, and `platform-resource` emitted this turn is taken from this
turn's matching `list_*` result (exact `name` or `resourceType`), unless this
turn is a user-asked reconsider. When nothing the catalog returns fills the
role, omit the entry and list the gap under **Needs your input** rather than
coining a name: a `resourceType` that was not in this turn's
`list_platform_resource_types` result fails Build.

```json
"dependencies": [
  { "kind": "component", "name": "expense-api" },
  { "kind": "platform-resource", "name": "orders-db", "resourceType": "postgres-cnpg" },
  { "kind": "external", "name": "github",
    "description": "Call GitHub issues + PRs for the sync story." }
]
```

The `github` entry is a REFERENCE: its definition — provider, style, contract,
config keys — is `specs/design/dependencies/github/dependency.json`, written
once and shared by every component that uses it. A component's `description`
on the edge says why THIS component uses it; the dependency's own description
says what the system is.

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
  The same for a `platform-resource`: the PRD names a capability, `resourceType`
  is the catalog row's `name` from this turn's `list_platform_resource_types`.
  Copy that `name`. Look it up — a name coined from the role or capability words
  matches no provider or type and hard-fails the build.
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
  unable to sign in. `thunder-app` takes no `parameters` and **nothing about the
  sign-in client is authored by hand**: its display name, its redirect URIs and
  its scope list are all derived by the platform (the scopes from the permission
  catalog `security-design` writes on `security.json`). A project with no web
  application still declares the dependency on its API — the client is what the
  console's Test tab and the validation agent sign in through.
  `thunder-authentication` owns the coding-time rule, and `security-design`
  owns which roles sign in through it and what they may do.

### Resolving an `external` dependency

**One dependency, one definition.** An external dependency lives in its own
directory, `specs/design/dependencies/<name>/`, and a component only
references it by `name`. The write-gate refuses a component that names an
external dependency whose directory has no `dependency.json` yet, so write
the dependency file BEFORE the component that uses it (a `declare_plan`
entry per dependency keeps the rail honest). The name is the same
identifier everywhere: the directory, the cell's `south` node, the
component's reference, and — for a reuse — the org registry.

`dependency.json` — ONE PROJECT'S USE OF A RESOURCE. The `resource` block is
what the thing is, in the one shape a resource has everywhere (the org
registry holds the same shape); around it sit this project's provenance and
open suggestions:

```json
{
  "name": "payment-service",
  "resource": {
    "name": "payment-service",
    "description": "Charges the customer for shipping once a box is priced.",
    "provider": "Stripe",
    "config": [ { "key": "PAYMENT_API_KEY", "secret": true, "description": "Stripe secret API key" } ],
    "contract": { "type": "openapi", "path": "openapi.yaml", "origin": "provider" }
  },
  "provenance": { "sourceUrl": "https://…/spec3.json" }
}
```

There is no `style`: how the component consumes the system is computed from
`contract.type` (`openapi` → a REST client, `graphql` → a GraphQL client, `sdk`
→ a vendor library). The contract is ONE WHOLE DOCUMENT beside the file, never
a slice — the coding agent cuts what its component calls at coding time — and
it is always `{ "type", "path" }`, a file name in the directory: there is no
URL form, and the coding agent must find nothing in the repo it could follow
off it. `contract.origin` says where the file came from: `registry` (copied
from the organization's record), `provider` (the provider's published
document, fetched by the platform), `derived` (you wrote it from the
provider's developer reference), `assumed` (you wrote it from less, under the
user's authorization — `contract.accepted` is that record, and the platform
writes it, never you).

Work each one in order:

1. **Reuse first.** Call `list_external_resources`. It lists the resources
   the ORGANIZATION registered — never one another project defined for
   itself. When a row fits, write the stub and nothing more:
   `{ "name": "<its exact name>", "resource": { "ref": "<its exact name>", "name": "<its exact name>" } }`.
   The platform fills the block at save — provider, config keys, the
   organization's consumption instructions — and copies the record's contract
   document beside the file (`origin: "registry"`). Never retype the keys or
   the instructions: their identities are load-bearing, and a retyped key is
   how they drift. The build collects no values for it — org values stay on
   the registered name. Then stop. A user-asked reconsider may switch to a
   different registered name, or define a **Project External resource**
   inline under a new name.
2. **The PRD names the service, or nobody has.** The user chooses providers;
   you never do. Two outcomes, never a `status`:
   - **The PRD's Product Decisions name a service for this capability**
     ("Payments: Stripe") → write `resource.provider` and go on to the
     contract (step 3). An org or platform skill that mandates a vendor
     counts the same way. Nothing else does: "the requirement implies it",
     "this one is popular", "there is only one real option" are guesses, and
     a guess is the user's to make.
   - **No provider named** → write the NEED only: `name`, `resource.name`,
     `resource.description`, and `suggestions` — providers commonly used for
     this capability, from what you know, any number, each
     `{ "name", "style"?, "description"? }` with the one distinction that
     matters for THIS product. No `web_search`, no `provider`, no `contract`,
     no `config` — the config keys follow the provider, and none is chosen.
     The definition then offers **Select a provider**, which runs the
     `resolve-dependency` flow: it asks the user, with your suggestions as
     the options, and does the research. This is the EXPECTED outcome for a
     choosable dependency; do not force a pick the PRD does not make.
   The dependency IS the service the product needs, so name it
   `<capability>-service` (`currency-service`, `payment-service`,
   `email-service`); the chosen system is its provider.
3. **Get the contract document on disk — for a named provider only.** A
   ladder, climbed in order:
   - **Point at the published document.** `web_search` for the provider's
     published OpenAPI or GraphQL document, then write, INSIDE `resource`,
     `"contract": { "type": "openapi", "path": "openapi.yaml", "origin": "provider" }`
     (or `graphql` / `schema.graphql`), and `"provenance": { "sourceUrl": "<its URL>" }`
     beside `resource` at the top level — and STOP. A `contract` written
     anywhere but inside `resource` is refused. Do NOT fetch the document and
     do NOT `addFile` it: the
     platform fetches it at save (https only, at most 5 MiB), lands it beside
     the definition and fills the hash. A document a user hands over goes
     through **Provide interface** on the definition and lands the same way.
   - **Derive** it from the provider's OWN developer reference when it names
     every operation the design calls with parameters and responses:
     `addFile` an `openapi.yaml` with an `x-aep-source: <page>` on every
     operation, `resource.contract.origin: "derived"`, `provenance.sourceUrl` =
     the reference's root page. That needs no permission: the dependency reads
     resolved, flagged *derived*.
   - With no such documentation either, never guess during the design turn:
     leave `contract` unset, say so under **Needs your input**, and the
     `resolve-dependency` flow takes it from there (it may write an ASSUMED
     contract, but only under the user's authorization).
   An SDK is a contract of its own type: `{ "type": "sdk", "path": "sdk.json" }`,
   and `sdk.json` names the package per implementation language. This rung
   is for a NAMED provider only — an open capability gets no interface
   search at all.
4. **Derive `resource.config` keys** from the contract — a REST API's
   `components.securitySchemes`, an SDK's auth documentation — for a named
   provider only; a definition with no provider carries no keys (the gate
   refuses them). A copied registered resource already carries its keys —
   keep them.
5. **Reference it** from each consuming component:
   `{ "kind": "external", "name": "<name>", "description": "<why this component uses it>" }`.
   `style`, `package`, `specPath`, `suggestions`, `config` on the component
   are refused — they belong in the dependency file.

### Config-key conventions

`resource.config` is the env-var schema the consuming component codes against. Use
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

You never author `status`/`reason`. The platform reads them off the dependency
file at read time, plus one registry lookup for a copy, first match wins:

1. `resource.ref` set and the organization has a registered resource of that
   name → the copy stands; only the contract is checked (rule 4)
2. `resource.ref` set and NO registered resource of that name → `unresolved`/
   `needs-input` — the definition says "The organization has no registered
   resource with this name" and offers Select a provider
3. no `resource.provider` (`suggestions` open or not) → `unresolved`/
   `needs-input` — the user has not chosen a service; the definition asks them
4. no `resource.contract`, or a contract whose file is not on disk (the
   platform's fetch may still be owed or may have failed) → `unresolved`/
   `needs-contract`
5. `contract.origin: "assumed"` with no `accepted` → `unresolved`/
   `needs-acceptance`
6. otherwise → `resolved` — flagged `registered` for a copy, `assumed` when
   the contract was accepted as an assumption, `derived` when it was written
   from the provider's own documentation, `sdk-only` for an sdk contract,
   `stale` when a copy's document no longer matches the organization's

`component` is always `resolved` here. A `platform-resource` is too — once
emitted — so only emit one whose `resourceType` is a `name` from this turn's
`list_platform_resource_types`. An `org-service` resolves on catalog visibility,
and is `blocked`/`access-required` when the provider exists but this project
cannot see it. `needsSpec`, `specPath` and the other definition fields are gone from the
component's schema — a draft carrying them fails the write-gate with a message
naming the dependency file they moved to.

### Narrating the design turn

The design-generate turn runs in the chat panel, so your turn text is what the
user watches live. **Narrate each dependency decision in one plain-prose line as
you settle it**, before moving to the next:

- resolved → `✓ <capability>: using <choice>` (say `, document fetched at
  save` when you pointed at a published one, `, interface derived from docs`
  when you wrote it from the provider's reference, `, registered — the
  organization's resource, copied here` for a reuse)
- needs-input → `<capability>: your choice — A / B / C are common; select a
  provider on its definition`
- needs-contract → `<capability>: <provider> chosen, no published contract
  found — you can upload one or let me assume it, from the dependency's definition in the spec view`

Never block the design on an unresolved dependency — print the line
and keep emitting the rest; the user replies in the same chat to steer it, now or
later. Then **close with three parts and nothing more**: one line per component
(name, type, one-clause role); a **"Needs your input"** block listing ONLY the
dependencies still unresolved, each as a LINK to its definition —
`[currency-service](aep://spec/specs/design/dependencies/currency-service/dependency.json)`
— followed by the single thing you need (the console opens the definition
from the link; this link form is the one place a repo path is allowed in
your prose); and a one-line pointer to `specs/design/`. The narration already carried the
play-by-play, so a file-by-file recap would only bury the user's next action.
Each **Needs your input** line names the dependency the way its definition in the spec view does, so
the user can click through and press **Resolve** — that runs the
`resolve-dependency` flow, which is where the contract gets provided or
assumed; the design turn never waits for it.

### Resolving or reconsidering a named dependency on request

`/resolve-dependency <name>` runs the `resolve-dependency` skill — the guided
flow that takes one dependency from open to resolved; read that skill when the
instruction names it. A plain chat turn may still point you at a dependency
("reconsider `stripe`"): read `specs/design/dependencies/<name>/dependency.json`
from the snapshot and act on its current state.

- **No service chosen — it carries `suggestions`, or nothing.** The user's
  answer, if the instruction carries one (a service name or a document URL),
  IS the choice: set `provider` + `style`, remove `suggestions`, and go for
  the contract (step 3). With no answer, put the choice to them — the
  suggestions, or what your research finds, each with a one-line distinction,
  plus that they may name another — and write nothing until they answer.
- **Already resolved — reconsider.** This is the only branch that may leave a
  catalog row that still fills the role. Present fresh alternatives in the
  conversation, or repin to the Registered name the user picks, or start a
  **Project External resource** under a **new** name.

Edit ONLY that dependency's file: re-emit the whole `dependency.json` (never a
patch) with every field you are not changing carried over exactly. The
components' references do not change. Never write or alter `assumed` — that is
the user's record; the write-gate refuses it.

### Descriptions, and the per-component artifacts

Every dependency carries a one-line `description`: what the target is and how
this component uses it. Source it per kind — an `external`'s definition file
carries what the system is (the consumption instructions when the name is a
Registered External resource) and the component's reference why this component
calls it; an `org-service`'s
says the specific
operations it calls from the discovered contract, or plainly that no contract was
resolvable, never a guess; a `platform-resource`'s says what it stores. The
console shows it in the dependency drawer and the coding agent relies on it to
integrate correctly.

One component per directory. Every `service` gets an `openapi.yaml`
(`openapi-conventions` governs it), emitted after domain-model.md's ER model.
A `web-application`'s screens are its `prototype.json`, which `/prototype`
writes after the design (`prototype` governs it) — the design writes none.
Other kinds (scheduled tasks, workers, …) carry no extra artifact yet — capture
their behaviour fully in `description` and `dependencies`.
