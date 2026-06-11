# Coding-agent dispatched as a K8s Job via cluster-gateway-proxy, not OC WorkflowRun

## Status

accepted (2026-05-25, per wso2cloud team direction)

**Update (2026-06, #23):** the cloud `cluster-gateway-proxy` now validates
platform-idp JWTs; the BFF attaches its M2M token (the additive token path this
ADR anticipated). The "un-authed today" statements below describe the original
2026-05-25 assumption — see [[adr-dual-mode-oc-auth-impersonation]] for the
current auth model.

## Context

Today app-factory dispatches the coding-agent as an OC `WorkflowRun` of a
`ClusterWorkflow`, projected onto the WP cluster (`cloud-dp-oc-ci`) where
Argo executes it. The new direction (see
[[adr-coding-agent-off-workflow-plane]]) moves coding-agent off WP onto a
DP-style namespace on `cloud-dp-oc-dp`, where wso2cloud has confirmed
**there is no Argo and there will be no Argo** — coding-agent should run
as a plain K8s Job. OC's `WorkflowRun` stack is hardcoded around Argo and
cannot project to a non-Argo target.

## Decision

App-factory's BFF dispatches the coding-agent as a plain K8s `Job` on
`cloud-dp-oc-dp` by posting the Job + ExternalSecret manifests directly
to wso2cloud's existing `cluster-gateway-proxy` (in-cluster service URL
`http://cluster-gateway-proxy.openchoreo-control-plane.svc.cluster.local:8085`).
**As of this decision no authentication was sent** — the proxy enforced none,
and wso2cloud's own `ou` service called it the same way (no `Authorization`
header, just `X-Correlation-ID`). (Since changed — see the Status update.) The
`APP_FACTORY_BFF_TO_REMOTE_WORKER` Thunder M2M client (which we initially
thought to use) was actually provisioned for the now-removed long-lived
`remote-worker` service component and is not relevant here. The
`app-factory-coding-agent` `ClusterWorkflow` CR is retired; the Job
template lives inline in app-factory's BFF code (and/or as a checked-in
manifest in the lab repo). The `dockerfile-builder` flow is **unchanged**
— builds remain OC `WorkflowRun`s on WP with Argo.

## Why

- **No new infra on DP.** Avoids installing Argo on the user-app data
  plane (an SRE project on its own, plus a security review for mixing CI
  compute with runtime workloads).
- **Reuses proven mechanism.** wso2cloud's `ou` service already drives
  cluster-gateway-proxy to apply per-org bootstrap resources to DP. The
  proxy is namespace-agnostic; the only ask of wso2cloud is expanding
  `CLUSTER_GATEWAY_PROXY_ALLOWED_CRS` to include `jobs` and `externalsecrets`.
- **No new credentials to provision (at the time).** The proxy was un-authed
  when this was decided; no M2M client, no token-minting on app-factory's side.
  Matched the `ou-service` caller pattern. (wso2cloud later added JWT
  enforcement — #23 — and the `clustergatewayproxy` client now attaches the
  BFF's M2M token, the additive path this anticipated.)
- **Stays true to the boundary rule.** App-factory's BFF never holds a
  DP-cluster kubeconfig; it only ever speaks HTTP to a single
  wso2cloud-owned in-cluster service. Matches the "no long-lived service
  is a direct cluster writer" invariant in the OC refactor docs
  (rejecting option B below).

## Consequences

- App-factory authors per-run `ExternalSecret` manifests directly (no
  longer templated by OC `WorkflowRun.spec.resources`). Each dispatch =
  two proxy calls (ExternalSecret, Job).
- Status tracking shifts from `WorkflowRun.status` polling to Job-status
  polling (also via the proxy) and/or runner callbacks.
- The `app-factory-coding-agent.yaml` `ClusterWorkflow` CR in the cloud
  GitOps deployment repo is deleted; the Job template moves into app-factory.
- **wso2cloud team dependency:** allowlist expansion
  (`CLUSTER_GATEWAY_PROXY_ALLOWED_CRS += jobs,externalsecrets[,…]`) and
  per-org bootstrap NS template for `wc-<org8>-<hash>-remote-worker`
  (with RBAC + ESO ClusterSecretStore reference) must land before
  app-factory's cloud rollout.
- **Network-level isolation is the only protection** against
  unauthorized callers from elsewhere on `cloud-dp-oc-cp`. That's a
  wso2cloud-design choice (any CP component can call the proxy
  unauthenticated). Worth flagging as a longer-term hardening note but
  not app-factory's problem to solve.
- **Local dev consequence:** local stack has no cluster-gateway-proxy
  equivalent; local dispatch needs a separate path (TBD — see open
  questions).

## Rejected alternatives

- **(B) Direct kubeconfig / scoped SA on DP** — re-creates the
  "long-lived service is a direct cluster writer" anti-pattern the OC
  refactor docs explicitly call out as a sin (analysis/02-secrets.md
  "git-service holds K8s API client and SSA-writes plaintext"). Even
  with a tighter SA, the boundary erosion is real.
- **(C) New `remote-worker-dispatch-api` wso2cloud service** — extra
  service to build/deploy/maintain with no sharing benefit; the proxy
  already does the job.
- **(D) Extend openchoreo-api with a "create Job" endpoint** — Jobs are
  not OC primitives; polluting OC's product-agnostic surface with
  app-factory specifics re-creates the kind of coupling we're escaping.
