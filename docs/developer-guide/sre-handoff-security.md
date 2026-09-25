# SRE handoff security notes

The SRE handoff is intentionally narrow: the OpenChoreo SRE agent can search
related AE issues and create exactly one issue. AE owns classification,
deduplication, recurrence, adoption, dispatch, and human-attention state.

## Secret boundaries

- Anthropic keys are organization credentials managed through AE Console.
- Runtime projection uses secret references and a file mount; the SRE image
  reads `RCA_LLM_API_KEY_FILE`.
- The key value must not be serialized into Helm values, ConfigMaps, logs, MCP
  arguments, or documentation examples.
- The MCP bearer token is separate from the Anthropic key and is used only to
  let `aep-mcp-server` forward the caller identity to `aep-api`.
- That bearer is a dedicated long-lived secret (`AEP_MCP_TOKEN` /
  `AEP_MCP_DEFAULT_BEARER` on `aep-mcp-server`, `SRE_HANDOFF_TOKEN` on
  `aep-api`), not a Thunder-issued JWT: the OpenChoreo SRE agent's generic
  extensions loader resolves an MCP server's `headers` from `${VAR}` once at
  process start and never refreshes them, so a short-lived Thunder token
  would expire mid pod-lifetime. `aep-api` verifies it with a narrow,
  disabled-by-default checker (`auth.SREHandoffVerifier`) scoped to exactly
  `create_issue`/`search_related_issues` and bound to one configured org
  (`SRE_HANDOFF_ORG`) — it never widens what the normal Thunder JWT verifier
  accepts, and any other route still requires a real JWT.
- The SRE agent never holds this bearer. Its `remediation/mcp.json` has no
  `headers` entry because the extension loader will not send credentials to
  a plaintext URL. `aep-mcp-server` applies it instead, as the fallback
  `AEP_MCP_DEFAULT_BEARER` for requests without their own `Authorization`
  header. The fallback is off unless configured; a blank value or a bare
  `Bearer` scheme also leaves it off, and a malformed value stops the server
  at startup.
- While the fallback is on, any caller that reaches `aep-mcp-server` acts
  with the handoff's rights (list/create issues in `SRE_HANDOFF_ORG`), so
  each deployment limits who can reach the port.
- Local dev wires it in `deployments/docker-compose.yml` from one
  `AEP_MCP_TOKEN` variable, which `setup-aep.sh` generates at random into
  `deployments/.env` and preserves across re-runs. There is no built-in
  default: unset or empty disables the shortcut on both services. An
  explicit empty `AEP_MCP_TOKEN=` survives re-runs, so a disabled shortcut
  stays disabled; deleting the line and re-running `setup-aep.sh` generates
  a new token (rotation). The compose port (`3401`) is published so the k3d
  SRE pod can reach it through `host.k3d.internal`, which also makes it
  reachable from the host's network; keep the local stack on a trusted
  network, or set `AEP_MCP_TOKEN=` when the handoff is not needed.
- A full k8s install wires it through `deployments/helm-charts/platform`:
  `values.yaml`'s `sreHandoff` block (`enabled`, default `false`; `org`;
  `callerNamespace`; `callerPodLabels`), an `ExternalSecret` in
  `templates/external-secrets/external-secrets.yaml` that reads the
  `aep/aep-mcp-token` OpenBao path into `aep-sre-handoff-secrets`, the
  `SRE_HANDOFF_TOKEN`/`SRE_HANDOFF_ORG` env vars in
  `templates/aep-api/deployment.yaml`, and `AEP_MCP_DEFAULT_BEARER` in
  `templates/aep-mcp-server/deployment.yaml` (one shared credential, not a
  second secret to keep in sync). `templates/aep-mcp-server/networkpolicy.yaml`
  then admits only pods matching `callerPodLabels` in `callerNamespace` (the
  SRE agent's pods and namespace) to `aep-mcp-server`; this needs a CNI that
  enforces NetworkPolicy. The `callerPodLabels` default is the SRE agent
  Deployment's selector labels in the observability-plane chart `aectl`
  installs; a different chart version may need a different component label.
  Off by default. Enabling it requires a random value at that OpenBao path, for
  example via `aectl platform secret import --path aep/aep-mcp-token`.

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
