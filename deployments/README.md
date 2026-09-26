# AEP — v1 local setup (pure OpenChoreo)

A lighter alternative to `deployments-v2/` (which uses WSO2 Cloud's Flux/kustomize
layered model). v1 runs the same code, with OpenChoreo + Thunder + OpenBao + ESO
+ kgateway installed via direct `helm install`s (no Flux).

The Docker Compose local-dev path (host-container services + this repo's own
`setup.sh`/`setup-aep.sh`/Agent Manager bootstrap chain) has been removed. Local
dev now runs entirely in-cluster, installed by the `aectl` CLI (`tools/aectl`) —
the same install path a real user follows, not a repo-specific shortcut.

## Local dev — aectl + k3d (in-cluster)

Cluster bring-up (`scripts/setup-env-for-aectl.sh`) installs a plain upstream
OpenChoreo + ThunderID cluster, then hands off to the `aectl` CLI.

```bash
# 1. One-shot bring-up — cluster + platform, via aectl (idempotent)
make dev-env
# Console: http://console.openchoreo.localhost:8080
# aep-api: kubectl -n wso2-aep port-forward svc/aep-api 9090:9090

# 2. Edit source, then run this to rebuild + redeploy just the changed image(s)
make dev-update
```

`make dev-env` builds `tools/aectl` as `aectl-skaffold` (git-ignored — this
flow's own copy of the binary), installs a bare OpenChoreo + ThunderID cluster
with `WITH_SKAFFOLD_CLIENT=1` (bootstraps `aectl`'s own Thunder admin client,
`ae-install-client` — see that script's step 3c for why it can't register
itself), then runs `aectl-skaffold platform config import` (against
`skaffold/defaults.yaml`) and `aectl-skaffold platform install --addons=all
--platform-version=latest --platform-chart deployments/helm-charts/platform`.

`make dev-update` (`skaffold run`, `skaffold.yaml`) is one-shot, not a watch
loop — run it again after every edit you want reflected in the cluster. It
only rebuilds images whose dependencies changed and re-points the
already-installed `aep-platform` release at them; it does not re-derive any of
aectl's own settings (Thunder/OpenBao/webhook URLs), which is why its Helm
step passes `--reuse-values`.

Coding-agent runs as an ephemeral OpenChoreo Job Component in the project's
dataplane (image from `AGENT_RUNNER_IMAGE`); builds use the `dockerfile-builder`
ClusterWorkflow, whose `generate-workload-cr` step exchanges OAuth tokens at
Thunder via the `openchoreo-workload-publisher-client` `aectl` registers.

That ClusterWorkflow and the four Argo ClusterWorkflowTemplates it chains
(`checkout-source`, `containerfile-build`, `publish-image`, `generate-workload`)
are **OpenChoreo's**, applied by its own samples — `all.yaml` in Step 4 and the
workflow-plane templates in Step 6 of `scripts/setup-env-for-aectl.sh`. AEP
neither ships nor forks them, deliberately: a product on a shared cluster either
uses OpenChoreo's build templates as they are or ships its own under a prefixed
name. A repo-local `aep-*` copy existed until it was removed; it had drifted to
an older, less hardened vintage of the same upstream files, and its only
substantive deviation — pushing to the registry Service in-cluster rather than
`host.k3d.internal` — dated from the Docker Compose era.

WSO2 Agent Manager's platform-resources chart renders four of those template
names unprefixed, so installing it takes OpenChoreo's build path over for
everything on the cluster. That is a naming defect in that chart (it already
prefixes a fifth as `amp-generate-workload`), to be fixed there rather than
worked around here.

- **One AI gateway per environment, alongside the API gateway.** Agent Manager
  has two: the **API gateway** fronts an agent's own inbound API, the **AI
  gateway** is the LLM proxy an agent calls outbound, and guardrails run on the
  second. `scripts/setup-environment-aigateway.sh` registers it with `amp-api`,
  installs `wso2-amp-ai-gateway-extension` into `<org>-<env>` with
  `bootstrap.enabled=false`, and writes the binding onto the OpenChoreo
  `Environment` as annotations — endpoint, internal endpoint, admin URL,
  gateway id, secret path. **The binding record is the contract**, exactly as
  it is for Thunder (`design/two-tier-thunder.md`): `aep-api` reads it to
  decide whether an environment is governed at all, and an environment without
  one deploys agents on the org's own Anthropic key, as before Agent Manager
  existed. `scripts/verify-convergence.sh` check 15 asserts it is bound and
  ACTIVE. What aep-api then does with it is
  `services/aep-api/internal/delivery/agentgovernance/design/governed-model-access.md`.

## What was removed from the previous v1

- The Docker Compose local-dev path in its entirety: `scripts/setup.sh` and
  every script only it used (k3d/prereqs/OpenChoreo/Thunder bring-up, Agent
  Manager setup, environment-Thunder/gateway lifecycle, seed/verify/teardown
  helpers), `docker-compose.yml`, `scripts/start.sh`/`stop.sh`, and the
  Skaffold-flow's old per-developer chart values
  (`values.local.yaml`/`values.local.dev.yaml.example`). See git history for
  that chain if you need to recover something from it.
- `collab-server` — collaborative editing is deferred.
- Long-lived `remote-worker` container — coding agent is now an ephemeral
  OpenChoreo Job Component (`AGENT_RUNNER_IMAGE`), not a ClusterWorkflow.

## Orphaned-but-kept: `single-cluster/` and `manifests/`

Both directories predate the Compose-chain removal and are **not applied by
any script in this repo anymore** — nothing here currently wires them into
either local-dev path. They're kept anyway because parts of them are load-bearing
elsewhere and were deliberately NOT deleted along with the Compose chain:

- `single-cluster/resource-types/thunder-app/operator/` — a real Go module
  (`go.work` member), built and released as its own product image by
  `.github/workflows/images.yml` / `release.yml`, and deployed by the shared
  platform Helm chart (`helm-charts/platform/templates/thunder/operator-namespace.yaml`).
  Unrelated to local dev.
- `manifests/api-platform/api-configuration-trait.yaml` — the source of truth
  `scripts/check-trait-copies.sh` diffs against its Helm-chart copy; gated by
  `make test` (CI).
- `manifests/api-platform/observability-alert-rule-trait.yaml` — the only copy
  in this repo of the `observability-alert-rule` ClusterTrait, which the chart's
  ComponentTypes name in `allowedTraits`. Not applied by `aectl platform
  install` — a fresh aectl-only cluster needs
  `kubectl apply -f manifests/api-platform/observability-alert-rule-trait.yaml`
  by hand for auto-RCA to work.
- `single-cluster/thunder-resources/`, `thunder-env-resources/`,
  `values-cp.yaml`, `values-dp.yaml`, `values-openbao.yaml` — the old
  Compose-chain's Thunder bootstrap bundle and OC values, genuinely unused now.
  `tools/aectl/internal/envidp/*.go` and `internal/thunder/client.go` carry
  comments citing these files as the shell reference implementation their Go
  logic was ported from — those comments are now historical (the files are
  still here, just unexercised by any script).
- `design/two-tier-thunder.md` and the ADRs describing the two-Thunder-tier
  model (ADR-0027/0028/0029, ADR-0022) still document real architecture
  (`tools/aectl/internal/envidp` implements it), independent of whether this
  repo's own Compose-chain scripts exist.

If you're picking this repo back up and don't need any of the above, it's
safe to delete in a follow-up pass — it just wasn't done here because parts of
it are genuinely still load-bearing and the risk of deleting the wrong file
(breaking CI or the release pipeline) outweighed doing it in the same pass as
the Compose-chain removal.

## Credentials

`aectl platform install` registers AEP's OAuth clients in Thunder itself (see
`tools/aectl/internal/thunder`) — there is no fixed `admin`/`admin` login from
this repo's own bootstrap anymore. Check `skaffold/defaults.yaml` and
`deployments/scripts/setup-env-for-aectl.sh`'s own output for current
credentials on a given cluster.

For GitHub repo provisioning, connect a PAT (or GitHub App) at **Settings → GitHub Integration**.
For AI generation, connect an Anthropic key at **Settings → Anthropic Integration** — per-org, with no platform fallback.

## Tear down

```bash
k3d cluster delete openchoreo       # destroy cluster (loses all OC state)
```
