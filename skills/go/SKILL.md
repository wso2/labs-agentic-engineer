---
name: go
description: How to build a Go service on the platform — pinned golang:1.25-alpine builder (the build pod runs with GOTOOLCHAIN=local), pure-Go modernc.org/sqlite driver (CGO times out under the build pod's CPU throttle), suggested layout, port 9090, GET /health liveness, multi-stage Dockerfile → slim runtime, embedded SQLite for per-user data inside the owning service. Apply to every Go component.
---

# Go

## What this skill does

The platform's coding-agent + build pipeline have specific constraints
on the Go toolchain (no network-installed newer Go), on CGO (build pods
are CPU-throttled), and on persistence (embedded only — there is no
external Postgres for v1). This skill tells the agent to pin its
Dockerfile base image, use the pure-Go SQLite driver, and follow a
production-shaped project layout.

## Platform facts

- The runner sandbox ships `go` and `node` + `npm` only. No Python, no
  Rust, no custom toolchains.
- The build pod runs with `GOTOOLCHAIN=local` — it will NOT auto-
  download a newer Go toolchain. Pinning an older base image
  (`golang:1.23-alpine` etc.) when `go.mod` requires a newer toolchain
  causes `go mod download` to fail with `go.mod requires go >= X.Y`
  even when the local `go build` verification succeeded.
- The required builder base image is `golang:1.25-alpine`. Using any
  other version is a HARD ERROR at build time. This is enforced by the
  build pod, not by code review — pick the right image up front.
- CGO is not available at build time in any reasonable wall-clock
  budget. The CPU-throttled build pod compiling the SQLite amalgamation
  (`sqlite3-binding.c`, ~3 MB of C) takes 10–20 minutes and frequently
  times out.
- The pure-Go `modernc.org/sqlite` driver compiles in ~30 seconds and
  has the same `database/sql` interface. Use it everywhere.
- Default backend port is 9090.
- Every service exposes `GET /health` returning 200 (the platform's
  readiness probe hits this).
- `/health` is exempt from auth (the gateway lets it through).

## Recommended practice

### Architect

- Default new backend services to Go + `net/http` on port 9090.
- Prefer fewer components: a single Go service owns its API + its
  embedded SQLite database. Do NOT spin off a separate `storage` /
  `database` / `persistence` component.
- Do NOT create scheduled-task / cronjob components in Go (or anywhere
  else). Fold periodic work into the owning service as a background
  goroutine kicked off at startup. Call this out in
  `componentAgentInstructions`.
- For routing, suggest `net/http` (standard library) by default. For
  larger services with grouped routes or middleware chains, `chi`
  (`github.com/go-chi/chi/v5`) is a fine choice. Avoid framework-heavy
  options (Gin, Echo, Fiber) for v1 — they pull large dep trees and
  add little for the platform's typical 5–20-endpoint services.
- Suggest the embedded `modernc.org/sqlite` driver in
  `componentAgentInstructions` when the component owns per-user data
  (e.g. todos, drafts, notes, profile-extension data). Include a
  short note: "Use `modernc.org/sqlite` (pure-Go); driver name is
  `\"sqlite\"`. Store the DB under `/data/<name>.db`."

### Tech-lead — issue body bullets

For every Go service task, include this Scope bullet (HARD requirement):

- "Dockerfile builder base image: Use `FROM golang:1.25-alpine AS builder`
  in the component's `Dockerfile`. The build pod runs with
  `GOTOOLCHAIN=local` and will NOT auto-download a newer Go toolchain
  — picking an older base image (`golang:1.23-alpine` etc.) causes
  `go mod download` to fail with `go.mod requires go >= X.Y` at build
  time even when the local `go build` verification succeeded."

For every Go service task whose component is expected to persist
per-user data, include this Scope bullet:

- "Persistence: use the pure-Go `modernc.org/sqlite` driver (import
  `_ \"modernc.org/sqlite\"`; `sql.Open(\"sqlite\", ...)` — note the
  driver name is `\"sqlite\"`, not `\"sqlite3\"`). Do NOT use
  `mattn/go-sqlite3` — its CGO compilation step times out under the
  build pod's CPU throttle."

For a Go service a `web-application` calls directly — the web-app declares a
`component`-kind dependency on it — that is NOT a managed API (`exposesAPI`
unset), include this Scope bullet:

- "CORS: a browser calls this service cross-origin (the web-app is served from
  a different gateway host), so you MUST wrap the router in a permissive CORS
  middleware that sets `Access-Control-Allow-Origin`/`-Methods`/`-Headers` and
  answers the `OPTIONS` preflight with 204 — the browser blocks every call
  otherwise, and the web-app is dead on arrival. This is not optional for this
  kind of service. A managed API (`exposesAPI` set) instead relies on the
  gateway for CORS and must NOT add its own; see the api-management skill."

For every Go task, include this Acceptance criteria bullet:

- "Local `go build -o /dev/null ./...` exits 0 and, if the service has
  external dependencies, the committed `go.sum` matches a fresh
  `go mod tidy` run. A stdlib-only service has NO `go.sum` — that is
  expected; do not hand-create one."

For a Go service a `web-application` calls directly (a non-managed API,
`exposesAPI` unset — the SAME condition as the CORS Scope bullet above), you
MUST ALSO include this Acceptance criteria bullet, and confirm it before you
open the PR:

- "The router is wrapped in the CORS middleware (the raw `mux` is NOT served
  directly): a cross-origin browser `GET`/`POST` succeeds and an `OPTIONS`
  preflight returns 204 with `Access-Control-Allow-Origin`/`-Methods`/`-Headers`
  set. Verify explicitly: `curl -i -X OPTIONS <one endpoint>` returns `204` AND
  shows an `Access-Control-Allow-Origin` header. Serving the raw mux with no
  CORS wrapper is an INCOMPLETE task for this kind of service — the deployed
  web-app will fail every fetch."

### Coding agent — implementation

Layout (production-shaped, ~5–20 endpoints):

```
<app-path>/
├── go.mod               # module path matches the app folder name
├── go.sum               # ONLY when you have external deps — a stdlib-only service has none
├── main.go              # entrypoint — for small services, all in one file
├── cmd/                 # optional — multiple binaries; usually omitted
├── internal/
│   ├── handlers/        # http handlers, one file per resource
│   ├── store/           # database access (SQLite)
│   ├── models/          # request/response/domain types
│   └── middleware/      # any cross-cutting middleware (rare — see api-management)
├── Dockerfile
└── workload.yaml
```

`go.mod` — pick `go 1.25` (or older if you genuinely don't need newer
features). Module path = app folder name unless the spec dictates
otherwise.

```go
module example.com/<component-name>

go 1.25
```

Routing — `net/http` for small services, `chi` if it earns its keep:

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /health", healthHandler)
mux.HandleFunc("GET /todos", listTodos)
mux.HandleFunc("POST /todos", createTodo)
mux.HandleFunc("PATCH /todos/{id}", updateTodo)
// ... etc.

log.Printf("listening on :9090")
log.Fatal(http.ListenAndServe(":9090", mux))
```

When a `web-application` calls this service directly and it is not a managed
API (`exposesAPI` unset), wrap the router so the cross-origin browser fetch is
allowed — set the headers on every response and answer `OPTIONS` with 204:

```go
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// serve it: http.ListenAndServe(":9090", withCORS(mux))
```

SQLite — pure-Go driver, use literal `"sqlite"` (not `"sqlite3"`):

```go
import (
    "database/sql"
    _ "modernc.org/sqlite"
)

db, err := sql.Open("sqlite", "/data/todos.db")
if err != nil {
    log.Fatal(err)
}

// Initialise schema. Use IF NOT EXISTS so re-deploys are idempotent.
if _, err := db.Exec(`
    CREATE TABLE IF NOT EXISTS todos (
        id        INTEGER PRIMARY KEY AUTOINCREMENT,
        user_id   TEXT NOT NULL,
        title     TEXT NOT NULL,
        done      INTEGER NOT NULL DEFAULT 0,
        created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_todos_user_id ON todos (user_id);
`); err != nil {
    log.Fatal(err)
}
```

Performance is comparable to `mattn` for typical CRUD workloads; the
only loss is FTS3/FTS5 which the platform's todo-shaped services don't
need.

**A nil slice binds as SQL `NULL`, not an empty array.** A list field the
client omitted from the JSON body stays a nil slice, and `database/sql` (incl.
`pq.StringArray`) binds nil as `NULL`. Inserting that into a `NOT NULL`
collection column — e.g. `tags TEXT[] NOT NULL DEFAULT '{}'` — fails at
runtime (a 500), not at compile time. Normalize before the insert
(`if in.Tags == nil { in.Tags = []string{} }`), or leave the column out of the
`INSERT` so its `DEFAULT` fills it. A `DEFAULT` never fires for a column you
list explicitly with a `NULL` value.

`/health` handler (no auth, no DB ping required — keep it cheap):

```go
func healthHandler(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("OK"))
}
```

Errors — `application/problem+json` (see `api-management` for the full
convention):

```go
func problemJSON(w http.ResponseWriter, status int, title, detail string) {
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(map[string]any{
        "type":   "about:blank",
        "title":  title,
        "status": status,
        "detail": detail,
    })
}
```

Consuming another service — the platform injects the upstream's address into
an env var (e.g. `SERVICE2_URL`) through an OpenChoreo connection (the `aep`
skill covers the wiring). Build every request URL onto that address with
`url.JoinPath`, which normalizes the slashes. The injected address can end with
a `/` (the provider endpoint's `basePath` is `/`), and a bare `base + "/path"`
on a slash-terminated address makes `//path` — see the pitfalls table for what
that costs.

```go
base := os.Getenv("SERVICE2_URL") // may arrive as "http://…-service2…:9090/"
if base == "" {
    log.Fatal("SERVICE2_URL not set")
}
target, err := url.JoinPath(base, "simulated-work") // ".../simulated-work", never "//…"
if err != nil {
    log.Fatal(err)
}
resp, err := http.Post(target, "application/json", strings.NewReader(`{}`))
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()
```

`Dockerfile` — multi-stage, pinned builder, slim runtime:

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /src
# Copy only go.mod here — it is ALWAYS present. Do NOT name go.sum as a
# COPY source: a stdlib-only service has no external deps and therefore
# no go.sum, and `COPY go.mod go.sum ./` HARD-FAILS the build when it is
# absent ("go.sum: no such file or directory"). When a go.sum does exist
# it is brought in by the `COPY . .` below and verified by `go build`.
COPY go.mod ./
RUN go mod download
COPY . .
# Build the main package: `./` (the module root, where main.go lives) or
# `./cmd/<name>`. A real `-o` target takes exactly ONE package — with main +
# internal/* present, `go build -o /out/app ./...` fails "cannot write multiple
# packages to non-directory". (`./...` is for the `-o /dev/null` verify only.)
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/app ./

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/app /app
RUN mkdir -p /data
EXPOSE 9090
ENTRYPOINT ["/app"]
```

`workload.yaml` for a Go service:

```yaml
apiVersion: openchoreo.dev/v1alpha1
metadata:
  name: <service-component-name>

endpoints:
  - name: http
    type: HTTP
    port: 9090
    basePath: /
    visibility:
      - external
```

### Build verification

Run BEFORE opening the PR — this catches lockfile hash mismatches,
missing imports, syntax errors, type errors:

```bash
cd <app-path>
go mod tidy 2>&1 | tail -20   # regenerate go.sum from real checksums
go build -o /dev/null ./...   # compile everything; fails on any error
```

After `go mod tidy` succeeds, COMMIT the updated `go.sum` along with
your source IF one was produced. A stdlib-only service (no external
imports) has no dependencies, so `go mod tidy` produces NO `go.sum` —
that is correct and expected, and the Dockerfile above handles its
absence. Only when you DO have external dependencies must the committed
`go.sum` match a fresh `go mod tidy`; without it the build's
`go mod download` / `go build` step fails on missing lockfile entries.

### Don't

- ❌ Use `mattn/go-sqlite3` (CGO times out).
- ❌ Use the driver name `"sqlite3"` — for `modernc.org/sqlite` it's
  `"sqlite"`.
- ❌ Pin `golang:1.23-alpine` or any other Go base image — the build
  pod's `GOTOOLCHAIN=local` rejects toolchain auto-upgrades.
- ❌ Spin off a separate `db` / `storage` component — every service
  owns its own SQLite.
- ❌ Use port 8080, 3000, or any other port — the platform expects 9090.
- ❌ Skip `go mod tidy` before committing — hand-written `go.sum`
  hashes cause `checksum mismatch ... SECURITY ERROR` at build time.
- ❌ Use CGO. Set `CGO_ENABLED=0` explicitly or use the pure-Go driver
  to make sure you're not accidentally linking C code.
- ❌ Concatenate a path onto an injected upstream address
  (`base + "/path"`) — the address can end in `/`, so join with
  `url.JoinPath(base, "path")` instead.

### Common pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Build fails `go: cannot write multiple packages to non-directory /out/app` | Dockerfile used `go build -o /out/app ./...` on a multi-package module | Build the main package only: `go build -o /out/app ./` (or `./cmd/<name>`). `./...` is valid only with `-o /dev/null` for verification. |
| Build fails with `go.mod requires go >= 1.25` | Dockerfile pinned older Go | Use `FROM golang:1.25-alpine AS builder`. |
| Build times out at the `mattn/go-sqlite3` step | CGO compilation under throttle | Switch to `modernc.org/sqlite`. |
| `sql.Open` returns `unknown driver "sqlite3"` | Used `mattn` driver name with `modernc` import | Use `sql.Open("sqlite", ...)`. |
| `checksum mismatch ... SECURITY ERROR` at build | `go.sum` is stale or hand-edited | `go mod tidy` locally; commit the result. |
| Build fails `COPY go.mod go.sum ./ ... go.sum: no such file or directory` | Dockerfile names `go.sum` but a stdlib-only service has none | Use the Dockerfile above (`COPY go.mod ./` only); `COPY . .` brings `go.sum` when it exists. |
| Pod won't start; logs show "panic: listen tcp :8080" | Used wrong port | Use port 9090. |
| `POST` to an injected upstream returns `405` (or a `301` then a `GET`) | The connection-injected address ended in `/`, so `base + "/path"` built `//path`; `net/http` `ServeMux` `301`-redirects to the clean path and the client re-issues the redirect as `GET`, which a `POST`-only route rejects with `405` | Join with `url.JoinPath(base, "path")` — it collapses the doubled slash. |
| Create/POST 500s only when an optional list field is omitted (present, even `[]`, works) | A nil Go slice binds as SQL `NULL` (not `[]`), violating a `NOT NULL` array column; the column's `DEFAULT` is skipped because the INSERT lists it explicitly | Normalize nil→empty before the insert (`if s == nil { s = []T{} }`), or omit the column so its `DEFAULT '{}'` applies. |
