# SRE handoff security notes

The SRE handoff is intentionally narrow: the OpenChoreo SRE agent can search
related AE issues and create exactly one issue. AE owns classification,
deduplication, recurrence, adoption, dispatch, and human-attention state.

## Secret boundaries

- The SRE agent's model connection is resolved and pushed by aep-api — see
  [`services/aep-api/design/sre-model-connection.md`](../../services/aep-api/design/sre-model-connection.md).
  The agent never reads AE's database or Console; it reads plain env vars
  (`RCA_LLM_API_KEY`, `RCA_MODEL_NAME`, `RCA_LLM_BASE_URL`) sourced from the
  AE-owned Secret `sre-agent-aep` in the observability-plane namespace.
- The key value must not be serialized into Helm values, ConfigMaps, logs,
  MCP arguments, or documentation examples.
- The MCP bearer token (`AEP_MCP_TOKEN`) is separate from the model key and
  is used only to let `aep-mcp-server` forward the caller identity to
  `aep-api`.
- That token is a **per-org token aep-api mints itself**
  (`internal/sreagent.Tokens`, stored in `org_secrets` under
  `sre/handoff-token`), not a Thunder-issued JWT and not an operator-supplied
  static secret: the OpenChoreo SRE agent's generic extensions loader
  resolves an MCP server's `headers` from `${VAR}` once at process start and
  never refreshes them, so a short-lived Thunder token would expire mid
  pod-lifetime. `aep-mcp-server` never holds this token — it only forwards
  whatever `Authorization` header the SRE agent sent, unchanged, to
  `aep-api`. `aep-api` verifies it with a narrow checker
  (`auth.SREHandoffVerifier`) scoped to exactly
  `create_issue`/`search_related_issues` and bound to the one org configured
  via `SRE_AGENT_ORG` (the same push-target config that names which org's
  connection this plane's agent runs on) — it never widens what the normal
  Thunder JWT verifier accepts, and any other route still requires a real
  JWT. The verifier is disabled whenever the push target is not fully
  configured (all of `SRE_AGENT_ORG`/`_NAMESPACE`/`_DEPLOYMENT`/`_SECRET`),
  and it rejects any bearer until aep-api has minted a token for that org at
  least once.
- The token is delivered to the agent the same way its model key is: pushed
  into `sre-agent-aep`'s `AEP_MCP_TOKEN` key by aep-api's reconciler, and
  read into the agent's `remediation/mcp.json` as
  `Authorization: Bearer ${AEP_MCP_TOKEN}`, expanded by the extension loader
  from the agent's own env. There is no fallback bearer applied by
  `aep-mcp-server` — every caller must present its own `Authorization`
  header.
- `aep-mcp-server` is reached only over **https**, through an `HTTPRoute` on
  the OpenChoreo control-plane gateway's `https` listener (`sectionName
  https`) plus a `ReferenceGrant`. The extension loader sends `headers` only
  to https URLs, which is why a plaintext route cannot carry the bearer. A
  `NetworkPolicy` admits only the gateway's own proxy pods
  (`gateway.networking.k8s.io/gateway-name: gateway-default` in
  `openchoreo-control-plane`) to `aep-mcp-server:3400` — the SRE agent's pods
  reach it only through that route, never directly.
- Local dev (`make dev-env`, unless `WITH_SRE=0`) runs the same path:
  `deployments/scripts/setup-sre.sh` runs `aectl sre install --org <org>
  --platform-chart ...`, which creates `sre-agent-aep`, applies the push
  Role, and enables `sreAgent.enabled=true` on the platform release so the
  route, grant and NetworkPolicy above render. There is no separate
  local-only wiring step and no shared static secret to rotate — the token
  aep-api mints is per-org and rotates only if that `org_secrets` row is
  cleared, which re-mints one on the next reconcile pass.
- A full k8s install wires the push target through
  `deployments/helm-charts/platform`: `values.yaml`'s `sreAgent` block
  (`enabled`, default `false`; `org`; `namespace`; `deployment`; `secret`;
  `mcpHostname`), the `SRE_AGENT_*` env vars on the `aep-api` Deployment
  (`templates/aep-api/deployment.yaml`), and the `aep-mcp-server`
  `HTTPRoute`/`ReferenceGrant`/`NetworkPolicy` in
  `templates/aep-mcp-server/`. Off by default; enabling it is what
  `aectl sre install --org <org>` does end to end.

## Automation boundaries

- `ae_create_issue` is the SRE agent's only write.
- The SRE agent does not call a dispatch tool.
- Server-side issue classification decides whether an issue is adoptable.
- `provision` issues and terminal `not_planned` issues stop automated dispatch.
- Repeated recurrence reaches human attention instead of silently looping.

## Human review points

Keep auto-dispatch enabled only where automated code changes are acceptable.
Even then, AE leaves explicit review points:

- low-confidence fixes remain open for a human to verify and close;
- no-code-fix verdicts are visible as `not_planned`;
- fourth-and-later recurrence is escalated in the Console bell and Issues tab.
