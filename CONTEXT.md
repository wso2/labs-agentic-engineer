# AEP — Ubiquitous Language

Glossary of domain terms for the Agentic Engineer Platform. Implementation-free:
this file defines what terms *mean*, not how anything works.

## Agents service (`services/agents`)

**Skill**:
A unit of procedural guidance — a `SKILL.md` (frontmatter `name` + `description`,
markdown body) — that the main agent may follow while editing a spec bundle. The
caller passes the candidate Skills (`name`, `description`, `content`) in the turn
request payload. A Skill is *guidance*, not code: never executed, never uploaded
to a model provider, never read from disk by the service (the caller — the eval —
resolves Skills from the repo).
_Avoid_: plugin, capability, tool (a tool is the agent's executable action, e.g. `editFile`).

**Skill catalog**:
The list of available Skills — `name` + one-line `description` only, no bodies —
appended to the end of the agent's system prompt. It is the agent's index for
deciding which Skills to pull. Progressive disclosure: metadata is always visible,
the body is fetched on demand.

**`loadSkill`**:
The tool the agent calls to fetch a Skill's full `content` by name. The body enters
context only when loaded, and then persists as a tool result in message history.
This is the only way a Skill body reaches the model.

**Spec bundle**:
The in-memory set of files (a snapshot, keyed by path) the main agent reads and
mutates during a turn. Lives only in the service process; never sent to a sandbox.
_Avoid_: workspace, repo, project.

**Turn**:
One request→response cycle of the main agent: a user instruction plus the current
spec bundle in, a stream of file mutations out. One turn = one POST.

## Dependencies (`services/aep-api`)

**Platform resource**:
A dependency kind: platform-provisioned infrastructure from a typed catalog
(`resourceType`), approved and provisioned by the user in the console drawer —
e.g. `postgres-cnpg`, `thunder-app`. Authored under a component's
`dependencies[]` in `design.json`; resolved against live platform state, never
a stored "connected/not" flag.
_Avoid_: connection (in OpenChoreo that names a consumed endpoint — the
opposite side of the wire).

**Resource-type marker**:
A declaration a platform engineer attaches to a resource type in the catalog,
naming a generic consumption behavior that type needs — for example, "using
this resource requires end-user sign-in," or "attach this skill wherever this
resource is used." The platform decides how to treat a dependency by reading
these markers, never by recognizing the resource type's name; introducing a
new resource type, including a new way of doing end-user auth, is a catalog
addition, not a platform change.
_Avoid_: reserved name, well-known type name (no resource type's name is ever
meaningful to the platform itself).

**Thunder application**:
A platform resource (`resourceType: thunder-app`) representing a per-project
OAuth client on the Platform IdP. Declared under the same dependency name by
both the SPA that signs users in and the service whose API it protects — the
architect proposes it when the spec implies end-user sign-in; the user
approves and provisions it like any other platform resource.
_Avoid_: connection, caller identity (the retired implicit mechanism this
replaces — see below).

**Platform IdP**:
The single Thunder instance every generated app's end-user sign-in and every
gateway JWT verification trusts — one issuer, one JWKS, one keymanager-gateway
trust chain, never one per project or per org.
_Avoid_: tenant IdP, dedicated IdP (a future bring-your-own-instance reference
is out of scope today).

**Caller identity** — _retired_:
Formerly an implicit per-component flag describing who calls a service's API.
Superseded by the explicit Thunder application dependency: a design.json still
carrying the field is rejected on parse, not silently migrated.
_Avoid_: reviving `callerIdentity` as a design.json key — it no longer parses.

## Tasks

**Task**:
A unit of work on a project — implement or change a component, remediate an
incident, or perform a platform operation. A Task *is* a GitHub issue in the
project repo (its labels, body, open/closed state); it exists the moment such an
issue exists, regardless of who created it. GitHub is the sole owner of Task state.
_Avoid_: component task, ticket, work item, issue-row.

**Execution**:
One platform attempt at a single kind of work for a Task — coding (dispatch →
agent run → pull request), build (merge → build → deploy), or ops (a platform
operation). Owned by the platform, referencing the Task by issue number. A retry
is a new Execution, never a mutation of an old one or of the Task. The platform
projects Execution progress onto the Task (labels/comments); never the reverse.
No Execution spans a human gate: merging a pull request ends nothing — it *spawns*
the build Execution.
_Avoid_: run (collides with OpenChoreo WorkflowRun), attempt, job.

**Executor class**:
The single dimension that routes a Task to its executor: `coding` (fulfilled by a
coding agent, produces a pull request) or `ops` (fulfilled by a platform-operations
executor, e.g. create a database, provision an IDP application). Carried as a label
on the Task.
_Avoid_: task type, sre (a role, not a work class; incident-born code fixes are `coding`).

**Origin**:
Where a Task came from: spec-plan generation, an incident, or a human. Non-routing
metadata — an incident-born Task needing a code fix is still `coding`.
_Avoid_: source (collides with source spec/design version lineage).

**Machine block**:
The versioned, machine-readable document embedded invisibly in a Task's issue body
— the authoritative encoding of its structured facts (component, dependsOn as
component names, lineage, origin, idempotency key). Validated reactively, repaired
when mangled, and re-verified against the design at the moment of use.
_Avoid_: front-matter (a spec-file concept), metadata comment.

**Lineage**:
The spec and design versions a Task was planned from. The idempotency baseline for
incremental task generation — Tasks sharing a design version *are* that plan's batch.
_Avoid_: batch id, baseline batch.

**Command label**:
A label set on a Task by a human or external system to instruct the platform —
the reactive control surface of a Task. Edge-triggered commands (execute) are
consumed by the platform on receipt; level-triggered commands (hold) stand while
present. Only humans/external systems write command labels.
_Avoid_: status label.

**Projection label**:
A label the platform maintains on a Task as a human-facing reflection of derived
state. Written by the platform only, never read back as truth.
_Avoid_: status (as a stored fact).

**Task tool**:
A domain tool registered on the generic agent for planning turns — plan, update,
or close a Task, or set its body. Each call's input is a complete, self-contained
operation; the platform (not the agent) performs it against GitHub.
_Avoid_: file tool (task tools carry domain operations, not file mutations).

**Plan turn**:
A Turn of the generic agent with task tools registered, the spec/design bundle
plus existing open Tasks as read-only context, and planning behavior steered by
a Skill. Its output stream of task tool calls is executed by the platform as it
arrives.
_Avoid_: task generation stream (the legacy two-phase plan/detail orchestration).

**Playground token**:
A short-lived MCP token minted for a human driving the playground locally,
via an endpoint that exists only when explicitly enabled in a local deployment.
Scoped to one org; minted fresh per turn. Never part of the production
authentication story (which remains an open decision).
