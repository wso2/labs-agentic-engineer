---
name: code-to-spec
description: Use when writing the AEP specs of a product that already exists as code — the pass that reads a codebase and writes specs/requirements/ (with a coverage file per module) and specs/design/domain-model.md, so /design can take it from there. Runs from any developer's Claude Code, never in a platform turn.
argument-hint: <product root> [output dir]
---

# Code to spec

A product exists as code and is going to be recreated on AEP. This skill
replaces the two flows that would otherwise ask a human what the product
does — `start` and `interview` — because the code already answers them. It
writes the requirements and the domain model; **`/design` writes everything
else**, exactly as it does for a product that started from an idea.

It runs in a developer's Claude Code, from any directory: the platform
contracts it follows travel with it under `references/`. If you were loaded
inside a platform turn — no shell, no file reads — stop and say this skill
is run from a developer's terminal.

## Read the contracts first

Two files beside this one govern every line you write. Read both now; this
skill restates neither, and a rule remembered instead of read drifts.

- `references/prd-contract.md` — every requirements file, the line grammar,
  the IDs, the `sources/` coverage file, what the requirements exclude.
- `references/domain-model-shape.md` — the shape of
  `specs/design/domain-model.md`.

Both are the platform's own skills copied verbatim and pinned by a test in
the platform repository, so what you read is the contract the platform
enforces. Never edit them here.

## What this writes, and what it never writes

```
<out>/specs/requirements/prd.md
<out>/specs/requirements/product-wide.md          (+ product-wide/<topic>.md when a topic grows)
<out>/specs/requirements/features/F<n>-<slug>.md
<out>/specs/requirements/sources/<module>.md      one per module, one per document
<out>/specs/design/domain-model.md
<out>/.inventory/*.md                             working notes; never pushed
```

Never `design.cell`, a `design.json`, `security.json`, `flows/`, `openapi.yaml`,
`wireframes.dsl` or acceptance criteria. Each encodes a decision the platform
makes from the requirements with catalog lookups and write gates you do not
have here; written from legacy code they would hand `/design` a topology
nobody chose. The as-is engineering is recorded, but in the coverage files,
where it informs without becoming the design.

## Arguments

- **Product root** — the directory that is the product: a repository, or one
  app inside a monorepo (`…/digiops-finance/apps/allocation`). Everything
  below it is the product; nothing above it is, with one exception under
  **Read scope**.
- **Output dir** — defaults to `<basename of the product root>-specs/` under
  the current working directory. Refuse an output directory inside the
  product root, and never write into the product's own repository. Someone
  working in the platform repository may point it at the playground's
  projects directory, which git ignores there, to run the local design phase
  on the result. The product's name for `prd.md` comes from its documents,
  else from the root's basename.

## Read scope

Stories, decisions and rules come from the product root only. When a service
the product calls has its source in the same tree — a sibling directory, an
`entity/` or `services/` folder — read **only its schema or contract files**
(GraphQL schema, OpenAPI document, record types) to name the entities and
fields the product uses, and cite them. Never derive a story from a
dependency's code: the product boundary is the root, and a dependency's
behaviour is its own product's requirement.

Configuration files yield **key names only**. A value — a URL, a secret, an
address — never reaches a spec or an inventory. A `Config.toml.local` with
real credentials is an ordinary input; copying one line of it is a leak.

## The inventory pass — before any writing

Nothing can be cut into features before the whole product is in view, and a
codebase is too large to hold in one read. Build six inventories first, each a
markdown list of points in the form `- <file>:<line or symbol> — <fact, in the
code's own terms>`, written to `<out>/.inventory/<name>.md`. The folder is
dot-led on purpose: every turn snapshot, local or platform, drops dot-led
paths, so a `/design` run never inlines the working notes.

| Inventory | What it lists | Where it hides |
|---|---|---|
| `surfaces` | every route, page, screen, navigation entry, scheduled job, CLI verb | routers, resource functions, page components, nav menus, cron |
| `access` | roles or groups, privileges or permissions, which surface each gates, how roles map to privileges | auth middleware, interceptors, route guards, privilege constants |
| `data` | record types, tables, entities fetched from other services, their fields and relations, status enums | type modules, migrations, ORM models, GraphQL queries |
| `integrations` | every outbound client: what it reaches, its auth style, what the product uses it for | client constructors, SDK imports, HTTP clients, mail senders |
| `documents` | every README, ADR, doc page, inline design note, with its claims | `README*`, `docs/`, `design/`, long header comments |
| `rules` | constants, validation patterns, limits, user-facing message strings that state a rule | constants modules, validators, i18n or message files |

When the product has more source than you can read in one pass, run the six
as **parallel subagents**, one per inventory, each told the product root, the
read-scope rule and the point format, each returning its list. Otherwise read
directly. Either way the inventory files exist before step two starts; a
feature cut made from a partial view puts stories in the wrong feature, and
moving them later retires IDs.

## The feature cut

A feature is a capability a user would recognise by name (`prd-contract`,
"What a feature is"). Cut from the `surfaces` inventory:

- **Cluster surfaces by the job a user comes to do**, not by the directory
  that implements them. A package or module name is evidence for a source
  tag, never a boundary; one module routinely serves three features and one
  feature routinely spans backend and frontend.
- **A shared concept is its own feature** for the stories about the concept
  itself — a settings page that manages allocation types is a feature named
  for allocation types; each use of a type stays with the feature that uses it.
- **A grab-bag surface splits.** "Settings" and "Admin" pages are several
  features wearing one menu entry.
- **The documents' feature list is a seed.** Where a README names features,
  start from its names; where the code has a capability the documents lack,
  it is still a feature, and the gap is an open question for the product page.

Write the cut to `<out>/.inventory/features.md`: each feature, its one-line
purpose, and every surface, access point and rule assigned to it. A surface
assigned to no feature, or to two, is a defect to fix here, not later.

## Writing the requirements

The contract governs the files. What the contract leaves to the author of a
code-derived spec is settled here, once.

**Source tags.** Every line that code or a document states carries one, in
the contract's `[<document> · <place>]` form with the module as the document
and the file as the place:

```
- F2.3 As a manager, I approve or reject a claim with a reason. [backend · service.bal:312]
- P1 Every household member signs in through the organization's SSO. [webapp · oauth.js] Applies to: all.
- A claim over 1,000 in the company currency also needs Finance. [README.md · Features]
```

The module is the path relative to the product root; a document is its file
name. `[org default]` is the platform's tag for the organization skill's
defaults and is never yours to write.

**Actors** come from the `access` inventory: a role or group that gates a
surface is an actor, named in product language ("Manager", not
`CUSTOMER_ENGAGEMENT_APPROVER`). Define each once in `prd.md`.

**Stories** come from surfaces: one line per thing a user does, naming its
actor, in the user's words. A route that only serves another route's data is
not a story; the story is what the screen lets the user do.

**Decisions** come from the `rules` inventory and from status enums: a
validation pattern, a numeric limit, an email-routing rule, a status
transition is one Decision line each, in the feature it governs. A rule too
long for a line goes in that file's `## Further Notes`; a topic that spans
features and outgrows `product-wide.md` moves to `product-wide/<topic>.md`.
The requirements are the only route a rule has to the build — `/design` mints
the acceptance criteria from them alone — so a rule left in a coverage file
does not exist to the recreate.

**Translate integrations into the platform's vocabulary** rather than copying
the legacy construct:

| The code has | The requirements say |
|---|---|
| a call to an LLM API for some task | a Decision naming what the agent does: "Skill recommendations: suggested by an agent." No provider, no model |
| a client to a third-party SaaS | a Decision naming the capability and the provider as a given the business already holds: "Consultant skills lookup: ServiceNow — the current system of record" |
| a client to another in-house service | the same, naming the service: "Employee and org data: the HR entity service" |
| sign-in via directory groups or an IdP | a P item: "Every user signs in through the organization's SSO." Roles as actors |
| a runtime screen that edits role → privilege mappings | stories for what the screen does, and an Open Question whether editing privileges at runtime is a requirement or an artifact of the old directory, since the platform fixes scopes at design |
| synthetic identities (pseudo-emails, service accounts standing in for people) | a Decision stating the need they serve, and an Open Question on how the recreate identifies those people |

**`*assumed*`** closes every line you inferred rather than read: a feature's
Purpose beyond naming the capability, a role's intent, an Out of Scope line
no document states, the reading of a quirk as deliberate. A line the code or
a document states carries its source and no tag. The console draws its Settle
control on assumed lines, so the tag is how the team reviews your inferences.

**Open Questions** hold what only the team knows: whether a behaviour is
intended or a workaround, whether an undocumented capability is wanted in the
recreate, what a disabled or half-built path was for. Where a document and the
code disagree, write what the code does and raise the disagreement as the
question. Use `*blocking*` only as the contract defines it, when every story
of a feature waits on the answer; expect it rarely.

**Fog, Out of Scope.** A capability behind a feature flag, a stub or an
unreachable path is Fog with an open question, not a feature. Out of Scope
takes what a document states the product does not do, and what the code
explicitly refuses when a user would expect it; the latter is `*assumed*`.

**What stays out of the requirements:** how it is built. Pagination loops,
retry policies, connection pools, header names, the database split, the
hand-written authorization table. The recreate decides those under the
platform's conventions. They go to the coverage file as `not used` points; one
that turns out to encode a product constraint comes back as a Decision.

## Coverage files — `sources/<module>.md`

The contract's coverage file, with a module standing where an attached
document stands: how the team sees that their code was read, and the one
place that says what was set aside. One per module and one per document.

- **A module** is a directory under the product root at the granularity that
  gives a file of roughly twenty to eighty lines — `backend/`, `webapp/`, or
  a backend's `modules/email/` when the top level is too coarse. The title is
  the module's path relative to the root. No `Pages:` line. Its second line
  records where the code was read, so the line references can be resolved
  later: `Read from <the product root's git remote, or "a local checkout"> at
  <short commit>`.
- **A document** keeps the contract's shape exactly: its file name as the
  title, `Pages:` when it has pages, its sections as places.
- One line per point, in file order, each ending in an arrow:

```
- service.bal:312 (patch engagements/{id}): completes, reopens or cancels an engagement; cancelling cancels its allocations → F3.4, F3.5
- service.bal:561 (get user-privileges): the caller's privileges from their roles → P2
- jwt_interceptor.bal:67: path-by-path privilege table → not used: how it is built; roles and gates are F7
- README.md · Features §2: "Fetch new updated dates from salesforce" → F2.6
- README.md · API: documents 10 of 60 endpoints → not used: superseded by the code
```

Every point in the six inventories appears exactly once across the coverage
files. A `not used` reason is one of: *how it is built*, *superseded by the
code (<file>)*, *dead path*, *test scaffolding*, *outside the product root*.

The design agent reads every `.md` under `specs/` as context, so a coverage
file is where `/design` learns the as-is shape — keep each line to one point
and resist prose.

## `specs/design/domain-model.md`

From the `data` inventory, in the shape `references/domain-model-shape.md`
fixes: an H1, one or two sentences, exactly one mermaid `erDiagram`, a few
lines of entity notes. Include every entity a user would name, including
lookup tables that are product concepts (allocation types, categories);
exclude infrastructure (sessions, migrations, audit tables, job queues). Key
fields only; relations with their cardinality; an entity that lives in another
service is still an entity here if the product's stories act on it. Status
values are Decisions, not notes.

## Self-check before closing

Run these over `<out>/specs/`, and fix what they find:

- **IDs**: no duplicate `F<n>.<m>` or `P<n>`; features numbered in creation
  order; each feature's stories numbered from 1 with no gaps, since nothing
  has been retired yet.
- **Actors**: every actor a story names is defined in `prd.md`.
- **Tails**: every story, decision and P item carries a source tag or
  `*assumed*`; every P item carries `Applies to:`.
- **Arrows**: every coverage line ends in `→ <ID>` or `→ not used: <reason>`.
- **Vocabulary**: `grep -rniwE 'http|https|sql|mysql|jwt|graphql|endpoint|token|json|regex' <out>/specs/requirements` — each hit is rewritten in product language or moved to a coverage file.
- **Secrets**: `grep -rniE 'secret|password|api[-_ ]?key|bearer' <out>/specs <out>/.inventory` — a hit may be a word in a sentence, never a value.
- **Design files**: nothing under `<out>/specs/design/` but `domain-model.md`.

## Close

Report, in a few lines: the features by name, counts of stories, decisions,
P items, assumed lines and open questions, the files written, and that the
output is in `<out>`. Then point at `references/handoff.md` for how the tree
enters an AEP project — the order matters there, and this skill does not
perform it.
