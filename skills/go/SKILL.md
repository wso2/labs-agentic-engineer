---
name: go
description: How to build a Go service on the platform — project layout, the build-verify command, and this stack's constraints and pitfalls. Apply when a component's `language` is Go. For a Ballerina service, use `ballerina` instead.
metadata:
  aep:
    kind: org
    audience: [coding]
---

# Go

A Go service on this platform: one binary, `net/http` on port 9090, its own
platform-provisioned Postgres, built by a CPU-throttled pod that will not
download a toolchain and will not compile C.

## Development flow

1. **Scaffold** — `go.mod` (module path = app folder name, `go 1.25`),
   `main.go`, `Dockerfile`, per Layout. `workload.yaml` follows your prompt — as
   given when it carries one, else per the component contract.
2. **Implement** — handlers, store, models. Every rule under Constraints is a
   build- or runtime-failure if broken, not a style preference. The
   platform-wide rules (port, no required env vars, error shape, dependency
   wiring) live in the `aep` skill's component contract, not here.
3. **Verify** — from the app path:
   ```bash
   go mod tidy                   # regenerate go.sum from real checksums
   go build -o /dev/null ./...   # compile everything
   ```
   Commit the `go.sum` this produces. A stdlib-only service produces none —
   correct and expected; never hand-write one.
4. **PR** — only once step 3 exits 0.

## Constraints

**Toolchain.** Builder base image is `golang:1.25-alpine` — any other version
is a hard error at build time. The build pod runs `GOTOOLCHAIN=local` and will
NOT auto-download a newer Go, so an older image fails `go mod download` with
`go.mod requires go >= X.Y` even though your local `go build` passed.

**No CGO.** Set `CGO_ENABLED=0`, and pick pure-Go libraries. Under the build
pod's CPU throttle, a dependency that compiles a few MB of C takes 10–20
minutes and frequently times out.

**Persistence.** Postgres, provisioned by the platform as a
`platform-resource` dependency the design declares — never a file DB (the pod
filesystem is ephemeral) and never a separate `db`/`storage`/`persistence`
component. Driver is `github.com/jackc/pgx/v5` (pure Go). Connection values
arrive as injected env vars read by name at startup — the `aep` skill's
Dependencies section covers where the names come from. Create schema with
`IF NOT EXISTS` at startup so re-deploys are idempotent.

**A nil slice binds as SQL `NULL`, not `[]`.** A list field the client omitted
stays nil, and `NULL` into a `NOT NULL` collection column (`tags TEXT[] NOT
NULL DEFAULT '{}'`) 500s at runtime. Normalize before insert
(`if in.Tags == nil { in.Tags = []string{} }`) or omit the column so its
`DEFAULT` fires — a `DEFAULT` never fires for a column you list with `NULL`.

**Routing.** `net/http` method patterns
(`mux.HandleFunc("PATCH /todos/{id}", …)`). `chi` is fine for grouped routes
or middleware chains. Not Gin/Echo/Fiber — large dep trees, little gain at
5–20 endpoints.

**Upstreams.** `url.JoinPath(base, "path")`, never `base + "/path"` — an
injected address can end in `/`.

**Periodic work.** A background goroutine started in `main`.

## Layout

```
<app-path>/
├── go.mod               # module path matches the app folder name
├── go.sum               # ONLY with external deps — stdlib-only has none
├── main.go              # entrypoint — small services keep it all here
├── internal/
│   ├── handlers/        # http handlers, one file per resource
│   ├── store/           # Postgres access
│   ├── models/          # request/response/domain types
│   └── middleware/      # cross-cutting only (rare)
├── Dockerfile
└── workload.yaml
```

`workload.yaml` HTTP endpoint when a sibling SPA calls this service:

```yaml
    visibility:
      - project
      - external
```

**Done when:** both list items are present. A single-item `project` list is
wrong even though the SPA uses `/api`. Never put `external` on a
*dependency* entry.

`Dockerfile` — multi-stage, pinned builder, slim runtime:

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /src
# go.mod ONLY: a stdlib-only service has no go.sum, and naming it as a COPY
# source hard-fails the build. An existing go.sum arrives with `COPY . .`.
COPY go.mod ./
RUN go mod download
COPY . .
# Build the main package — `./` (module root) or `./cmd/<name>`. A real `-o`
# target takes exactly ONE package; `./...` is for the `-o /dev/null` verify.
RUN CGO_ENABLED=0 go build -ldflags='-s -w' -o /out/app ./

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/app /app
EXPOSE 9090
ENTRYPOINT ["/app"]
```

Postgres — pure-Go `pgx` pool, DSN built from the injected env vars:

```go
import "github.com/jackc/pgx/v5/pgxpool"

pool, err := pgxpool.New(ctx, os.Getenv("<DB_URL_ENV_VAR>"))
```

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Build fails `go.mod requires go >= 1.25` | Dockerfile pinned an older Go | `FROM golang:1.25-alpine AS builder` |
| Build times out compiling a dependency | A cgo dependency compiling C under the build pod's throttle | Swap it for a pure-Go library; `CGO_ENABLED=0` |
| Data vanishes on every re-deploy | Wrote to a file DB on the pod's ephemeral filesystem | Use the provisioned Postgres |
| `panic: … connection refused` / empty DSN at startup | Read a guessed env-var name, not the injected one | Read the env-var names the platform injected for the resource dependency |
| Build fails `cannot write multiple packages to non-directory /out/app` | `go build -o /out/app ./...` on a multi-package module | Build the main package: `./` or `./cmd/<name>` |
| `checksum mismatch … SECURITY ERROR` at build | `go.sum` stale or hand-edited | `go mod tidy` locally; commit the result |
| Build fails `COPY go.mod go.sum ./ … go.sum: no such file or directory` | Dockerfile names `go.sum`, stdlib-only service has none | `COPY go.mod ./` only |
| Pod won't start; `panic: listen tcp :8080` | Wrong port | Listen on 9090 |
| `POST` to an injected upstream returns `405` (or a `301` then a `GET`) | Address ended in `/`, so `base + "/path"` built `//path`; `ServeMux` 301s to the clean path and the client re-issues it as `GET` | `url.JoinPath(base, "path")` |
| Create/POST 500s only when an optional list field is omitted (`[]` works) | Nil slice bound as `NULL` into a `NOT NULL` array column; its `DEFAULT` skipped because the INSERT lists it | Normalize nil→empty, or omit the column |
| API reachable via SPA `/api` but not curl-able on the public gateway | Provider `visibility` is only `project` (misread "not `external`" as the endpoint list) | List both `- project` and `- external` on the service's own endpoint |
