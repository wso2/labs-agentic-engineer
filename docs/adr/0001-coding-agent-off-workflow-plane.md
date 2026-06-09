# Coding-agent WorkflowRuns run on the data plane, not the workflow plane

## Status

accepted (2026-05-25, per wso2cloud team direction)

## Context

OpenChoreo provides two plane types for running pods: the **workflow plane**
(`ClusterWorkflowPlane`, today `cloud-dp-oc-ci`) and the **data plane**
(`ClusterDataPlane`, today `cloud-dp-oc-dp`). Today **all** app-factory
WorkflowRuns — both `dockerfile-builder` (image build) and
`app-factory-coding-agent` (LLM-driven source editing) — are projected onto
the workflow plane in namespaces `workflows-wc-<orgUUID8>-<orgHash8>`. This
mirrors the agent-platform (deployed agent-manager) convention.

## Decision

Move only the **coding-agent** WorkflowRuns off the workflow plane onto the
data plane, in a new namespace shape `wc-<orgUUID8>-<orgHash8>-remote-worker`
on `cloud-dp-oc-dp`. The **build** WorkflowRuns (`dockerfile-builder`) stay
on the workflow plane unchanged. The data-plane cluster will host a new
`ClusterWorkflowPlane` instance (named e.g. `remote-worker`) alongside its
existing `ClusterDataPlane/default` — same cluster, both plane CRs pointing
at it via the cluster-agent.

## Why

The wso2cloud team's framing: the **workflow plane was designed for user
component builds** (CI work — turn user code into images). "Developing or
building component source using an agent" is a different category of
workload — long-running, interactive, agent-driven, with persistent state
(workspaces, git clones) — and doesn't fit WP's CI model.

## Consequences

- Argo Workflows must be installed on the data-plane cluster (it lives only
  on WP today). SRE work, but a one-time install.
- The same cluster will be addressed via two OC plane CRs — supported
  natively (they're independent CRs pointing at one cluster-agent), but
  worth documenting in the runbook.
- Mixing CI-style ephemeral compute (coding-agent) with runtime workloads
  on one cluster needs explicit quota/PSP review.
- App-factory's BFF must distinguish at dispatch time: builds target the
  old `ClusterWorkflowPlane/default` + `workflows-wc-<...>` NS; coding-agent
  runs target the new `ClusterWorkflowPlane/remote-worker` + `wc-<...>-remote-worker`
  NS. This is a small change in `dispatch_service.go` once the plane and NS
  exist.

## Rejected alternatives

- **Stay on WP, just rename the NS to a DP-style name** — superficially
  cheaper, but contradicts the wso2cloud team's "WP is for builds" framing
  and re-creates the original mismatch under a new name.
- **New dedicated 'remote-worker' cluster** — clean isolation but
  multi-week SRE project; not justified at current scale.
