# ADR-0040 — The SRE agent is configured at install

**Status:** Accepted · 2026-10-02
**Supersedes:** [ADR-0038](ADR-0038-an-organization-has-one-model-connection.md)'s
amendments of 2026-09-29 and 2026-10-01 (the SRE model connection)
**Related:** [`services/aep-api/design/sre-handoff.md`](../../services/aep-api/design/sre-handoff.md) ·
[`sre-handoff-security.md`](../developer-guide/sre-handoff-security.md)

## Context

The OpenChoreo SRE agent runs one Deployment per observability plane, with
one model and one static MCP `Authorization` header that its extensions
loader resolves once at start. It cannot be made org-specific: it cannot send
a different header per org.

AE nonetheless stored an org-scoped SRE model connection, minted a per-org
handoff token in `org_secrets`, and ran a reconciler that pushed both into
the agent's Secret and restarted and scaled it. A separate service,
`aep-mcp-server`, forwarded the agent's bearer to two REST operations, which
a gate on aep-api's public edge let through for that one bearer. Once the
model became an install-time value (ADR-0038's 2026-10-01 amendment), aep-api
was only relaying what `aectl sre install` had passed it, and every per-org
layer served exactly one org.

## Decision

**`aectl sre install` configures the SRE agent, and aep-api serves its
handoff directly.**

1. **The model is written by aectl.** The model, base URL and key are probed
   and then written into the agent's own Secret. aep-api neither stores nor
   pushes them. A re-run with a new key file rotates the key.
2. **One handoff key per installation.** aectl generates a random key and
   writes it into the agent's Secret and aep-api's (two copies, because a
   Secret cannot be read across namespaces). It authenticates the agent, not
   an org.
3. **Each call names its org, and aep-api verifies it.** One agent sees every
   org's alerts on its plane, so both tools take the alert's OpenChoreo
   namespace. That value passes through a model that reads pod logs, so
   aep-api acts on it only after the observer confirms an alert fired
   recently for that namespace, project and component. OpenChoreo's record
   is the trust anchor, not the model's argument.
4. **aep-api is the MCP server.** The agent calls
   `/internal/v1/sre-handoff/mcp` on aep-api, which serves exactly the two
   tools (search and create issues) in process. There is no `aep-mcp-server`,
   and no exception on the public edge for the agent's bearer.
5. **The MCP plumbing is aep-api's own.** The single-response form of
   Streamable HTTP is a few JSON-RPC methods, already hand-written for the
   agents' discovery MCP; both surfaces share it (`platform/mcprpc`) rather
   than take on an MCP SDK dependency.

## Consequences

- Removed: the reconciler, its Kubernetes client, the SRE model connection
  service and table (dropped by migration `phase24`), the per-org token, the
  push Role, the REST handoff gate, the `SREAgent` model-connection capability
  and `aep-mcp-server` with its image and chart templates.
- An org's own OpenAI-compatible model connection no longer doubles as the
  SRE agent's model; the agent has its own, set at install.
- Without a model the agent waits at 0 replicas, as before; `aectl` now sets
  that, not aep-api.
- One installation serves every org on its observability plane, with no
  `--org`. The bound: the key reaches only components that alerted recently,
  so a prompt-injected agent can at most act on another real incident within
  the window. A derived namespace that is not an org handle (WSO2 Cloud's
  `wc-…`) resolves to no org and fails closed.

## Alternatives rejected

- **Keep the per-org machinery for later multi-org.** It cannot deliver
  multi-org while the agent carries one header, and it costs a reconciler, a
  table, a token store and a service now.
- **One org per installation (`--org`).** Simple, but one agent sees every
  org's alerts, so an incident in one org could be filed into another org's
  repo on a project-name clash.
- **Trust the namespace the agent sends.** Multi-org with no check, but the
  one key would then reach any org, and a filed issue can dispatch a coding
  agent.
- **One key in OpenBao, synced to both namespaces.** One source of truth, but
  an OpenBao write from aectl and a sync delay, for a key aectl already holds
  when it writes the agent's Secret.
- **An MCP SDK in aep-api.** A new dependency for four JSON-RPC methods the
  repository already implements.
