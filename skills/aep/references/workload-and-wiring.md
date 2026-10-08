# Wiring a dependency into `workload.yaml`

Read this file before you write or edit the `workload.yaml` of a component.
The data comes from `specs/`. The one exception is an `org-service`: the
platform resolves it, and your prompt gives it. An error in this file shows
only at deploy.

## The kinds

Each entry in the `dependencies[]` of `design.json` is one thing that the
component consumes:

| `kind` | Wiring comes from | You write |
|---|---|---|
| `platform-resource` | its `wiring` object | one `resources:` entry |
| `external` | its `wiring` object, only when it declares `config` keys | one `resources:` entry (none without keys) |
| `component` | its `wiring` object | one `endpoints:` entry |
| `org-service` | the platform, not `specs/` | one `endpoints:` entry, with `project:` and `visibility: namespace` |

If the `wiring` has an `endpoint` object, the entry goes in
`dependencies.endpoints[]`. If it has `ref` and `envBindings`, the entry goes in
`dependencies.resources[]`.

- Copy a `wiring` object verbatim. It is already the correct entry.
- The platform injects values only under the env-var names in `envBindings`.
  Do not change or add a name. The output of a `platform-resource` has the
  name `<DEP>_<OUTPUT>` (`user-auth` + `jwks_url` → `USER_AUTH_JWKS_URL`). The
  skill of the resource (for example `thunder-authentication`) tells what each
  output is.
- A `platform-resource` with no `wiring` is broken input. Say so in one line
  and stop the run.
- A `web-application` also writes its `resources:` entries, but it reads the
  values from `window._env_`.

**The `component` of a `wiring.endpoint` is `<project>-<component>`.** It is
not the name that the other files use. Copy the `wiring` value. With a
different name, the deploy has no address env var, and the project shows
"deploying" for ever.

## The file

The file is next to the `Dockerfile`. It uses the flat WorkloadDescriptor
format. It is not a Kubernetes CR: do not write `kind: Workload` or `spec:`.

A file that already exists is edited, never regenerated. Keep each field that
the issue does not change.

```yaml
apiVersion: openchoreo.dev/v1alpha1
metadata:
  name: <component-name>        # logical name — no project prefix

endpoints:
  - name: http                  # MUST equal design.json `endpoint.name` (default
                                # `http`); the API gateway binds to this name
    type: HTTP                  # HTTP | GraphQL | Websocket | TCP | UDP | gRPC
    port: 9090
    basePath: /                 # optional; root path for API services
    visibility:                 # see "Visibility" below
      - project
      - external

dependencies:                    # omit a half that you have no entries for
  endpoints:                     # component: `wiring.endpoint`, verbatim
                                 # org-service: resolved by the platform
    - project: <provider-project> # org-service only; absent = same project
      component: <provider-component> # `<project>-<component>`
      name: <provider-endpoint>   # e.g. http
      visibility: namespace       # or project (same-project)
      envBindings:
        address: <ENV_VAR>        # the resolved URL is injected here
  resources:                      # platform-resource / external
    - ref: <resource-name>         # both fields straight from `wiring`
      envBindings:
        <output-name>: <ENV_VAR>
```

The file is done when it opens with `apiVersion` and `metadata.name`, lists the
component's own endpoint (without it, the deploy fails), and has one
`dependencies:` entry for each dependency that has wiring.

A `web-application` can put safe defaults under `configurations.env`; they
become `window._env_` entries. Never a secret or a per-environment value:

```yaml
configurations:
  env:
    - name: SUPPORT_EMAIL
      value: support@example.com
```

## Visibility

### Provider endpoint visibility

The `visibility` of a component's own endpoint:

| Component | `endpoints[].visibility` |
|---|---|
| a `web-application` or a service | `[project, external]` |
| a service with `exposesAPI.orgPublished: true` | `[project, external, namespace]` |

- `project`: access from the same project. It is always on, but write it.
- `external`: the public URL. On a service, it also lets the API gateway
  through the NetworkPolicy. Without it, calls through the gateway get `503`.
  On a `web-application`, the platform uses this URL as the OAuth callback.
  Without it, sign-in fails.
- `namespace`: access from other projects. Add it only when `orgPublished` is
  set.
- Do not write `internal`. It is for platform components, and WSO2 Cloud
  refuses it.

Write `project` and `external` on each environment, also when the design sets
`exposure: intranet`.

### A dependency entry

A sibling SPA reaches a service through same-origin `/api`, not `external`.
In a dependency entry, use only these values:

- `project`, for a provider in the same project.
- `namespace` with `project:`, for a provider in a different project. That
  provider must list `namespace`.

The `react-webapp` skill owns the `/api` proxy.
