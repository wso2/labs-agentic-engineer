# ADR-0029 — Environment Thunders and gateways are platform infrastructure bound by a record

**Status:** Accepted · 2026-09-05
**Context:** the second identity tier, after [ADR-0028](ADR-0028-the-platform-idp-is-neutral-infrastructure.md)
settled ownership of the first.

## Context

ADR-0027 gave the cluster one platform IdP; ADR-0028 named it for what it is and
made both products publishers into it. Neither covered the tier below.

Agent Manager runs a **second** Thunder per environment, for AgentID and
workload identity, and beside it an API Platform **gateway** per environment.
AEP had neither: it registered every generated app's OAuth client on the
platform IdP, and it ran one cluster-wide gateway whose keymanager was the
platform IdP. On one shared cluster that arrangement stops working, for three
separate reasons:

* **A gateway can only terminate against one identity.** A managed API's
  authentication is terminated at the gateway. With one gateway for the cluster,
  either both products' APIs trust the platform IdP — and an environment's own
  Thunder is then useless for the workloads deployed into it — or one product's
  APIs 401. There is no third configuration.
* **Roles and users are per environment, and the platform IdP is not.** A build
  provisions the roles and test users a version's `security.json` declares. A
  login minted on the platform IdP is valid nowhere the version is deployed, so
  those objects have to live on the Thunder the deployed app validates against
  ([ADR-0022](ADR-0022-roles-and-test-users-are-shared-directory-objects.md)'s
  2026-09-05 amendment).
* **Both products would provision the same pair.** Two provisioners deriving the
  same release name from the same `(org, env)` is a collision the moment either
  changes its derivation, and neither has any way to notice.

The tempting answer is a naming convention: agree that an environment's Thunder
is `thunder-<org>-<env>`, and let every consumer derive the issuer from it. That
is what a cluster-wide gateway config already did, and it fails in the way
conventions fail — silently, at a consumer, long after the divergence. The
release name is truncated and hash-suffixed above 53 characters; the issuer is a
*handle* registered with Agent Manager rather than a function of the name; and a
gateway installed against a Thunder that is later re-provisioned keeps trusting
an issuer nothing answers on, while reporting `Programmed`.

## Decision

**An environment's Thunder and its gateway are platform infrastructure, and the
pair is described by a binding record that publishers write and consumers read.
Nobody derives it.**

**1. Both tiers are the platform's.** `setup-environment-thunder.sh` and
`setup-environment-gateway.sh` provision one Thunder and one gateway per
`(org, env)`, and they run for every environment on the cluster — AEP's from
`setup-aep.sh`, Agent Manager's from `setup-agent-manager-env.sh`. One script
either way. Neither tier is behind `ENABLE_AGENT_MANAGER`: an AEP-only cluster
gets exactly the same shape, because AEP's own components need a gateway to
serve their managed APIs and a directory to hold their versions' users.

**2. Each script has a CREATE mode and a BIND mode, and never crosses.** If the
release already exists, it was somebody's — no `helm upgrade`, no re-provision.
Binding means reading the instance's own values back and recording them.
Publishing into an instance you did not create is done through the chart's own
bootstrap importer, re-run as a plain Job against an **AEP-owned** ConfigMap
rendered from the release's own values, so nothing of the other publisher's is
written to.

**3. The binding record is the interface.** ConfigMap
`thunder-binding-<org>-<env>`, selected by
`aep.wso2.com/kind=thunder-binding` + `/org` + `/env` — never by name, because
the name and the namespace are derivations that can truncate. It names the
release, the issuer, the in-cluster admin URL, the resource-server identifier,
the trusted-issuer triple, and where the credential's other copies live. The
credential is delivered to each consumer where that consumer can reach it: a
Secret mirrored into `thunder-app-operator-system` for the operator, whose
Secret informer and RBAC are namespace-scoped by design, and an OpenBao document
for `aep-api`, which runs outside the cluster. The non-secret half is also
projected onto the OpenChoreo `Environment` as annotations, because `aep-api`
sees the OpenChoreo API and nothing else.

**4. Consumers resolve per request, not per process.** `thunder-app` picks its
target from the `(org, environment)` labels on each CR; `aep-api` resolves a
`Target` through a port whose adapter reads the `Environment` annotations and
OpenBao. Neither holds a single configured issuer any more. A resolution that
finds no binding is a visible status or error naming the provisioning command —
not a fallback to the platform IdP, which would silently write an environment's
objects into the wrong directory.

**5. Ownership is recorded, and removal is guarded by it.** Both scripts label
the namespace at install time with `aep.wso2.com/namespace-created-by` and
`aep.wso2.com/release-created-by`. `remove-environment-thunder.sh` uninstalls a
release and deletes a namespace only when those say this repo created them;
otherwise it withdraws AEP's own artefacts — its client and role deregistered
from the instance over its admin API, its Secrets and ConfigMaps, the OpenBao
entry, the annotations — and leaves the release running.

## Consequences

**The platform-IdP fallback is gone, and that is a behaviour change.** A
`ThunderApplication` for an environment with no binding does not register
anywhere: it reports a missing binding with the setup command in the message.
That is the intended failure — the alternative is an app whose users exist on an
IdP its gateway does not trust.

**Four projections of one record, reconciled by nothing.** No controller keeps
the ConfigMap, the mirrored Secret, the OpenBao document and the `Environment`
annotations in step; the provisioning script writes all four in one pass and a
re-run rewrites them. Drift is therefore possible and invisible, so
`verify-convergence.sh` check 11 compares all four and exercises the credential,
and check 12 compares each gateway's rendered keymanager issuer against the
binding.

**Removing an environment removes it for both products.** The guard is about who
*created* a release, not who uses it. An environment whose Thunder AEP created
loses that Thunder when AEP removes the environment, exactly as Agent Manager's
own removal script behaves for one it created. The tier is per environment; an
environment is not half-removable.

**`thunder-app` cannot deregister after the binding is gone.** Its finalizer is
released without deleting the OAuth client when the binding no longer resolves,
because there is no longer an address to delete it at. So removal order matters:
components first, then the environment. The removal script warns with the list
of CRs rather than refusing, because an environment must stay removable when its
Thunder is already unreachable.

**The name comes from Agent Manager's library, including when it is the wrong
name.** `thunder-naming.sh` is staged at `AMP_VERSION` and sourced, never
copied — a second implementation of a truncating, hashing derivation is a
collision waiting for a long org name. Until the configurable-prefix change
ships upstream, a published ref yields `amp-thunder-<org>-<env>` and the script
says so and continues. A stale prefix is cheaper than two derivations.

**AEP's generated-app identities moved tiers.** ADR-0027 named this as an open
question; this is its answer. Every `ThunderApplication` now lives on its
environment's Thunder, and applications registered on the platform IdP by the
single-target operator are stale — asserted as `aep-dp-*` absence in
`verify-convergence.sh` check 10.

## Alternatives rejected

**Derive the issuer from the environment name.** The convention already exists
and is not enough: the issuer is a handle registered with Agent Manager, the
release name truncates and hashes, and a derivation cannot notice that the
instance it names was re-provisioned. Every failure it produces is a 401 at a
consumer with a healthy-looking gateway and a healthy-looking Thunder.

**One gateway per cluster with several keymanagers.** It reintroduces exactly
what the tier removes: an environment's APIs accepting a sibling environment's
tokens, and a platform token valid at every managed API on the cluster.

**Let the operator and `aep-api` read the binding from the same place.** They
cannot. The operator has cluster-wide ConfigMap access and namespace-scoped
Secret access; `aep-api` has no Kubernetes access at all. The record has three
delivery mechanisms because there are three reachability envelopes, not because
three copies were desirable.

**Have `remove-environment-thunder.sh` delete the `Environment` too.** It reads
as the tidy thing to do and is the destructive one: the `Environment` is
OpenChoreo's, deleting it takes every Component's deployment with it, and the
script exists to be safe to run when only the identity tier is wrong.
