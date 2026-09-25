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

Anthropic credentials are managed by AE per organization. The Console is the
authoritative write path: it saves the org's default Anthropic key through AE's
organization credential flow; AE stores the value in its org secret store and
mirrors the secret-ref metadata for runtime projection. The SRE agent must not
have a separate hand-entered Anthropic key.

The SRE hotfix image consumes the key from a file:

```text
RCA_LLM_API_KEY_FILE=/etc/rca-agent/anthropic/RCA_LLM_API_KEY
```

Local setup and `aectl sre install` both mount the `rca-agent-anthropic-secret`
Kubernetes secret at `/etc/rca-agent/anthropic`. Local setup makes that volume
required when `AE_HANDOFF=true` and optional otherwise; `aectl sre install`
waits for the secret to sync before it wires the handoff. The key value must
not be placed in the image, checked into config, or logged.

For local Docker Compose + k3d development, `scripts/start.sh` runs credential
convergence before it checks or restarts `sre-agent`. That convergence installs
an ExternalSecret which watches the AE-stored default org Anthropic key and
projects it into the SRE agent secret:

```text
AE Console org key
  -> AE org secret store / OpenBao
  -> ExternalSecret openchoreo-observability-plane/rca-agent-anthropic-secret
  -> /etc/rca-agent/anthropic/RCA_LLM_API_KEY
```

If you rotate or reconnect the Anthropic key from the AE Console after the
stack is already running, no SRE-specific key entry is needed. External Secrets
Operator refreshes `rca-agent-anthropic-secret` from the AE-managed OpenBao
path. To repair the watcher itself, run:

```bash
cd deployments
bash scripts/reconcile-sre-anthropic-externalsecret.sh
```

The reconcile script is local-only and refuses non-`k3d-openchoreo` contexts.
It does not ask for or store a separate SRE key; it only points the SRE
Kubernetes Secret at AE's default org Anthropic credential.

`setup-observability.sh` also attempts the same ExternalSecret reconcile after wiring the
`RCA_LLM_API_KEY_FILE` volume. With handoff enabled, the projected secret is a
required SRE pod dependency rather than an optional fallback: a pod that cannot
mount the AE-managed key should wait instead of accepting an alert and failing
inside the RCA analysis.

## Local setup

Use the scripted local install:

```bash
cd deployments
bash scripts/setup-observability.sh
```

The script:

1. installs/reconciles the observability plane;
2. uses the hotfix SRE image by default;
3. wires alert suppression (`ALERT_SUPPRESSION_WINDOW=1h`);
4. mounts the AE-owned remediation extension into the SRE pod;
5. wires `RCA_LLM_API_KEY_FILE` to the Anthropic secret file, whose volume is
   required when `AE_HANDOFF=true` (the default), so the SRE pod waits for the
   projected secret, and optional when `AE_HANDOFF=false`; and
6. keeps the MCP bearer token in runtime configuration, not in the image.

Fast local assertions:

```bash
bash deployments/scripts/setup-observability_test.sh
docker compose -f deployments/docker-compose.yml config >/dev/null
```

`setup-observability.sh` has an active-pipeline postcondition: it restores the
observability workloads with `park-observability.sh up` before it exits. That is
intentional. A parked OpenSearch, Fluent Bit, logs adapter, or SRE agent means
no log alert is evaluated and no RCA handoff can run, even though the Helm
releases and CRDs still exist.

The full `scripts/setup.sh` still supports the memory-saving parked profile,
but it no longer applies that profile to AEP-only/SRE setups by default. When
`ENABLE_AGENT_MANAGER=0`, setup keeps observability running so a freshly set up
cluster can exercise the alert → RCA → coding-agent path immediately. Override
the default explicitly when needed:

```bash
# Keep OpenSearch/logs-adapter/Fluent Bit/SRE agent running after full setup.
ENABLE_AGENT_MANAGER=0 PARK_OBSERVABILITY_AFTER_SETUP=0 bash deployments/scripts/setup.sh

# Save local memory; disables alert evaluation and SRE handoff until restored.
PARK_OBSERVABILITY_AFTER_SETUP=1 bash deployments/scripts/setup.sh

# Restore an already parked observability plane without reinstalling.
bash deployments/scripts/park-observability.sh up
```

## Kubernetes setup with aectl

After `aectl init`, install the SRE integration:

```bash
cd tools/aectl
go run . sre install
```

The command reconciles the observability namespace, ExternalSecrets, charts,
SRE extension ConfigMap, and SRE deployment mounts. It reads the extension
assets from an AE repository checkout: the one containing the working
directory, or the one passed as `--assets-root <checkout>`. It resolves them
before changing the cluster. It uses the same extension layout as local setup,
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

First check whether the observability plane is parked:

```bash
bash deployments/scripts/park-observability.sh status
```

If `opensearch-master`, `logs-adapter-opensearch`, `fluent-bit`, or `sre-agent`
is parked, alerts will not be evaluated and no RCA request will reach the SRE
agent. Restore them with:

```bash
bash deployments/scripts/park-observability.sh up
```

If SRE receives the RCA request but immediately fails with
`Anthropic authentication failed`, check the projected key secret:

```bash
kubectl -n openchoreo-observability-plane get secret rca-agent-anthropic-secret
bash deployments/scripts/reconcile-sre-anthropic-externalsecret.sh
```

The fixed setup path runs that reconcile automatically when local AE is available.
