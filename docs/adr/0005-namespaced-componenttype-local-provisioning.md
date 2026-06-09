# Reference the per-org namespaced ComponentType; provision it locally

## Status

accepted (2026-06-09)

## Context

`buildCreateComponentBody` in
`asdlc-service/clients/openchoreo/component_client.go` referenced the
cluster-scoped `ClusterComponentType` (name e.g. `deployment/service`). In dev
cloud that resolves to the OpenChoreo **built-in**, whose render template has
no `registry-pull-secret` / `imagePullSecrets` — so user workloads can't pull
their per-org ECR image and the Release stalls `ResourcesProgressing` /
ImagePullBackOff. The per-org **namespaced** `ComponentType` (provisioned by
platform-api's `ProvisionOrgUnit`) carries the pull secret. Devant and
agent-manager already reference `kind: ComponentType`; app-factory was the
outlier (#36).

## Decision

Reference `kind: ComponentType` (the per-org namespaced type),
unconditionally local + cloud. Because local `setup-asdlc.sh` only shipped
cluster-scoped `ClusterComponentType`s — so a `kind: ComponentType` reference
hit `ComponentTypeNotFound` and components never deployed locally — **also**
provision per-org namespaced `service` + `web-application` ComponentTypes in
the local org namespace (`default`), derived verbatim from the cluster-scoped
specs so they can't drift. This is the local stand-in for cloud's
`ProvisionOrgUnit`.

## Why

Keeps a **single unified BFF code path** (no env-branching on kind), matching
cloud / devant / agent-manager. Verified empirically: cloud user components
with `kind: ComponentType` reach `Ready=True`; locally the reference hit
`ComponentTypeNotFound` until the namespaced type was provisioned, after which
a full build → deploy reached a Running workload.

## Consequences

- Local setup now provisions per-org namespaced ComponentTypes — a small
  platform-touching addition to `setup-asdlc.sh`.
- Note: locally the in-cluster registry is unauthenticated, so the pull-secret
  benefit is cloud-only; locally the win is that the reference **resolves at
  all**.
- PR #36 + #37.

## Rejected alternatives

- **Env-conditional kind in Go** (`ComponentType` in cloud,
  `ClusterComponentType` locally) — diverges the local vs cloud code path; the
  less-preferred option the original code comment itself flagged.
