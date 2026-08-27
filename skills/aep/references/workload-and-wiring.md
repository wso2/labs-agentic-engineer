# Wiring a dependency into `workload.yaml`

Read this before you write or edit a component's `workload.yaml`. What a
dependency's *contract* is, and how code reads its injected values, is in
`component-contract.md` beside this file.

Everything here derives from what `specs/` already fixed, so it reads the same
for a component's first line and for a change to one that shipped weeks ago. The
one kind that is **not** derivable — an `org-service`, which belongs to another
project — stays in the skill body under **Dependencies and `workload.yaml`**.

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
| `component` | its `wiring` object | one `endpoints:` entry |
| `org-service` | not derivable from `specs/` — the skill body | one `endpoints:` entry, plus `project:` and `visibility: namespace` |

Three of the four kinds carry a `wiring` object the platform derived and
committed into `design.json`. Its **shape** tells you which half of
`dependencies:` the entry belongs in:

| `wiring` holds | The entry goes in |
|---|---|
| an `endpoint` object | `dependencies.endpoints[]` |
| `ref` + `envBindings` | `dependencies.resources[]` |

`org-service` is the one kind with no `wiring` object, because its provider
belongs to another project: its `project`, the platform's name for it and its
endpoint name are resolved live and reach you by the channel the skill body
names.

**A `platform-resource` with no `wiring` is broken input, not a licence to
substitute your own store** — say so in one line and stop the run.

**Copy a `wiring` object verbatim** — every field and every `envBindings` pair,
unchanged. It is already byte-identical to the entry that belongs there.

**Those env-var names are the keys the platform populates at runtime**: an output
arrives under that name and no other. Never rename one, never invent one.

**A `wiring.endpoint`'s `component` carries the project as a prefix** —
`<project>-<component>`. It deliberately does not match the name the rest of the
tree calls that component: this prefixed one is what OpenChoreo resolves a
connection by. Write the `wiring` value. Any other spelling parses, builds,
deploys and serves with the address env var silently absent, and the only symptom
is a project that reports "deploying" for ever.

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
  endpoints:                     # component: `wiring.endpoint`, verbatim
                                 # org-service: resolved live (skill body)
    - project: <provider-project> # org-service only; absent = same project
      component: <provider-component> # `<project>-<component>` — the platform's
                                  # own name for it, project-prefixed
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

The SPA browser calls `/api` on its own host; nginx in the SPA pod
reverse-proxies to the sibling. For a sibling whose design declares
`exposesAPI.auth` the platform also injects `<DEP_NAME>_GATEWAY_URL` — the
auth-terminating address — and the proxy prefers it over the direct
`<DEP_NAME>_URL` (`react-webapp` owns that rule). Both are pod env vars, never
`window._env_` keys.

**Provider endpoint visibility:** a service a sibling SPA calls lists
`visibility: [project, internal, external]`. Each item earns its place:

- `internal` — **required for a protected service.** It is the only value that
  admits the API gateway to the component's NetworkPolicy. Without it the
  gateway authenticates the caller and then cannot reach the upstream, so every
  call through the SPA's `/api` proxy returns `503`.
- `project` — the same-namespace lane, for a trusted service-to-service caller.
- `external` — so the API remains curl-able on the public gateway.

Write all three YAML list items. A single-item `project` list is wrong even when
the SPA uses `/api`, and `design.json` `exposure: intranet` does not drop
`external`. The SPA must not fetch that public URL — its nginx proxies to the
gateway's IN-CLUSTER address (`react-webapp`). Org-published services still add
`namespace` as below.

`namespace` is NOT a substitute for `internal`: it widens pod-to-pod reach to
sibling projects and grants the gateway nothing.

**Org-published services.** If the component's `design.json` sets
`exposesAPI.orgPublished: true`, components in OTHER projects consume it — also
add `namespace` (`visibility: [external, namespace]`). This is the only way a
service becomes an `org-service` target; the platform never edits your
`workload.yaml`. Add `namespace` **only** when `orgPublished` is set.
