# ADR-0024 — One cluster, one platform IdP

**Status:** Accepted · 2026-08-30
**Context:** running AEP and the WSO2 Agent Management Platform on the same
local k3d cluster.

## Context

Both products are built on OpenChoreo, and both were developed against their own
local cluster. Running them side by side is not possible: the two k3d configs
bind an identical set of host ports, so standing up a second cluster means
rewriting one product's whole port map, its hostnames, and every URL through
them — and paying for a second OpenChoreo.

That forces one cluster, which forces one OpenChoreo control plane. And an
OpenChoreo control plane has exactly one `security.oidc` issuer. There is no
configuration in which AEP's Thunder and Agent Manager's both survive.

## Decision

**One platform IdP, shared, installed unconditionally.**

The shared instance is `wso2-amp-thunder-extension` — Agent Manager's chart,
wrapping ThunderID 1.0.0 — installed by `scripts/setup-thunder.sh` into
namespace `amp-thunder`, with its hostname overridden to AEP's existing
`thunder.openchoreo.localhost`. AEP's own `thunder` release is gone.

It is **not** behind `ENABLE_AGENT_MANAGER`, even though every other Agent
Manager component is. Switching IdP release means a different PVC and a
different issuer, so making it a toggle would mean every flip of the flag
invalidated every login and every stored OAuth client. The cost of the choice is
named plainly: AEP's base now pulls one chart from Agent Manager's release line,
and the shared Thunder carries roughly a hundred `amp:*` scopes AEP never uses.

**The bootstrap is a merge, not a second source.** The chart pins
`thunder.bootstrap.configMap.name` to its own ConfigMap, and ThunderID's setup
Job fails the Helm render outright if both `bootstrap.scripts` and
`bootstrap.configMap` are set. There is one bootstrap channel and Agent Manager
holds it. So `setup-thunder.sh` renders Agent Manager's documents from the
pinned chart, adds AEP's from `single-cluster/thunder-resources/`, and applies
the union — which means Agent Manager's half tracks its own chart automatically
and bumping `AMP_VERSION` needs no edit on AEP's side.

`declarativeResources` was the obvious-looking alternative and is not
equivalent: it makes mounted files the *store* for a resource type, which would
take ownership of `application` away from the Job that creates Agent Manager's
own clients.

## Consequences

**The token subject moved.** ThunderID puts a `client_credentials` token's
subject in `client_id`, not `sub`. Every OpenChoreo service-account entitlement
binding had to move with it. The two changes land in the same step of
`setup-openchoreo.sh` on purpose: a cluster with the new IdP and the old claim
403s on every service call, and is not a state anyone should be able to reach.

**Four hostname keys, not three.** `configuration.jwt.issuer` is separate from
`configuration.server.publicUrl` and does not follow it. Missing it produces a
Thunder that serves on the right host and stamps the wrong `iss` into every
token, which OpenChoreo rejects — a 401 on every authenticated call with nothing
in Thunder's logs to explain it.

**Two tiers, and only one is shared.** Agent Manager also runs one Thunder *per
environment*, for AgentID and workload identity, pulled from the upstream
`thunderid` chart at its own version and deliberately decoupled from
`AMP_VERSION`. Convergence does not touch that tier.

A consequence worth naming: AEP's `thunder-app` ClusterResourceType provisions
OAuth applications for the apps AEP builds — end-user identity for generated
apps — and those now live in the same Thunder that serves platform login. That
was already true of AEP before convergence, so nothing regressed, but it does
contrast with Agent Manager's model of putting workload identity in the
per-environment tier. Whether AEP's generated-app identities should eventually
move there is a real design question, and out of scope here.

## Alternatives rejected

**Two clusters.** Identical host-port sets. Rejected above.

**Install upstream `thunderid` directly and overlay Agent Manager's clients when
the flag is on.** Cleaner ownership boundary, but AEP would have to
re-implement the HTTPRoute, the TLS ClusterIssuer that Agent Manager's
per-environment Thunders need, and the bootstrap format — for a boundary nobody
is asking for.

**Keep AEP's Thunder and make Agent Manager use it.** Agent Manager is newer on
every shared pin and cannot go backwards; its platform charts require
`ProjectType`, which only exists from OpenChoreo 1.2.0. AEP moves forward.
