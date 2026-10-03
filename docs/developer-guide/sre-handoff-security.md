# SRE handoff security notes

The SRE handoff is intentionally narrow: the OpenChoreo SRE agent can search
related AE issues and create exactly one issue. AE owns classification,
deduplication, recurrence, adoption, dispatch, and human-attention state.

## Secret boundaries

- The SRE agent's model key is set at install: `aectl sre install` reads it
  from `--llm-api-key-file`, checks it against the provider, and writes it
  into the agent's Secret `sre-agent-aep` in the observability-plane
  namespace (`RCA_LLM_API_KEY`, `RCA_MODEL_NAME`, `RCA_LLM_BASE_URL`). aep-api
  never holds it. See
  [`services/aep-api/design/sre-handoff.md`](../../services/aep-api/design/sre-handoff.md).
- The key value must not be serialized into Helm values, ConfigMaps, logs,
  MCP arguments, or documentation examples. `aectl` reads it only from a
  file, never from a flag value, and sends it only to the https base URL
  named, without following redirects.
- The handoff key (`AEP_MCP_TOKEN`) is separate from the model key. `aectl`
  generates it (32 random bytes) and writes it into `sre-agent-aep` and into
  aep-api's Secret `sre-handoff`; a re-run reuses it, and
  `--rotate-handoff-token` replaces it in both and restarts both sides.
- It is a long-lived key, not a Thunder-issued JWT: the OpenChoreo SRE
  agent's extensions loader resolves an MCP server's `headers` from `${VAR}`
  once at process start and never refreshes them, so a short-lived token
  would expire mid pod-lifetime. The agent's `remediation/mcp.json` sends it
  as `Authorization: Bearer ${AEP_MCP_TOKEN}`.
- aep-api checks it with `auth.SREHandoffVerifier` (a constant-time compare)
  on exactly one mount, `POST /internal/v1/sre-handoff/mcp`, which serves only
  `search_related_issues` and `create_issue`. It never widens what the Thunder
  JWT verifier accepts, and the public issue operations refuse the fields only
  the handoff may send. Without `SRE_HANDOFF_TOKEN` the mount does not exist.
- The key authenticates the agent, not an org. One agent serves every org on
  its plane, so each call names its org, the alert's OpenChoreo `namespace`.
  That value reaches aep-api through a model that reads pod logs, so it is
  never trusted as given: before either tool reads or writes, aep-api asks the
  observer, with its own service token, whether an alert fired in the last
  hour for that namespace, project and component. No alert, or an
  observer that cannot answer, and nothing is read or filed. The bound this
  leaves: a prompt-injected agent can at most act on another component that
  really alerted within the window.
- The mount is reached from the agent only over **https**, through the
  platform chart's `aep-api-sre-handoff` `HTTPRoute` on the OpenChoreo
  control-plane gateway's `https` listener, matching that one path. The
  extension loader sends `headers` only to https URLs, which is why a
  plaintext route cannot carry the bearer. Inside the cluster aep-api's
  Service is not restricted by a `NetworkPolicy` (a platform-wide gap), so
  the key is the boundary there.
- Local dev (`make dev-env`, unless `WITH_SRE=0`) runs the same path:
  `deployments/scripts/setup-sre.sh` runs `aectl sre install
  --platform-chart ...`, which writes both Secrets and sets
  `sreAgent.enabled=true` on the platform release.

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
