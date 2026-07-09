# resource-types/ — the local cluster's platform-resource catalog

One directory per `ClusterResourceType`: this is the PE (platform engineer)
catalog for the local single-cluster dev stack. Each type's directory holds:

- `resourcetype.yaml` — the `ClusterResourceType` itself (what an architect's
  platform-resource dependency names via `resourceType`).
- `rbac.yaml` — the data-plane RBAC grant that lets OpenChoreo's data-plane
  agent apply this type's rendered manifest (a foreign CRD) into the
  consuming project's `dp-*` namespace.
- `operator/` — present only when AEP itself provides the type's backing
  implementation (e.g. `thunder-app`). It is a real Go module (added to the
  root `go.work`, built with the rest of the workspace) with its own Helm
  chart. The local dev stack installs this chart via `helm` as part of
  `setup-aep.sh`; a real cluster's PE may reuse this chart or bring its own
  backing for the same type name.

The `resourcetype.yaml` and `rbac.yaml` are **LOCAL-ONLY samples**, applied by
`deployments/scripts/setup-aep.sh` acting as the cluster PE. Nothing here is
installed by the `wso2-ae-platform` or `wso2-agentic-engineer-bundle` charts —
app-factory's BFF only discovers and references whatever `ClusterResourceType`s
exist on a cluster; it never authors them. A real cluster's PE curates its own
catalog (these types, others, or different backings for the same type name)
independently of the platform install.

Adding a new type here is "a cluster install," never an app-factory code
change — see the design goal recorded in ADR-0006 / the dependency-resources
design notes.
