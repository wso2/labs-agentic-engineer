---
name: go
description: "How to build a Go service on the platform — project layout, the OpenAPI server generation, verifying the gateway's signed assertion, the build-verify command, and this stack's constraints and pitfalls. Apply when a component's `language` is Go. For a Ballerina service, use `ballerina` instead."
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

## The OpenAPI server

A service whose `design.json` sets `exposesAPI.auth` implements
`specs/design/components/<name>/openapi.yaml`. Generate the server from the
contract rather than hand-writing routes: the contract is also what the API
gateway enforces, so generating keeps the two from drifting.

**The gateway has already done the authorization.** It validated the caller's
token and checked the scope each operation declares in that same contract; a
request that failed either never reached this process. So this service holds
**no operation → scope table**, in any form. Writing one is keeping a second
copy of the contract that nothing keeps in sync.

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

Commit the output. **Nothing you write parses the spec.**

## Who the caller is: the gateway's signed assertion

The gateway decides *whether* a request may happen. It cannot prove to your code
that a request *came through it* — any pod that can open a socket to this
service can send whatever headers it likes. So it signs a JWT of the caller it
authenticated and puts it on the upstream request, and this service verifies
that signature against one certificate the platform publishes per environment.

**Copy the verifier.** From the App Path:

```bash
mkdir -p internal/auth
cp "$AEP_SKILLS_DIR/go/assets/gateway_assertion.go" internal/auth/gateway_assertion.go
```

If `$AEP_SKILLS_DIR` is unset, copy from `assets/` next to this skill's
`SKILL.md` (the BFF mirrors that directory to `.claude/skills/go/`). It is
**verbatim — there is no line to edit**, it imports only the standard library,
and it adds nothing to `go.mod`.

**Wire it in `main`, and make a half-configured assertion fatal:**

```go
verifier, err := auth.NewVerifierFromEnv()
if err != nil {
	log.Fatalf("gateway assertion: %v", err)   // refuse to start
}

r := chi.NewRouter()
r.Use(verifier.Middleware)
handler := gen.HandlerWithOptions(gen.NewStrictHandler(srv, nil), gen.ChiServerOptions{BaseRouter: r})
```

`r.Use` is correct here and there is no ordering trap: the verifier needs
nothing from the generated wrapper. The platform sets
`GATEWAY_ASSERTION_CERTIFICATE`, `GATEWAY_ASSERTION_ISSUER` and
`GATEWAY_ASSERTION_HEADER` on the container. A **partially set** trio is fatal:
a service that starts on half of them cannot tell a real caller from a forged
one, and that is a broken deployment rather than an unconfigured one. All three
**absent** is the separate, temporary case an environment gateway provisioned
before the backend-JWT keypair existed still lands in — the verifier starts
without verifying anything, says so on every boot, and is tracked for removal in
issue #789. Do not write code that depends on it.

**Read identity from the verified caller, never from a header.**

| Rule | Why |
|---|---|
| **`auth.RequireCaller(ctx)`** in any handler that needs an identity; 401 on its error. | The assertion is the only statement about the caller that a forged request cannot make. |
| **Never read `X-User-Id`, `X-User-Scopes`, `X-User-Groups` or `Authorization`.** | The first three are unsigned — the gateway sets them, and so can anyone else who reaches this pod directly. `Authorization` is stripped. |
| **A `security: []` handler reads NO identity at all.** | The gateway serves a public operation with no assertion, so `CallerFrom` is absent there by design. |
| **Compare a scope with `==` on the whole handle** (`caller.HasScope`). | `strings.Contains` matches `claims:read` inside `claims:read-all`. The gateway compares whole strings too. |

**Reach is the path, never a branch on a scope.** A `/me/…` handler resolves
rows through the caller's `sub`; a handler outside `/me/` reaches every row and
filters on nothing (`openapi-conventions`):

```go
// GET /me/claims
caller, err := auth.RequireCaller(ctx)
// …401 on err…
rows := s.store.ByOwner(caller.UserID)

// GET /claims — a different operation, guarded by claims:read-all
rows := s.store.All()
```

`auth.HasScope` never decides which rows come back and is never an operation's
only check — both were settled before the handler ran, at the gateway and by
the path.

A **single-row** read is the same rule at the other end: when the caller holds
only the `own` handle and the row belongs to someone else, answer **404, not
403** — a caller who may not learn a row exists may not learn it exists from a
status code either (`api-management` owns the rule; this is its Go shape).

**Test through the real router.** Build the wired handler in the test —
`gen.HandlerWithOptions(…)` with the verifier middleware, not the bare
handlers. Mint the test's assertions with a throwaway RSA key and set **all
three** variables: `GATEWAY_ASSERTION_CERTIFICATE` to that key's self-signed
certificate, and `GATEWAY_ASSERTION_ISSUER` and `GATEWAY_ASSERTION_HEADER` to
the values the test's own assertions are minted with. The certificate alone is a
partial trio, so `NewVerifierFromEnv` returns an error and the test fails before
it has asserted anything. Nothing in the test talks to a gateway. Write four
tests, by these names:

- **`TestForgedAssertionIs401`** — the same request signed by a *different* key
  is rejected, and so is one with its payload edited after signing. This is the
  one test that proves the trust anchor is the signature and not the header.
- **`TestHealthIsPublic`** — the `security: []` operation answers 200 with no
  assertion *and* with forged `X-User-*` headers, and the handler reads neither.
- **`Test<Resource>ListOwnershipAndWidening`** and
  **`Test<Resource>GetOwnershipWidening`** — the `own`/`any` pair: the same
  operation returns the caller's rows with the `own` handle and every row with
  the `any` one, and the single-row read 404s on another owner's id.

There is deliberately no per-operation scope matrix here: that table lives in
the contract and is enforced at the gateway, and a copy of it in these tests
would pass while the deployed API disagreed. Domain invariants (an
already-approved claim, a conflicting transition) get their own test beside
these.

## Layout

```
<app-path>/
├── go.mod               # module path matches the app folder name
├── go.sum               # ONLY with external deps — stdlib-only has none
├── main.go              # entrypoint — small services keep it all here
├── internal/
│   ├── gen/             # oapi-codegen output — generated, committed, never edited
│   ├── auth/            # gateway_assertion.go, copied VERBATIM from this
│   │                    #   skill's assets/ — stdlib only, nothing to edit
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
| Every call 401s right after a deploy | The environment's gateway publishes a different key than the container holds — it was re-provisioned and this component was not redeployed | Redeploy the component; the certificate rides its ReleaseBinding |
| The service will not start: `…must be set together or not at all` | Some of the three are set, not all | Locally, set all three from a throwaway keypair. In a cell, the environment's gateway needs provisioning |
| A public operation 401s | A handler for a `security: []` operation called `RequireCaller` | A public handler reads no identity; the gateway sends no assertion with one |
| Every caller looks anonymous | Read `X-User-Id` instead of the verified caller | `auth.RequireCaller(ctx)` — the headers are unsigned and prove nothing |
