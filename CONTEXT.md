# AEP — Ubiquitous Language

Glossary of domain terms for the Agentic Engineer Platform. Implementation-free:
this file defines what terms *mean*, not how anything works.

## Agents service (`services/agents`)

**Skill**:
A unit of procedural guidance — a `SKILL.md` (frontmatter `name` + `description`,
markdown body) — that the main agent may follow while editing a spec bundle. The
turn's Skills come from the workspace's immutable skills snapshot, which the
caller names by ref; bodies never cross the wire. A Skill is *guidance*, not
code: never executed, never uploaded to a model provider.
_Avoid_: plugin, capability, tool (a tool is the agent's executable action, e.g. `editFile`).

**Skill catalog**:
The list of available Skills — `name` + one-line `description` only, no bodies —
appended to the end of the agent's system prompt. It is the agent's index for
deciding which Skills to pull. Progressive disclosure: metadata is always visible,
the body is fetched on demand.

**`loadSkill`**:
The tool the agent calls to fetch a Skill's full `content` by name. The body enters
context only when loaded, and then persists as a tool result in message history.
This is the only way a *catalogued* Skill body reaches the model — the standing
blocks (the org's defaults, the Surface's narration policy) are inlined instead,
and are kept out of the catalog precisely because nothing needs to load them.

**Surface**:
Where the person reading a turn's prose is sitting — the console, or a terminal
in the repository. The right vocabulary belongs to the Surface, not to the Skill:
`design.cell` is exactly the right word for someone standing in the repo and
names nothing on a console user's screen. A Surface names the narration policy
its callers' turns carry, so the shared flow Skills stay byte-identical wherever
they run.
_Avoid_: client, channel, caller (the caller is who dispatches a turn; the
Surface is who reads it).

**Spec bundle**:
The in-memory set of files (a snapshot, keyed by path) the main agent reads and
mutates during a turn. Lives only in the service process; never sent to a sandbox.
_Avoid_: workspace, repo, project.

**Turn**:
One request→response cycle of the main agent: a user instruction plus the current
spec bundle in, a stream of file mutations out. One turn = one POST.

## Org skills (`services/aep-api`)

**Org skills repo**:
The per-organization repository that IS the org's skill library — the single source
of truth an agent's Skills are resolved from. The platform seeds it from its own
shipped library and keeps offering updates; the org may edit, add, and remove
skills in it freely.
_Avoid_: skill store, skill database (there is no table — the repo is the record).

**Skill kind**:
**Ownership** of a skill, and nothing else: `platform` (AE-owned), `org`
(the organization's — both platform-seeded defaults and ones it authored), or
`imported` (brought in from a third-party ecosystem). Deliberately independent of
who may edit a skill and of where its updates come from.
_Avoid_: type, category, custom (a retired fourth value that folded into `org`).

**Origin (of a skill)**:
Where a skill's platform-tracked baseline came from, and therefore how its updates
arrive: seeded from the shipped library, or reviewed in from an upstream source.
Orthogonal to ownership — a skill can be the org's to edit while the platform still
offers it updates. Distinct from a Task's **Origin**; the two never appear in the
same conversation, so each is qualified when ambiguity is possible.
_Avoid_: kind (the overload this term was introduced to end).

**Skills manifest**:
The platform-managed record, kept beside the skills themselves, of what the platform
believes about each one — its origin and its baseline. Having no record for a skill
is meaningful: it means the org authored it, so the platform tracks nothing and
offers it nothing.
_Avoid_: index, lockfile, registry.

**Manifest baseline**:
The version of a skill the platform last agreed with the org on — the third point
that lets a comparison distinguish "the platform moved" from "the org edited" from
"both did". Qualified as *manifest* baseline because **Lineage** already uses
"baseline" for Task-generation idempotency.
_Avoid_: original, upstream version, base batch.

**Skill override**:
An org's edit to a skill the platform still tracks. The edit is preserved, never
overwritten — the platform's newer version is offered for review instead. An
override that happens to arrive at exactly the platform's current content is not a
divergence at all: the baseline simply advances.
_Avoid_: conflict, fork, dirty (the org's own edit is a normal act, not a fault).

**Pinned skills**:
The skills a component's build is guaranteed to have loaded, recorded on that
component by the agent that designed it. Deliberately not an exhaustive list of
what a build might consult — the rest of the library stays available, and skills
that turn out to be loaded repeatedly are candidates for pinning.
_Avoid_: required skills, needed skills (both imply the list is complete).

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

**Publisher client**:
The organization's confidential Thunder OAuth application. The coding agent is
this client when it calls the platform. One per organization, reused across
cycles.
_Avoid_: Task JWT (a per-cycle bearer, not this identity), M2M client (other
service-to-service apps), design-agent token.

## LLM credentials (`services/aep-api`)

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

**Reuse**:
The state of an org that has no coding agent key, so the coding agent runs on the
default key. It is the absence of a key, not a stored setting — nothing records
"reuse", and there is no configuration that can claim isolation without a key
behind it.
_Avoid_: shared mode, inherit (nothing is copied — the same key is read).

## Dependencies (`services/aep-api`)

**Platform resource**:
A dependency kind: platform-provisioned infrastructure from a typed catalog
(`resourceType`), approved and provisioned by the user in the console drawer —
e.g. `postgres-cnpg`, `thunder-app`. Authored under a component's
`dependencies[]` in `design.json`; resolved against live platform state, never
a stored "connected/not" flag.
_Avoid_: connection (in OpenChoreo that names a consumed endpoint — the
opposite side of the wire).

**External dependency**:
**One component's** declared need for a third-party API or SDK — a `kind:
external` entry in its `design.json` `dependencies[]` (`style: rest-api | sdk`),
naming the config keys it reads and, for a REST API, an optional `specPath` (a
URL or a committed spec file) the coding agent starts from (ADR-0010). Resolved
at read time (ADR-0003), never a stored flag. It **resolves to** an External
resource: a **Registered External resource**'s exact name when the catalog
already has a fit, otherwise a new **Project External resource** name.
_Avoid_: `needsSpec`, `specUrl`, `sources` — retired fields, rejected on parse.

**External resource**:
The org-level shared record of one third-party integration — name, description,
config-key schema — so many components reuse it by name instead of each
redefining it. Listed on **Resources**. Two kinds below. The OpenChoreo
`ResourceType` *is* the record (ADR-0009).
_Avoid_: "external_resources table" (removed); connection.

**Registered External resource**:
An External resource the org registered once, with org-held environment values
and **consumption instructions**, so a later project that needs the same API
reuses that name instead of collecting the values again.
_Avoid_: org API, shared secret (the resource is the integration, not the secret).

**Project External resource**:
An External resource invented for one project's design when nothing in the
catalog fits, or the user asks to reconsider. Its environment values are that
project's.
_Avoid_: unregistered external, local external.

**Consumption instructions**:
How a consuming project should use a Registered External resource — distinct
from what the resource *is* (description), never a restatement of it. Their
presence on the catalog record is what makes the resource Registered (ADR-0021).
_Avoid_: usage notes, description (a different field).

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

## Security & access (`services/aep-api`)

**Security architecture**:
The prose half of a project's security design (`specs/design/security.md`) — how
a caller's role is resolved from a token, and the policy narrative behind the
access rules. It names no Role and declares no Test user: the Roles document
owns both, so the two documents cannot contradict each other.
_Avoid_: security spec, auth doc; and never restate a fact `design.json` already
holds (the Thunder application's dependency name, its scopes, and which
components sit on each side of sign-in are all read from there).

**Roles document**:
The structured half of a project's security design (`specs/design/roles.json`) —
which Roles this project uses, what each may do **within this project**, and its
Test users. A design artifact like any other: authored during design, versioned
into the project's `v<N>` tag, read at build time by the platform alone. It
DECLARES Roles rather than owning them; only the permissions it grants them are
this project's.
_Avoid_: security.json, access model, permissions file, RBAC config (it is not
enforcement — it is the declaration the platform provisions from and the coding
agent wires to).

**Role**:
A named group of people on the Platform IdP, reaching an app as a `groups` claim.
A Role is **shared, not project-scoped**: its scope is whatever the IdP's scope
is — cluster-wide while one IdP serves the cluster, narrower once the IdP is.
Two projects naming the same Role mean the same Role, and a person who holds it
holds it everywhere. What a Role may DO is per-project (the Roles document); the
Role itself is not — so a Role OUTLIVES the projects that declare it: dropping it
from a design, or deleting the project, leaves the Role standing.
_Avoid_: group (the IdP's word for what a Role is; use Role in the domain),
permission (a Role is granted permissions, it is not one), scope (an OAuth
concept, unrelated), project role (there is no such thing).

**Role catalog**:
The Roles that already exist on the Platform IdP, read at design time so a design
REUSES an existing Role instead of minting a near-duplicate. Read-only to the
design agent, exactly like the external-resource and platform-resource-type
catalogs it already consults before inventing a name.
_Avoid_: role registry, directory (the directory is the whole user store; the
catalog is the readable list of Roles in it).

**Test user**:
An account on the Platform IdP, holding a Role, that exists so that Role's
behaviour can be exercised — the validation agent signs in as one to judge
role-gated acceptance criteria. Shared on the same terms as a Role, and outliving
a project the same way. Every Role has at least one: the user may name their own,
and the platform supplies any the design does not. Its password is
platform-generated and PUBLISHED in the Roles gate ticket, which is where the
validation agent reads it; a Test user is therefore a disposable account for
agents, readable by anyone who can read the repository, and never a real person's.
_Avoid_: demo user, seed user, service account (a Test user is a person-type
account standing in for a real end user, never a machine identity).

**Roles gate**:
The `provision` gate titled "Provision roles and test users", minted once per
version beside the per-dependency gates and resolved by the platform itself in
the same pass. It is driven by the DESIGN at the tag, not by the Build drawer's
inputs, which is what makes a Role added in v2 actually get created; and it
carries `aep:gate/roles`, not an `aep:dep/` label, so it can never be mistaken
for a dependency's gate. Like every gate it holds the next dispatch while open —
which earns its keep only on failure, when validation would otherwise run
unable to sign in. Before closing, it PUBLISHES every Test user's login as a
comment, under an `<!-- aep:test-users -->` marker: that comment is the
validation agent's source for the credentials it signs in with, and a failure to
publish fails the build rather than sending validation into a run it cannot sign
in for.
_Avoid_: roles issue, provisioning task (it is never agent work — it carries no
`aep` arming label, and nothing may work it).

## Tasks

**Task**:
A unit of work on a project — implement or change a component, remediate an
incident, or perform a platform operation. A Task *is* a GitHub issue in the
project repo (its labels, body, open/closed state); it exists the moment such an
issue exists, regardless of who created it. GitHub is the sole owner of Task state.
_Avoid_: component task, ticket, work item, issue-row.

**Execution**:
One platform attempt at a single kind of **non-agent** work for a Task — build
(merge → build → deploy), ops (a platform operation), or provisioning. Owned by
the platform, referencing the Task by issue number. A retry is a new Execution,
never a mutation of an old one or of the Task. The platform projects Execution
progress onto the Task (labels/comments); never the reverse. **Agent work mints
no Execution**: a coding, conflict, fix or validation dispatch is a run cycle
(below), and the cycle record is the platform's bookkeeping for it.
_Avoid_: attempt, job; calling anything a coding agent does an Execution.

**Milestone run**:
One supervised pass over one milestone — the platform's single dispatch door, and
a first-class domain term (`milestone_runs`, `MilestoneRunWorkflow`). A milestone
sees sequential runs across its life, and a run dispatches its cycles one at a
time.
_Avoid_: execution (a run is not one), pipeline, build (the console's "build" is
the click that starts a run, not the run).

**Run cycle**:
One dispatch within a run — `coding | conflict | fix | validation` — and the unit
the coding agent actually runs as: one ephemeral OpenChoreo `coding-agent` job
Component in the milestone's own project, per cycle, never reused. The cycle
record carries branch, pull-request number and merge SHA, all learned from
webhooks. Its live progress is the pod's log; its history is an observer query
that lasts only as long as the Component is retained.
_Avoid_: execution component (the retired term — a cycle is milestone-scoped, not
task-scoped), task job, run (that is the supervising pass above).

**Executor class**:
The single dimension that routes a Task to its executor: `coding` (fulfilled by a
coding agent, produces a pull request) or `ops` (fulfilled by a platform-operations
executor, e.g. create a database, provision an IDP application). Carried as a label
on the Task.
_Avoid_: task type, sre (a role, not a work class; incident-born code fixes are `coding`).

**Origin (of a Task)**:
Where a Task came from: spec-plan generation, an incident, or a human. Non-routing
metadata — an incident-born Task needing a code fix is still `coding`. Distinct from
a skill's **Origin**, which describes where its tracked baseline came from.
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

## Project overview

**Stage aggregate**:
One of the three per-stage summaries — spec, build, deploy — a project's status
reports, so the overview renders the whole pipeline from a single read.
_Avoid_: phase (the legacy flat field), pipeline state.

**Spec version**:
The `v<N>` tag: a snapshot of a validated requirements+design pair, cut at the
moment a build starts. Implementation lands *after* the version is cut; the
version names what the build implements, not the resulting code state.
_Avoid_: release, build number.

**Open question**:
A numbered entry under `## Open Questions` in the PRD — a recorded gap in the spec, and
specifically one the agent may not close by assuming: a fact only the user holds. Deliberately
a property of the *document*, not of any conversation. It **gates nothing** — design and build
both proceed with open questions outstanding. An entry marked *deferred* is one the user has
declined for now, which tells the agent to stop raising it rather than releasing any gate.
_Avoid_: interview question (the agent's live request for the user's input, which is a
mechanism for closing an open question, not the thing itself); blocker (it blocks nothing).

**Dirty (spec)**:
The spec content has moved past the latest spec version in committed truth.
Always derived, never stored — a spec is "approved" exactly when it has a
version and is not dirty.
_Avoid_: draft flag, spec status (as a stored fact).

**Build progress**:
A build run's own task tally (total/done/failed/active), frozen when the run
ends. Describes *that run*; the Tasks page remains the live per-task truth.
_Avoid_: task list (unbounded detail — the opposite of a tally).

**Live version**:
The spec version whose implementation most recently completed a build run —
what the platform reports as live in the dev environment.
_Avoid_: deployed tag (no tag is cut at deploy time today).

## Spec collaboration

**Committed truth**:
The durable, git-stored form of a project's spec bundle — the authority. Every
read that must be correct (a build tag, a plan turn, validation) reads it. There
is exactly one write chokepoint to it.
_Avoid_: saved state, draft store (the live doc is not a draft of it — see below).

**Live doc**:
The ephemeral, co-edited representation of the *same* spec bundle while people (and
agents) are editing it together — a snapshot that exists only while a session is
open. It is a second representation of the committed-truth aggregate, not a separate
aggregate: rejoining reseeds it from committed truth. Not durable on its own.
_Avoid_: draft, working copy (it is not a fork of the truth; it *is* the truth's
live face while a room is open).

**Room**:
One live collaboration session over a single project's spec bundle. While a room is
live for a project, the live doc is that project's spec authority and the session's
committer is the sole writer to committed truth.
_Avoid_: workspace, session-id (a room is scoped to one project's spec bundle).

**Room-mode (turn)**:
A generation Turn that runs while a room is live: it streams its file mutations into
the live doc and commits nothing itself — the room's committer lands them. This is
what keeps the two write paths from racing (only one writer to committed truth while
a room is open).
_Avoid_: dry-run, preview turn (a room-mode turn's edits are real, just landed by the
committer rather than the turn).

## Secrets

**SecretReference**:
An OpenChoreo CR that names a vault path for a secret. It lives in the same
control-plane namespace as the Workload that consumes it. The vault path's
`wc-…` segment (`OrgBaseNamespace`) is a storage key, not that namespace.
_Avoid_: treating OrgBaseNamespace as the SecretReference CR namespace.
