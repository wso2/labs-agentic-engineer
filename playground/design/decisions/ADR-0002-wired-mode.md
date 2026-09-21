# ADR-0002: `wire` runs the generated app locally, under Compose, with the dev server as the gateway

**Status:** Accepted (2026-09-17)

## Context

The playground could generate an application and never find out whether it
worked. Its checks ended at the file level: `play check` validates each
`design.json` against the published schema and parses every `openapi.yaml`, the
coding agent verifies its own toolchain (`bal build`, `tsc --noEmit`), and a
`web-application` gets a mock-verification walk. That walk is the strongest
signal there was, and its own rules say what it is not: it judges reach and
wiring against `mock/handlers.ts`, and "data, layout and wording are not your
questions".

So three things went unchecked until a deployment:

- **The service vertical.** Nothing ran the service. Its Dockerfile had never
  been built, its schema had never been created, and no business rule had ever
  executed against a real database.
- **The agreement between the two halves.** The generated client and the
  hand-written service are each checked against `openapi.yaml` alone. A request
  key the service names differently, or a response field renamed after the
  client was generated, is invisible to both checks.
- **Reach, for real.** `/me/*` operations key on the caller's username from the
  gateway's signed assertion (ADR-0031, ADR-0032). In mock mode, MSW answers
  them; nothing had ever verified that the service's own interceptor returns
  the caller's rows and 404s everybody else's.

## Decision

**One verb**, `pnpm play <dir> wire`, one foreground process, and the process
lifetime is the session: it plans, brings everything up, asks who you are
entering as, opens the browser, keeps a panel up while you test, and tears
everything down when it exits. No `up`, `down`, `status` or `restart` — quitting
is the cleanup, and a session that died hard is reaped by the next start, by
compose project name.

**Compose is the runtime, and it builds from the Dockerfiles.** Each `type:
service` component is built from the file the coding agent authored, with a
`postgres:16` container per `postgres-cnpg` dependency beside it. This is the
one place the playground builds an image, and it is deliberate: it proves the
Dockerfile, and it removes the per-stack "how do I run this" table a host-process
mode would have needed. It costs a real build per service change; `r` on the
panel rebuilds one.

**Ports are compose's problem.** Every service listens on 9090 inside its
container, as the component contract requires, and siblings reach each other by
service name. Only the host side is mapped. Nothing in the stack skills changes,
and two or ten services never collide.

**The dev server is the gateway.** With `AEP_WIRED_API` set, `mock/plugin.ts`
proxies `/api` to the real service and registers `mock/wired.ts` in front of it.
That module answers the gateway's question — may this caller call this operation
— from the same operation table the browser mock already reads out of
`openapi.yaml`, and then does the one thing a request interceptor structurally
cannot: mints the signed `x-jwt-assertion` the service verifies. The service
cannot tell a stand-in from the platform's gateway, because the whole trust
anchor is one certificate (`GATEWAY_ASSERTION_CERTIFICATE`), and `wire`
generates that keypair per project.

**The webapp stays on the host, under Vite**, because that is where mock mode's
two levers are: `?role=` signs a caller in without an IdP, and the dev server can
be the gateway. A production nginx image can do neither.

**AI is optional and hook-bounded.** Two tasks, both skippable, both shaped by
one rule: *the agent proposes a file or text; the harness executes.* Seeding
writes exactly `.aep-playground/wire/seed.sh`, which the harness replays with no
model in the loop on every seed after the first. Triage, on a failed bring-up,
writes nothing at all and answers in a paragraph. Enforcement is a `PreToolUse`
hook (`engine/wire/agents/guard.ts`), not prompt wording — the same mechanism
and the same reasoning as the runner's workspace guard. Neither task writes
application code: a defect found here becomes an issue and a coding run, which
keeps the coding skill the only thing that authors an application.

**Nothing is deliberately guessed.** The plan is a pure function of
`design.json` and `security.json` — env var names come from `wiring.envBindings`,
the sibling URL from the dependency's name, the roles and their login names from
`security.json`. A dependency wired mode cannot stand in for (`org-service`,
`external`, redis) is listed as unresolved and stops the run; `--skip <name>`
starts anyway with that env unset.

## Consequences

**An app whose `mock/` predates this is refused.** Checked twice: the plan looks
for `mock/wired.ts` and the `AEP_WIRED_API` branch in `mock/plugin.ts` before
anything is built, and the session asks the running dev server whether
`/env-config.js` sets `__AEP_WIRED__`. Found by wiring a completed project:
without the check it came up, printed `READY`, and served every screen from MSW
with the service running for nobody — indistinguishable from working until the
data is wrong. Every app generated before this change hits it, and the fix is
the re-copy `references/mock-mode.md` already prescribes.

`playground/AGENTS.md`'s scope line changes from "no image builds" to "builds
and runs locally, never deploys". The one-way door is small: `wire` writes only
inside `<project>/.aep-playground/wire/`, so a session leaves the project tree
exactly as the coding run left it, and a first-run consent prompt per project
mirrors the coding run's.

What this proves before a push: the Dockerfile builds and the image answers;
the client and the service agree on paths, shapes and status codes; business
rules run against real persistence; reach returns the caller's rows per role;
and every screen a role's rail shows loads without a 401.

What stays with `aep-validation` after a deploy: Thunder sign-in and the real
token's claims (including whether the deployed gateway's assertion carries
`username` — open since 2026-09-16 and only a deployed call settles it),
platform env injection, `workload.yaml`, the webapp's nginx image and its `/api`
proxy, and CORS at the gateway.

### Four decisions the shape forced

**A project with no web application gets a printed `curl` per role, not a
standalone proxy.** Standing the gateway up outside the dev server means either
importing `skills/` — which `knip.jsonc` forbids in as many words — or keeping a
second copy of the gateway stand-in and the contract projection, which is two
copies of security-relevant logic. So such a project gets one `curl` per role
carrying an assertion this side mints, valid for two hours because a person
pastes it by hand, and the printed note says what it is: identity, not the scope
check. The dev-server path stays the only place scopes are enforced.

**Two processes mint the assertion, and one literal keeps them honest.**
`mock/wired.ts` mints per request inside the dev server; `engine/wire/assertion.ts`
mints for that `curl` table. They cannot share code — one is a verbatim app asset,
the other is playground TypeScript — so a role's subject id is pinned as the same
literal in both suites. Change the derivation and both fail.

**The role badge is `react-webapp/assets/mock-badge.ts`, plain DOM.**
`mock/browser.ts` mounts it and is copied to every app, auth dependency or not,
while the `thunder-authentication` tree arrives only with an auth dependency; and
a React component would drag JSX into a plain-TS mock file. Its role list rides
`/env-config.js`, the channel the operation table already uses.

**Database passwords are stored, not regenerated.** Postgres burns the password
into the data directory at first init, so a fresh one authenticates against
nothing and the service exits with `password authentication failed for user` —
which reads like a defect in the generated app. They live in
`.aep-playground/wire/secrets.json` (0600). Triage gets 20 turns for a related
reason: eight was measured too few to reach any answer at all.
