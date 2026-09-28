# aep-api e2e golden harvest (Phase 0a)

A **one-time** capture of real aep-api HTTP responses over the public read-spine
plus representative errors. It is the before/after **diff oracle** that protects
the Phase 0b `internal/app.Build` extraction (`buildApp` has zero coverage today),
and a source of realistic inputs for later component tests. It is **not** a
maintained e2e suite — see [ADR-0003](../../../../docs/decisions/ADR-0003-aep-api-test-migration-strategy.md)
and [the progress tracker](../../../../docs/design/aep-api-test-migration-progress.md).

- **Captured:** 2026-07-01, against the live local OpenChoreo cluster.
- **Org:** `default` (single tenant). Org is derived from the JWT, never the URL.
- **Subject project:** `hello-world-api` (the fuller SDLC state; `apii` also exists).
- **Surface:** the public edge at `http://localhost:9090/api/v1` (routes register
  server-relative, e.g. `/projects`; the `/api/v1` prefix is applied by the humago
  adapter). The `/internal/v1/*` S2S surface is a separate root and is **out of
  scope** for this harvest.

## Layout

```
harvest/
├── README.md            # this file
├── manifest.json        # [{name, method, path, status, golden_file, notes}] × 37
├── golden/              # 37 pretty-printed response bodies (raw, un-normalized)
├── console-calls.log    # /api/v1 calls observed by driving the real console
└── replay.sh            # re-hit every entry + normalize → for the 0b diff
```

## What was captured (37 responses)

**Public GET read-spine (33 × 200):** organizations list; projects list/get/status;
requirements (bundle, versions, version@tag, collab-session); collab/validate
(S2S carve-out, needs `X-Room-Id`); design (get, bundle, versions, bundle@tag);
tasks (list, generated, get, status, progress/agent, progress/build); board;
components (list, get, builds, deployments, openapi, configs); org settings
(skills list/updates/get, idp profile, idp discovery, github status, anthropic
status).

**Representative errors (4):**

| golden | status | shape | note |
|---|---|---|---|
| `err_401_projects` | 401 | `{error, message}` | **middleware** error shape (JWT gate), *not* RFC 9457 |
| `err_404_project` | 404 | RFC 9457 `{$schema,title,status,detail}` | unknown project |
| `err_422_create_project` | 422 | RFC 9457 + `errors[]` | huma validation (platform returns 422, not 400) |
| `get_component_build_logs` | **500** | RFC 9457 `{...,detail:"failed to get build logs"}` | see caveat below |

## Discovered fixture params

The parametrized routes used ids discovered from their parent list responses:

| param | value | source |
|---|---|---|
| project | `hello-world-api` | seeded |
| component | `hello-api` | `get_components` |
| requirements tag | `v1` | `get_requirements_versions` |
| design tag | `v1-1` | `get_design_versions` |
| skill | `go` | `get_org_skills` |
| idp discovery issuer | `http://thunder.openchoreo.localhost:8080` | required `?issuer=` query |
| build name | `hello-world-api-hello-api-1782887236864` | `get_component_builds` |
| collab room | `spec-default-hello-world-api` | `X-Room-Id` header |

## Caveats

- **build-logs 500 is real, not a harvest artifact.** `GET
  .../builds/{buildName}/logs` returns `500 failed to get build logs` for the
  seeded completed build (the build pod's logs are no longer retrievable from
  OpenChoreo). It is kept as the representative **500** golden (proves the
  500→RFC-9457 mapping). It is **deterministic** on this cluster today but is the
  most likely golden to change if the build data is re-provisioned.
- **401 uses a different error shape** than 404/422/500. The missing-auth 401 is
  emitted by the upstream JWT middleware (`{error, message}`), *before* huma's
  RFC-9457 mapper runs. Error-mapping tests must expect **two** shapes.
- **`get_component_config` is literal `null`** (200) — no config set for the
  component. Valid JSON, kept as `null`.
- **`seq` is volatile.** The `progress/agent` and `progress/build` event streams
  carry a monotonically-increasing per-emit `seq` counter that shifts on every
  read. `replay.sh` blanks it (see normalization).

## Volatile / normalized fields

`golden/*.json` are stored **raw** (real values, for human reading + as component
fixtures). `replay.sh` writes **normalized** bodies so before/after diffs are
stable. Normalization blanks:

| what | rule | placeholder |
|---|---|---|
| UUIDs | any string matching the UUID pattern | `<uuid>` |
| timestamps | any string starting `YYYY-MM-DDThh:mm:ss` (`createdAt`, `updatedAt`, `dispatchedAt`, `lastEventAt`, `ts`, …) | `<ts>` |
| `$schema` host | `http(s)://<host>/api/v1/*.json` → strip host | `<host>/api/v1/*.json` |
| `seq` | object key `seq` (progress streams) | `<seq>` |

Git commit hashes (`commitHash`, `lastBuildSha`) are **stable** across a refactor
and left as-is.

## Re-minting the token

The saved bearer is a Thunder end-user JWT with a **~24h TTL** — it will be
expired for any run after 2026-07-02. To re-mint:

1. Log in to the console `http://localhost:8090` as `admin` / `admin`.
2. Read localStorage key
   `session_data-instance_0-aep-console-client` → its `.access_token` field.
   (`agent-browser storage local get session_data-instance_0-aep-console-client`,
   or the coordinator can re-extract the same way it produced `token.txt`.)

## Running replay.sh (the 0b before/after oracle)

```bash
cd services/aep-api/testdata/harvest

# BEFORE the 0b refactor (against the current binary):
TOKEN='<bearer>' ./replay.sh before      # or: TOKEN_FILE=/path/token.txt ./replay.sh before

# ...perform Phase 0b (extract internal/app.Build), rebuild + restart aep-api...

# AFTER:
TOKEN='<bearer>' ./replay.sh after

diff -ru before after                    # MUST be empty → behavior preserved
```

Inputs are env vars: `TOKEN` (or `TOKEN_FILE`) required; `BASE`
(default `http://localhost:9090`), `ROOM`, `CURL`, `JQ` optional. The output dir
is the first arg (default `./replayed`). The route list, methods, and per-entry
params all come from `manifest.json`, so replay stays in lock-step with the harvest.

**Run the two passes sequentially, not concurrently** — firing two full replays
at once transiently exhausts the server (the `design/bundle` git checkouts are
heavy) and yields spurious `000` connection failures. A single sequential run is
clean and its normalized output is byte-identical run-to-run (verified 2026-07-01).
