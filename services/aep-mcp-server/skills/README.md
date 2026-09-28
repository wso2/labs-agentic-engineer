# aep-mcp-server skills — canonical source of truth

These `SKILL.md` files are **owned by AEP** and consumed by the OpenChoreo
SRE/RCA agent, which drives the handoff MCP tools this server exposes
(`ae_search_related_issues`, `ae_create_issue`).

They live here — next to the MCP surface whose contract they describe — and
**not** in the repo-root `skills/` agent library, so AEP's own skill reconcile
does not inject them into AEP's coding/design agents.

## coding-agent-handoff

`coding-agent-handoff/SKILL.md` is the handoff skill: it tells the SRE agent's
handoff stage how to search related GitHub issues, file exactly one issue with
RCA context and cross-links, and include the ordered `actionStatuses` that let
aep-api classify the result. Its content is AEP's contract for the two MCP
tools and the one-write handoff.

## How it reaches the SRE agent (deploy-time mount)

The SRE agent does not bake this skill into its image and does not fetch it at
runtime. Instead the skill is **materialized at deploy time**:

1. This file is rendered into a ConfigMap (`rca-agent-skill-coding-agent-handoff`,
   key `SKILL.md`) in the agent's namespace.
2. The ConfigMap is mounted into the agent pod at
   `/etc/rca-agent/skills/coding-agent-handoff/`.
3. The agent's `EXTERNAL_SKILLS_DIR=/etc/rca-agent/skills` makes its loader read
   the mounted copy (searched before its built-in `src/skills` library).

This wiring is done by the local and Kubernetes SRE setup tasks, which read this
file, render the ConfigMap or bind mount, and point the SRE agent's
`EXTERNAL_SKILLS_DIR` at the mounted copy. Edit the skill here, re-run setup
and restart the agent — no SRE image rebuild.

> Edit this file, not any copy on the agent side. There is no committed copy in
> the SRE repo; the only other instance is the transient ConfigMap.
