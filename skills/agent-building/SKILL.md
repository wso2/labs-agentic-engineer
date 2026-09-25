---
name: agent-building
description: How to build an AI agent component on the platform — designing its `agent.afm.md` contract and implementing that contract as a TypeScript service with the Vercel AI SDK, generating its tools from the OpenAPI contracts of the components it depends on. Apply when a component's `type` is `ai-agent`. For a plain backend service, use `ballerina` or `go` instead.
metadata:
  aep:
    kind: org
    audience: [design, coding]
---

# Building an agent

An `ai-agent` component is one contract and one implementation of it:

- **`specs/design/components/<name>/agent.afm.md`** — the agent itself. Its
  markdown body becomes the system prompt verbatim; its allow-list becomes the
  only operations the model can reach.
- **A TypeScript service** running a Vercel AI SDK loop, which implements that
  document the way a service implements its `openapi.yaml`.

Both halves are here on purpose. They used to be two skills, and the facts
below — which each half states from its own side — drifted apart in ways that
only showed up in a deployed agent.

## Read one of these next

**Writing or editing `agent.afm.md`** → read `references/designing.md`, and stop.

**Implementing the component** → read `references/building.md`, and stop. It
carries the code to copy, and an agent built without it will be missing its
conversation store or its identity gate.

The rest of this page is what BOTH halves must agree on. Read it either way.

## The contract

**`POST /chat`** — `{ conversationId?, message }` in, `{ conversationId, text,
toolCalls }` out. History lives in the agent's own store and never crosses the
wire. `GET /healthz` is the other route; there are no more.

**`conversationId` is the caller's only piece of state.** ABSENT means "new
conversation": create one and return its id. An id that is present but does not
resolve **for this user** is **404, never a new conversation** — adopting a
caller-chosen id lets one caller pick another's and write under it.

**Memory is server-held.** The agent keeps the conversation; the caller holds an
identifier. `x-aep.memory.type: "server"` is the default and what the store
implements. `client` remains valid only where the caller genuinely owns the
transcript — rare, and the design says why.

**Identity is the gateway's, not the caller's.** Every turn arrives with a
gateway-injected `x-user-id`. The agent 401s without it, and scopes every
conversation row by it. The agent never derives identity from the request body
or from the conversation itself.

**The allow-list is the security boundary.** `x-aep.tools.openapi[].allow` names
operations by `operationId` from the dependency's committed contract. An
operation left out is never generated as a tool, so no phrasing can reach it.
A prohibition written in the prompt is a hint; omission from `allow` is a
guarantee. This is why neither half may "helpfully" add an operation the design
did not list.

**Nothing is a literal.** Addresses and credentials are `${env:NAME}`, injected
from the component's dependencies at deploy. `specs/` is committed to git.
`MODEL_*` needs no dependency — an `ai-agent` gets model access from its
component type, either on the organisation's own key or through the platform's
AI gateway. Which one is not the agent's business: it reads the same three
variables either way. See `references/building.md`, "Model access", for the one
branch that differs.

## Where each fact is enforced

A design that declares something the implementation ignores is not a design.

| The contract says | The design declares | The implementation does |
|---|---|---|
| one turn per request | `interfaces: webchat`, path `/chat` | serves `POST /chat`, that shape |
| the agent owns history | `x-aep.memory.type: "server"` | the conversation store, scoped by user |
| only these operations | `allow: [...]` | generates a tool per entry, and no others |
| the caller is whoever signed in | `x-aep.identity.mode: "on-behalf-of"` | gates on `x-user-id`, forwards the caller's token |
| behaviour is graded before the PR | `specs/validation/agent-scenarios.json`, from the requirements alone | runs the scenarios, may revise only the prompt body, reports the score — **unless evaluation is disabled** (`AEP_AGENT_EVAL` unset or not `on`), in which case it is skipped and the PR says so. See `references/building.md`, "Evaluate before you open the PR" |
| no secrets in git | `${env:NAME}` | reads them from config, never hardcodes |
