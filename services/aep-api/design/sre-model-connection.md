# SRE model connection — org setting, aep-api-pushed

The OpenChoreo SRE (RCA) agent is one Deployment per plane, not dispatched
per-org like the coding agent. It still needs a model to call, and AE decides
which one and pushes it in — the agent never reads AE's database or Console.

## The connection: an org override, or the org's own connection

`SreModelConnectionService` (`internal/organization/sre_model_connection_service.go`)
and its row `OrgSreModelConnection` (`entity_org_sre_model_connection.go`,
table `org_sre_model_connections`) hold an **optional, org-scoped override**:
an OpenAI-compatible endpoint, a Bearer key, and a model, saved from the
Console's **Settings > Credentials > SRE agent model** row under the model
connection card, `/config` section `sreLlm`. It follows the same rules as the
org's main [model connection](../../../docs/decisions/ADR-0038-an-organization-has-one-model-connection.md):
the save probes the host before persisting, a host change needs a fresh key,
and the key lives in `org_secrets` (`sre-model/key`), never in the row.

Resolving what the SRE agent actually runs on
(`organization.ResolveEffectiveSRE`) checks, in order:

1. **The SRE model connection**, if one is saved.
2. **The org's own model connection**, if it has the `SREAgent` capability
   (`modelconn.CapabilitiesOf`; true for any `openai-compatible` connection —
   the agent speaks that format, never Anthropic Messages).
3. **Unconfigured** — no model, agent scaled to zero (below).

`GET /config` projects `sreAgent{enabled, source, model, host, status,
reason}` — `source` names which of the three applied — filled only when a
push target is wired (an org running its own agent; see below). Every other
org sees `sreAgent: null`.

## Delivery: aep-api pushes, the agent never asks

There is one push target, `internal/config.SREAgentConfig` — the org,
namespace, Deployment and Secret name of **the** SRE agent this AE deployment
owns — set by `SRE_AGENT_ORG` / `SRE_AGENT_NAMESPACE` / `SRE_AGENT_DEPLOYMENT`
/ `SRE_AGENT_SECRET`, all four or none, written by `aectl sre install --org`
through the platform Helm chart's `sreAgent.*` values. This mirrors the local
dev-vs-cloud split every other AE credential follows: the Console is the
write path, and getting it into a running pod is a separate, non-live step.

`internal/sreagent.Reconciler` (built over `internal/clients/kubeobs`, the
plain-HTTP in-cluster apiserver client shared with `thunderapp` via
`internal/clients/kubeauth`) owns the push:

- **Ensure** mints a per-org handoff token (`internal/sreagent.Tokens`,
  `org_secrets` key `sre/handoff-token`) once, the same token the [SRE
  handoff](../../../docs/developer-guide/sre-handoff-security.md) verifier
  checks.
- **Desired state** is `ResolveEffectiveSRE`'s connection turned into the
  agent's four env values: `RCA_LLM_API_KEY`, `RCA_MODEL_NAME` (always
  `openai:<model>` — the stock image speaks OpenAI format only),
  `RCA_LLM_BASE_URL`, `AEP_MCP_TOKEN`.
- A hash of that desired state is compared against the pod-template
  annotation `aep.wso2.com/sre-llm-hash` on the Deployment. A mismatch pushes
  the four keys into the AE-owned Secret (`sre-agent-aep` by default — the
  name `aectl sre install` created empty, so a re-run of the installer never
  overwrites what the reconciler already pushed), bumps the annotation
  (forcing a rollout), and reconciles replicas: **0 when unconfigured**
  (nothing to run the agent on), **1 once a connection resolves**.
- Runs a 60-second tick, plus an immediate kick on every SRE model connection
  or org model connection save (`OnChange`), so a Console save reaches the
  cluster without waiting for the next tick.
- `Status(ctx, org)` reports the rollout (`unconfigured | applying | running
  | failed` with a reason) for the **one** org this plane serves — any other
  org reads back `ok=false`, never another org's status. A cluster-read
  failure is returned as an error, unchanged.
- `organization.Service.sreAgentProjection`, called from `Service.Get` for
  `GET /config`, is what degrades that error: it catches it, logs a warning
  (`slog.WarnContext`), and projects `status: failed` with the reason "SRE
  agent status unavailable: cannot read the observability plane" in its
  place. So `GET /config` never fails because of it — settings stay loadable
  when the observability plane's apiserver is unreachable.

aep-api needs a namespaced `Role` in the observability-plane namespace to do
any of this — `aep-api-sre-push` (get/update/patch on the named Secret,
get/patch on the named Deployment and its `/scale` subresource, list on
pods), bound to the `aep-api` ServiceAccount, applied by `aectl sre install`.
RBAC and OpenBao hardening beyond this narrow Role are out of scope here.

## Install-time seed

`aectl sre install --llm-api-key-file/--llm-model` (Task A2) can seed the SRE
model connection at install time, so the agent has a model before anyone
opens the Console. It writes the three values into a Secret named by the
platform chart's `sreAgent.seed.secretName` value in the AE namespace, which
the deployment template wires into `SRE_AGENT_SEED_API_KEY` /
`SRE_AGENT_SEED_MODEL` / `SRE_AGENT_SEED_BASE_URL` (`config.SREAgentSeed`,
all optional `secretKeyRef`s — a Secret missing a key never blocks the pod
from starting).

`organization.SreModelConnectionService.ApplySeed`
(`internal/organization/sre_model_seed.go`) is the one place a seed becomes a
connection. It runs on the reconciler's own tick, ahead of
`ResolveEffectiveSRE`, and applies at most once per distinct seed:

- **Already stored**: a connection already saved (Console or API) always
  wins — the seed is not even probed.
- **Unseen seed**: probed and persisted through the same `Check`/`Persist`
  path a Console save takes (`Persist(ctx, org, "aectl-seed", draft)`), then
  remembered by a sha256 of its three values under the `org_secrets` key
  `sre-model/seed-applied` (`"<hash>:applied"`).
- **Seen seed**: the marker's hash matches — skipped, no re-probe.
- **Refused**: the probe or validation failed. Logged
  (`sre_model.seed_refused`, with the refusal's `SectionError` code) and
  marked `"<hash>:refused"` so it is not retried until the seed's values
  change or a Console/API save succeeds.

A changed seed (a rotated key, a different model) is tried again even though
the org still has no stored connection; disconnecting a Console-saved
connection does not bring an already-tried seed back, since its marker
already reflects that seed's outcome.

## The sqlite report store is a single point of failure

The stock agent's RCA report history lives in sqlite on a `ReadWriteOnce`
PVC, so the Deployment's rollout strategy is `Recreate`: the old pod is
killed before the replacement starts. Every push that changes the hash —
including a bad one, such as a save that resolves to an unreachable host —
tears down the running agent first. There is no second pod to fall back to
while the replacement starts (or fails to), so a bad push is a brief outage
of RCA handling, not a graceful degradation. Nothing in this design adds a
second replica or an HA store; this is a fact to operate around, not a
defect to fix here.
