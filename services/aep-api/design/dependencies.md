# Dependencies feature — final shape

`internal/feature/dependencies/` is the umbrella for dependency management,
aligned with OpenChoreo's Workload taxonomy: the **endpoints** family
(component and org-service kinds, wired via WorkloadConnection) and the
**resources** family (external and platform-resource kinds, wired via the OC
Resource model: ResourceType → Resource → ResourceReleaseBinding).

- `dependencies/` (parent) — the authenticated MCP server (`POST /internal/v1/mcp`,
  JSON-RPC 2.0, aud `aep-api-mcp`) exposing the four discovery tools; tool calls
  run under the BFF's OC service identity (see the E2E-caught 401 in git history).
- `endpoints/` — org endpoint catalog (reads deployed Workloads), org-service
  URL env naming, and the single-owner access-request state machine.
- `resources/` — external-resource registry + per-project value collection
  (secrets go to SM-API/OpenBao, never the DB) and the two provisioners
  (external: value-backed ResourceType per org; platform: catalog
  ClusterResourceTypes such as `postgres-cnpg` and `thunder-app`), both pinning
  ResourceReleaseBindings to controller-cut releases.

Task lifecycle is event-driven through `internal/contracts` (the Projector is
the sole status writer): `values.provisioned`, `provision.started`,
`resource.ready`, `resource.provision_failed`. Typed SYSTEM rows
(config-collection, resource-provisioning) gate component tasks via three JSONB
columns (`depends_on_external_resources/org_services/resources`); deploy events
cascade-release held siblings. See ADR-0003 (read-time resolution) and
ADR-0004 (declarative wiring).
