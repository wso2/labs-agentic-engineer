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

**Coding agent key**:
An organization's optional second Anthropic API key, used by the coding agent and
by nothing else. It is an override on the default key, not a peer: it can only
exist while a default key exists, and it changes which key is billed, never
whether an agent can run.
_Avoid_: secondary key, coding LLM credential.

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
platform.
_Avoid_: dataplane IdP (it does not run in the dataplane), tenant IdP, Platform
IdP (a different issuer).


## Spec terms

Words used only in this spec. The `ae-*` names are implementation names, so they are not in `CONTEXT.md`.

| Term | Meaning |
|---|---|
| **AE** | Agentic Engineer, the product. |
| **CP** | Control plane: where `aep-api`, Postgres, the SM API, the Platform IdP and the OpenChoreo control plane run. In WSO2 Cloud, cloud-cp. |
| **DP** | The organization's dataplane: where Resource `ae-studio` and the coding agent Job run. |
| **`aep-api`** | The Go backend (BFF). Authorizes users, keeps rows, writes secrets through the SM API, Ensures the runtime, mints short tokens, runs the Temporal worker in the same process. |
| **gitpat** | The GitHub personal access token an organization gives AE. The only GitHub connect path in this spec. |
| **gitpat submit** | The procedure that runs when a person pastes the gitpat (also called Connect). The only time the control plane holds the gitpat. |
| **org HMAC** | The per-organization secret GitHub uses to sign webhooks (`X-Hub-Signature-256`). Checked only by `ae-studio-tools`. |
| **Ensure** | `aep-api` creates or heals Project `ae-system`, its `development` ProjectReleaseBinding and Resource `ae-studio`. |
| **`ae-system`** | The OpenChoreo Project that holds Resource `ae-studio`, environment `development`. |
| **`ae-studio`** | The OpenChoreo ResourceType and Resource for the dataplane authoring runtime: one pod, three containers. Not a Room. |
| **`ae-design-agent`** | Container (and image) in `ae-studio` that runs the design agent model. Mounts the Default key only. |
| **`ae-collab`** | Container (and image) in `ae-studio` that serves Room WebSockets. Mounts no secrets. |
| **`ae-studio-tools`** | Container (and image) in `ae-studio` that runs no model: git, GitHub, webhook receive and HMAC check, publisher client calls. |
| **`ae-coding-agent`** | Container (and image) in the coding agent Job that runs the coding agent model. Mounts an Anthropic key only. |
| **`ae-coding-tools`** | Container (and image) in the coding agent Job that runs no model: git and GitHub for this run's repository, platform calls for this run. |
| **`*-agent` / `*-tools`** | Naming rule: a `*-agent` container runs a model and holds only an Anthropic key; a `*-tools` container holds the gitpat, the org HMAC (studio only) and the publisher client. |
| **CP → DP service token** | Short RS256 JWT minted by `aep-api` per call, `aud` org + the receiving container, TTL 5 minutes. |
| **Room token** | Short RS256 JWT minted by `aep-api` for the browser, `aud` org + `ae-collab` + Room, TTL 5 minutes. |
| **JWKS** | The public keys an issuer publishes so receivers can check its tokens. |
| **Token exchange** | RFC 8693: trade one token for another at an identity provider. Intended at Environment Thunder. |
| **org kgateway** | The public gateway of the org dataplane. TLS only; it does not check identity. |
| **ESO** | External Secrets Operator. Reads vault through a ClusterSecretStore and writes Kubernetes Secrets in the dataplane. |
| **vault** | The secret store behind the SM API (OpenBao on a local install). |
| **emptyDir** | A pod-local scratch volume. The only writable mounts in the agent pods. |
| **smee** | A public relay that forwards GitHub webhooks to a local cluster. Local install only. |
| **brain vs hands** | The split between a model container (brain) and its tools container (hands). |
| **TB-n** | Trust boundary n in WSO2 Cloud, TB-1 to TB-9 ([10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md)). |
| **GAP-n** | A control designed but not yet in place in WSO2 Cloud, GAP-1 to GAP-3 ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)). |
| **O-n** | An open item this spec does not decide ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)). |
| **Flow n** | Network flow n, 1 to 11 ([04-flows.md](04-flows.md)). |
