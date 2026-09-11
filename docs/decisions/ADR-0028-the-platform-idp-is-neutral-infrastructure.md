# ADR-0028 — The platform IdP is neutral infrastructure

**Status:** Accepted · 2026-09-05 · the second tier it names is settled by
[ADR-0029](ADR-0029-environment-identity-is-bound-by-a-record.md)
**Context:** the one shared ThunderID from ADR-0027, now that both AEP and the
WSO2 Agent Management Platform depend on it and Agent Manager has grown a
second, per-environment Thunder tier.

## Context

ADR-0027 settled that there is exactly one platform IdP on the cluster, because
there is exactly one OpenChoreo control plane and it has exactly one
`security.oidc` issuer. It did not settle who owns it.

In practice one product did, by name. The release was `amp-thunder-extension` in
namespace `amp-thunder`, taken verbatim from the chart AEP happened to install
it from. Every address derived from that name — `amp-thunder-extension-service.
amp-thunder.svc.cluster.local:8090` — and the literal was copied into eleven
places across two repos' worth of charts, manifests, compose files and Go
defaults. So a component owned by both products was spelled as if it belonged to
one, and the spelling was load-bearing.

Three things made that untenable rather than merely untidy:

* **Reading the topology wrongly.** `amp-thunder` is one letter away from Agent
  Manager's *other* Thunder tier — one instance per (org, environment), for
  AgentID and workload identity, whose releases were prefixed `amp-thunder-` too.
  A namespace list showed `amp-thunder` and `amp-thunder-default-default` side by
  side with nothing saying that the first is shared platform infrastructure and
  the second is one product's per-environment detail. The teardown selected
  releases by that prefix, and a name-pattern selection in a cluster two products
  share is precisely how this repo once deleted AEP's data plane.
* **Ownership of the singletons.** ThunderID has server-wide `server_config`
  documents — `cors`, the default resource server, CSP — of which there is
  exactly ONE. A redeclaration replaces it, so whichever product's bootstrap
  document imported last silently owned the whole value. AEP's CORS document
  carried a hand-maintained union of both products' browser origins, which meant
  Agent Manager adding an origin required an edit in AEP's repo, and forgetting
  it produced a console that fails before it can render a login form.
* **Trust from the second tier.** An environment Thunder trusts the platform IdP
  as an issuer and fetches its JWKS. That edge did not exist when ADR-0027 was
  written, and it needs the platform IdP to be addressable as itself.

## Decision

**The platform IdP is named for what it is, owned by neither product, and both
products are publishers into it.**

Concretely:

**1. A neutral name.** The release and namespace are both `platform-idp`
(`THUNDER_NS` / `THUNDER_RELEASE` in `deployments/scripts/env.sh`). The chart it
is installed from is still Agent Manager's `wso2-amp-thunder-extension` — that
part of ADR-0027 stands, and re-implementing the HTTPRoute, TLS ClusterIssuer
and bootstrap format to avoid it would buy nothing. The chart is an
implementation detail and no longer leaks into the release name. Environment
Thunders take the `thunder-<org>-<env>` prefix, so the two tiers no longer share
one.

`env.sh` is the single source of truth for that name, and every in-cluster
address in this repo derives from `THUNDER_INTERNAL_URL` / `THUNDER_SVC_HOST` —
directly where a script sets a value, through `envsubst` where a script renders
a values file. Chart defaults and Go defaults stay literals, because a chart
default cannot read a shell variable, but each carries a comment naming `env.sh`
as the authority it must agree with.

**2. Both products are publishers, under one contract.** Each product owns a
bundle of bootstrap documents it alone edits — Agent Manager's ship in its
chart, AEP's in `deployments/single-cluster/thunder-resources/`. Every document
is prefixed so the import order is explicit, and no product edits the other's.

**3. Singletons are composed by the installer, not owned by a publisher.** The
platform IdP has three server-wide `server_config` documents — `cors`,
`defaultResourceServer` and `csp` — and exactly one of each. `setup-thunder.sh`
emits a platform-owned document for every one of them, numbered to sort after
Agent Manager's, and computes its value at install time from what the rendered
chart and `thunder-resources/` declare.

What "composed" means differs by singleton, because what each product knows
differs. `cors` is a **union**: each product declares only its own browser
origins, neither file changes when the other adds one, and the result is a
superset by construction — it cannot drop an origin. `defaultResourceServer`
and `csp` are **adopted**: the value is Agent Manager's, unchanged, because
changing the IdP's default resource server or the CSP its own gate and console
pages are served under would change amp-console and amp-api behaviour. What the
composition buys there is not the value but the ownership — a publisher that
renumbers a document cannot take the singleton over, and a singleton Agent
Manager stops declaring is dropped from the bundle rather than imported with a
value the platform invented. `verify-convergence.sh` check 13 asserts each of
these against the merged ConfigMap.

**4. Every `scope=system` mint names the System resource server.** ThunderID
resolves a requested scope against a resource server, and the *default* resource
server on a converged cluster is Agent Manager's, which does not define
`system`. A mint without a `resource` indicator therefore gets a 200 and a token
with no scope, and every admin call afterwards 403s with nothing in either
response explaining why. So every system-token request — aep-api, the
`thunder-app` operator, `seed-test-users.sh`, `verify-convergence.sh` — sends
`resource=<public IdP URL>/mcp` explicitly rather than relying on a default
that belongs to somebody else.

**5. The IdP's public hostname resolves to its own HTTPS gateway inside the
cluster.** An environment Thunder trusts the platform IdP as an issuer, and
ThunderID will fetch a trusted issuer's JWKS over HTTPS only — so the URL it is
given is `https://<public host>:8443/oauth2/jwks`, the hostname the certificate
is actually issued for. Inside the cluster that hostname otherwise resolves,
via CoreDNS's `openchoreo.override`, to the DATA-plane gateway, which serves
nothing for it on 8443; the fetch times out and the trust is never established.
`ensure_platform_idp_in_coredns` (`utils.sh`) installs a `rewrite stop` to the
chart's own control-plane HTTPS Gateway Service. Its ConfigMap key is
`0-platform-idp.override` because CoreDNS imports fragments in name order and
`rewrite stop` is first-match — it has to sort before `openchoreo.override` or
the broader rule swallows the name first.

## Consequences

**The neutral name needs the subchart's fullname pinned.** The `thunderid`
subchart derives object names Helm-style: a release whose name contains
"thunder" keeps it, any other gets `-thunder` appended. `amp-thunder-extension`
matched by accident; `platform-idp` does not, so `setup-thunder.sh` sets
`thunder.fullnameOverride=${THUNDER_RELEASE}` — the names the scripts derive are
the names the chart creates, and the wrapper's HTTPS route points at a Service
that exists.

**Renaming the release is not an in-place upgrade.** A different release name is
a different Helm release: the PVC, and therefore every user, client and consent
in the IdP's database, does not follow it. Moving an existing cluster to the new
name means a teardown and a fresh `setup.sh`, exactly as a change of IdP would.
There is no migration path and none is offered, because the local cluster is
disposable and the alternative — a rename that silently starts an empty IdP
while the old one still holds the data — is worse.

**Agent Manager's charts default to their own Thunder, and every one of those
defaults is now overridden.** `wso2-agent-manager`
(`keyManager.jwksUrl`, `oidc.tokenUrl`, `thunder.resolveToHost`),
`wso2-amp-api-platform-gateway-extension` (`agentManager.idp.tokenUrl` and the
ThunderKeyManager entry in `apiGateway.config.policyConfigurations.jwtauth_v1.
keymanagers`), `wso2-amp-observability-extension` (`observer.idpTokenUrl`,
`auth.jwksUrl`) and `wso2-amp-evaluation-extension` (`publisher.idpTokenUrl`,
and the NetworkPolicy's `evaluationJob.idp.namespace`) all name
`amp-thunder-extension-service.amp-thunder` out of the box. Helm accepts an
unknown `--set` path without complaint, so a typo in one of these paths is
indistinguishable from a correct override until something 401s at runtime. Each
was verified by rendering the chart and grepping the output for both addresses;
that check has to be repeated when `AMP_VERSION` moves.

**The NetworkPolicy one fails silently and differently.** The evaluation job's
egress to the IdP is allowed by NAMESPACE. Miss it and the token request is
simply denied egress: the job times out with nothing naming the policy.

**Agent Manager's `add-environment-thunder.sh` no longer finds the certificate
it waits for.** It waits on `certificate/amp-thunder-extension-local-tls` when
that object exists, and falls back to reading the chart's root CA secret — which
is hardcoded regardless of release name — when it does not. With the neutral
name the wait is skipped and the fallback is taken, which is correct but removes
a barrier against racing cert-manager. `setup-thunder.sh` therefore waits on
`certificate/${THUNDER_RELEASE}-local-tls` itself, before the environment step
can run.

**The teardown matches on the chart, not the prefix alone.** With environment
Thunders now `thunder-<org>-<env>`, a prefix match would also catch the platform
IdP and the `thunder-app` operator. Selection requires the release to be a
`thunderid-*` chart as well, and `THUNDER_NS` is protected outright. The legacy
`amp-thunder-<org>-<env>` prefix is still matched so an older cluster is cleaned
up.

**What did not change.** There is still one IdP, still installed
unconditionally, still bootstrapped by a merge rather than a second source, and
`thunderHostBaseDomain` is still Agent Manager's `amp.localhost` — the
per-environment tier is not shared and its hostnames are not the platform's
business.

## Alternatives rejected

**Leave the name and document the ownership.** The name is the documentation
most people read: it is what `kubectl get ns` shows and what a values file
spells. A comment explaining that `amp-thunder` is not Agent Manager's loses to
the name every time — and it would have left the two tiers sharing a prefix.

**Give AEP its own copy of every singleton document and let import order
decide.** That is what the CORS document was, and it is the failure this ADR
removes: a value one product must maintain on the other's behalf, which is
silently wrong the moment the other changes.

**Have the installer own the singletons outright, declared in neither product's
bundle.** That is what happens for `defaultResourceServer` and `csp`, where
there is nothing per-product to declare. It does not work for `cors`: the
platform would have to know each product's browser origins — the one thing each
product actually knows about itself. So `cors` stays declared by its publishers
and only unioned by the platform.
