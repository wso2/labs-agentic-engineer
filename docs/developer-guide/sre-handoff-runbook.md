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

- SRE image: the **stock, unmodified** upstream image, pinned by digest —
  `ghcr.io/openchoreo/sre-agent:v1.3.0@sha256:25e5e6c423d049460f4a60d95497747c2a99b02713faf8523a38ef8e6a85c599`
  (`tools/aectl/cmd/sre.go`). No AE-forked patch.
- Extension root in the SRE pod: `/opt/aep/sre-agent-extensions`
  (`EXTENSIONS_DIR`).
- Remediation extension files:
  - `remediation/CONTEXT.md`
  - `remediation/mcp.json`
  - `remediation/skills/coding-agent-handoff/SKILL.md`
- Canonical skill source:
  `services/aep-mcp-server/skills/coding-agent-handoff/SKILL.md`.
- MCP endpoint: `aep-mcp-server` `/mcp`, reached over **https** through the
  OpenChoreo control-plane gateway (`AEP_MCP_URL`,
  `https://<sreAgent.mcpHostname>[:port]/mcp`).
- MCP tools exposed to the SRE agent:
  - `ae_search_related_issues`
  - `ae_create_issue`

`ae_create_issue` is the only write the SRE agent makes. The request must carry
`actionStatuses`, ordered to match the RCA report's recommended actions, using
`"revised"`, `"suggested"`, or `null`.

## Credentials

The stock agent speaks **OpenAI-compatible chat completions only** — it
cannot call Anthropic's Messages API. An org's model connection may be
Anthropic-format, so AE resolves what the agent runs on rather than always
reusing the org's default connection:

1. The org's **SRE model connection** (Console: Settings > Credentials >
   **SRE agent model**, under the model connection card) — OpenAI-compatible,
   Bearer, its own key — if saved.
2. Else the org's own model connection, if it carries the `SREAgent`
   capability (any `openai-compatible` connection).
3. Else the agent has no model, and aep-api scales its Deployment to 0.

See
[`services/aep-api/design/sre-model-connection.md`](../../services/aep-api/design/sre-model-connection.md)
for the full resolution and push mechanics. There is no console-live path to
the running pod: aep-api's reconciler pushes the resolved connection's model,
key, base URL and a minted MCP token into the AE-owned Secret
`sre-agent-aep` in the observability-plane namespace, on a save, and on a
60-second tick. `aectl sre install --org <org>` is what wires *which* org's
connection a given plane serves (`SRE_AGENT_ORG` / `SRE_AGENT_NAMESPACE` /
`SRE_AGENT_DEPLOYMENT` / `SRE_AGENT_SECRET` on aep-api, set via the platform
chart's `sreAgent.*` values) — it does not take a key on the command line;
the key always comes from the Console save.

The key value must not be placed in the image, checked into config, or
logged.

## Prerequisites

1. A `make dev-env` cluster (or any `aectl platform install`) with OC ≥ 1.3.0
   and the observability plane.
2. AEP and the SRE agent share one Thunder (`thunder.openchoreo.localhost:8080`).
3. The AEP org is connected to GitHub. For the SRE agent to have a model, it
   needs either its own SRE model connection or an OpenAI-compatible org
   model connection (see Credentials above); the coding agent uses the org's
   main model connection regardless of format.
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

Then save a model for the SRE agent (an OpenAI-compatible SRE model
connection, or an OpenAI-compatible org model connection) in the Console. AE
pushes it in on its own reconcile tick; to force it immediately, re-run the
SRE step:

```bash
bash deployments/scripts/setup-sre.sh
```

`setup-sre.sh` is idempotent and, in order:

1. applies the `observability-alert-rule` ClusterTrait, which
   `aectl platform install` does not; and
2. runs `aectl sre install --org <org> ...` (next section).

## Kubernetes setup with aectl

After `aectl platform install`, install the SRE integration:

```bash
cd tools/aectl
go run . sre install --org <org> --platform-chart deployments/helm-charts/platform
```

`--org` is required — it is the org whose resolved SRE model connection this
plane's agent runs on, and doubles as the SRE handoff's trusted org.
`--platform-chart` (or `--platform-version`) is also required — it pins the
platform-chart release this command upgrades in-process (`sreAgent.*`
values), so a re-run can never silently drift the platform release to
whatever is latest.

The command requires OC ≥ 1.3.0 (`--skip-oc-version-check` to bypass) and
picks its plane mode from the cluster:

- **A plane is installed**: it upgrades that release at its own chart
  version with `--reuse-values`, setting the `rca` block (`rca.enabled=true`,
  the stock image, `rca.extraEnvs`). It warns when no `fluent-bit` DaemonSet
  exists, since log alerts then never fire.
- **No plane is installed**: it installs the plane and logs charts itself at
  `--obs-plane-version` (default `1.3.0`) and `--obs-logs-version`, with
  their secrets, route and `ClusterObservabilityPlane`.

See
[`deployments/helm-charts/design/sre-agent-install.md`](../../deployments/helm-charts/design/sre-agent-install.md)
for the full step-by-step (AE-owned Secret, push Role, Helm post-renderer,
CA bundle, https route).

Focused check:

```bash
cd tools/aectl
go test ./cmd -run 'SRE|Extensions'
```

## Seed the SRE model at install

`aectl sre install` can seed the org's SRE model connection at install time,
so the agent has a model before anyone opens the Console. Write the key to a
`0600` file, pass it and the model to the install command, and delete the
file afterwards — the key is only ever read from a file, never taken as a
flag value or logged.

```bash
umask 077
echo -n "$OPEN_API_KEY" > /tmp/sre-llm-key

cd tools/aectl
go run . sre install --org <org> --platform-chart deployments/helm-charts/platform \
    --llm-api-key-file /tmp/sre-llm-key --llm-model gpt-5.4

rm /tmp/sre-llm-key
```

`make dev-env` forwards the same seed through env vars, so `setup-sre.sh`
passes it to `aectl sre install` on your behalf:

```bash
SRE_LLM_API_KEY_FILE=/tmp/sre-llm-key SRE_LLM_MODEL=gpt-5.4 WITH_AGENT_MANAGER=0 make dev-env
```

`--llm-base-url` (`SRE_LLM_BASE_URL`) defaults to `https://api.openai.com/v1`
and rarely needs setting.

Verify without ever printing the key itself:

```bash
kubectl -n wso2-aep get secret sre-model-seed -o jsonpath='{.data.apiKey}' | base64 -d | wc -c
kubectl -n wso2-aep get secret sre-model-seed -o jsonpath='{.data.model}' | base64 -d; echo
kubectl -n openchoreo-observability-plane get secret sre-agent-aep -o jsonpath='{.data.RCA_LLM_API_KEY}' | base64 -d | wc -c
```

Rules the seed follows (`organization.SreModelConnectionService.ApplySeed`,
[`sre-model-connection.md`](../../services/aep-api/design/sre-model-connection.md)):

- **A Console/API save always wins.** The seed is never applied, and never
  even probed, once the org has a stored SRE model connection.
- **A given seed is applied at most once**, tracked by a hash of its three
  values in `org_secrets`. Re-running `aectl sre install` with the same
  `--llm-*` values is a no-op; a changed value (a new model, a rotated key)
  is tried again. Re-running with a changed key or model also rolls aep-api
  (a pod-template annotation stamped with the seed's hash), so the new seed
  is actually read — still applied only if the org has no stored SRE model
  connection yet.
- **A console removal is not re-seeded.** Disconnecting the SRE model
  connection in the Console does not bring the old seed back — the marker
  for that seed's hash still says it was already tried.
- **A refused seed is logged once and not retried.** A validation or probe
  failure is logged as `sre_model.seed_refused` and marked tried; nothing
  retries it until the seed's values change or a Console/API save succeeds.
- **Rotating the key is a Console/API concern**, same as the org's main
  model connection — re-running `aectl sre install` with a new key only
  takes effect when the org still has no stored connection.

The key lives in the Secret `sre-model-seed` in `wso2-aep` until you delete
it. That is safe once the seed has applied (`kubectl -n wso2-aep get secret
sre-model-seed` no longer being read by anything on the next reconcile):

```bash
kubectl -n wso2-aep delete secret sre-model-seed
```

**Without aectl**, save the SRE model connection directly through the same
probe/persist path a Console save takes: `PATCH /config` with a `sreLlm`
body, through `http://console.ae.localhost:8080/aep-api-service/api/v1/config`
with a signed-in user's token. This is a save, not a seed — it always wins
over, and is never touched by, the install-time seed above.

## Migration from a pre-1.3.0 install

Earlier revisions of this plane ran an AE-patched image
(`tharindulak/sre-agent:v1.0.1-hotfix.1-anthropic`) with a static handoff
bearer and an Anthropic-only key file. Moving to the stock v1.3.0 agent is a
two-step upgrade, not an in-place config change:

1. **Upgrade OpenChoreo to 1.3.0 first.** The stock agent's https handoff
   route needs the control-plane gateway's `https` listener, which OC ≥
   1.3.0 provides; `aectl` itself now refuses to install against an older
   control plane (`minOCVersion`).
2. **Re-run `aectl sre install --org <org> --platform-chart ...`.** This
   replaces the patched image with the stock one, wires the AE-owned Secret
   and push Role, mounts the extensions and CA bundle through the post-
   renderer, and switches the handoff to the minted per-org token over
   https.

What this changes, and what it does not carry over:

- **The patched agent is replaced outright** — there is no side-by-side
  running of both images.
- **The sqlite RCA report history on the old pod's volume is not
  migrated.** AE's own GitHub issues (filed by `ae_create_issue`) are the
  durable record of what the agent found; nothing reads the old sqlite file
  after the cutover.
- **The old `AE_*` keys vanish from `rca-agent-config`** on the Helm
  upgrade — they were part of the patch that added them, and that patch no
  longer applies. The stock agent's model, key, base URL and MCP endpoint
  now arrive as plain env vars from `sre-agent-aep`, not a ConfigMap patch.
- The static `SRE_HANDOFF_TOKEN`/`SRE_HANDOFF_ORG` env, the
  `AEP_MCP_DEFAULT_BEARER` shared secret, and the chart's old `sreHandoff.*`
  values block are gone; the handoff bearer is now the per-org token
  aep-api mints and aep-mcp-server forwards unchanged (see
  [`sre-handoff-security.md`](sre-handoff-security.md)).

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
- Settings > Credentials shows the **SRE agent model** row under the model
  connection card: inherited from the org connection, overridden by a saved
  SRE model connection, or unavailable (naming why), plus a status chip
  (`Applying…` / `Running` / `Failed: <reason>` / `Not running`) — visible
  only to the org this plane's agent actually serves.

## Troubleshooting findings

### `ae_create_issue` was called, but no GitHub issue appeared

`aep-api` verifies the forwarded bearer with the scoped SRE handoff verifier
described in [sre-handoff-security.md](sre-handoff-security.md). It accepts
the bearer only on `GET`/`POST /api/v1/projects/{projectName}/issues`, binds
the configured org and the server-owned incident context, and leaves every
other route on Thunder JWT verification.

If the symptom appears, check in this order:

1. `aep-api` logs `JWT validation failed: token is malformed` for the issue
   route. The handoff verifier is disabled or the bearer does not match, so
   the request fell through to Thunder JWT verification. Confirm `aep-api`
   has all four `SRE_AGENT_ORG` / `SRE_AGENT_NAMESPACE` /
   `SRE_AGENT_DEPLOYMENT` / `SRE_AGENT_SECRET` set (the push, and with it the
   handoff verifier, is off unless every one is; on Kubernetes,
   `sreAgent.enabled` is `false` by default) — the handoff verifier checks
   the bearer against the per-org token aep-api itself minted and stored,
   not a static shared secret, so there is nothing to compare by hand.
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

If the SRE agent's replicas are `0`, aep-api has resolved no model for it
(see [Credentials](#credentials)): save an SRE model connection or an
OpenAI-compatible org model connection in the Console, wait for the next
60-second reconcile tick (or re-run `bash deployments/scripts/setup-sre.sh`
to force it), and check:

```bash
kubectl -n openchoreo-observability-plane get secret sre-agent-aep -o jsonpath='{.data.RCA_MODEL_NAME}' | base64 -d
kubectl -n openchoreo-observability-plane get deploy sre-agent -o jsonpath='{.spec.replicas}'
```

If SRE receives the RCA request but fails to authenticate to its model
provider, check the pushed Secret against what the Console shows for the
resolved connection (`GET /config`'s `sreAgent.host`/`sreAgent.model`) —
they should match; if not, the reconciler has not yet converged, or the
connection's host changed without a fresh key.
