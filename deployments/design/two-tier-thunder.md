# Two identity tiers, and the record that binds them

The cluster runs **one platform IdP** and **one Thunder per environment**. Both
tiers are platform infrastructure, owned by neither AEP nor the WSO2 Agent
Management Platform; both products publish into them under one contract, and
each environment carries a **binding record** that says which instance is its
own.

Why there is one platform IdP: **ADR-0027**. Who owns it: **ADR-0028**. Why the
second tier and the per-environment gateway are the platform's rather than one
product's, and why a record rather than a naming convention: **ADR-0029**.

---

## The shape

```
  T1  platform IdP ── release and namespace `platform-idp`
      thunder.openchoreo.localhost:8080  (HTTPS gateway :8443)
      one per cluster · both consoles log in here · OpenChoreo's oidc issuer
        │
        │  trusted issuer (HTTPS JWKS)
        ▼
  T2  environment Thunder ── one per (org, env)
      release/namespace `thunder-<org>-<env>`  ·  issuer http://<env>-idp.amp.localhost:8080
      AgentID, workload identity, generated apps' end-user sign-in,
      a version's roles and test users
        │
        │  the ONLY keymanager of
        ▼
      APIGateway `api-platform-<org>-<env>` in namespace `<org>-<env>`
      runtime …-gw-gateway-gateway-runtime.<org>-<env>:22893
      vhost <env>-<org>.gateway.localhost:19080
```

A platform-IdP token is **not** valid at an environment's gateway, and neither
is a sibling environment's. That is the point of the tier: a gateway terminates
a managed API's authentication, so it must terminate against exactly one
identity — the one the APIs it serves were deployed against.

## The publisher contract

Both products provision into both tiers. Neither owns either. The rules are the
same at both tiers, and every one of them exists because breaking it is silent:

- **Own a bundle, prefix everything in it.** AEP's documents for T1 are
  `single-cluster/thunder-resources/`, for T2 `single-cluster/thunder-env-resources/`
  (an `aep-system-client` application and an `aep-`prefixed role granting it
  `system`). Agent Manager's ship in its charts. Neither publisher edits the
  other's.
- **Never set a singleton.** ThunderID keeps exactly one `cors`, one
  `defaultResourceServer`, one `csp` server_config. A redeclaration replaces it,
  so the installer composes all three rather than letting the last import win
  (ADR-0028). `setup-thunder.sh` emits a platform-owned document for each —
  `thunder-resources/89-platform-cors-config.yaml`,
  `90-platform-default-resource-server.yaml`, `91-platform-csp.yaml`, all
  numbered to sort after Agent Manager's — and fills its value in at install
  time. `cors` is the **union** of both publishers' declared origins, so the
  result cannot drop one. The other two are **adopted**: the value is Agent
  Manager's, unchanged, because the platform IdP's default resource server and
  its CSP are what amp-console and amp-api already run against. What the
  composition buys there is that no publisher's file is the last word — a
  renumbered document cannot take the singleton over. A singleton Agent Manager
  stops declaring is dropped from the bundle rather than imported with a value
  the platform invented. `verify-convergence.sh` check 13 asserts all of it
  against the merged ConfigMap.
- **Never `helm upgrade` a release you did not create, and never uninstall
  one.** Both provisioning scripts have a CREATE mode and a BIND mode, decided
  by whether the release already exists; the removal script has the same guard
  in reverse.
- **Bind to what exists, do not re-derive it.** The gateway reads its issuer out
  of the binding record, not out of a name.
- **Always send the resource indicator.** See below.
- **Register by handle, not by URL.** `amp-api`'s URL-registration path is
  SSRF-hardened and rejects `*.localhost`, so the local flow registers the
  handle (`<env>-idp`) and lets Agent Manager derive the URL.

## The binding record

One environment, one record, written to the four places its consumers can
actually reach. None of them can read the other three.

| Projection | Where | Read by |
|---|---|---|
| `ConfigMap thunder-binding-<org>-<env>` | the T2 namespace | `thunder-app` (by label, cluster-wide watch), `setup-environment-gateway.sh`, `verify-convergence.sh`, `seed-test-users.sh` |
| `Secret <release>-aep-system-client` | the T2 namespace | the record of what the instance was given; what a re-run reuses instead of rotating |
| `Secret thunder-binding-<org>-<env>` | `thunder-app-operator-system` | `thunder-app` — its Secret informer and RBAC are restricted to its own namespace, deliberately, so the credential has to be delivered there |
| OpenBao `secret/aep/thunder/<org>/<env>` | OpenBao | `aep-api`, which runs outside the cluster and has no Kubernetes access |
| `aep.wso2.com/thunder-*` annotations | the OpenChoreo `Environment` | `aep-api` again — it sees the OpenChoreo API and OpenBao, and nothing else |

The ConfigMap is the authority: it names the release, the issuer, the in-cluster
admin URL, the System resource-server identifier, the trusted-issuer triple, and
**where the other projections live** (`secretName` / `secretNamespace` for the
operator, `secretPath` for `aep-api`). The labels are the contract, not the
names — `aep.wso2.com/kind=thunder-binding` plus `/org` and `/env` — because the
namespace is derived by Agent Manager's naming library and can be truncated.

Consumers in the code: `thunder-app`'s target resolution in
`single-cluster/resource-types/thunder-app/operator/`; `aep-api`'s in
`services/aep-api/internal/identity/` (the `TargetResolver` port) with the
adapter in `internal/app/identity_targets.go`. `aep-api` reaches the instance at
whichever of the two addresses its own location can resolve —
`THUNDER_ENV_ADMIN_ROUTE` defaults to the public issuer outside the cluster and
to the in-cluster Service inside a pod, because neither resolves from where the
other is right.

## Naming

`env.sh` holds T1's release and namespace (`THUNDER_NS` / `THUNDER_RELEASE`) and
every in-cluster address in this repo derives from it. One thing makes that
derivation hold: the wrapper chart's `thunderid` subchart names its objects the
usual Helm way — `<release>` only when the release name contains the chart
name, `<release>-thunder` otherwise — so `setup-thunder.sh` pins
`thunder.fullnameOverride` to the release. Without it a release called
`platform-idp` gets a `platform-idp-thunder-service` that neither the scripts
nor the chart's own HTTPS route can reach. T2's names come from
**Agent Manager's `thunder-naming.sh`**, staged at `AMP_VERSION` by
`am-scripts.sh` and never copied into this repo: it is the single source of
truth for the 53-character cap, the truncate-to-46-plus-sha256-6 suffix, and the
`<env>-idp.amp.localhost` host derivation. Two publishers deriving the same name
by two implementations is a collision waiting for a long org name.

`THUNDER_RELEASE_PREFIX=thunder` is what makes the tier's names
product-neutral. **Debt:** the library honours it only in a checkout that has
the configurable prefix (see *Where the platform works around Agent Manager*);
at a published `AMP_VERSION` that does not, the library returns
`amp-thunder-<org>-<env>` and `setup-environment-thunder.sh` prints a notice and
continues with the library's answer. The library is the truth for names even
when the name is the wrong one — a second derivation here would be worse than a
stale prefix.

## The trust chain

An environment Thunder trusts the platform IdP as an issuer so a platform token
is *understood* there (and then refused on audience, which is the correct
answer — the tiers are not interchangeable).

ThunderID fetches a trusted issuer's JWKS over **HTTPS only**; a plain-`http`
JWKS URL makes the pod crash-loop. So the URL is
`https://<public IdP host>:8443/oauth2/jwks` — the hostname the certificate is
issued for. Inside the cluster that hostname otherwise resolves, via CoreDNS's
`openchoreo.override`, to the *data*-plane gateway, which serves nothing on 8443:
the fetch times out and every platform token 503s.
`ensure_platform_idp_in_coredns` (`utils.sh`) installs a `rewrite stop` to the
IdP chart's own control-plane HTTPS Gateway Service, under the key
`0-platform-idp.override` — CoreDNS imports fragments in name order and
`rewrite stop` is first-match, so it has to sort before `openchoreo.override`.
Each T2 also mounts a CA bundle (Mozilla roots **plus** the platform CA, so
`SSL_CERT_FILE` stays a complete trust store).

## The resource-indicator rule

ThunderID resolves a requested scope against a resource server. The server-wide
default on a converged cluster is Agent Manager's, which does not define
`system` — so a `client_credentials` request that does not name the System
resource server as its `resource` gets a **200 and a token with no scope**, and
every admin call afterwards 403s with nothing in either response explaining why.

Every `scope=system` mint therefore sends `resource=<that instance's issuer>/mcp`:
`aep-api`, `thunder-app`, `seed-test-users.sh`, `tools/aectl`,
`verify-convergence.sh`. The indicator is **per instance** — a T2's is its own
issuer's, never the platform IdP's.

**The trap is T1's, not the tier's.** A T2 created by either publisher defaults
to the System resource server, so an unscoped `system` request happens to work
there. That is a coincidence of the default, not a property to rely on: send the
indicator everywhere. Every re-import bundle must also carry the System RS
identifier fix, because ThunderID's shipped default hardcodes
`https://localhost:8090/mcp` and the importer re-runs the shipped defaults
alongside the mounted files.

## Lifecycle

| Script | Does |
|---|---|
| `setup-environment-thunder.sh <org> <env>` | CREATE: installs the upstream `thunderid` chart with Agent Manager's own values shape, its bootstrap bundle carrying the platform documents plus AEP's, and registers the handle with `amp-api` when it answers. BIND: no helm *release* is touched — AEP's client is added by re-running the chart's own importer as a plain Job against an AEP-owned ConfigMap, rendered with `helm template` from the release's OWN values and chart version (`helm get metadata`/`get values` read them; nothing is installed or upgraded). Both modes end at the same binding record and the same gate: a `scope=system` token that reaches `GET /users`. |
| `setup-environment-gateway.sh <org> <env>` | Installs `api-platform-<org>-<env>` with the binding's issuer as its only Thunder keymanager, from a values FILE (Helm drops an unknown `--set` path silently, so a typo would install a gateway that trusts the wrong issuer and looks correct). BIND reports drift loudly rather than converging it. |
| `remove-environment-thunder.sh <org> <env> [--yes]` | The reverse, in Agent Manager's order: gateway → Thunder release → HTTPRoute → namespace → the record's other projections → the `Environment` annotations → the registrations in `amp-api`. |

Both provisioning scripts run from `setup-aep.sh` and `setup-aectl.sh`, and
again from `setup-agent-manager-env.sh` — one script either way, and on the
local stack one environment: AEP and Agent Manager both deploy into
`Environment/default`, so there is one environment Thunder and one gateway for
both. `setup-aep.sh` CREATEs that Thunder before `amp-api` exists; Agent
Manager's platform-resources chart then adopts the Environment object, and its
own environment-Thunder script performs a values-identical `helm upgrade` of the
release to register the handle and import its client. That upgrade is the
reason CREATE mirrors Agent Manager's values shape to the letter, and the BIND
run that follows it re-proves AEP's client still mints.

### The removal guard

A publisher that must not provision on top of another's release must not remove
one either. Ownership is recorded on the **namespace**, in the same breath as
creating it and before the install — the one step that runs earlier, claiming
the handle with `amp-api`, mutates nothing in the cluster, so no exit path can
leave an unlabelled namespace behind:

```
aep.wso2.com/namespace-created-by = setup-environment-thunder.sh | setup-environment-gateway.sh
aep.wso2.com/release-created-by   = …the same, or `external`
```

(The binding ConfigMap carries its own copy of `release-created-by` as a
backstop for an environment provisioned before the labels existed; it is written
last and cannot answer for a run that never got there.)

Anything not ours is left running, and only AEP's own artefacts are withdrawn:
its `aep-system-client` application and `aep-system-client-thunder-admin` role
**deregistered from the instance over its admin API**, its Secrets and
ConfigMaps deleted by their `managed-by` label, the OpenBao entry removed, the
`Environment` annotations stripped. Agent Manager's registrations are only
deleted for an instance AEP created and has just removed, and the
`thunder-url` handle is freed only after the system-client credential is
confirmed gone — freeing it first would let a re-provision claim a *new* handle
while the old credential still names the old, immutable issuer.

Two more properties, both deliberate:

- **The `Environment` CR is kept.** AEP does not own it, and deleting one takes
  every Component's deployment with it. Delete it separately.
- **`ThunderApplication` CRs still bound to the environment are a loud
  warning, not a refusal.** `thunder-app` releases its finalizer *without*
  deleting the OAuth client once the binding is gone — it has no way to reach
  the instance any more — so removing the binding first orphans clients.

## What `verify-convergence.sh` asserts

Checks 5 and 10–13 are this design's invariants. None of them fails loudly on
its own: a binding that drifts fails at one consumer, one 401 at a time.

- **5** — every `Environment` has a Programmed `api-platform-<org>-<env>`, and
  every `RestApi`'s `restapi-target` label names an existing gateway.
- **10** — no `aep-dp-*` application exists on the platform IdP (the operator's
  single-target past), and every `ThunderApplication`'s `status.issuer` is its
  own environment's binding issuer.
- **11** — every `Environment` has exactly one binding ConfigMap; the release it
  names is deployed; the five `Environment` annotations equal the ConfigMap; the
  OpenBao document exists and carries the same issuer; and the credential in the
  mirrored Secret mints `scope=system` and reaches `GET <issuer>/users` with 200.
- **12** — each gateway's *rendered* `ThunderKeyManager` issuer equals its
  binding's. The rendered config, not the values file: it is what the controller
  actually loaded, and these scripts never upgrade a release in place, so a
  gateway that outlives a re-provisioned Thunder keeps trusting the old issuer.
- **13** — in the platform IdP's merged bootstrap ConfigMap, each server_config
  singleton is owned by exactly one platform document, that document sorts last,
  and it carries a composed value rather than the placeholder its file holds in
  Git. Check 8 asserts the *effect* on the running IdP; 13 asserts the bundle
  that produced it, which is where a renumbered publisher document shows up.

Run it after any change to either tier:

```
bash deployments/scripts/verify-convergence.sh
```

## Facts that cost a day each

- **The gateway vhost is `<env>-<org>.gateway.localhost`** — environment first.
  That is the chart's derivation, the two only coincide when org and env are the
  same word, and the vhost is **write-once** in Agent Manager once registered:
  a wrong one can be warned about but not corrected.
- **`amp-api-client` is `client_secret_basic`** (curl `-u`), while
  `aep-system-client` is `client_secret_post`. Mixing them up reads as bad
  credentials.
- **An `Environment` will not delete while `DeploymentPipeline/default` promotes
  through it** — OpenChoreo's `environment-cleanup` finalizer holds it, silently
  from `kubectl`'s side (the reason is in the controller-manager log). Drop the
  promotion path first. `remove-environment-thunder.sh` says so in its summary.
- **ThunderID 1.0.0's application contract has two traps for an imperative
  client.** `type` is required on create (`APP-1042` without it; the publisher
  is `m2m`), and an OU is released into a client_credentials token only when
  the application's `token.accessToken.clientConfig.attributes` names `ouId`
  and `ouHandle` — otherwise the token verifies but aep-api refuses it with
  "ouHandle claim missing" and the runner reports "mcp auth: unauthorized". A
  `PUT` of the application without a `clientSecret` keeps the existing secret,
  so the claims can be added to a live publisher without rotating anything.
  The declarative documents and the thunder-app operator carried both; the
  aep-api publisher client is the third copy and lagged twice.
- **`helm upgrade` with no values re-applies the LAST release's values**, so a
  chart whose defaults are the contract (`amp-platform-resources`) has to be
  upgraded with `--reset-values`, or an old `--set` outlives the script that
  stopped passing it.
- **The API Platform gateway's at-rest AES key is per namespace.** The name is
  set once on the gateway *operator* and applied by it to every gateway it
  deploys, including Agent Manager's, so one operator means one secret name and
  only the namespace varies. `setup-environment-gateway.sh` generates one per
  environment and never rotates it — rotating makes every already-encrypted
  gateway secret undecryptable. Nothing in `openchoreo-data-plane` needs one any
  more, now that there is no cluster-wide gateway.
- **These scripts need Helm v4.** Rancher Desktop's 3.16 shadows Homebrew's on a
  developer machine: `PATH=/opt/homebrew/bin:$PATH`.

## Where the platform works around Agent Manager

Three things this design needs are not in a published Agent Manager release, so
the platform carries a workaround for each. Each workaround is load-bearing:
removing it without the upstream change breaks the thing it stands in for.

1. **A configurable environment-Thunder release prefix**
   (`THUNDER_RELEASE_PREFIX` / `IDP_RELEASE_PREFIX` / `idpReleasePrefix`,
   default `amp-thunder`). `thunder-naming.sh` honours it only in a checkout
   that has it, so `AM_SCRIPTS_DIR` pointed at such a checkout is what exercises
   the neutral prefix; a published `AMP_VERSION` produces the debt notice under
   *Naming* and the `amp-thunder-` names.
2. **CoreDNS overrides that resolve from pods.** Agent Manager's rewrite
   `*.amp.localhost` and `*.gateway.localhost` to `host.k3d.internal`, which is
   NXDOMAIN from a pod on a k3d cluster whose NodeHosts lacks that entry. This
   repo's own helpers (`ensure_amp_localhost_in_coredns`,
   `ensure_platform_idp_in_coredns` in `utils.sh`) rewrite to Service names
   instead.
3. **`controlPlane.enabled=false` on the gateway-extension chart.** The chart
   always emits a `tokenSecretRef`, and the gateway controller reads it as a
   non-optional env var, so a gateway installed without an Agent Manager to
   register with wedges in `CreateContainerConfigError` forever. The workaround
   is an empty token Secret, created by `setup-environment-gateway.sh`.
