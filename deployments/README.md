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

# 2b. Read-only check of an org's AE Studio Resource and pod (ok/FAIL per check)
make ae-studio-check ORG=<org>

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
install against it, and `make dev-env` parks its tracing, metrics and RCA
workloads (Prometheus, collectors, tracing adapter) as its last step. The logs
plane (OpenSearch, Fluent Bit, the logs adapter) stays running for run history.
Parked, Agent Manager's trace and metric views are empty; logs and everything
else work. `make obs-unpark` brings it back and
`make obs-park` parks it again: unpark between builds on an 8 GiB VM, where
the plane and a coding Job together overload the node. `WITH_OBSERVABILITY=0
make dev-env` skips the plane, and with it Agent Manager.

No model key is needed to bring this up: every agent runs on the calling
org's model connection (format, base URL, key, model), connected in the
console's welcome step or on Settings' **AI agents** card, and there is no
platform fallback. aep-api writes it into the org's AE Studio pod env
(`AE_MODEL_CONNECTION` and `ANTHROPIC_API_KEY`, read once at boot) when it
converges the pod, and hands it to each coding run at dispatch.

`make dev-update` is one-shot, not a watch loop — run it again after every
edit you want reflected in the cluster. Its steps are the `dev-update` target
in the `Makefile`: build the images (only those whose dependencies changed),
check the AE Studio refs, re-create aep-api's OpenBao role, upgrade the
`aep-platform` release with `aectl platform update`, run
`aectl platform sync-clients`, and restart the platform Deployments (never the
`ae-studio` pod, below). The upgrade does not re-derive aectl's install
settings (Thunder/OpenBao URLs), which is why it passes
`--reset-then-reuse-values` (helm >= 3.14); it does re-apply the `aeStudio.*`
values aectl derives from its config. The values the install set are
kept, and the current chart's defaults apply. Plain `--reuse-values`
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
everything on the volume: the run recordings under `runs/` and the old
`repos/`, `trash/` and `tmp/` trees. Nothing is migrated, and losing the recordings is intended: run history now
comes from the observer (a cycle's feed is read from its pod log, then the
observability plane). On a StorageClass with
`reclaimPolicy: Retain` the PersistentVolume is left `Released`; delete it by
hand (`kubectl delete pv <name>`) once you no longer need the data. Remove any
`workspaces:` block from your own values files; Helm ignores it.

Runs from before the upgrade lose their history. Their cycles never stored a
Component UID, so the observer cannot filter their logs and a closed one reads
`recording: expired`; the settler deletes their Components once their pods are
gone, as for any closed cycle. Only runs dispatched after the upgrade keep
their logs (until `OBSERVER_LOG_RETENTION`).

`make dev-update` also runs `aectl platform sync-clients` after the upgrade,
since an update never runs the install's Thunder setup. On an install that
predates a Thunder client (the AE-only `ae-studio-internal-client`) it seeds
only the MISSING `aep/thunder-clients/*` vault keys (an existing key is never
rotated, and no database or Thunder admin secret is touched), nudges the
ExternalSecrets that sync them, and registers the Thunder clients (idempotent).
A client whose Secret is still unavailable is skipped with a warning; the
others register.

Registering `openchoreo-observer-resource-reader-client` (install and
sync-clients alike) also writes to the observability plane, which OpenChoreo
owns: when its `observer-secret` ExternalSecret in
`openchoreo-observability-plane` does not already read
`aep/thunder-clients/oc-observer-reader`, aectl rewrites it to do so, waits for
ESO to sync it and restarts `deploy/observer`, so expect a short observer
outage on that run. A plane already pointed there, or no plane, is left alone.

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
Never put a value on a command line (no `echo`, no `value=<literal>`). That
includes the OpenBao token: log the in-pod CLI in from a 0600 file on stdin
(`kubectl -n openbao exec -i openbao-0 -- bao login -no-print - < "$tokfile"`).
Never `bao kv get` a multi-field document without `-field=<name>` piped to
`wc -c`: the whole document prints its secrets.

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
     `bao kv put secret/<path> value=@/tmp/v`, then remove `/tmp/v`. A
     multi-field path takes one `<field>=@/tmp/<file>` per field in the same
     `kv put`.

   Delete `$f` (and `$body`) once the key is verified (step 2). Each key's
   source:

   | Vault path | Secret (namespace `wso2-aep`) | Key |
   |---|---|---|
   | `aep/thunder-admin/client-id`, `.../client-secret` | `aep-thunder-admin-creds` | `client-id`, `client-secret` |
   | `aep/postgres-password` | `postgres-secrets` | `POSTGRES_PASSWORD` |
   | `aep/thunder-clients/<name>` | `aep-thunder-secrets` (and `aep-ae-studio-internal-secrets` for `ae-studio-internal`) | `OC_WORKLOAD_PUBLISHER_SECRET`, `OC_OBSERVER_READER_SECRET`, `AEP_API_CLIENT_SECRET`, `BFF_TO_GIT_SERVICE_SECRET`, `BFF_TO_REMOTE_WORKER_SECRET`, `LOCAL_DEV_SEEDER_SECRET`, `THUNDER_SYSTEM_CLIENT_SECRET`, `OC_RCA_AGENT_SECRET`, `AE_STUDIO_INTERNAL_CLIENT_SECRET` (for `oc-workload-publisher`, `oc-observer-reader`, `aep-api-client`, `bff-git-service`, `bff-remote-worker`, `local-dev-seeder`, `system-client`, `openchoreo-rca-agent`, `ae-studio-internal`, in that order) |
   | `aep/aep-mcp-token` | `aep-sre-handoff-secrets` | `SRE_HANDOFF_TOKEN` |
   | `aep/webhook-relay-seed` | `aep-webhook-relay` | `AE_STUDIO_WEBHOOK_RELAY_SEED` |

   Do NOT restore `aep/openbao-token`, `aep/task-signing-key` or
   `aep/webhook-secret`: nothing reads them any more, even if a Secret still
   holds an old copy.

   `aep/anthropic-api-key`, `aep/opensearch-username` and `aep/opensearch-password`
   have no ESO target Secret; re-enter them from where you hold them (the
   OpenSearch pair is in the observability plane's own Secret, if installed).
   Restore the `aep/thunder-clients/*` keys too, from their Secrets. Leaving
   one out does not preserve it. The sync step's `sync-clients` seeds a **new** random
   value for each missing key, syncs it into the Secret and updates the Thunder
   client to match. That is a rotation of that client's secret. Every holder of
   the old value then fails until it restarts on the new Secret. Do it only if
   a rotation is what you want. `sync-clients` also refuses until every
   non-generated path above is back.
2. **Verify** each written key by length (or sha256) against the Secret it came
   from.
3. **Environment identity bindings.** Each environment with a Thunder binding
   has a document at `secret/aep/thunder/<org>/<env>`, aep-api's only vault
   read (environment-tier sign-in fails without it). aectl wrote it when it
   bound the environment (`tools/aectl/internal/envidp`); rewrite its 5 fields
   from what survives, one 0600 file per field, in one `kv put`:

   | Field | Source |
   |---|---|
   | `issuer` | Environment `<env>` (org namespace) annotation `aep.wso2.com/thunder-issuer` |
   | `adminURL` | annotation `aep.wso2.com/thunder-admin-url` |
   | `systemResourceIdentifier` | annotation `aep.wso2.com/thunder-system-resource-identifier` |
   | `clientId` | Secret `thunder-<org>-<env>-aep-system-client` (namespace `thunder-<org>-<env>`), key `client-id` |
   | `clientSecret` | the same Secret, key `client-secret` |

   An environment lists its binding path in the annotation
   `aep.wso2.com/thunder-secret-path`; one without it has nothing to restore.
4. **Org secrets (`user-app-secrets/<vault-org-ns>/<ref>`).** Every org secret
   (`<ns>-github-pat-…`, `<ns>-github-webhook-secret-…`, `<ns>-default-key-…`,
   `<ns>-coding-agent-key-…`, `<ns>-ae-publisher-client-…`,
   `<ns>-ae-studio-client-…`) lives only in the vault, but each one a workload
   consumes has an ExternalSecret whose target Secret keeps the last synced
   value. List them by remote path, names only:

   ```bash
   kubectl get externalsecret -A -o json | jq -r '.items[] | .metadata.namespace as $ns
     | (.spec.target.name // .metadata.name) as $t | .spec.data[]?
     | select(.remoteRef.key | test("user-app-secrets/"))
     | [.remoteRef.key, .remoteRef.property, $ns, $t, .secretKey] | @tsv' | sort -u
   ```

   For each remote path, write every property from its target Secret's key
   (0600 file per field, as in step 1) in one `kv put`. When several Secrets
   feed the same path and property, check they agree by sha256 first. A path
   that `org_secrets` names but no Secret feeds has no surviving copy:
   re-enter it in Settings (the GitHub token regenerates the webhook secret
   and both org clients; the model key and the Claude token are re-saved on
   their cards). Do not delete `org_secrets` rows by hand.
5. **aep-api's OpenBao access.** The wipe also took aep-api's write-only
   policy `aep-api-writer` and its Kubernetes-auth role `aep-api`; until they
   are back every secret write aep-api makes fails (log event
   `openbao.login_failed`). Re-apply both (idempotent; `make dev-update` runs
   the same script before its upgrade):

   ```bash
   bash deployments/scripts/openbao-aep-api-auth.sh
   kubectl -n openbao exec openbao-0 -- bao read auth/kubernetes/role/aep-api
   ```
6. **`host.k3d.internal`.** A colima or Docker restart that did not go through
   `k3d cluster start` can also drop `host.k3d.internal` from the node's
   `/etc/hosts` (and from CoreDNS `NodeHosts`). Check from inside a pod
   (`kubectl -n wso2-aep exec deploy/aep-api -- nslookup host.k3d.internal`);
   if it fails, add `<k3d network gateway, e.g. 172.18.0.1> host.k3d.internal`
   to both and restart CoreDNS. Do not stop and start the cluster to fix it:
   that wipes OpenBao again.
7. **Sync.** Force-sync the ExternalSecrets
   (`kubectl annotate externalsecret <name> -n wso2-aep force-sync=$(date +%s) --overwrite`)
   and run `aectl platform sync-clients`.

## Upgrading to write-only secrets

From this release aep-api only writes secrets: no org secret value lives in
Postgres, and aep-api reads none back. It logs in to OpenBao by Kubernetes auth
(role `aep-api`, policy `aep-api-writer`, see
`deployments/scripts/openbao-aep-api-auth.sh`) instead of a static token. The
chart no longer renders the objects below. Helm removes the ones it rendered
itself (the `aep-openbao-secrets` ExternalSecret, and with it its Secret), so
their deletes below are no-ops after the upgrade, kept for installs that drifted
from the chart. Remove the rest by hand. Every command is value-free: a root token is piped on
stdin, never put on argv, and no command prints or compares a value.

**Cloud: SRE request.** Each item's Cloud step is a request to SRE,
not something done from this repo.

### Rollout

Do not run old and new aep-api replicas side by side. Migration step
`phase26_secrets_refs_only` drops `org_secrets.value` and the other value and
triplet columns that the old code still reads. The chart sets no update
strategy, so Kubernetes does a rolling update. Scale to zero first, then
upgrade:

```bash
kubectl -n wso2-aep scale deploy/aep-api --replicas=0
# helm upgrade (or make dev-update), which brings aep-api back at the chart's replica count
```

### Environment variables

Drop from any overlay or `.env`; they are no longer read:
`OPENBAO_TOKEN`, `TEST_MODE`, `LOCAL_OPENBAO_REPAIR`, `GITHUB_APP_ID`,
`GITHUB_APP_PRIVATE_KEY_PATH`.

Added (all optional, with defaults): `OPENBAO_AUTH_ROLE` (default `aep-api`),
`OPENBAO_AUTH_MOUNT` (default `kubernetes`), `OPENBAO_AUTH_TOKEN_PATH` (default
`/var/run/secrets/kubernetes.io/serviceaccount/token`). Cloud needs the
OpenBao `aep-api` role and `aep-api-writer` policy created by SRE before the
rollout.

### Legacy objects to delete

Set `bao` up once (root token for the local dev OpenBao is its public dev value,
override with `OPENBAO_ROOT_TOKEN`):

```bash
bao_do() { printf '%s' "${OPENBAO_ROOT_TOKEN:-root}" \
  | kubectl -n openbao exec -i openbao-0 -- sh -c 'IFS= read -r BAO_TOKEN; export BAO_TOKEN; exec bao "$@"' sh "$@"; }
```

| What | Local delete | Cloud |
|---|---|---|
| Static OpenBao token: vault `secret/aep/openbao-token`, ExternalSecret and Secret `aep-openbao-secrets` (`wso2-aep`). Replaced by Kubernetes auth. | `bao_do kv metadata delete secret/aep/openbao-token`; `kubectl -n wso2-aep delete externalsecret aep-openbao-secrets --ignore-not-found`; `kubectl -n wso2-aep delete secret aep-openbao-secrets --ignore-not-found` | SRE request |
| Task signing key: vault `secret/aep/task-signing-key`, ExternalSecret and Secret `aep-task-signing-key`. Nothing reads it any more. | `bao_do kv metadata delete secret/aep/task-signing-key`; delete ExternalSecret and Secret `aep-task-signing-key` the same way | SRE request |
| Webhook secret: vault `secret/aep/webhook-secret`, ExternalSecret and Secret `aep-webhook-secrets`. Webhooks are verified per org by the org's `ae-studio-tools` (`/webhooks/github`); the org's own `github-webhook-secret` row is separate and stays. | `bao_do kv metadata delete secret/aep/webhook-secret`; delete ExternalSecret and Secret `aep-webhook-secrets` the same way | SRE request |
| Per-org OpenChoreo **GitSecret** `aep-component-build-git-secret` (a `GitSecret` CR in each org's control-plane namespace, created and deleted through OpenChoreo's `gitsecrets` API; not a plain Secret). No code references it. | Delete the GitSecret the way aep-api did, through the OpenChoreo API (there is no `gitsecret` kubectl resource type; OpenChoreo backs a GitSecret by a SecretReference labelled `openchoreo.dev/secret-type=git-credentials`): `DELETE /api/v1alpha1/namespaces/<oc-org-ns>/gitsecrets/aep-component-build-git-secret` (204). Then confirm nothing of that name is left: `kubectl -n <oc-org-ns> get secretreference,secret aep-component-build-git-secret --ignore-not-found`, and check the vault copy `bao_do kv metadata get secret/default/git/aep-component-build-git-secret`. Locally that vault copy is seeded by OpenChoreo when OpenBao starts; leave it. On an install where OpenChoreo did not seed it, delete it with `bao_do kv metadata delete`. | SRE request |
| Orphaned vault references (below). | `bao_do kv metadata delete secret/user-app-secrets/<vault-org-ns>/<ref>` | SRE request |

Delete the ExternalSecret before its Secret, or ESO recreates the Secret. The
`aep-eso-openbao-token` RBAC objects are unrelated and stay.

### Orphaned vault references

Earlier code retired a replaced key's old vault copy; this release only retires
references it created itself, so some stay behind on an upgraded install:

1. Org copies from before the `org_secrets` reference rows: vault entries of references written before the
   `org_secrets` reference rows existed.
2. References named only in the dropped columns: the `secret_ref_*` columns of
   `org_credentials`, `org_anthropic_credentials`, the model connection table and
   the IDP profile table.

Two namespaces are involved. Do not mix them:

- `<vault-org-ns>` is the vault path segment, `wc-<8 hex>-<8 hex>`, derived from
  the org's Thunder OU id (`tenant.OrgBaseNamespace`). It is the middle segment of
  `secret/user-app-secrets/<vault-org-ns>/<ref>`. Read it off any existing path of
  the org: `bao_do kv list secret/user-app-secrets`.
- `<oc-org-ns>` is the org's OpenChoreo control-plane namespace, the `ocOrgID`
  (for the local org: `default`). SecretReference CRs and the `org_secrets` rows
  (`oc_org_id`) use it.

`secret/user-app-secrets/` also holds other users' application secrets, and
`ai-agent-model-access` and the AMP model and tracing references are live without
an `org_secrets` row. A key is an orphan **candidate** only if both hold:

1. Its name has AEP's form `<oc-org-ns>-<entity>-<8 hex>` with `<entity>` one of
   `github-pat`, `github-webhook-secret`, `default-key`, `coding-agent-key`,
   `ae-publisher-client`, `ae-studio-client` (a long namespace is trimmed, so
   match on the tail `-<entity>-<8 hex>`). Anything else is not AEP's: leave it.
2. Nothing live points at it: no `org_secrets` row names it, and no
   SecretReference or ExternalSecret has its vault path as a `remoteRef.key`
   (that covers `ai-agent-model-access`).

Names only, no value is read. Run it as a bash script (not pasted into your
login shell: it sets `-euo pipefail` and an exit trap). The scratch files go in
a private temporary directory:

```bash
set -euo pipefail
# Paste the bao_do function from "Legacy objects to delete" here.
VNS='<vault-org-ns>'; ONS='<oc-org-ns>'  # replace both placeholders
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
ENT='github-pat|github-webhook-secret|default-key|coding-agent-key|ae-publisher-client|ae-studio-client'

# Vault keys of AEP's form
bao_do kv list -format=json "secret/user-app-secrets/$VNS" \
  | jq -r '.[]' | { grep -E "(^|-)($ENT)-[0-9a-f]{8}\$" || true; } | sort > "$T/vault-aep-refs"

# 1. Names an org_secrets row still holds
kubectl -n wso2-aep exec postgres-0 -- sh -c \
  "psql -U \"\$POSTGRES_USER\" -d \"\$POSTGRES_DB\" -Atc \
   \"SELECT secret_ref_name FROM org_secrets WHERE oc_org_id = '$ONS'\"" | sort > "$T/live-rows"

# 2. Vault paths any SecretReference or ExternalSecret in the cluster points at
{ kubectl get secretreferences -A -o json \
    | jq -r '.items[].spec.data[]?.remoteRef.key';
  kubectl get externalsecrets -A -o json \
    | jq -r '.items[].spec.data[]? | .remoteRef.key'; } \
  | sed -n "s#^user-app-secrets/$VNS/##p" | sort -u > "$T/live-paths"

# Candidates: AEP-shaped, in neither list
comm -23 "$T/vault-aep-refs" <(sort -u "$T/live-rows" "$T/live-paths")
```

Review each candidate by hand before running the delete in the table. By
construction no SecretReference names a candidate, so there is none to delete.
The scratch files hold names only and are removed on exit.
Both lists must come back non-empty on a cluster with a connected org; if one is
empty, fix its query before trusting the candidate list.

Local-only example of a stale path:
`user-app-secrets/<org-ns>/default-ae-publisher-client-14f4038d`, a leftover
publisher client copy from a local incident. It is not a Cloud item.

### Orgs connected before secret references

There is no backfill. After the upgrade such an org sees the setup wizard on its
next login and re-enters the GitHub token and the model key. Until then Settings
shows GitHub and the model as not configured, builds answer 409
`publisher_credentials_missing`, and the coding-agent token reads `Not set` until
it is entered again.
