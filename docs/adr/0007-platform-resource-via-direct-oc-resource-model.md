# Platform resources via direct OpenChoreo Resource authoring; wso2cloud #638 reserved

## Status

accepted (2026-06; marketplace P5)

## Context

P5 lets a design-phase `platform-resource` dependency (a database / cache /
queue) be provisioned and consumed by a deployed component. In wso2cloud,
minting a real backing instance is expected to go through a central
**platform / provisioning API** — but that API does not exist yet:
`wso2-enterprise/wso2cloud#638` ("Platform Services Provisioning") is an
**unbuilt tracking stub** (the Aiven-migration successor), and wso2cloud uses
**none** of OpenChoreo's Resource model today. Meanwhile app-factory already
authors OC Resource CRs directly against `openchoreo-api` for P4 external
connections (`connections.Provisioner` + `clients/openchoreo/resource_client.go`),
and the OC Resource mechanism (the controller fills a binding's
`status.outputs`) is installed and proven live on the local cluster.

## Decision

P5 provisions platform-resources on the **same OC Resource model as P4**,
authored **directly** by the BFF: discover a cluster-installed
`ClusterResourceType` (read-only — app-factory **never authors the type**),
author a per-project `Resource` + per-env `ResourceReleaseBinding` that
references it, and let the cluster's real provisioner fill the outputs. The one
point that genuinely differs between local and cloud — *who fills the outputs* —
is a narrow `ResourceProvisioner` seam: `OCNativeProvisioner` is the only
implementation built; a `PlatformProvisioner` for wso2cloud#638 is **reserved**
(interface boundary + doc comment, no code). Discovery, the typed task graph,
the readiness watcher, the drawer, and consumption
(`workload.spec.dependencies.resources[]`) are all provisioner-agnostic.
Locally the sample type is `postgres-cnpg` (CloudNativePG operator), installed
by the `deployments/` scripts standing in for the cluster platform-engineer.

## Why

- There is **no wso2cloud provisioning API to integrate with today** (#638 is a
  stub), so building one inside app-factory would fork platform responsibility.
- The OC Resource model already works and is OC-native, so consumption is
  **identical** whether an in-cluster controller or a future #638 API fills the
  outputs — when #638 ships, only the "fill outputs" step swaps.
- A wide seam (abstracting provisioning *and* wiring) would fork the
  P4 `Resource`/binding/`dependencies.resources[]` chain that already works,
  for no benefit.

## Consequences

- **R1 (async):** a real DB takes minutes; provisioning is async — a readiness
  watcher (modeled on `build_watcher.go`) completes the `resource-provisioning`
  task on the binding's native `Ready` condition. No synchronous wait in the
  request path.
- **R2 (cloud controllers):** wso2cloud's DP cluster must have the OC Resource
  controllers enabled and a real `ClusterResourceType` + provisioner installed
  before P5 works there — neither exists today. Local-only until then; engage
  #638 rather than fork.
- **R4 (quota):** app-factory authors OC CRs bypassing platform-api
  (wso2cloud#549), so the billing / entitlement gate does not fire on Resource
  authoring — a governance gap to close when platform-api routing / #638 lands.
- **R6 (generated creds):** the provisioner *mints* credentials (unlike P4's
  user-supplied values); rotation = re-provision. Secret values never leave the
  OC-rendered Secret — the BFF reads only output *references*.
- Resource authoring runs under service-identity + `X-Impersonate-Org`
  ([[adr-dual-mode-oc-auth-impersonation]]).
