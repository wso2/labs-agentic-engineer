# SRE-agent → coding-agent handoff

This flow wires an OpenChoreo observability alert into AE's normal issue-driven
coding-agent dispatch path.

```
observability alert
  → OpenChoreo SRE agent RCA/remediation
  → AE MCP: ae_search_related_issues + ae_create_issue
  → AE-owned GitHub issue classification/adoption
  → existing issue-to-coding-agent dispatch
  → PR, build, deploy, and human verification when required
```

AE owns the issue lifecycle after `ae_create_issue`. The SRE agent does not
dispatch the coding agent directly.

## Runtime pieces

- SRE image: `tharindulak/sre-agent:v1.0.1-hotfix.1-anthropic`.
- Extension root in the SRE pod: `/etc/openchoreo/sre-agent`.
- Remediation extension files:
  - `remediation/CONTEXT.md`
  - `remediation/mcp.json`
  - `remediation/skills/coding-agent-handoff/SKILL.md`
- Canonical skill source:
  `services/aep-mcp-server/skills/coding-agent-handoff/SKILL.md`.
- MCP endpoint: `aep-mcp-server` `/mcp`.
- MCP tools exposed to the SRE agent:
  - `ae_search_related_issues`
  - `ae_create_issue`

`ae_create_issue` is the only write the SRE agent makes. The request must carry
`actionStatuses`, ordered to match the RCA report's recommended actions, using
`"revised"`, `"suggested"`, or `null`.

## Credentials

The org's model connection is managed by AE per organization. The Console is
the authoritative write path: saving the connection's key stores it in AE's org
secret store and mirrors it to OpenBao, and aep-api publishes where it put it
as the `model-connection-secrets` SecretReference in the org's OpenChoreo
namespace. The SRE agent has no key of its own; it uses that one.

The SRE image calls Anthropic (`--rca-model`, default
`anthropic:claude-sonnet-4-6`), so this only works when the org's connection
is an Anthropic key. With an `openai-compatible` connection the agent gets a
key its model cannot use. aectl cannot tell the connection's format from the
SecretReference, so it does not check.

The SRE hotfix image consumes the key from a file:

```text
RCA_LLM_API_KEY_FILE=/etc/rca-agent/anthropic/RCA_LLM_API_KEY
```

`aectl sre install` reads the KV path from the org's `model-connection-secrets`
SecretReference and authors an ExternalSecret that projects it into the SRE
agent's namespace:

```text
AE Console model connection key
  -> OpenBao user-app-secrets/<org base namespace>/model-connection-secrets#api-key
     (recorded in SecretReference <org-namespace>/model-connection-secrets)
  -> ExternalSecret openchoreo-observability-plane/rca-agent-anthropic-secret
     (ClusterSecretStore default, refreshInterval 1m)
  -> /etc/rca-agent/anthropic/RCA_LLM_API_KEY
```

The volume is required: until the key is saved, the SRE pod waits in
`ContainerCreating` instead of accepting an alert and failing inside the
analysis. The SecretReference only appears on the first save, so after saving
the key for the first time, re-run `deployments/scripts/setup-sre.sh` (or
`aectl sre install`). Later rotations from the Console need no re-run: ESO
re-reads the same path every minute. The key value must not be placed in the
image, checked into config, or logged.

An org whose key predates the model connection still has an
`anthropic-secrets` reference until aep-api's background rename moves it
(`services/aep-api/internal/organization/model_key_rename.go`). aectl falls
back to that name, and a re-run after the rename picks up the new one.

`--org-namespace` picks the org (default: config `oc.default_org_namespace`,
else `default`).

## Prerequisites

1. A `make dev-env` cluster (or any `aectl platform install`) with the
   observability plane.
2. AEP and the SRE agent share one Thunder (`thunder.openchoreo.localhost:8080`).
3. The AEP org is connected to GitHub, with an Anthropic model connection saved in the
   Console. Both the coding agent and the SRE agent use it.
4. The target project and components were **created through AEP** and
   deployed; the OC project slug equals the AEP project slug.

## Local setup

`make dev-env` installs the observability plane (OpenSearch, Fluent Bit and
the logs adapter) and then the SRE agent on it. To save memory, skip Agent
Manager, which the SRE handoff does not use:

```bash
WITH_AGENT_MANAGER=0 make dev-env
```

| Variable | Default | Effect |
|---|---|---|
| `WITH_OBSERVABILITY` | `1` | `0` skips the observability plane and the SRE agent with it. |
| `WITH_SRE` | `1` | `0` keeps the plane but skips the SRE agent. |
| `WITH_AGENT_MANAGER` | `1` | `0` skips Agent Manager. |

Then save the org's model connection (an Anthropic key) in the Console and run the SRE step once
more, so the agent picks the key up:

```bash
bash deployments/scripts/setup-sre.sh
```

`setup-sre.sh` is idempotent and, in order:

1. generates the handoff bearer at `aep/aep-mcp-token` once and keeps it;
2. runs `aectl platform update --set sreHandoff.enabled=true` against the
   local chart and waits for `aep-api` and `aep-mcp-server` to roll out;
3. applies the `observability-alert-rule` ClusterTrait, which
   `aectl platform install` does not; and
4. runs `aectl sre install` (next section).

## Kubernetes setup with aectl

After `aectl platform install`, install the SRE integration:

```bash
cd tools/aectl
go run . sre install
```

The command picks its mode from the cluster:

- **A plane is installed** (`setup-env-for-aectl.sh` installs chart 1.2.5): it
  upgrades that release at its own chart version with `--reuse-values`,
  setting only the `rca` block: `rca.enabled=true` and the SRE image
  (`--rca-image-repo`/`--rca-image-tag`, default
  `tharindulak/sre-agent:v1.0.1-hotfix.1-anthropic`). The plane's
  OpenSearch secret, logs chart, Fluent Bit and `ClusterObservabilityPlane`
  stay with whoever installed them. It warns when no `fluent-bit` DaemonSet
  exists, since log alerts then never fire.

  It does take over two observer settings the SRE agent's queries depend on,
  both because ThunderID 1.0 identifies a service account by `client_id`
  rather than `sub`: the observer's service-account claim
  (`observer.security.subjectTypes`), and the observer's Thunder client secret
  (`observer-secret`), which it points at the one `aectl platform install`
  registers. With either left as the plane installer set it, the agent's log
  queries come back empty and its RCA has no evidence to hand off.
- **No plane is installed**: it installs the plane and logs charts itself at
  `--obs-plane-version` (default `1.0.1-hotfix.1`) and `--obs-logs-version`,
  with their secrets, route and `ClusterObservabilityPlane`.

In both modes its authz grants (`rca-agent-dispatch`, `aep-observer-reader`)
are keyed on `claim: client_id` for the same reason, and it finds the SRE agent
Deployment by label (`sre-agent` from
chart 1.2.0, `ai-rca-agent` before) and reconciles the model connection key
ExternalSecret, the SRE extension ConfigMap, and the deployment mounts. It reads the extension
assets from an AE repository checkout: the one containing the working
directory, or the one passed as `--assets-root <checkout>`. It resolves them
before changing the cluster. It
renders the MCP URL below into `remediation/mcp.json` in the
`sre-agent-extensions` ConfigMap (the extension loader validates that URL
before it expands env vars), and patches the SRE deployment with:

- `EXTENSIONS_DIR=/etc/openchoreo/sre-agent`
- `RCA_LLM_API_KEY_FILE=/etc/rca-agent/anthropic/RCA_LLM_API_KEY`
- `AEP_MCP_URL=http://aep-mcp-server.<aep-namespace>.svc.cluster.local:3400/mcp`

The SRE pod gets no MCP credential. Authentication comes from the platform
chart: install it with `sreHandoff.enabled=true` (after seeding
`aep/aep-mcp-token` in OpenBao) so `aep-mcp-server` applies the shared handoff
bearer and only the SRE agent pods in the observability namespace
(`sreHandoff.callerNamespace` and `sreHandoff.callerPodLabels`) can reach it.
See `sre-handoff-security.md`.

Focused check:

```bash
cd tools/aectl
go test ./cmd -run 'SRE|Extensions'
```

## Issue outcomes

AE classifies and acts on the issue server-side:

- `code_level` / `mixed`: AE adopts the issue into the normal task funnel and
  dispatches the coding agent when the issue is armed.
- `config_level` / `none`: AE records the issue without dispatching code work.
- `provision` kind: acts as a dispatch brake.
- Low-confidence coding-agent result: the issue remains open/disarmed and the
  Console surfaces `unverified_fix` for human review.
- `not_planned`: the coding agent closes the issue when no code fix is possible
  or warranted; recurrence stops for that signature and the Console surfaces
  `no_change_verdict`.
- Recurrence attempt 4 or later: AE reopens/updates the issue and surfaces
  `escalated` for loud human attention.

Related incident alerts are deduplicated by the server-owned incident key and
the observability alert suppression window. Search results are context only;
the create response decides dedupe, suppression, recurrence, adoption, and
dispatch.

## Console surfaces

- Alert detail shows the SRE stage progression and any linked GitHub issue.
- Project → Issues lists the server-provided issue state, labels, URL, and
  attention reason.
- The notification bell includes SRE attention items for alert-linked issues
  with `unverified_fix`, `no_change_verdict`, or `escalated`.

## Troubleshooting findings

### `ae_create_issue` was called, but no GitHub issue appeared

History: on the local cluster on 2026-09-19 the SRE remediation agent called
`ae_create_issue`, but `aep-api` had no way to accept the forwarded MCP bearer
on the issue routes, so it rejected the request as a malformed Thunder JWT.
That gap is closed: `aep-api` now verifies the forwarded bearer with the scoped
SRE handoff verifier described in
[sre-handoff-security.md](sre-handoff-security.md). It accepts the bearer only
on `GET`/`POST /api/v1/projects/{projectName}/issues`, binds the configured org
and the server-owned incident context, and leaves every other route on Thunder
JWT verification.

If the same symptom appears now, check in this order:

1. `aep-api` logs `JWT validation failed: token is malformed` for the issue
   route. The handoff verifier is disabled or the bearer does not match, so the
   request fell through to Thunder JWT verification. Confirm `aep-api` has both
   `SRE_HANDOFF_TOKEN` and `SRE_HANDOFF_ORG` set (the verifier is off when
   either is empty; on Kubernetes, `sreHandoff.enabled` is `false` by default),
   and that `SRE_HANDOFF_TOKEN` holds the same value as `aep-mcp-server`'s
   `AEP_MCP_TOKEN`. Compare the values without printing them.
2. The create returns `400` with `trusted incident identity and component are
   required`. The request authenticated as a normal user JWT instead of the
   handoff bearer, or the SRE agent sent no component name.
3. The create returns `409`. The component's incident identity matches only
   closed issues whose closure reason cannot recur (for example `duplicate`).
   AE files nothing until a human reopens the matching issue or closes it as
   `completed` or `not_planned`.
4. The create returns `200` with `deduped` or `suppressed`. This is expected:
   an open issue already tracks the incident, or a human closed it as
   `not_planned`. See [Issue outcomes](#issue-outcomes).

### Alert rule is ready, but SRE never runs

Check that the observability workloads are running:

```bash
kubectl -n openchoreo-observability-plane get deploy,sts,ds
```

If `opensearch-master`, `logs-adapter-opensearch`, `fluent-bit`, or the SRE
agent is not ready, alerts are not evaluated and no RCA request reaches the
SRE agent.

If the SRE pod sits in `ContainerCreating` with a missing
`rca-agent-anthropic-secret`, no org key has been projected yet. Save the
org's model connection in the Console, then re-run the SRE step:

```bash
kubectl -n default get secretreference model-connection-secrets   # appears on the first save
bash deployments/scripts/setup-sre.sh
```

If SRE receives the RCA request but fails with
`Anthropic authentication failed`, check that the ExternalSecret synced from
the Console key's path:

```bash
kubectl -n openchoreo-observability-plane get externalsecret rca-agent-anthropic-secret
```
