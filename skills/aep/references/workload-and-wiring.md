# Wiring a dependency into `workload.yaml`

Read this before you write or edit a component's `workload.yaml`. What a
dependency's *contract* is, and how code reads its injected values, is in
`component-contract.md` beside this file.

Everything here derives from what `specs/` already fixed, so it reads the same
for a component's first line and for a change to one that shipped weeks ago. The
one half that is **not** derivable — a provider's live coordinates — stays in the
skill body under **Dependencies and `workload.yaml`**.

Most of what goes wrong here is silent: an env var you renamed arrives empty, a
`visibility` you omitted leaves a dependent's config unwritten, and nothing fails
until deploy.

## The kinds

Each entry in `design.json`'s `dependencies[]` is one thing the component
consumes. Its `kind` decides where the wiring comes from and what you write:

| `kind` | Wiring comes from | You write |
|---|---|---|
| `platform-resource` | its `wiring` object | one `resources:` entry |
| `external` | its `wiring` object — **only when it declares `config` keys** | one `resources:` entry (none when it declares no keys) |
| `component` | not derivable from `specs/` — the skill body | one `endpoints:` entry, `visibility: project` |
| `org-service` | not derivable from `specs/` — the skill body | one `endpoints:` entry, plus `project:` and `visibility: namespace` |

`component` and `org-service` need no `wiring` object: their env var is always
`<DEP_NAME>_URL`. What does need resolving is the provider's *coordinates* — its
`project`, the platform's name for it, and an endpoint name that comes from a
`workload.yaml` nobody may have written yet.

**A `platform-resource` with no `wiring` is broken input, not a licence to
substitute your own store** — say so in one line and stop the run.

**Copy a `wiring` object verbatim** — `ref` and every `envBindings` pair,
unchanged. **Those env-var names are the keys the platform populates at
runtime**: an output arrives under that name and no other. Never rename one,
never invent one.

**Every component writes its `resources:` entries, a `web-application`
included** — a web app reads the values from `window._env_` rather than pod env
(see the `react-webapp` skill), but the block is what records the dependency, and
shipping without a ref you declared has a fix issue minted against it.

## The file

Beside the `Dockerfile` at the App Path root. This is the **flat
WorkloadDescriptor** format, **not** a Kubernetes CR: no `kind: Workload`, no
`spec:`, no `autoBuild`/`autoDeploy`.

**One that already exists is edited, never regenerated.** Merge into it and leave
every field the issue does not move — an endpoint's `visibility`, a `resources`
ref an earlier issue added. Rewriting it from the template below drops wiring
somebody already established, and nothing fails until deploy.

```yaml
apiVersion: openchoreo.dev/v1alpha1
metadata:
  name: <component-name>        # logical name — no project prefix

endpoints:
  - name: http                  # MUST equal design.json `endpoint.name` (default
                                # `http` when it declares none). The managed-API
                                # gateway binds to THIS name; a mismatch fails
                                # deploy rendering with
                                # `workload.endpoints["<name>"]: no such key`.
    type: HTTP                  # HTTP | GraphQL | Websocket | TCP | UDP | gRPC
    port: 9090
    basePath: /                 # optional; root path for API services
    visibility:
      - project
      - external

dependencies:                    # what you resolved above — omit a half you have none of
  endpoints:                     # component / org-service
    - project: <provider-project> # cross-project only; absent = same project
      component: <provider-component> # the platform's name — never "correct" it
      name: <provider-endpoint>   # e.g. http
      visibility: namespace       # or project (same-project)
      envBindings:
        address: <ENV_VAR>        # the resolved URL is injected here
  resources:                      # platform-resource / external
    - ref: <resource-name>         # both fields come straight from the
      envBindings:                 # dependency's `wiring` object — verbatim
        <output-name>: <ENV_VAR>
```

**A `web-application` may declare its own safe defaults** under
`configurations.env`; they become `window._env_` entries the browser reads (the
`react-webapp` skill covers reading them). Never a secret and never a per-env
value — the platform owns those:

```yaml
configurations:
  env:
    - name: SUPPORT_EMAIL
      value: support@example.com
```

| Visibility | Reachable from |
|---|---|
| `project` | same OpenChoreo project (implicit — always on) |
| `namespace` | any component in the same Kubernetes namespace (cross-project) |
| `internal` | across all namespaces in the cluster |
| `external` | public internet via the ingress gateway |

**A sibling SPA reaches a service through same-origin `/api`, not `external`.**
OpenChoreo connections may only use `project` or `namespace`; `external` on a
*dependency* is rejected. Same project → consumer `visibility: project`. Other
project → `visibility: namespace` plus `project:` (and the provider must already
list `namespace` / be org-published). That "not `external`" is the SPA's
**dependency** entry only — not the service's own `endpoints[].visibility`.

The SPA browser calls `/api` on its own host; nginx in the SPA pod reverse-proxies
to the project Service URL injected as `<DEP_NAME>_URL`. That pod env var is not
a `window._env_` key.

**Provider endpoint visibility:** a service a sibling SPA calls lists
`visibility: [project, external]` — `project` for the nginx hop, `external` so
the API remains curl-able on the public gateway. Write both YAML list items.
A single-item `project` list is wrong even when the SPA uses `/api`, and
`design.json` `exposure: intranet` does not drop `external`. The SPA must not
fetch that public URL. Org-published services still add `namespace` as below.

**Org-published services.** If the component's `design.json` sets
`exposesAPI.orgPublished: true`, components in OTHER projects consume it — also
add `namespace` (`visibility: [external, namespace]`). This is the only way a
service becomes an `org-service` target; the platform never edits your
`workload.yaml`. Add `namespace` **only** when `orgPublished` is set.
