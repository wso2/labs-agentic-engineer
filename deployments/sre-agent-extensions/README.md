# SRE-agent extensions

Content mounted into the OpenChoreo SRE agent's `EXTENSIONS_DIR`
(`/opt/aep/sre-agent-extensions`), using the SRE agent extension point. A
Helm post-renderer (`aectl sre post-render`) mounts the ConfigMap holding
this content into the rendered `sre-agent` Deployment — see
`deployments/helm-charts/design/sre-agent-install.md`.

The AE handoff lives under `remediation/` because the remediation agent already
has the RCA report, root cause, evidence, and recommended actions in scope. That
is the right point to search AE issues and file exactly one AE issue for coding
agent adoption.

- `remediation/mcp.json` points the remediation agent at aep-api's SRE
  handoff (`${AEP_MCP_URL}`, rendered at deploy time) with
  `Authorization: Bearer ${AEP_MCP_TOKEN}`, which the agent expands from its
  own env. The extension loader sends headers only to https URLs, so
  `AEP_MCP_URL` is the https route the platform chart publishes on the
  OpenChoreo control-plane gateway when `sreAgent.enabled`
  (`https://<sreAgent.mcpHostname>[:port]/internal/v1/sre-handoff/mcp`,
  `aectl sre install --mcp-hostname`/`--mcp-port`). `AEP_MCP_TOKEN` is the
  handoff key `aectl sre install` generates and writes into both the agent's
  Secret and aep-api's.
- `remediation/CONTEXT.md` is the unconditional handoff trigger.
- `remediation/skills/coding-agent-handoff/SKILL.md` tells the remediation
  agent how to search, then file exactly one issue with its `actionStatuses`.
  It is written against the two tools' descriptions in aep-api
  (`internal/sourcecontrol/issues/sre_mcp_tools.go`): change them together.

There is no `rca/` directory here. The RCA agent has no AE-specific extension;
the remediation phase owns the handoff.
