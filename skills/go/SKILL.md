---
name: go
description: "How to build a Go service on the platform — project layout, the OpenAPI server generation, scope enforcement, the build-verify command, and this stack's constraints and pitfalls. Apply when a component's `language` is Go. For a Ballerina service, use `ballerina` instead."
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
   go test ./... > /tmp/go-test.txt 2>&1; echo "rc=$?"   # read the rc, not the tail
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

## The OpenAPI server, and the scope it enforces

A service whose `design.json` sets `exposesAPI.auth` implements
`specs/design/components/<name>/openapi.yaml`, and that contract carries the
authorization rules: one scope per protected operation. Generate the server
from it rather than hand-writing routes, because the generated code is what
hands the operation's scope to the middleware.

**Generate.** `internal/gen/oapi-codegen.yaml`:

```yaml
package: gen
# Relative to the directory you RUN the generator from (the App Path), not to
# this config file. It must land in the package `package:` names, or the build
# fails on a package/directory mismatch.
output: internal/gen/server_gen.go
generate:
  chi-server: true
  strict-server: true
  models: true
output-options:
  name-normalizer: ToCamelCaseWithInitialisms
  prefer-skip-optional-pointer: true
```

```bash
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.0 \
  -config internal/gen/oapi-codegen.yaml \
  ../specs/design/components/<name>/openapi.yaml
```

Commit the output. The generated wrapper puts each operation's declared scopes
on the request context as `gen.Oauth2Scopes` (`[]string`), in three states:
absent for `security: []` (public), empty for the inherited document default
(signed in, no scope), and one element for an operation that names a scope.
**Nothing you write parses the spec.**

**Copy the middleware.** From the App Path:

```bash
mkdir -p internal/auth
cp "$AEP_SKILLS_DIR/go/assets/scopes_middleware.go" internal/auth/scopes_middleware.go
# then edit the ONE marked import line to your module path + /internal/gen
```

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` next to this skill's
`SKILL.md` (the BFF mirrors that directory to `.claude/skills/go/`). The file is
verbatim apart from that one import: it cannot be a shared library, because the
scope context key's type is unexported in the generated package.

**Wire it in `Middlewares`, never in `r.Use`.** This is the whole point and
getting it wrong is silent:

```go
handler := gen.HandlerWithOptions(
	gen.NewStrictHandler(srv, nil),
	gen.ChiServerOptions{
		BaseRouter:  chi.NewRouter(),
		Middlewares: []gen.MiddlewareFunc{auth.RequireScope},
	},
)
```

`ChiServerOptions.Middlewares` runs **after** the generated wrapper has put the
scopes on the context and **before** the strict handler decodes the body, so a
401 or 403 costs no parse. A middleware registered with `chi`'s `r.Use` runs
*before* the wrapper, sees `nil`, and treats **every operation as public** — a
full authorization bypass that no test of the handlers can see.

**Read identity only from the injected headers, and read them the way they are
actually shaped.** `api-management` holds the full table; these four are what a
Go handler gets wrong:

| Rule | Why |
|---|---|
| **Never read `Authorization`.** | The gateway's jwt-auth filter **strips** it and re-presents the verified token as `X-Forwarded-Authorization`. `Authorization` reaches a handler only on a **public** operation — where any caller can set it — so it is never an authorization input. |
| **Test a header for *empty*, never for *missing*.** | A claim absent from the token still yields its header, set to `""` (`X-User-Name: ""`, `X-User-Groups: ""`). `r.Header.Get` cannot tell the two apart anyway; `strings.TrimSpace(r.Header.Get("X-User-Id")) == ""` is the check, and it is what the asset does. |
| **`X-User-Scopes` is the authorization authority — and it carries the five OIDC scopes too** (`openid profile email group ou`). | Every signed-in user therefore holds five scopes, so "holds some scope" is never "is authorized". Tokenise on **space** (`strings.Fields`) and compare **whole strings**: `claims:read-all` is not `claims:read`, at this layer or at the gateway. |
| **`X-User-Groups` is JSON** (`["Finance"]`), and this stack does not read it. | Roles reach a service only as scopes. `strings.Split(h, ",")` is wrong even for the one caller that ever needs the list — that is `json.Unmarshal`. |

**What the middleware is, and is not.** It converts *misconfiguration* into a
401/403: an operation accidentally left public, a stale trait, a local run with
no gateway in front. It does **not** stop a hostile pod in the same namespace —
that pod sets `X-User-Scopes` itself and is believed, because the forwarded
headers carry no proof of who set them. The boundary is the NetworkPolicy that
`visibility: internal` creates; this middleware is the second line behind it.
Do not try to close that gap by verifying a JWT in the service.

**Widen inside a handler with `auth.HasScope`.** Where the catalog pairs an
`own` action with an `any` one:

```go
rows := s.store.ByOwner(caller.UserID)
if auth.HasScope(ctx, "claims:read-all") {
	rows = s.store.All()
}
```

Never use `HasScope` as an operation's only check — that is `RequireScope`'s
job, and the contract's.

A **single-row** read is the same rule at the other end: when the caller holds
only the `own` handle and the row belongs to someone else, answer **404, not
403** — a caller who may not learn a row exists may not learn it exists from a
status code either (`api-management` owns the rule; this is its Go shape).

**Test the matrix, through the real router.** Build the wired handler in the
test — `gen.HandlerWithOptions(…)`, not the bare handlers — and write five
tests, by these names:

- **`TestScopeMatrix`** — table-driven over *every* operation × four header
  combinations: no headers, `X-User-Id` only, id + a wrong scope, id + the
  right scope. Assert the status, and on a 403 assert the exact header
  `WWW-Authenticate: Bearer error="insufficient_scope", scope="<handle>"` —
  byte for byte, and assert its **absence** on every other row.
- **`TestHealthIsPublic`** — the `security: []` operation answers 200 with no
  headers *and* with forged ones, and the handler reads neither.
- **`Test<Resource>ListOwnershipAndWidening`** and
  **`Test<Resource>GetOwnershipWidening`** — the `own`/`any` pair: the same
  operation returns the caller's rows with the `own` handle and every row with
  the `any` one, and the single-row read 404s on another owner's id.
- **`TestRouterUseIsFailOpen`** — wire `RequireScope` with `r.Use` instead, call
  a protected operation anonymously, and assert the **200**. It is a test that
  asserts the bug, so the wiring rule above is pinned by code rather than by a
  comment.

Domain invariants (an already-approved claim, a conflicting transition) get
their own test beside these; they are not part of the scope matrix.

## Layout

```
<app-path>/
├── go.mod               # module path matches the app folder name
├── go.sum               # ONLY with external deps — stdlib-only has none
├── main.go              # entrypoint — small services keep it all here
├── internal/
│   ├── gen/             # oapi-codegen output — generated, committed, never edited
│   ├── auth/            # scopes_middleware.go, copied from this skill's assets/
│   │                    #   it IMPORTS internal/gen: the scope context key's type
│   │                    #   is unexported there, so this cannot be a shared library
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
      - internal
      - external
```

**Done when:** all three list items are present. `internal` is the only value
that admits the API gateway to the component's NetworkPolicy — without it the
gateway authenticates the caller and then cannot reach this service, so every
call through the SPA's `/api` proxy answers **503** — and a single-item
`project` list is wrong even though the SPA uses `/api`. Never put `external` on
a *dependency* entry. (`skills/aep`'s `references/workload-and-wiring.md` is the
authority on this list, including the `namespace` an org-published service
adds.)

`Dockerfile` — multi-stage, pinned builder, slim runtime:

```dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /src
# Two branches, and you pick one. A service with ANY dependency — every
# oapi-codegen service is one — has a go.sum and copies both, as below.
# A STDLIB-ONLY service has no go.sum; naming a file that does not exist as a
# COPY source hard-fails the build, so that one is `COPY go.mod ./` instead.
COPY go.mod go.sum ./
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
| Build fails `COPY go.mod go.sum ./ … go.sum: no such file or directory` | Dockerfile names `go.sum`, stdlib-only service has none | `COPY go.mod ./` only — but a service with any dependency needs both |
| Every `/api` call through the SPA 503s | `workload.yaml` endpoint visibility is missing `internal` | List all three: `project`, `internal`, `external` |
| Pod won't start; `panic: listen tcp :8080` | Wrong port | Listen on 9090 |
| `POST` to an injected upstream returns `405` (or a `301` then a `GET`) | Address ended in `/`, so `base + "/path"` built `//path`; `ServeMux` 301s to the clean path and the client re-issues it as `GET` | `url.JoinPath(base, "path")` |
| Create/POST 500s only when an optional list field is omitted (`[]` works) | Nil slice bound as `NULL` into a `NOT NULL` array column; its `DEFAULT` skipped because the INSERT lists it | Normalize nil→empty, or omit the column |
| API reachable via SPA `/api` but not curl-able on the public gateway | Provider `visibility` is missing `external` (misread "not `external`" as the endpoint list) | List all three — `- project`, `- internal`, `- external` — on the service's own endpoint |
| Every operation answers 200 for an anonymous caller, in tests and in the cell | `RequireScope` registered with `r.Use` — it runs before the generated wrapper writes the scopes and sees `nil` | Put it in `gen.ChiServerOptions{Middlewares: …}` |
| `undefined: gen.Oauth2Scopes` | The spec's security scheme is not named `oauth2`, or the server was generated without `chi-server`/`std-http-server` | The const is `<SchemeName>Scopes`; the contract's scheme is `oauth2` |
| A public operation still 401s | `security: []` puts NO value on the context, so a `len(required) == 0` check reads it as "signed-in required" | Use the comma-ok form the asset ships (`required, guarded := …`) |
