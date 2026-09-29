# 14 Glossary

Every term this spec uses.

## Domain terms

Copied word for word from the repository's `CONTEXT.md`. If the two ever differ, `CONTEXT.md` wins.

**Room**:
One live collaboration session over a single project's spec bundle. While a room is
live for a project, the live doc is that project's spec authority and the session's
committer is the sole writer to committed truth.
_Avoid_: workspace, session-id (a room is scoped to one project's spec bundle).

**Live doc**:
The ephemeral, co-edited representation of the *same* spec bundle while people (and
agents) are editing it together — a snapshot that exists only while a session is
open. It is a second representation of the committed-truth aggregate, not a separate
aggregate: rejoining reseeds it from committed truth. Not durable on its own.
_Avoid_: draft, working copy (it is not a fork of the truth; it *is* the truth's
live face while a room is open).

**Committed truth**:
The durable, git-stored form of a project's spec bundle — the authority. Every
read that must be correct (a build tag, a plan turn, validation) reads it. There
is exactly one write chokepoint to it.
_Avoid_: saved state, draft store (the live doc is not a draft of it — see below).

**Room-mode (turn)**:
A generation Turn that runs while a room is live: it streams its file mutations into
the live doc and commits nothing itself — the room's committer lands them. This is
what keeps the two write paths from racing (only one writer to committed truth while
a room is open).
_Avoid_: dry-run, preview turn (a room-mode turn's edits are real, just landed by the
committer rather than the turn).

**Spec bundle**:
The in-memory set of files (a snapshot, keyed by path) the main agent reads and
mutates during a turn. Lives only in the service process; never sent to a sandbox.
_Avoid_: workspace, repo, project.

**Turn**:
One request→response cycle of the main agent: a user instruction plus the current
spec bundle in, a stream of file mutations out. One turn = one POST.

**Design agent**:
The agent that authors and edits a project's spec, design, and Task plan. It reads
skills as guidance for that work, and records which skills each component's build
will need.
_Avoid_: engineering agent (coding is engineering too), architect (a role heading
inside a skill's body, not the agent).

**Coding agent**:
The agent that implements a component — it builds, verifies, and opens the pull
request. It reads skills as guidance for construction. When it calls the
platform, it is the organization's **publisher client**, not a per-cycle token
and not the design agent.
_Avoid_: builder, implementer agent, runner (the runner is the pod it executes in).

**Run cycle**:
One dispatch within a run — `coding | conflict | fix | validation` — and the unit
the coding agent actually runs as: one ephemeral OpenChoreo `coding-agent` job
Component in the milestone's own project, per cycle, never reused. The cycle
record carries branch, pull-request number and merge SHA, all learned from
webhooks. Its live progress is the pod's log; its history is an observer query
that lasts only as long as the Component is retained.
_Avoid_: execution component (the retired term — a cycle is milestone-scoped, not
task-scoped), task job, run (that is the supervising pass above).

**Publisher client**:
The organization's confidential Thunder OAuth application. The coding agent is
this client when it calls the platform. One per organization, reused across
cycles.
_Avoid_: Task JWT (a per-cycle bearer, not this identity), M2M client (other
service-to-service apps), design-agent token.

**Default key**:
The organization's Anthropic API key. Every reader uses it — the design agent, the
coding agent, the RCA agent — unless a more specific key overrides it. An org
without one cannot run any agent; there is no platform-provided fallback.
_Avoid_: platform key (the platform provides none), primary key (implies a
secondary that does not exist, and collides with the SQL sense).

**Coding agent token**:
An organization's optional Claude subscription token, used by the coding agent and
by nothing else (ADR-0036). It is not a second API key: it bills a Claude plan,
not API credits. It can only exist while a default key exists, and it changes
what is billed, never whether an agent can run. Without one, coding runs bill the
default key.
_Avoid_: coding agent key, second API key, secondary key, coding LLM credential.

**Platform IdP**:
The single Thunder instance every generated app's end-user sign-in and every
gateway JWT verification trusts — one issuer, one JWKS, one keymanager-gateway
trust chain, never one per project or per org.
_Avoid_: tenant IdP, dedicated IdP (a future bring-your-own-instance reference
is out of scope today).

**SecretReference**:
An OpenChoreo CR that names a vault path for a secret. It lives in the same
control-plane namespace as the Workload that consumes it. The vault path's
`wc-…` segment (`OrgBaseNamespace`) is a storage key, not that namespace.
_Avoid_: treating OrgBaseNamespace as the SecretReference CR namespace.

**SM API**:
The platform's secret manager API: the one door through which the platform
stores an organization's secret values in vault. It is write-only to the
platform. It gives back names and keys, never a value, so a secret written
through it cannot be read back by the platform that wrote it.
_Avoid_: vault (the store behind it), secret store, secrets service.

**Environment Thunder**:
The Thunder identity provider for one organization and one environment. It is a
different issuer from the Platform IdP, with its own keys: there is one per
organization and environment, where the Platform IdP is one for the whole
platform. Agentic Engineer does not use it.
_Avoid_: dataplane IdP (it does not run in the dataplane), tenant IdP, Platform
IdP (a different issuer).

**`ae-studio-<org>`**:
The per-organization identity the design agent uses to join a Room. A Thunder
Agent entity in the org's organization unit on the Platform IdP, created by
`aep-api` with the Resource. Its secret is mounted only on `ae-studio-tools`, which
hands the design agent a token for a Room join.
_Avoid_: agent Room token (retired), publisher client (a different identity: a
coding run holds that one and must not join Rooms).

**AE-only control-plane client** (`APP_FACTORY_BFF_TO_AE_STUDIO`, working name):
The Platform IdP client `aep-api` uses for every call to the `/internal/v1/*`
routes of `ae-studio-tools`: server-started turns and the low-level git and GitHub
operations. It sends the org in the `X-Impersonate-Org` header. Only
`ae-studio-tools` accepts it, so it never reaches a model container. It is not in
platform-api's impersonation policy, and its secret never leaves `aep-api`.
_Avoid_: `APP_FACTORY_BFF_TO_PLATFORM_API` (the shared client for platform-api only,
never sent to the dataplane).

## Spec terms

Words used only in this spec. The `ae-*` names are implementation names, so they are not in `CONTEXT.md`.

| Term | Meaning |
|---|---|
| **AE** | Agentic Engineer, the product. |
| **CP** | Control plane: where `aep-api`, Postgres, the SM API, the Platform IdP and the OpenChoreo control plane run. In WSO2 Cloud, cloud-cp. |
| **DP** | The organization's dataplane: where Resource `ae-studio` and the coding agent Job run. |
| **`aep-api`** | The Go backend (BFF). Authorizes users, keeps rows, writes secrets through the SM API, Ensures the runtime, creates the per-org clients, tells the console where `ae-studio` is, runs the Temporal worker in the same process. It mints no token. |
| **gitpat** | The GitHub personal access token an organization gives AE. The only GitHub connect path in this spec. |
| **gitpat submit** | The procedure that runs when a person pastes the gitpat (also called Connect). The only time the control plane holds the gitpat. |
| **org HMAC** | The per-organization secret GitHub uses to sign webhooks (`X-Hub-Signature-256`). Checked only by `ae-studio-tools`. |
| **Ensure** | `aep-api` creates or heals Project `ae-system`, its `development` ProjectReleaseBinding and Resource `ae-studio`. |
| **`ae-system`** | The OpenChoreo Project that holds Resource `ae-studio`, environment `development`. |
| **`ae-studio`** | The OpenChoreo ResourceType and Resource for the dataplane authoring runtime: one pod, three containers. Not a Room. |
| **`ae-design-agent`** | Container (and image) in `ae-studio` that runs the design agent model. Mounts the Default key only. Accepts only the user JWT (flow 13) and turns started by `ae-studio-tools` on the turn socket. Holds the one-active-turn lock and the conversation thread. |
| **`ae-collab`** | Container (and image) in `ae-studio` that serves Room WebSockets: a public Room listener for the user JWT, and a `localhost` listener for the `ae-studio-<org>` token. Mounts no secrets. |
| **`ae-studio-tools`** | Container (and image) in `ae-studio` that runs no model: git, GitHub, `/v1/*` git-only reads for the browser, `/internal/v1/*` for `aep-api` (server-started turns, git and GitHub operations), webhook receive and HMAC check, the MCP server for `ae-design-agent`, the Room-join token, usage batches, publisher client calls. |
| **`ae-coding-agent`** | Container (and image) in the coding agent Job that runs the coding agent model. Mounts only the org's AI keys (the Coding agent token or the Default key). |
| **`ae-coding-tools`** | Container (and image) in the coding agent Job that runs no model: git and GitHub for this run's repository, platform calls for this run. |
| **`*-agent` / `*-tools`** | Naming rule: a `*-agent` container runs a model and holds only the org's AI keys; a `*-tools` container holds the gitpat, the org HMAC (studio only), the publisher client and the `ae-studio-<org>` client (studio only). |
| **User JWT** | The Platform IdP token of the signed-in user (`aud=APP_FACTORY_CONSOLE`). The browser sends it to `aep-api` and to the three `ae-studio` containers. |
| **AE-only M2M token** | The machine-to-machine token of the AE-only control-plane client, sent with `X-Impersonate-Org` to `ae-studio-tools` `/internal/v1/*` only. `aep-api` mints no token. |
| **JWKS** | The public keys an issuer publishes so receivers can check its tokens. The Platform IdP publishes one, and `ae-studio` checks every token against it. |
| **Token exchange** | RFC 8693: trade one token for another at an identity provider. Not used by AE. |
| **org kgateway** | The public gateway of the org dataplane. TLS and CORS, no token check. |
| **ESO** | External Secrets Operator. Reads vault through a ClusterSecretStore and writes Kubernetes Secrets in the dataplane. |
| **vault** | The secret store behind the SM API (OpenBao on a local install). |
| **emptyDir** | A pod-local scratch volume. The only writable mounts in the agent pods. One emptyDir holds the Files API Unix socket and is mounted only into `ae-collab` and `ae-studio-tools`. Another holds the MCP Unix socket and the turn socket and is mounted only into `ae-design-agent` and `ae-studio-tools`. |
| **Turn socket** | A Unix socket that `ae-design-agent` serves, next to the MCP socket. `ae-studio-tools` uses it to start a server-started turn (flow 2) and get the result. No token. |
| **smee** | A public relay that forwards GitHub webhooks to a local cluster. Local install only. |
| **brain vs hands** | The split between a model container (brain) and its tools container (hands). |
| **TB-n** | Trust boundary n in WSO2 Cloud, TB-1 to TB-9 ([10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md)). |
| **GAP-n** | A control designed but not yet in place in WSO2 Cloud, GAP-3 ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)). |
| **O-n** | An open item this spec does not decide ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)). |
| **Flow n** | Network flow n, 1 to 15 ([04-flows.md](04-flows.md)). |
