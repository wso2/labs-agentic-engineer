# ADR-0020 — Agent conversation memory is server-held behind a store-shaped contract

**Status:** Accepted

## Context

A generated `ai-agent` component's conversation history lives behind the
agent, not in the caller. The caller — a generated web app, the console's Test
tab, or any other client — holds only the `conversationId` it was issued and
its own rendered transcript; it never assembles, inspects, or replays a
message array. This ends the class of bug where a caller mishandles the
history it is responsible for: the wire no longer carries one.

## Decision

**`POST /chat`** is the entire wire contract, for every `ai-agent`:

```
in:  { conversationId?: string, message: string }
out: { conversationId: string, text: string, toolCalls: unknown[] }
```

An omitted or unknown `conversationId` starts a new conversation and returns
its id. `text` is what a UI renders; `toolCalls` is what a test asserts on. No
`messages` array crosses the wire in either direction, in either request or
response.

Conversations are keyed by (`conversationId`, gateway-injected `x-user-id`).
Every store statement — read and write alike — is scoped by that user id. A
request for a `conversationId` that does not exist, or that belongs to a
different user, answers **404**, never 403: a 403 would confirm the id exists,
which is exactly the information a foreign or guessed id must not get back.

The AFM declares the capability: `x-aep: memory: { type: "server" }` (the
`client` type remains valid for designs that predate this decision). The
generated agent reaches its store through a `postgres-cnpg` platform-resource
dependency, declared in `design.json` like any other platform resource:

```json
{ "kind": "platform-resource", "name": "memory-db", "resourceType": "postgres-cnpg" }
```

The dependency is **dedicated to the agent by default**. A design may instead
share the project's existing Postgres by giving the dependency the **same
name** a sibling component already uses — the ordinary `thunder-app`
same-name sharing rule, not new platform machinery. Either way, the agent owns
its `conversations` table exclusively: no sibling reads or writes it, and the
agent touches nothing else in a shared instance. Sharing a Postgres process is
permitted; sharing data ownership is not — a shared instance is not licence to
reach into another component's tables, and the platform's data-ownership rule
holds at table granularity regardless of which physical database a table
lives in.

The schema and turn flow prescribed to the generated agent live in the
`agent-building` skill (`references/building.md`, "Conversation store"). The
first caller built against this contract was the console's Test tab chat
tester, since retired in favour of the platform's test app (`apps/tryit`),
which speaks the same `/chat` contract as the project's test user.

## Alternatives considered

The first cut is **per-agent Postgres, accessed directly** (option 3 below).
Four shapes were weighed for where conversations live:

1. **A conversation-store service over one shared database** — rejected: every
   org's end-user conversations separated only logically, in a database the
   platform owns; wrong for end-user data.
2. **A store service over a per-project database shared with the project's
   services** — rejected: dual-ownership databases and migration fan-out.
3. **A per-agent database, accessed directly by the generated agent** —
   accepted as the first cut. Cheapest real step: the `postgres-cnpg`
   ClusterResourceType exists and platform-resource wiring is generic, so
   provisioning and env injection needed no platform change, for the dedicated
   and the shared-by-name form alike.
4. **A per-project database with no store, every component reading it** —
   rejected: it breaks component data ownership. What was wrong there was
   components reaching into each other's data, not sharing a Postgres process
   — which is why the shared-instance form of option 3 is allowed at table
   granularity.

Storage is `postgres-cnpg` (a CloudNativePG cluster with a volume), not the
emptyDir `postgres` type: conversations survive a pod restart, at the cost of
composing the DSN from `host`/`port`/`dbname`/`user`/`password` outputs.

Memory is a **capability, not a dependency choice** — the AFM says
`memory: { type: "server" }` — but the first cut's database IS declared, because
that is how provisioning works today. When the store service lands the
dependency disappears and the capability is granted by component type, as model
access already is (ADR-0016).

## Accepted limitations of the first cut

| Limitation | Resolved by |
|---|---|
| User fencing lives in generated SQL | the store service enforcing it platform-side |
| One Postgres per agent (~200–300 Mi of node), unless shared by name | the store consolidating to one database per org |
| A shared instance shares its restart and resize blast radius | the org's accepted trade in choosing the shared form; the store removes it |
| No cross-agent platform surface (retention, delete-my-data) | the store's API |
| A schema change means regenerating agents | the store owning the schema |

## Consequences

- Every caller of an `ai-agent` — generated web apps, the console Test tab,
  anything else — is symmetric: hold an id, render a transcript, never touch
  a message array. A caller cannot reintroduce the old failure mode because
  the contract gives it nothing to mishandle.
- Conversation storage is **per-agent** (or per-project, where a design shares
  the dependency by name): each `ai-agent` with server memory owns a
  `conversations` table in a Postgres it either has to itself or shares with
  named siblings. This is a recorded, permanent design point for agents
  built this way, not a stage awaiting consolidation on its own timeline.
- The recorded end state for the platform overall is a single
  conversation-store service with one database per org, replacing per-agent
  direct access outright. That store is not built by this decision; its
  trigger is the point at which agent code stops being able to own
  persistence, user fencing, schema migration, and cross-agent operations
  (retention, delete-my-data) itself — because that code is generated per
  build and those concerns need to live in code the platform owns, once,
  outside any single agent's trust domain. Org is the isolation line because it
  is already the platform's trust boundary (keys, Thunder OUs); the design
  agent's own store (`services/agents/src/store/`) is the proven prototype —
  the same JSONB aggregate, load-append-save and org fence — and the end state
  is that store, extracted and given an end-user fence. The shapes rejected on
  the way are under *Alternatives considered*.
