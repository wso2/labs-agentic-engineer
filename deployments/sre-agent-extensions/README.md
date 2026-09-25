# SRE-agent extensions

Content mounted into the OpenChoreo SRE agent's `EXTENSIONS_DIR`
(`/etc/openchoreo/sre-agent` by default), using the SRE agent extension point.

The AE handoff lives under `remediation/` because the remediation agent already
has the RCA report, root cause, evidence, and recommended actions in scope. That
is the right point to search AE issues and file exactly one AE issue for coding
agent adoption.

- `remediation/mcp.json` points the remediation agent at `aep-mcp-server`
  (`${AEP_MCP_URL}`, rendered at deploy time). It deliberately has no
  `headers` entry: the extension loader will not send credentials to a
  plaintext URL, and both the local and in-cluster URLs are plaintext.
  Instead `aep-mcp-server` applies the shared handoff credential itself as
  `AEP_MCP_DEFAULT_BEARER` (`Bearer <AEP_MCP_TOKEN>`), and `aep-api` verifies
  it as `SRE_HANDOFF_TOKEN`. Locally, `setup-aep.sh` generates `AEP_MCP_TOKEN`
  into `deployments/.env` and `docker-compose.yml` wires both services from
  it. On Kubernetes, the platform chart's `sreHandoff.enabled=true` wires both
  from the `aep/aep-mcp-token` OpenBao path and restricts `aep-mcp-server`
  ingress to the SRE agent's pods in its namespace. See
  `docs/developer-guide/sre-handoff-security.md`.
- `remediation/CONTEXT.md` is the unconditional handoff trigger.
- `remediation/skills/coding-agent-handoff/` is not stored here. It is mounted
  from `services/aep-mcp-server/skills/coding-agent-handoff/SKILL.md` at deploy
  time so that MCP server behavior and the SRE agent instructions stay in sync.

There is no `rca/` directory here. The RCA agent has no AE-specific extension;
the remediation phase owns the handoff.
