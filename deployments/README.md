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

Every hostname on the cluster is composed onto one DNS suffix, `AE_DOMAIN`.
`make dev-env` passes `localhost`, which reproduces the k3d install exactly;
a cluster other machines reach passes its own (`<ip>.sslip.io`, or a wildcard
domain you control) to the scripts directly. It is **required** rather than
defaulted: ThunderID's bootstrap bundle is read once, by a pre-install hook
Job, so a cluster built on the wrong suffix cannot be corrected — only
deleted. The scripts refuse to start without it, before touching anything.

The suffix is composed onto fixed parents (`openchoreo.`, `ae.`, `amp.`,
`gateway.`, `am-gateway.`, `openchoreoapis.`) and never substituted for the
bare token `localhost`, which still has to mean loopback on the operator's own
machine — the Backstage and AMP console dev origins and the `occ`/`amctl`
callback URIs are all registered against it.

`aectl` reads its own config rather than the environment, so the same suffix
appears in the config file you import (`console.public_url`, `tryit.public_url`,
`gateway.hostname`, `environment.idp_base_domain`,
`environment.gateway_base_domain`, `thunder.public_url`). On a non-localhost
run the cluster script prints that block already composed, so it doesn't have
to be written by hand.

```bash
# 1. One-shot bring-up — cluster + platform, via aectl (idempotent).
#    Builds this checkout's service images and installs onto them, so the
#    cluster never runs released images against local chart templates.
make dev-env
# Console: http://console.ae.localhost:8080
# aep-api: http://console.ae.localhost:8080/aep-api-service/ (the console proxies it)
# Try it:  http://tryit.ae.localhost:8080

# 2. Edit source, then run this to rebuild + redeploy just the changed image(s)
make dev-update

# 3. Build the coding-agent runner images (Claude Code + OpenCode) from this
#    checkout and point the release at them; again after a runner change
make dev-runner
```

`make dev-env` also enables the OpenChoreo SRE agent on the observability
plane and wires its alert → RCA → AE issue handoff (`scripts/setup-sre.sh`),
using the org's model connection key saved in the Console (an Anthropic key). `WITH_OBSERVABILITY=0`
skips the plane and the SRE agent; `WITH_SRE=0` skips only the agent;
`WITH_AGENT_MANAGER=0` is the lean profile for SRE work. See
`docs/developer-guide/sre-handoff-runbook.md`.

`make dev-env` builds `tools/aectl` as `aectl-skaffold` (git-ignored — this
flow's own copy of the binary), installs a bare OpenChoreo + ThunderID cluster
with `WITH_SKAFFOLD_CLIENT=1` (bootstraps `aectl`'s own Thunder admin client,
`ae-install-client` — see that script's step 3c for why it can't register
itself), builds this checkout's six service images into the cluster
(`make dev-images`), then runs `aectl-skaffold platform config import` (against
`skaffold/defaults.yaml`) and `aectl-skaffold platform install --addons=all
--platform-version=latest --platform-chart deployments/helm-charts/platform
--image-tag=dev-local`.

**Local dev runs on locally built images, end to end.** `--image-tag=dev-local`
re-points every service at the images `dev-images` just built and imported, so
the pods come up on your branch's code the first time rather than pulling the
released `:latest` and being swapped afterwards. That flag is local-dev only:
it defaults to empty, and a real `aectl platform install` leaves the chart's
released tags exactly as they are.

The reason it is not optional: the chart is installed from your working tree,
so without it a cluster runs your branch's Deployment templates against
`main`'s containers. That is fine until a branch changes the contract between
the two — a template setting an env var only the new image's entrypoint knows
to read, say — and then the break surfaces far from its cause. ADR-0039's
`ae:*` resource indicator did exactly this: the console template set
`VITE_THUNDER_RESOURCE`, the released console image had no code that read it,
and the symptom was a wrong-audience 401 in `aep-api`'s log.

The observability plane is installed running, because Agent Manager's charts
install against it, and `make dev-env` parks its heavy half (OpenSearch,
Prometheus, collectors, adapters) as its last step. Parked, Agent Manager's
trace and metric views and the console's archived cycle logs are empty; live
logs and everything else work. `make obs-unpark` brings it back and
`make obs-park` parks it again: unpark between builds on an 8 GiB VM, where
the plane and a coding Job together overload the node. `WITH_OBSERVABILITY=0
make dev-env` skips the plane, and with it Agent Manager.

No model key is needed to bring this up: every agent runs on the calling
org's model connection (format, base URL, key, model), connected in the
console's welcome step or on Settings' **AI agents** card, and there is no
platform fallback. aep-api hands it to the agents per turn (`X-Model-Key`
plus the connection) and to each coding run at dispatch.

`make dev-update` (`skaffold run`, `skaffold.yaml`) is one-shot, not a watch
loop — run it again after every edit you want reflected in the cluster. It
only rebuilds images whose dependencies changed and re-points the
already-installed `aep-platform` release at them; it does not re-derive any of
aectl's own settings (Thunder/OpenBao URLs), which is why its Helm
step passes `--reset-then-reuse-values` (helm >= 3.14): the values the install
set are kept, and the current chart's defaults apply. Plain `--reuse-values`
would render on the defaults of the chart the release was installed with, so a
new default (for example `aeStudio.webhookRelay.image`) would never arrive.

The three images of the `ae-studio` data-plane pod (design agent, collab,
studio tools) are built by a second config, `skaffold/ae-studio.yaml`, with a
**unique tag per build** (`.skaffold/ae-studio-images.json`, git-ignored, is its
`--file-output`). aep-api writes those refs into the OpenChoreo Resource, so a
fixed tag would leave it unchanged and the pod would never roll. `dev-images`
imports the refs into k3d and pins them against kubelet image GC;
`dev-env` passes them to `aectl platform install --ae-studio-image-*` and
`dev-update` sets `aeStudio.images.*`, which changes aep-api's env so Helm rolls
aep-api. The `ae-studio` pod itself rolls at the next console visit, when
aep-api's converge sees the new refs. It is never `kubectl rollout restart`ed:
its Deployment belongs to OpenChoreo.

**Upgrading an existing install.** `make dev-update` upgrades through
`aectl platform update` (`helm upgrade --reset-then-reuse-values`), which also re-applies
the `aeStudio.*` values aectl derives from its config (gateway host, IdP
issuer/JWKS/token URLs, console origins, egress). An install made before AE
Studio existed therefore picks them up on its next `make dev-update` or
`aectl platform update`; no existing secret is read or regenerated (the one
secret it may write, create-only, is the relay seed below). Never run
`aectl platform install` on an existing install to get them: it regenerates
vault secrets (unless run with `--reuse-secrets`).

**Upgrading an install that used the install-wide smee client.** The per-org
webhook relay (a `gosmee` container in each org's `ae-studio` pod) replaces the
install-wide smee client, which the upgrade removes. The switch is
`ae_studio.webhook_relay.enabled` in the aectl config; while it is absent,
aectl reads the legacy `webhook.local_smee.enabled` in its place and warns once,
so an install that forwarded through smee keeps a relay. Set the new key to
silence the warning. With the relay on, `aectl platform update` (and
`aectl platform install --reuse-secrets`) also creates the relay seed `aep/webhook-relay-seed` in OpenBao when it is missing (create-only:
an existing seed is never rewritten, and the value is never printed); every
org's channel URL is derived from it. GitHub hooks of projects created before
the upgrade keep the URL they were created with, which no longer receives
anything (no hook is migrated): re-create such a project, or disconnect and
reconnect the org's GitHub in Settings, which forgets the org's hook ids so the
sweep's hook repair installs every project's hook on the relay URL.

**Upgrading an install that mounted `/workspaces` on aep-api.** aep-api no
longer has a workspace volume: the chart drops PVC `aep-workspaces`, the mount,
the `workspaces.*` values and the `AEP_WORKSPACE_*` env. The PVC carries no
`helm.sh/resource-policy: keep`, so `helm upgrade` deletes it, and with it
everything on the volume: the run recordings under `runs/` and the pre-phase-4
`repos/`, `trash/` and `tmp/` trees. Nothing is migrated; a cycle's feed is now
read from its pod log, then the observability plane. On a StorageClass with
`reclaimPolicy: Retain` the PersistentVolume is left `Released`; delete it by
hand (`kubectl delete pv <name>`) once you no longer need the data. Remove any
`workspaces:` block from your own values files; Helm ignores it.

`make dev-update` also runs `aectl platform sync-clients` after the upgrade,
since an update never runs the install's Thunder setup. On an install that
predates a Thunder client (the AE-only `ae-studio-internal-client`) it seeds
only the MISSING `aep/thunder-clients/*` vault keys (an existing key is never
rotated, and no database or Thunder admin secret is touched), nudges the
ExternalSecrets that sync them, and registers the Thunder clients (idempotent).
A client whose Secret is still unavailable is skipped with a warning; the
others register.

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

The same chart renders the org's `ProjectType/default`, which the platform
chart owns (`localOrgProvisioning`; every project AEP creates references it).
`setup-agent-manager.sh` drops Agent Manager's identical copy in its
post-renderer. On a cluster where Agent Manager's release already owns that
object, `aectl platform install` stops on it: see
[`agent-manager/README.md`](agent-manager/README.md) for the reinstall.

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
  one deploys agents straight onto the org's model connection, as before Agent
  Manager existed. What aep-api then does with it is
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
  by hand for auto-RCA to work (`scripts/setup-sre.sh` applies it).
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

For GitHub repo provisioning, connect a PAT (or GitHub App) at **Settings → Credentials → GitHub**.
For AI generation, connect a model on the **AI agents** card under **Settings → Credentials** (Anthropic's API or any public https endpoint speaking the Anthropic or OpenAI-compatible format) — per-org, with no platform fallback.

## Tear down

```bash
k3d cluster delete openchoreo       # destroy cluster (loses all OC state)
```

## Local OpenBao wipe recovery

The local OpenBao (OpenChoreo's dev-mode instance) is in-memory: a pod restart
or `colima`/cluster reset wipes it. Everything else survives, including the
ESO-synced Kubernetes Secrets, which keep their last synced value until ESO
next syncs (refresh interval 1h). Verify that on your cluster before relying
on it. If a Secret is already blank, recover the value from another copy.

`aectl platform sync-clients` detects the wipe (a non-generated `aep/*` path is
gone) and refuses.

**Do NOT run `make dev-env`, `aectl platform install` or `--reuse-secrets`.**
`install` regenerates and overwrites every `aep/*` key (only Postgres is
rescued), which rotates secrets under the running platform. Never write a value
with `value=-` over `kubectl exec -i`: it intermittently stores an empty value.

Never print a value, not even to compare. Compare lengths or sha256 hashes.
Never put a value on a command line (no `echo`, no `value=<literal>`).

1. **Re-import each `aep/*` key from the Secret it feeds.** Write each value
   to a 0600 file straight from its Secret. The file then holds the exact
   bytes, with no trailing newline:

   ```bash
   umask 077; f=$(mktemp)
   kubectl get secret <secret> -n wso2-aep -o jsonpath='{.data.<key>}' | base64 -d > "$f"
   ```

   Then write it to `secret/data/<path>` as `{"data":{"value": ...}}`, in one
   of two ways:

   - **The OpenBao HTTP API** (preferred). Port-forward OpenBao and get a token
     from the Kubernetes-auth login the aectl code uses. Build the body with
     `jq -Rs '{data:{value:.}}' < "$f" > "$body"` (also 0600), then send it with
     `curl --data-binary @"$body"`. Do not use `-d @file`: it strips newlines.
   - **Inside the OpenBao pod.** Copy the file in with
     `kubectl cp "$f" <ns>/<pod>:/tmp/v` (it needs `tar` in the image), so argv
     carries only its path. Run
     `bao kv put secret/<path> value=@/tmp/v`, then remove `/tmp/v`.

   Delete `$f` (and `$body`) once the key is verified (step 2). Each key's
   source:

   | Vault path | Secret (namespace `wso2-aep`) | Key |
   |---|---|---|
   | `aep/thunder-admin/client-id`, `.../client-secret` | `aep-thunder-admin-creds` | `client-id`, `client-secret` |
   | `aep/openbao-token` | `aep-openbao-secrets` | `OPENBAO_TOKEN` |
   | `aep/postgres-password` | `postgres-secrets` | `POSTGRES_PASSWORD` |
   | `aep/thunder-clients/<name>` | `aep-thunder-secrets` (and `aep-ae-studio-internal-secrets` for `ae-studio-internal`) | `OC_WORKLOAD_PUBLISHER_SECRET`, `OC_OBSERVER_READER_SECRET`, `AEP_API_CLIENT_SECRET`, `BFF_TO_GIT_SERVICE_SECRET`, `BFF_TO_REMOTE_WORKER_SECRET`, `LOCAL_DEV_SEEDER_SECRET`, `THUNDER_SYSTEM_CLIENT_SECRET`, `OC_RCA_AGENT_SECRET`, `AE_STUDIO_INTERNAL_CLIENT_SECRET` (for `oc-workload-publisher`, `oc-observer-reader`, `aep-api-client`, `bff-git-service`, `bff-remote-worker`, `local-dev-seeder`, `system-client`, `openchoreo-rca-agent`, `ae-studio-internal`, in that order) |
   | `aep/aep-mcp-token` | `aep-sre-handoff-secrets` | `SRE_HANDOFF_TOKEN` |
   | `aep/webhook-relay-seed` | `aep-webhook-relay` | `AE_STUDIO_WEBHOOK_RELAY_SEED` |

   `aep/anthropic-api-key`, `aep/opensearch-username` and `aep/opensearch-password`
   have no ESO target Secret; re-enter them from where you hold them (the
   OpenSearch pair is in the observability plane's own Secret, if installed).
   Restore the `aep/thunder-clients/*` keys too, from their Secrets. Leaving
   one out does not preserve it. Step 5's `sync-clients` seeds a **new** random
   value for each missing key, syncs it into the Secret and updates the Thunder
   client to match. That is a rotation of that client's secret. Every holder of
   the old value then fails until it restarts on the new Secret. Do it only if
   a rotation is what you want. `sync-clients` also refuses until every
   non-generated path above is back.
2. **Verify** each written key by length (or sha256) against the Secret it came
   from.
3. **Org publisher secrets.** Restore each org's
   `user-app-secrets/<org>/publisher-secrets` from a surviving runner or pod
   Secret, if one exists.
4. **Vault-only org secrets.** `github-webhook-secret` and `ae-studio-client`
   have no surviving copy: delete those `org_secrets` rows and re-submit the
   GitHub token in Settings, which regenerates them. Re-save the model key and
   the Claude token in Settings.
5. **Sync.** Force-sync the ExternalSecrets
   (`kubectl annotate externalsecret <name> -n wso2-aep force-sync=$(date +%s) --overwrite`)
   and run `aectl platform sync-clients`.
