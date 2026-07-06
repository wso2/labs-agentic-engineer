---
name: high-level-architecture
description: Use when turning requirements into a design — creating or restructuring specs/design/design.md, deciding which components the system decomposes into, or writing a component's design.json.
---

# High-level architecture

Derive the design tree from `requirements.md`. The design lives under
`specs/design/` — never at the bundle root.

```
specs/design/design.md                        # the top-level design (this skill)
specs/design/components/<name>/design.json    # one per component (structured facts)
specs/design/components/<name>/openapi.yaml   # services only (openapi-conventions skill)
specs/design/components/<name>/wireframes.dsl  # webapps only (excalidraw-wireframes skill)
```

## The top-level design.md

YAML frontmatter first, then these sections. Depth rule: **every requirement
must have a home** in a capability, entity, role, or screen below — a
requirement you can't point to in this document is a defect, not an editing
choice.

1. **Overview** — what the system is, in one paragraph.
2. **Components** — a bullet per component: name, `type`, one-line
   responsibility.
3. **Capabilities** — per component, the exhaustive feature list the
   requirements imply, each with 1–2 sentences of responsibility. Group by
   module when the requirements do (e.g. "Risk register", "Audit evidence").
   This list drives the component's API resources and screens — anything
   missing here silently disappears downstream.
4. **Data model** — the core entities, their key fields, and relationships.
   These become the API's `components/schemas`.
5. **Roles & access** — the actors from the requirements and what each may
   see/do. Drives auth design and per-role screens.
6. **Interactions** — who calls whom and for what: component-to-component
   plus external integrations (email, AI/LLM, object storage, ...).
7. **Data flow** — the main lifecycles end to end (one numbered walkthrough
   per core workflow).

Do NOT add platform-owned boilerplate: no Kubernetes/monitoring/backup
sections, no generic performance targets, no "future enhancements" — unless
the requirements state them.

After emitting or changing the design, record the skills you actually applied:
use `setFrontmatterField` on `specs/design/design.md` with key `skillsApplied`
and the list of skill names (e.g. `["high-level-architecture",
"openapi-conventions"]`). Never hand-edit frontmatter with editFile.

## Deriving components — deployment units the requirements justify

A component is one independently deployable unit, NOT a domain concept. The
right number comes from the requirements: for every component you must be
able to say "this deploys and evolves independently because <something the
requirements state>". Write that justification into the component's
`description`.

A requirement justifies a SEPARATE component when it shows:

- a distinct user-facing surface — e.g. an internal admin portal AND a
  customer-facing app with different users and lifecycles → two webapps;
- a genuinely different runtime or scaling profile — e.g. an async
  worker/batch processor beside an interactive API, or a long-running
  AI/inference service;
- a technology the rest of the system doesn't share — e.g. a Python ML
  service beside a Go API;
- an explicitly separate lifecycle or ownership stated in the requirements.

Do NOT split by:

- entity or domain concept — claims-service, users-service,
  receipts-service... is a domain model dressed as a topology; those are
  modules of ONE service;
- layer — auth, notifications, file storage as own services when they are
  modules of the API;
- infrastructure — api-gateway, database, queue, auth server are NEVER
  components; the platform provides them.

When nothing above forces a split, a small system naturally lands at one
service + one webapp — that is an outcome of the rule, not a target. Name
components in kebab-case after their responsibility (`expense-api`,
`expense-webapp`, `report-worker`).

## Per-component design.json

Each component's structured facts live in ONE JSON document (no markdown, no
frontmatter). The platform validates each write against this schema and rejects
violations:

```json
{
  "name": "expense-api",              // MUST equal the directory name
  "type": "service",                  // "service" | "webapp" | any kind the requirements imply ("scheduled-task", "worker", ...)
  "version": "0.1.0",                 // semantic version; 0.1.0 for a new component
  "language": "Go",                   // implementation language, e.g. "Go", "TypeScript"
  "buildpack": "docker",              // always "docker"
  "appPath": "expense-api",           // repo-relative source dir — the component name
  "entrypoint": "deployment/service", // deploy entry
  "exposure": "internet",             // "internet" (public) | "intranet" (internal only)
  "dependencies": [ /* see below — every arrow in Interactions appears here */ ],
  "description": "One paragraph: single responsibility, port/entrypoint expectations, and what it explicitly does NOT do."
}
```

`name`, `type`, `version`, `language`, `buildpack`, `appPath`, `entrypoint`,
`exposure`, `description`, and `dependencies` are required. To CHANGE a
design.json, re-emit the whole corrected file (removeFile + addFile) — never
patch JSON with anchored edits. On INVALID_JSON or SCHEMA_VIOLATION, fix what
the message lists and re-emit.

Do NOT author `exposesAPI`, `callerIdentity`, `componentAgentInstructions`, or
any dependency `status`/`reason` — those are PLATFORM-owned. If the platform has
already written them into the file, preserve them verbatim.

### dependencies — the unified dependency edges

`dependencies` mirrors the Interactions section of the top-level design.md:
every arrow there appears here and vice versa — a mismatch is a defect. Each
entry has a `kind` (which selects the meaningful fields) and a `name`; pick the
kind by WHAT the target is:

- **`component`** — a SIBLING component in this same design (a `<name>/` under
  `components/`). Just `{ "kind": "component", "name": "expense-webapp" }`.
- **`org-service`** — a service owned by ANOTHER project in the org that
  publishes its endpoint for cross-project use. Discover it with
  `list_org_endpoints` first, and use the returned `name` **EXACTLY AND
  VERBATIM** — it is project-prefixed (e.g. `hr-directory-employee-api`, NOT
  `employee-api`); shortening or stripping the prefix resolves to nothing. Only
  a target whose `namespaceVisible` is `true` is actually published for
  cross-project use; if it shows `false` or isn't listed, still declare it by
  its exact name (the platform marks it blocked/unresolved so the user can
  request access) — never invent a substitute.
  `{ "kind": "org-service", "name": "hr-directory-employee-api" }`.
- **`external`** — a system OUTSIDE the platform (a SaaS API, a legacy
  service). Two shapes:
  - *SDK-style SaaS* (Stripe, SendGrid, ...): no spec needed — the component
    codes against the vendor SDK. Declare only the `config` keys it reads.
  - *REST-with-spec*: when the component must call specific endpoints, set
    `"needsSpec": true`. Point `specPath` at a stored contract
    (`dependencies/<name>.openapi.yaml`) or give `specUrl` for the platform to
    fetch. A `needsSpec` external with no spec yet is left UNRESOLVED for the
    user to supply — that is expected, not an error to fix.
- **`platform-resource`** — a backing resource the platform provisions (a
  database, cache, object store). Set `resourceType` to a registered type:
  `{ "kind": "platform-resource", "name": "orders-db", "resourceType": "postgres-cnpg", "description": "stores orders" }`.
  Do NOT invent instance `parameters` (size / version / storage) — the user
  supplies them in the console when provisioning; omit `parameters` unless the
  requirements literally state them.
  **Persistence trigger — not optional:** whenever a component must persist
  data or needs a datastore / cache / queue (the requirements say "database",
  "persistence", "store … in Postgres", "save records", a data store of any
  kind), you MUST emit a `platform-resource` dependency ON that component. Never
  treat persistence as an internal implementation detail and omit it, and never
  spin off a separate database/storage component (per the split rules above) —
  the datastore is a dependency on the component that owns it. Call
  `list_platform_resource_types` for a valid `resourceType`; if nothing offered
  matches, still emit the dependency with your best-guess type (the platform
  marks it unresolved so the user can pick a real one).

```json
"dependencies": [
  { "kind": "component", "name": "expense-webapp" },
  { "kind": "platform-resource", "name": "orders-db", "resourceType": "postgres" },
  { "kind": "external", "name": "stripe",
    "config": [ { "key": "STRIPE_API_KEY", "secret": true, "credentialClass": "secret" } ] },
  { "kind": "external", "name": "legacy-billing", "needsSpec": true,
    "specUrl": "https://billing.example.com/openapi.yaml" }
]
```

**Discover before you invent.** When the caller supplies the platform MCP
tools, USE them before authoring an `external`, `org-service`, or
`platform-resource` dependency — do not guess a name or a config schema:

- `list_external_resources` / `get_external_resource_schema` — reuse an
  already-registered external resource by its EXACT `name` and `config` schema
  rather than inventing a parallel one.
- `list_org_endpoints` — find the real provider component name for an
  `org-service` before referencing it.
- `list_platform_resource_types` — get a valid `resourceType` (and its
  parameters) before declaring a `platform-resource`.

**Naming.** An `external` dependency's `name` is lowercase kebab-case — it is
the external resource key the registry and per-project values are filed under.

**Config-key conventions.** `config` is the env-var schema the consuming
component codes against. Use `SCREAMING_SNAKE_CASE` keys. Mark credentials
`"secret": true` (they route through the secret path); set `credentialClass`
to `"secret"` for values the user supplies privately or `"publishable"` for
non-sensitive config. Keep the keys minimal — only what the component reads.
Derive the keys from the API's auth scheme when known: an `apiKey` scheme → one
secret key for the API key + a non-secret base-URL key; an `oauth2`
client-credentials flow → a non-secret client-id + a secret client-secret + a
non-secret base-URL key. When the scheme is unknown, fall back to API key +
base URL and note the uncertainty in `description`. An `external` almost always
carries at least one `config` key — the value-collection gate needs something
to collect. (Web-app secret rule: a true secret bound to a web-app leaks via
`window._env_`; place a `secret: true` external on a backend `service`, and put
only `credentialClass: "publishable"` keys on a web-app.)

**Discovering a NEW external.** When nothing registered fits, use `web_search`
to identify the service and its integration style — search the service name +
"OpenAPI spec" / "REST API docs" / "npm package". For a REST/GraphQL API set
`needsSpec: true` and, if the search surfaces a published OpenAPI/Swagger/
AsyncAPI URL, put it in `specUrl` (the platform fetches + stores it — never
fetch or inline it yourself). For an SDK-style SaaS omit `needsSpec` and name
the language package + exact version in `description`. Record every useful URL
you found in `candidates[]`.

**Resolution status is platform-computed.** A dependency's `status` (resolved /
ambiguous / unresolved / blocked) and its `reason` are computed by the platform
at read time against the live catalog — you never author those. `candidates`, by
contrast, ARE authorable: emit them with the URLs you find during discovery or
web research (the API homepage, a docs page, a spec URL) so the user can verify
the sources. Declare the intent (kind + name + fields above) and let the
platform resolve it. An `external` dependency should almost always carry at
least one `config` key — the value-collection gate needs something to collect.

Every dependency carries a one-line `description`: what the target is and how
the component uses it (for an `external`, which endpoints/SDK and auth scheme;
for a `platform-resource`, what it stores). The console shows it in the
dependency drawer and the coding agent relies on it to integrate correctly.

One component per directory. Every `service` gets an `openapi.yaml`
(load `openapi-conventions` before writing it); every `webapp` gets a
`wireframes.dsl` (load `excalidraw-wireframes` before writing it). Other
kinds (scheduled tasks, workers, ...) carry no extra artifact yet — capture
their behavior fully in `description` and `dependencies`.
