# ADR-0039 — The write target is a per-project fact resolved at use

**Status:** Accepted · 2026-09-29
**Related:** [CONTEXT.md](../../CONTEXT.md) **Write target** ·
[ADR-0017](ADR-0017-the-platform-owns-deploy.md) (the platform owns deploy)

## Context

aep-api read `default/DeploymentPipeline/default` once at boot into a process
global and wrote every project into the environment it named. That held for one
local org. In wso2cloud the namespace is per org (the platform API rewrites it
to `wc-…`), orgs have their own pipelines (the live dev cloud has two), and
OpenChoreo auto-deploys a project to the root of *its own*
`deploymentPipelineRef`, which is mutable. One global answer was wrong for any
project not on the default pipeline, and for any project that moved.

## Decision

**The write target is the root of the project's own deployment pipeline, read
when it is needed.**

1. **Per project.** Project `deploymentPipelineRef` → pipeline → root. Not per
   org and not per process. The one port is `openchoreo.WriteTargets`
   (`Resolve`, `OrgDefaultRoot`, `OrgWriteTargets`); the pure `PipelineRoot`
   does the graph work. Boot does not call OpenChoreo, and the global
   `DevEnvironmentName` and its two-minute retry are gone.

2. **OpenChoreo's root rule, exactly.** The root is the first promotion path,
   in list order, whose non-empty source is never any path's target. No paths,
   or none qualifying (which covers a cycle and a self-edge), is an error.
   Several roots resolve to the first; there is no ambiguity error.

3. **Resolved at use, once per operation.** No cache, no TTL, no retry. The
   value is resolved at the top of an operation and passed down. Pipelines are
   edited outside AEP, so there is no event to invalidate on.

4. **Writes fail, reads degrade.** A typed `*openchoreo.ErrNoWriteTarget` covers
   configuration faults only: no pipeline reference, a pipeline that is missing
   (a live 404 maps to `ErrNotFound`), empty, cyclic or too long. A 404 on the
   Project itself is not one: it propagates as not-found, never as
   `ErrNoWriteTarget`. Transient OpenChoreo errors (network, 5xx, 401/403)
   propagate unchanged so Temporal retries them. Project create resolves the
   write target after provisioning its cells and, on any resolve failure, fails
   and compensates; the typed error maps to 422 `no_write_target`. In the run
   supervisor the fault carries `delivery.ErrNoWriteTarget` (beside
   `ErrDeployPermanent` on deploy paths) and the Temporal type `NoWriteTarget`.
   Met at coding-agent dispatch or at any deploy step (the gate, the version
   read, a promote or a readiness poll), it settles the run `failed` with
   terminal reason and `RunFailure` code `no-write-target`, mints no fix issue
   and dispatches no agent. Console reads degrade instead: the project status deploy stage reads
   `none` with a WARN log, provisioning status and configuration readiness
   degrade, and the identity panel shows the directory unavailable.

5. **A root longer than 11 characters is refused** (`ocname.MaxEnvNameLen`),
   and the coding-agent component name budget is a constant derived from that
   bound, so an overlong project name is refused at `CreateComponent` with one
   message whatever the environment.

6. **A coding-agent cycle records the environment it bound into.**
   `run_cycles.environment` is written at launch (`NoteLaunch`, beside the model
   host). The watcher, log source and archive read it, and fall back to the
   project's current write target, logging that they did, when it is empty
   (a cycle dispatched before this change, or whose write failed).

7. **Org-scoped consumers.** Writes with no project fan out over the write
   targets of the org's projects: Agent Manager's `PublishOrgModelConnection`
   and `ClearOrgModelKey` publish per distinct gateway, and a project whose
   target cannot be resolved is logged and skipped. Reads use the org default
   pipeline's root (`default`, else the sole pipeline), through
   `ChooseOrgDefaultPipeline`, which provisioning's `resolvePipelineOrder`
   shares. The MCP `list_groups` catalog uses it. Identity's `TargetResolver` is
   `Scope(ctx, org, project)` then `Resolve(ctx, scope)`.

8. **Local parity is the same path.** Namespace `default` and the k3d
   quickstart's `development → staging → production` pipeline resolve to
   `development` through the code cloud uses. aectl keeps
   `oc.pipeline_source_environment` for install-time gateway and Thunder setup,
   which runs before aep-api exists; aep-api does not read it.

## Consequences

- Each operation costs one or two extra OpenChoreo reads (Project, then
  pipeline). Polling paths such as project status pay it per poll. A TTL is
  the answer if that is measured to matter.
- **v1 limitation: a project that moves pipelines leaves old-root resources in
  place.** New work follows OpenChoreo to the new root; what was provisioned in
  the old root is not migrated or removed, and OpenChoreo keeps its old binding.
- **v1 limitation: no API field carries the cause.** With no write target,
  project status shows deploy `none` and the cause is only in the logs.
- **v1 limitation: identity teardown can be skipped.** If a project's write
  target cannot be resolved when the project is deleted, teardown leaves its
  `idp_*` rows and directory objects. A same-name project on the same
  environment could adopt them. Identity is off in wso2cloud.
- **v1 limitation: a transient read at dispatch spends a re-dispatch.** A
  transient write-target error when dispatching a coding agent costs one
  re-dispatch, like other transient launch failures.
- An org's model connection reaches only environments that have a project.
  An org with no projects publishes nothing, and one broken project does not
  block the others.

## Alternatives rejected

- **A per-org write target.** Wrong for projects on a non-default pipeline
  (two exist on the live dev cloud) and for any project after a pipeline move.
- **Caching with a TTL.** Nothing signals a pipeline edit, so the cache would be
  stale by construction. Resolve at use and measure first.
- **Retrying the resolve.** A configuration fault does not heal in two minutes,
  and Temporal already retries the transient failures that do.
- **An ambiguity error for several roots.** OpenChoreo takes the first; a
  stricter rule would refuse a graph OpenChoreo deploys from.

## Amendment 2026-10-06 — org-wide model publishing lists environments

`OrgWriteTargets` is removed from `openchoreo.WriteTargets`. Decision 7's
org-scoped writes no longer fan out over the org's projects: a key save
publishes the connection to Agent Manager's provider for every org Environment
that has an AI gateway binding, once per distinct gateway, and a disconnect
clears it the same way. An environment with no binding is skipped, and one
broken environment does not stop the others. The last consequence above (a
connection reaches only environments that have a project) no longer holds.
The org's AE Studio binds to its `ae-system` project's write target through
`Resolve`, like any project
([ADR-0040](ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)).
