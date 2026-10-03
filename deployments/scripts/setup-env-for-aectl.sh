#!/bin/bash
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# Standalone OpenChoreo v1.2.5 k3d install for `aectl platform install` —
# Thunder swapped for ThunderID 1.0.0, plus the WSO2 API Platform operator
# `aectl` requires that the official guide doesn't install at all.
#
# This is the official k3d single-cluster guide:
#   https://openchoreo.dev/docs/getting-started/try-it-out/on-k3d-locally/
# with three deviations, each called out at its own step below:
#   1. the "Install ThunderID Identity Provider" sub-step of Step 3 is
#      replaced (see "The Thunder step" below)
#   2. a WSO2 API Platform operator install is added at the end of Step 2 —
#      the official page has no equivalent step; it satisfies `aectl platform
#      install`'s checkAPIPlatform prerequisite (see that step's own comment)
#   3. Step 2's CoreDNS rewrite is extended to *.openchoreoapis.localhost, so
#      pods can reach deployed endpoints by their public URL
# Step 4's sample resources are applied as published: the environment `aectl
# platform install` configures gateway ingress on is whichever one
# oc.pipeline_source_environment names (`development` on this sample), imported
# with `aectl platform config import` before the install runs.
# Every other step (cluster, prerequisites, control/data/workflow/
# observability planes, samples) is copied verbatim from the official page,
# version for version. It has NO dependency on this repo's own
# `deployments/scripts/` install chain (setup.sh, setup-thunder.sh, Agent
# Manager, ...) — it stands alone and produces a plain upstream OpenChoreo
# cluster, which is what a bare `aectl` (without AEP's own platform install)
# targets.
#
# ── The Thunder step ──────────────────────────────────────────────────────
#
# The official page installs Thunder from
#   oci://ghcr.io/asgardeo/helm-charts/thunder   version 0.28.0
# bootstrapped with imperative shell scripts (curl calls against Thunder's
# REST API) baked into values-thunder.yaml.
#
# ThunderID 1.0.0 ships from a different chart in a different registry:
#   oci://ghcr.io/thunder-id/helm-charts/thunderid   version 1.0.0
# Its bootstrap mechanism is `bootstrap.configMap` (declarative YAML
# "resource_type" documents, imported in-process) rather than
# `bootstrap.scripts` (shell). Its database config has 4 logical DBs
# (config/runtime_transient/entity/runtime_persistent) rather than 3
# (config/runtime/user).
#
# Everything below that isn't the Thunder install itself is UNCHANGED from the
# official page — same OpenChoreo version (v1.2.5 / release-v1.2), same
# prerequisite charts, same control/data/workflow/observability plane installs,
# same sample resources.
#
# ── What "full parity" means here ────────────────────────────────────────
#
# The official bootstrap scripts create: a default org unit + "openchoreo-user"
# schema, 4 users (admin/developer/platform-engineer/sre) + 4 groups, and 11
# OAuth applications (Backstage, Customer Portal, RCA Agent, OpenChoreo CLI,
# System App, User MCP App, Service MCP App, Workload Publisher, Observer
# Resource Reader, FinOps Agent, MCP E2E Subject) — all with FIXED client
# ids/secrets that the rest of the official docs (OpenBao's pre-seeded
# secrets, the observability/workflow-template installs) assume exist.
#
# This script recreates the same accounts, groups and applications — same
# client ids and secrets — as declarative documents instead of shell scripts,
# so every later official-docs step still works unmodified. The default org
# unit, the "Person" user type (username/email/given_name/family_name/
# password), and the built-in "System" resource server are NOT recreated: the
# chart already ships them baked into the image
# (backend/cmd/server/bootstrap/01-default-resources.yaml), and
# `bootstrap.configMap.files` mounts our documents ALONGSIDE those shipped
# defaults rather than replacing them.
#
# ── Open judgment calls ────────────────────────────────────────────────────
#
#  1. Application `type` for public/PKCE clients (OpenChoreo CLI, User MCP
#     App) is set to "browser" below, by analogy with this repo's own public
#     PKCE console client (single-cluster/thunder-resources/87-aep-console-app.yaml).
#     If the import job fails with APP-1040 ("invalid application type"), try
#     "mobile" for these two documents instead.
#  2. ThunderID 1.0.0 enforces a deny-first Content-Security-Policy by
#     default. This script does NOT ship a `csp` server_config document,
#     because the CDN allow-list this repo's own platform IdP uses
#     (single-cluster/thunder-resources/91-platform-csp.yaml) is specific to
#     AEP's own console, not vanilla ThunderID's /gate login page. If the
#     hosted login page renders unstyled or fails to load fonts/images, add a
#     `csp` server_config document for it (see that file for the mechanism).
#
# Usage: AE_DOMAIN=localhost bash deployments/scripts/setup-env-for-aectl.sh
#   AE_DOMAIN               REQUIRED. The DNS suffix every hostname on this
#                           cluster is published under — "localhost" for local
#                           k3d, "<ip>.sslip.io" or a wildcard domain you own
#                           for a cluster other machines reach. Pass the same
#                           value to setup-agent-manager.sh.
#   WITH_OC_PORTAL=0        leave OpenChoreo's Backstage portal out of the
#                           control plane. AEP reads nothing from it, and its
#                           liveness probe is too tight for its own startup on
#                           a cold cluster — see Step 3.
#   WITH_BUILD=0            skip the workflow plane (Step 6, optional upstream)
#   WITH_OBSERVABILITY=0    skip the observability plane (Step 7, optional upstream)
#   WITH_SKAFFOLD_CLIENT=1  also bootstrap ae-install-client (see step 3b) — the
#                           `make dev-env` local-dev path's own admin client for
#                           `aectl platform install`. Off by default: a bare
#                           `aectl` install brings its own bootstrap client some
#                           other way and must not silently pick this one up.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ============================================================================
# Versions — everything except THUNDER_* is copied straight from the official
# page for release-v1.2 / v1.2.5 (the minimum AEP's aectl enforces — see
# tools/aectl/cmd/platform.go's minOCVersion). Bump OC_BRANCH/OC_VERSION together if you track a newer release; the
# Thunder coordinates are independent of both.
# ============================================================================
OC_BRANCH="release-v1.2"
OC_VERSION="1.2.5"
CLUSTER_NAME="openchoreo"
CLUSTER_CONTEXT="k3d-${CLUSTER_NAME}"

# Dev control-plane gateway https listener (SRE agent -> aep-api's handoff, see
# deployments/scripts/setup-sre.sh / tools/aectl/cmd/sre.go's --mcp-hostname).
# The wildcard covers every *.openchoreo.localhost hostname the gateway
# fronts (aep-mcp, observer, rca-agent, ...), issued off the control-plane
# chart's own "cluster-gateway-ca" CA via its "cluster-gateway-selfsigned-
# issuer" Issuer (both chart-managed, created unconditionally — see
# install/helm/openchoreo-control-plane/templates/cluster-gateway/{issuer,
# ca-certificate,selfsigned-issuer}.yaml in the openchoreo/openchoreo repo).
DEV_GATEWAY_TLS_HOSTNAME="*.openchoreo.localhost"
DEV_GATEWAY_TLS_SECRET="openchoreo-gateway-wildcard-tls"
DEV_GATEWAY_CA_ISSUER="cluster-gateway-selfsigned-issuer"

THUNDER_CHART="oci://ghcr.io/thunder-id/helm-charts/thunderid"
THUNDER_VERSION="1.0.0"
# The ThunderID chart's own default username, kept rather than an email-shaped
# one: this is typed at every sign-in on a dev cluster. `sub` follows it — the
# chart's bootstrap sets the admin's subject claim to the username.
THUNDER_ADMIN_USER="admin"
THUNDER_ADMIN_PASSWORD="Admin@123"

WITH_BUILD="${WITH_BUILD:-1}"
WITH_OBSERVABILITY="${WITH_OBSERVABILITY:-1}"
WITH_SKAFFOLD_CLIENT="${WITH_SKAFFOLD_CLIENT:-0}"

# ============================================================================
# AE_DOMAIN — the DNS suffix every hostname on this cluster is published under
# ============================================================================
#
# "localhost" reproduces the k3d install exactly; anything else re-domains the
# whole cluster in one value (e.g. 10.0.0.5.sslip.io on a shared VM, or a
# wildcard record you own). Every product hostname is COMPOSED from it below,
# never substituted for the bare token "localhost" — see the derived block.
#
# Deliberately required rather than defaulted. ThunderID's bootstrap bundle is
# read once, by a pre-install hook Job that then deletes itself, so a silent
# default is not a wrong flag you can re-run past: it is a cluster that has to
# be deleted and rebuilt. Failing here costs nothing and happens before the
# first object is created.
: "${AE_DOMAIN:?set AE_DOMAIN — the DNS suffix for this cluster.
   local k3d : AE_DOMAIN=localhost
   shared VM : AE_DOMAIN=<ip>.sslip.io, or a wildcard domain you control
   Hostnames are composed onto it (console.ae.\$AE_DOMAIN,
   thunder.openchoreo.\$AE_DOMAIN, ...), so pass the SUFFIX only.}"

case "$AE_DOMAIN" in
    *://*)  echo "❌ AE_DOMAIN is a domain, not a URL: ${AE_DOMAIN}" >&2; exit 1 ;;
    *:*)    echo "❌ AE_DOMAIN must not carry a port: ${AE_DOMAIN}" >&2; exit 1 ;;
    */*)    echo "❌ AE_DOMAIN must not carry a path: ${AE_DOMAIN}" >&2; exit 1 ;;
    .*|*.)  echo "❌ AE_DOMAIN must not start or end with a dot: ${AE_DOMAIN}" >&2; exit 1 ;;
esac

# The five parents are structure, not configuration: charts, CoreDNS rewrites
# and Agent Manager's own composed origins all expect these exact labels. Only
# the suffix moves. With AE_DOMAIN=localhost every value below is identical to
# what this script produced before it took a parameter.
OC_DOMAIN="openchoreo.${AE_DOMAIN}"          # thunder, api, observer, portal
DP_INGRESS_HOST="openchoreoapis.${AE_DOMAIN}" # ClusterDataPlane external ingress
AE_CONSOLE_DOMAIN="ae.${AE_DOMAIN}"          # console, tryit

# The environment tier's identity provider. aectl installs it behind the
# CONTROL-plane gateway (envidp's thunderChartSpec pins httproute.parentRefs
# to gateway-default/openchoreo-control-plane), so it belongs on the same
# certificate and listener as everything else here — unlike that environment's
# API gateway, which is a separate Gateway on the data plane.
#
# The name follows the Environment that aectl provisions into, which is the
# source of DeploymentPipeline/default's promotion graph — "development" on
# the guide's own samples, and what oc.pipeline_source_environment says.
AE_ENV="${AE_ENV:-development}"
OC_ENV_IDP_HOST="${AE_ENV}-idp.${OC_DOMAIN}"

# The Environment's NAMESPACE — a different axis from its name, and the one
# aectl's oc.default_org_namespace sets. Both appear in the data-plane
# hostnames, which the ComponentType templates and the environment gateway
# build as "<env>-<org>.<suffix>" — environment first.
AE_ORG="${AE_ORG:-default}"
export AE_DOMAIN OC_DOMAIN DP_INGRESS_HOST AE_CONSOLE_DOMAIN AE_ENV AE_ORG OC_ENV_IDP_HOST

# Dots escaped for CoreDNS's `name regex`, computed once here rather than
# inside the heredoc that uses it — expansion and backslashes in an unquoted
# heredoc are a bad combination to debug.
OC_DOMAIN_RE="${OC_DOMAIN//./\\.}"

# ============================================================================
# WITH_TLS — whether the public endpoints are served over HTTPS
# ============================================================================
#
# Off reproduces the plain-HTTP cluster exactly. On is not a preference: a
# browser exposes crypto.subtle only in a secure context, and the console's
# OIDC login needs it for PKCE, so on any domain other than localhost the
# sign-in cannot complete without TLS. (*.localhost is a secure context by
# definition, which is why the local flow never needed this.)
#
# One toggle drives the scheme AND both gateway ports, because they move
# together: a cluster does not serve https on 8080. The ports are the ones
# k3d already publishes for each plane's TLS listener.
# Shared with setup-agent-manager.sh, which installs against the gateways this
# script provisions but runs from its own shell — the two must not drift.
# shellcheck source=lib/tls-env.sh
. "${SCRIPT_DIR}/lib/tls-env.sh"
# The ClusterDataPlane's ingress names the listener as well as the port, and
# the ComponentType templates build every component's HTTPRoute from it — so
# a mismatch here does not fail the install, it publishes endpoints on a
# listener that is not serving them.
export WITH_TLS SCHEME CP_PORT DP_PORT DP_LISTENER

# Let's Encrypt cannot answer a challenge for a name that resolves to a
# private address, and there is nothing to secure on a loopback-only cluster,
# so the combination is refused rather than half-configured.
if [ "$WITH_TLS" = "1" ] && [ "$AE_DOMAIN" = "localhost" ]; then
    echo "❌ WITH_TLS=1 with AE_DOMAIN=localhost." >&2
    echo "   *.localhost is already a secure context in browsers, and no CA will" >&2
    echo "   issue for it. Use WITH_TLS=0 locally." >&2
    exit 1
fi

# The ACME account is per-cluster and its address receives expiry warnings.
# Empty registers without one, which Let's Encrypt allows.
ACME_EMAIL="${ACME_EMAIL:-}"
# Staging issues untrusted certs from a CA with far higher rate limits — the
# right target while iterating on this path, because sslip.io is NOT on the
# Public Suffix List, so every sslip.io user on the internet shares one
# 50-certificates-per-week bucket for the production CA.
ACME_SERVER="${ACME_SERVER:-https://acme-v02.api.letsencrypt.org/directory}"
TLS_SECRET_NAME="ae-public-tls"

# Where a generated ae-install-client secret is kept. Root-only, and written
# as soon as it is generated — see the guard at step 3c for why losing it
# costs a rebuild rather than a retry.
AE_INSTALL_CLIENT_SECRET_FILE="${AE_INSTALL_CLIENT_SECRET_FILE:-/root/.ae-install-client-secret}"

# What a bare "localhost" must keep meaning: a loopback address on the
# OPERATOR's own machine, not on this cluster. Backstage's dev origin
# (:7007), the AMP console's (:3000) and the CLI redirect URIs (:8075,
# :33418, :33419) are all of that kind, which is why no substitution here
# touches the token itself.

RAW="https://raw.githubusercontent.com/openchoreo/openchoreo/${OC_BRANCH}"

echo "============================================"
echo "  OpenChoreo ${OC_VERSION} on k3d — Thunder swapped for ThunderID ${THUNDER_VERSION}"
echo "============================================"

# ============================================================================
# Prerequisites check (official page, Prerequisites section)
# ============================================================================
for bin in docker k3d kubectl helm; do
    command -v "$bin" >/dev/null 2>&1 || { echo "❌ $bin not found on PATH"; exit 1; }
done
docker info >/dev/null 2>&1 || { echo "❌ Docker is not running"; exit 1; }

# ============================================================================
# Step 1: Create the cluster (official page, Step 1; K3D_FIX_DNS=0 on Colima)
# ============================================================================
echo ""
echo "1️⃣  Creating the k3d cluster"
if ! k3d cluster list "${CLUSTER_NAME}" >/dev/null 2>&1; then
    # The one deviation in this step: on Colima, k3d's DNS fix points the node's
    # resolver at the VM gateway, whose DNAT to Docker's embedded DNS never
    # answers inside the k3s node, so containerd cannot pull even the pause
    # image and every pod sits in ContainerCreating. K3D_FIX_DNS=0 keeps Docker's
    # embedded DNS. https://github.com/k3d-io/k3d/issues/1449
    if docker info --format '{{.Name}}' 2>/dev/null | grep -qi colima; then
        export K3D_FIX_DNS=0
    fi
    # The guide's config publishes 8080/8443/19080/19443/... but not 80, and
    # ACME's HTTP-01 challenge is only ever served on 80 — the CA fetches
    # http://<name>/.well-known/acme-challenge/<token> and the protocol allows
    # no other port. Without this mapping cert-manager's solver is reachable
    # from inside the cluster and from nowhere else, so every order times out.
    #
    # Added to the fetched config rather than by `k3d cluster edit` after the
    # fact: port mappings belong to the cluster's definition, and an edit
    # recreates the load balancer anyway.
    CLUSTER_CONFIG="$(mktemp)"
    curl -fsSL "${RAW}/install/k3d/single-cluster/config.yaml" -o "$CLUSTER_CONFIG"
    if [ "$WITH_TLS" = "1" ]; then
        python3 - "$CLUSTER_CONFIG" <<'PY'
import sys, yaml
path = sys.argv[1]
cfg = yaml.safe_load(open(path))
ports = cfg.setdefault("ports", [])
if not any(p.get("port", "").startswith("80:") for p in ports):
    ports.append({"port": "80:80", "nodeFilters": ["loadbalancer"]})
yaml.safe_dump(cfg, open(path, "w"), default_flow_style=False, width=10**6)
PY
        echo "   + port 80 published (ACME HTTP-01)"
    fi
    k3d cluster create --config="$CLUSTER_CONFIG"
    rm -f "$CLUSTER_CONFIG"
else
    echo "⏭️  Cluster '${CLUSTER_NAME}' already exists"
fi
kubectl config use-context "${CLUSTER_CONTEXT}"

# ============================================================================
# Step 2: Prerequisites (official page, Step 2 — unchanged)
# ============================================================================
echo ""
echo "2️⃣  Prerequisites"

# COLD_PULL_TIMEOUT budgets the --wait installs below, which on a freshly
# created cluster are waiting on image pulls into an empty containerd rather
# than on the charts themselves. Sized against a measured cold pull on a slow
# link (external-secrets v2.0.1, 83MB, 9m01s — roughly 150KB/s), not against
# the warm-cache case where each of these becomes ready in seconds. A --wait
# that is already satisfied returns immediately, so a generous budget costs
# nothing on a fast link and is the difference between a completed install
# and a spuriously aborted one on a slow one.
COLD_PULL_TIMEOUT="${COLD_PULL_TIMEOUT:-20m}"

echo "   Gateway API CRDs"
kubectl apply --server-side \
  -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml

echo "   cert-manager"
# config.enableGatewayAPI is what lets cert-manager solve an HTTP-01 challenge
# on this cluster at all. It answers a challenge by starting a solver pod and
# routing the challenge path to it — as an Ingress where an ingress controller
# exists, as an HTTPRoute where Gateway API does. There is no ingress
# controller here (OpenChoreo routes through kgateway), so without this the
# order retries until it expires, with nothing in the logs but a challenge
# stuck pending.
#
# Not to be confused with the ExperimentalGatewayAPISupport feature gate,
# which this chart already defaults to true: the gate compiles the support in,
# this switch turns it on.
CERT_MANAGER_SET=()
if [ "$WITH_TLS" = "1" ]; then
    CERT_MANAGER_SET=(--set "config.apiVersion=controller.config.cert-manager.io/v1alpha1"
                      --set "config.kind=ControllerConfiguration"
                      --set "config.enableGatewayAPI=true")
fi
#
# Expanded as ${A[@]+"${A[@]}"} rather than "${A[@]}": under `set -u` bash 3.2
# — which is what /bin/bash still is on macOS, so it is what `make dev-env`
# runs — treats an EMPTY array's expansion as an unbound variable and aborts.
# WITH_TLS=0 is exactly the case that leaves it empty, so the plain form breaks
# the local path while working everywhere TLS is on.
helm upgrade --install cert-manager oci://quay.io/jetstack/charts/cert-manager \
  --namespace cert-manager --create-namespace --version v1.19.4 \
  --set crds.enabled=true ${CERT_MANAGER_SET[@]+"${CERT_MANAGER_SET[@]}"} \
  --wait --timeout "${COLD_PULL_TIMEOUT}"

echo "   External Secrets Operator"
helm upgrade --install external-secrets oci://ghcr.io/external-secrets/charts/external-secrets \
  --namespace external-secrets --create-namespace --version 2.0.1 \
  --set installCRDs=true --wait --timeout "${COLD_PULL_TIMEOUT}"

echo "   kgateway"
helm upgrade --install kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds \
  --create-namespace --namespace openchoreo-control-plane --version v2.3.1
helm upgrade --install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway \
  --namespace openchoreo-control-plane --create-namespace --version v2.3.1

echo "   OpenBao"
helm upgrade --install openbao oci://ghcr.io/openbao/charts/openbao \
  --namespace openbao --create-namespace --version 0.25.6 \
  --values "${RAW}/install/k3d/common/values-openbao.yaml" \
  --wait --timeout "${COLD_PULL_TIMEOUT}"

echo "   ClusterSecretStore"
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-secrets-openbao
  namespace: openbao
---
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: default
spec:
  provider:
    vault:
      server: "http://openbao.openbao.svc:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "openchoreo-secret-writer-role"
          serviceAccountRef:
            name: "external-secrets-openbao"
            namespace: "openbao"
EOF

echo "   CoreDNS rewrite"
# The guide's own coredns-custom.yaml, with the suffix taken from AE_DOMAIN.
# Applied from here rather than fetched because upstream's copy hardcodes
# "openchoreo.localhost", which is the one thing that moves. For
# AE_DOMAIN=localhost this ConfigMap is byte-identical to theirs.
#
# What it buys: a pod resolving a cluster hostname gets host.k3d.internal —
# the k3d loadbalancer — rather than its own loopback or (on a re-domained
# cluster) a round trip out to the node's external address.
#
# setup-agent-manager.sh merges its own rewrites into this same ConfigMap and
# key, so the name and the "<suffix>.override" data key are a contract
# between the two scripts.
kubectl apply -f - <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns-custom
  namespace: kube-system
data:
  openchoreo.override: |
    rewrite stop {
      name regex (.+\\.)?${OC_DOMAIN_RE} host.k3d.internal
      answer auto
    }
EOF

# The one deviation in this step: the official override covers only
# *.openchoreo.localhost, not *.openchoreoapis.localhost, the data plane's
# gateway host that every deployed endpoint URL is built on. Without it no pod
# can reach a deployed endpoint by its public URL: the runner's endpoint
# preflight (runners/remote-worker ADR-0006) fails ENOTFOUND and validation
# never starts. Rewritten to host.k3d.internal like the official key, so the
# request hairpins through the k3d load balancer with its Host header intact.
# Its own key beside the official one, checked on every run: the apply above
# prunes it on a re-run once setup-agent-manager.sh's own apply of this
# ConfigMap has recorded it.
#
# Built from DP_INGRESS_HOST rather than the literal openchoreoapis.localhost:
# that suffix is only correct when AE_DOMAIN is localhost, and on a re-domained
# cluster the data plane's gateway host is openchoreoapis.<AE_DOMAIN>. Left
# literal, the rewrite matches nothing there and reintroduces exactly the
# ENOTFOUND this key exists to prevent — silently, because a rewrite that
# matches no name is not an error.
OC_APIS_REWRITE="rewrite stop {
  name regex (.+\\.)?${DP_INGRESS_HOST//./\\.} host.k3d.internal
  answer auto
}"
if [ "$(kubectl get cm coredns-custom -n kube-system -o jsonpath='{.data.openchoreoapis\.override}')" != "$OC_APIS_REWRITE" ]; then
    kubectl patch cm coredns-custom -n kube-system --type merge \
        -p "$(V="$OC_APIS_REWRITE" python3 -c 'import json,os; print(json.dumps({"data": {"openchoreoapis.override": os.environ["V"]}}))')"
    kubectl -n kube-system rollout restart deployment/coredns
    kubectl -n kube-system rollout status deployment/coredns --timeout=120s
fi

# ── WSO2 API Platform operator ──────────────────────────────────────────────
# Not part of the official k3d guide. `aectl platform install` requires the
# gateway.api-platform.wso2.com CRD group to be registered
# (tools/aectl/cmd/platform_apiplatform.go's checkAPIPlatform). This step
# installs the operator that registers it, at the same chart/version pins as
# deployments/scripts/setup-prerequisites.sh.
#
# This installs the OPERATOR only: a shared, cluster-wide controller. It does
# NOT create an APIGateway CR or run an actual gateway instance — OpenChoreo
# runs one gateway per (org, environment), created later by
# deployments/scripts/setup-environment-gateway.sh once that environment's own
# Thunder exists, which is out of scope for this vanilla-OC script.
echo ""
echo "   WSO2 API Platform operator"
API_PLATFORM_OPERATOR_VERSION="0.11.0"
API_PLATFORM_GATEWAY_CHART_VERSION="1.2.2"
API_PLATFORM_GATEWAY_IMAGE_VERSION="1.2.1"

API_PLATFORM_VALUES="$(mktemp)"
BOOTSTRAP_DIR="$(mktemp -d)"
UPSTREAM_VALUES_DIR="$(mktemp -d)"
trap 'rm -rf "$BOOTSTRAP_DIR" "$UPSTREAM_VALUES_DIR"; rm -f "$API_PLATFORM_VALUES"' EXIT

# oc_values fetches one of the guide's own values files and re-domains it,
# printing the path to the local copy. The guide hardcodes
# "openchoreo.localhost" in values-cp.yaml (the portal base URL, the API and
# gateway hostnames, and all four IdP URLs) and values-op.yaml (the observer's
# own HTTPRoute hostname and OBSERVER_BASE_URL, the RCA and FinOps agent
# hostnames, the auth server). Passed unmodified those would leave the control
# plane addressing an issuer nothing serves and the ClusterObservabilityPlane's
# observerURL pointing at a host with no route — the plane installs green and
# every build log and archived cycle log comes back empty.
#
# Only the "openchoreo." parent is rewritten, for the reason the derived block
# above gives. At AE_DOMAIN=localhost this is a literal no-op, so the file is
# byte-identical to what upstream publishes.
#
# Written through a temporary file and moved into place, and every step is
# checked. Two reasons, both of which bite silently:
#
#   * `sed -i` is not portable. GNU takes a bare -i; BSD/macOS reads the next
#     argument as the backup suffix and then misparses the script. This runs on
#     whatever machine the operator is on.
#   * A failure here cannot propagate. The caller uses $(oc_values ...) in an
#     argument, and a non-zero command substitution neither trips `set -e` nor
#     stops the helm call — it would install against a half-written file, or
#     worse, a STALE one left by an earlier run. Emitting nothing on failure is
#     what makes the caller fail loudly instead.
oc_values() {
    local rel="$1" out="${UPSTREAM_VALUES_DIR}/$(basename "$1")" tmp
    tmp="$(mktemp "${out}.XXXXXX")" || {
        echo "❌ could not create a temporary file for ${rel}" >&2; return 1; }
    if ! curl -fsSL "${RAW}/install/k3d/${rel}" \
        | sed "s/openchoreo\\.localhost/${OC_DOMAIN}/g" > "$tmp"; then
        rm -f "$tmp"
        echo "❌ could not fetch or rewrite ${RAW}/install/k3d/${rel}" >&2
        return 1
    fi
    # With TLS the control-plane URLs in these files have to move scheme AND
    # port, not just domain. values-cp.yaml carries OpenChoreo's four IdP
    # URLs (issuer, jwks, authorize, token) and its own baseUrl; the issuer
    # there must equal what Thunder stamps into `iss` or the control plane
    # rejects every token the platform presents.
    #
    # Scoped to :8080 on this cluster's own domain so the observability
    # plane's 11080 endpoints and any loopback URL are left alone.
    if [ "$WITH_TLS" = "1" ]; then
        if ! sed "s|http://\\([A-Za-z0-9.-]*\\)${OC_DOMAIN}:8080|${SCHEME}://\\1${OC_DOMAIN}:${CP_PORT}|g" \
            "$tmp" > "${tmp}.tls"; then
            rm -f "$tmp" "${tmp}.tls"
            echo "❌ could not apply the TLS rewrite to ${rel}" >&2
            return 1
        fi
        mv "${tmp}.tls" "$tmp" || {
            rm -f "$tmp" "${tmp}.tls"
            echo "❌ could not replace ${rel} with its TLS rewrite" >&2
            return 1; }
    fi
    mv "$tmp" "$out" || {
        rm -f "$tmp"
        echo "❌ could not move ${rel} into place" >&2
        return 1; }
    echo "$out"
}

# Same rewrite for a manifest applied straight from upstream rather than passed
# to helm as values.
#
# generate-workload-k3d.yaml is why this exists. Its build step does NOT address
# the control plane by hostname — it connects to host.k3d.internal:8080 and
# routes with an explicit `Host:` header, and that header ships hardcoded as
# `thunder.openchoreo.localhost` / `api.openchoreo.localhost`. On a re-domained
# cluster those names match no HTTPRoute, so the gateway answers 404, curl
# --fail-with-body exits 22, and the build dies at "Requesting OAuth token"
# naming neither the domain nor the header. The component is created and the
# image is built; only the workload publish fails, so the console sits on
# "building" with a red step and no cause in it.
#
# The URL stays http://host.k3d.internal:8080 under WITH_TLS=1 on purpose: the
# control-plane Gateway keeps its 8080 listener beside 8443, this hop is a build
# pod reaching the node over the docker bridge rather than anything crossing the
# public network, and pointing it at 8443 would mean the pod validating a public
# certificate for a name (host.k3d.internal) the certificate cannot carry.
oc_manifest() {
    local rel="$1" out="${UPSTREAM_VALUES_DIR}/$(basename "$1")" tmp
    # Same temporary-file discipline as oc_values, and for the same reason: the
    # caller substitutes this into `kubectl apply -f`, where a failure here
    # would otherwise apply a half-written or stale manifest.
    tmp="$(mktemp "${out}.XXXXXX")" || {
        echo "❌ could not create a temporary file for ${rel}" >&2; return 1; }
    if ! curl -fsSL "${RAW}/${rel}" \
        | sed "s/openchoreo\\.localhost/${OC_DOMAIN}/g" > "$tmp"; then
        rm -f "$tmp"
        echo "❌ could not fetch or rewrite ${RAW}/${rel}" >&2
        return 1
    fi
    mv "$tmp" "$out" || {
        rm -f "$tmp"
        echo "❌ could not move ${rel} into place" >&2
        return 1; }
    echo "$out"
}
cat > "$API_PLATFORM_VALUES" <<'YAML'
# Bumped for the Agent Manager convergence (operator 0.6.0 -> 0.11.0, gateway
# chart 1.0.1 -> 1.2.2). What changed in the gateway chart's value schema:
#   * `gateway.router` and `gateway.policyEngine` are GONE — 1.2.x ships only
#     `controller` and `gatewayRuntime`. Their old image-tag overrides were
#     silently dead keys and have been removed.
#   * `gateway.developmentMode` is GONE. It used to auto-generate the AES-GCM
#     at-rest encryption key on first boot; the key must now be
#     pre-provisioned — not done here, since no gateway instance is created by
#     this script (see the comment above this step).
#   * The controller's liveness/readiness probes already default to
#     /api/admin/v1/health on the `admin` port, so no probe overrides are needed.
gateway:
  helm:
    chartName: "oci://ghcr.io/wso2/api-platform/helm-charts/gateway"
    # chartVersion comes from API_PLATFORM_GATEWAY_CHART_VERSION via --set.
  values:
    gateway:
      controller:
        image:
          repository: ghcr.io/wso2/api-platform/gateway-controller
          # tag comes from API_PLATFORM_GATEWAY_IMAGE_VERSION via --set.
        encryptionKeys:
          enabled: true
          secretName: api-platform-controller-aesgcm-key
          mountPath: /app/data/aesgcm-keys
      gatewayRuntime:
        image:
          repository: ghcr.io/wso2/api-platform/gateway-runtime
          # tag comes from API_PLATFORM_GATEWAY_IMAGE_VERSION via --set.
        service:
          # Pinned explicitly because the chart's DEFAULT flipped from
          # ClusterIP (1.0.1) to LoadBalancer (1.2.2). On k3d a LoadBalancer
          # Service makes klipper bind its ports as hostPorts on the node, and
          # these are 8080/8443 — the ports k3d already publishes for the
          # OpenChoreo control-plane gateway.
          type: ClusterIP
        deployment:
          # OC's component NetworkPolicies (auto-generated per Component)
          # allow ingress only from pods labeled
          # `openchoreo.dev/system-component`. Tag the runtime so
          # gateway → upstream traffic isn't blocked.
          podLabels:
            openchoreo.dev/system-component: api-gateway
YAML

helm upgrade --install api-platform-operator \
    oci://ghcr.io/wso2/api-platform/helm-charts/gateway-operator \
    --version "${API_PLATFORM_OPERATOR_VERSION}" \
    --namespace openchoreo-data-plane --create-namespace \
    --set gatewayApi.installStandardCRDs=false \
    --set "gateway.helm.chartVersion=${API_PLATFORM_GATEWAY_CHART_VERSION}" \
    --set "gateway.values.gateway.controller.image.tag=${API_PLATFORM_GATEWAY_IMAGE_VERSION}" \
    --set "gateway.values.gateway.gatewayRuntime.image.tag=${API_PLATFORM_GATEWAY_IMAGE_VERSION}" \
    --values "$API_PLATFORM_VALUES"
kubectl wait --for=condition=available deployment \
    -l app.kubernetes.io/instance=api-platform-operator \
    -n openchoreo-data-plane --timeout=180s || true

# RBAC: lets OC's cluster-agent-dataplane SA (created later, when the data
# plane installs in Step 5) reconcile RestApi CRs — k8s allows binding to a
# subject that doesn't exist yet.
kubectl apply -f - <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: wso2-api-platform-gateway-module
rules:
  - apiGroups: ["gateway.api-platform.wso2.com"]
    resources: ["restapis"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: wso2-api-platform-gateway-module
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: wso2-api-platform-gateway-module
subjects:
  - kind: ServiceAccount
    name: cluster-agent-dataplane
    namespace: openchoreo-data-plane
EOF
echo "✅ WSO2 API Platform operator at gateway-operator-${API_PLATFORM_OPERATOR_VERSION}"

# ============================================================================
# Step 3: Control Plane (official page, Step 3)
# 3a and 3d are unchanged. 3b (Thunder) and 3c (entitlement claim) are new —
# see the header comment for why.
# ============================================================================
echo ""
echo "3️⃣  Control Plane"

echo "   backstage-secrets"
kubectl apply -f - <<EOF
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: backstage-secrets
  namespace: openchoreo-control-plane
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: backstage-secrets
  data:
  - secretKey: backend-secret
    remoteRef:
      key: backstage-backend-secret
      property: value
  - secretKey: client-secret
    remoteRef:
      key: backstage-client-secret
      property: value
  - secretKey: jenkins-api-key
    remoteRef:
      key: backstage-jenkins-api-key
      property: value
EOF

# ── 3b. ThunderID 1.0.0 (REPLACES the official page's asgardeo/thunder:0.28.0 step) ──
echo ""
echo "3️⃣.2  Installing ThunderID ${THUNDER_VERSION} (replaces the official 0.28.0 step)"

# Users + groups. `admin` is left to the chart's OWN built-in bootstrap
# document (backend/cmd/server/bootstrap/01-default-resources.yaml, id
# 01900000-0000-7000-8000-000000000030) — setting setup.admin.username/
# password below (Thunder templates {{ .ADMIN_USERNAME }}/{{ .ADMIN_PASSWORD }}
# into that document) gives it the official login without a second
# credential write, which Thunder refuses (USR-1028) on a user that already
# has one. Groups use the LITERAL names "admins"/"developers"/
# "platform-engineers"/"sres" — NOT the chart's built-in "Administrators"
# group — because openchoreo-control-plane's shipped ClusterAuthzRoleBindings
# (admin-binding, developer-binding, ...) key on entitlement.claim=groups,
# value="admins" etc. verbatim. A different group name is a silent 403, not
# an error.
cat > "${BOOTSTRAP_DIR}/70-users-and-groups.yaml" <<'YAML'
resource_type: user
id: openchoreo-developer-user
type: Person
ouHandle: default
attributes:
  username: "developer@openchoreo.dev"
  email: "developer@openchoreo.dev"
  given_name: "Developer"
  family_name: "User"
credentials:
  password: "Dev@123"
---
resource_type: user
id: openchoreo-platform-engineer-user
type: Person
ouHandle: default
attributes:
  username: "platform-engineer@openchoreo.dev"
  email: "platform-engineer@openchoreo.dev"
  given_name: "Platform"
  family_name: "Engineer"
credentials:
  password: "PE@123"
---
resource_type: user
id: openchoreo-sre-user
type: Person
ouHandle: default
attributes:
  username: "sre@openchoreo.dev"
  email: "sre@openchoreo.dev"
  given_name: "SRE"
  family_name: "User"
credentials:
  password: "SRE@123"
---
# `admin` here is the id Thunder's own 01-default-resources.yaml assigns its
# built-in admin account (01900000-0000-7000-8000-000000000030) — referencing
# it by that fixed id, not by re-declaring the user.
resource_type: group
id: openchoreo-admins-group
name: admins
description: OpenChoreo admins group
ouHandle: default
members:
  - id: "01900000-0000-7000-8000-000000000030"
    type: user
---
resource_type: group
id: openchoreo-developers-group
name: developers
description: OpenChoreo developers group
ouHandle: default
members:
  - id: openchoreo-developer-user
    type: user
---
resource_type: group
id: openchoreo-platform-engineers-group
name: platform-engineers
description: OpenChoreo platform engineers group
ouHandle: default
members:
  - id: openchoreo-platform-engineer-user
    type: user
---
resource_type: group
id: openchoreo-sres-group
name: sres
description: OpenChoreo SREs group
ouHandle: default
members:
  - id: openchoreo-sre-user
    type: user
YAML

# Applications. Client ids/secrets match the official page's literal values
# byte for byte (values-openbao.yaml's pre-seeded secrets, and the workflow
# templates the official docs apply later, both assume these exact strings).
# `ouId` (not `ouHandle`) on every application: the importer resolves the
# handle for users/groups/roles but NOT for applications — an app bootstrapped
# with `ouHandle` gets no OU at all, and its client_credentials tokens then
# carry no ouId/ouHandle claim.
DEFAULT_OU_ID="01900000-0000-7000-8000-000000000001"

cat > "${BOOTSTRAP_DIR}/71-fix-system-resource-server-identifier.yaml" <<YAML
# ThunderID's own built-in "System" resource server
# (backend/cmd/server/bootstrap/01-default-resources.yaml, id
# 01900000-0000-7000-8000-000000000020) hardcodes
# identifier: https://localhost:8090/mcp — NOT templated to this
# deployment's actual publicUrl. The native /console app derives its OWN
# OAuth resource_identifier from configuration.server.publicUrl
# ("http://thunder.${OC_DOMAIN}:8080/mcp" here), so login redirects
# back with "invalid_target" unless the two match. This document
# re-declares the SAME resource server (same id -> update, not duplicate),
# resources tree preserved verbatim, only identifier corrected.
resource_type: resource_server
id: "01900000-0000-7000-8000-000000000020"
name: System
description: System resource server
identifier: "${SCHEME}://thunder.${OC_DOMAIN}:${CP_PORT}/mcp"
ouHandle: default
resources:
  - name: System
    handle: system
    description: System resource
YAML

cat > "${BOOTSTRAP_DIR}/80-backstage-app.yaml" <<YAML
resource_type: application
id: openchoreo-backstage-client
type: fullstack
name: "Backstage"
description: "OpenChoreo Backstage Portal"
ouId: "${DEFAULT_OU_ID}"
allowedUserTypes: ["Person"]
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-backstage-client"
      clientSecret: "backstage-portal-secret"
      redirectUris: ["${SCHEME}://${OC_DOMAIN}:${CP_PORT}/api/auth/openchoreo-auth/handler/frame"]
      grantTypes: ["authorization_code","client_credentials","refresh_token"]
      responseTypes: ["code"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          userConfig:
            validityPeriod: 86400
            attributes: ["given_name","family_name","username","groups"]
        idToken:
          validityPeriod: 86400
          userAttributes: ["given_name","family_name","username","groups"]
YAML

cat > "${BOOTSTRAP_DIR}/81-customer-portal-app.yaml" <<YAML
resource_type: application
id: customer-portal-client
type: m2m
name: "Customer Portal"
description: "Customer Portal Application"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "customer-portal-client"
      clientSecret: "supersecret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/82-rca-agent-app.yaml" <<YAML
resource_type: application
id: openchoreo-rca-agent
type: m2m
name: "RCA Agent"
description: "OpenChoreo RCA Agent Client. Bound by rca-agent-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-rca-agent"
      clientSecret: "openchoreo-rca-agent-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

# type: browser is a judgment call for this public/PKCE loopback client — see
# header comment item 1. Switch to "mobile" if the import job rejects it.
cat > "${BOOTSTRAP_DIR}/83-cli-app.yaml" <<YAML
resource_type: application
id: openchoreo-cli
type: browser
name: "OpenChoreo CLI"
description: "OpenChoreo CLI Default Application"
ouId: "${DEFAULT_OU_ID}"
allowedUserTypes: ["Person"]
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-cli"
      redirectUris: ["http://127.0.0.1:55152/auth-callback"]
      grantTypes: ["authorization_code","refresh_token"]
      responseTypes: ["code"]
      tokenEndpointAuthMethod: "none"
      pkceRequired: true
      publicClient: true
      token:
        accessToken:
          userConfig:
            validityPeriod: 3600
            attributes: ["given_name","family_name","username","groups"]
        idToken:
          validityPeriod: 3600
          userAttributes: ["given_name","family_name","username","groups"]
YAML

cat > "${BOOTSTRAP_DIR}/84-system-app.yaml" <<YAML
resource_type: application
id: openchoreo-system-app
type: m2m
name: "System Application"
description: "Generic system application for automation and integrations"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-system-app"
      clientSecret: "openchoreo-system-app-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

# type: browser is the same judgment call as the CLI app above (header
# comment item 1) — this client also opens a system browser for its redirect,
# but one of its redirect URIs is a custom URL scheme (cursor://...), which is
# more typically a "mobile" app shape. Untested either way.
cat > "${BOOTSTRAP_DIR}/85-user-mcp-app.yaml" <<YAML
resource_type: application
id: user_mcp_client
type: browser
name: "User MCP App"
description: "User MCP app to be used by terminals and user facing AI agents"
ouId: "${DEFAULT_OU_ID}"
allowedUserTypes: ["Person"]
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "user_mcp_client"
      redirectUris:
        - "http://localhost:8075/callback"
        - "cursor://anysphere.cursor-mcp/oauth/callback"
        - "http://127.0.0.1:19876/mcp/oauth/callback"
      grantTypes: ["authorization_code","refresh_token"]
      responseTypes: ["code"]
      tokenEndpointAuthMethod: "none"
      pkceRequired: true
      publicClient: true
      token:
        accessToken:
          userConfig:
            validityPeriod: 86400
            attributes: ["given_name","family_name","username","groups"]
        idToken:
          validityPeriod: 86400
          userAttributes: ["given_name","family_name","username","groups"]
YAML

# No authFlowHandle here. The official payload's auth_flow_graph_id:
# auth_flow_config_basic does not translate to an authFlowHandle field on
# this schema; the client works as a plain client_credentials m2m app
# without it. Note: the importer aborts its entire batch on the first
# invalid document, so a bad field here would block every other
# user/group/application in this bundle from being created too.
cat > "${BOOTSTRAP_DIR}/86-service-mcp-app.yaml" <<YAML
resource_type: application
id: service_mcp_client
type: m2m
name: "Service MCP App"
description: "Service MCP app to be used by backend services which can securely store the secret"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "service_mcp_client"
      clientSecret: "service_mcp_client_secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_basic"
      token:
        accessToken:
          clientConfig:
            validityPeriod: 86400
YAML

cat > "${BOOTSTRAP_DIR}/87-workload-publisher-app.yaml" <<YAML
resource_type: application
id: openchoreo-workload-publisher-client
type: m2m
name: "Workload Publisher"
description: "OpenChoreo Workload Publisher Client for creating workloads from CI workflows. Bound by workload-publisher-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-workload-publisher-client"
      clientSecret: "openchoreo-workload-publisher-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/88-observer-app.yaml" <<YAML
resource_type: application
id: openchoreo-observer-resource-reader-client
type: m2m
name: "OpenChoreo Observer Resource Reader"
description: "OpenChoreo Observer Resource Reader Client. Bound by observer-resource-reader-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-observer-resource-reader-client"
      clientSecret: "openchoreo-observer-resource-reader-client-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/89-finops-agent-app.yaml" <<YAML
resource_type: application
id: openchoreo-finops-agent
type: m2m
name: "FinOps Agent"
description: "OpenChoreo FinOps Agent Client. Bound by finops-agent-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-finops-agent"
      clientSecret: "openchoreo-finops-agent-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/90-mcp-e2e-subject-app.yaml" <<YAML
resource_type: application
id: mcp-e2e-subject-client
type: m2m
name: "MCP E2E Subject"
description: "Permission-less client_credentials subject for MCP e2e authorization tests. Intentionally has NO ClusterAuthzRoleBinding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "mcp-e2e-subject-client"
      clientSecret: "mcp-e2e-subject-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

# CORS as a server_config document — REQUIRED, not optional. ThunderID 1.0.0's
# static config has no CORS section; `--set thunder.configuration.cors.*`
# writes a key nothing reads. Without this document the browser's very first
# call (fetching /.well-known/openid-configuration from the console/portal
# origin) has no Access-Control-Allow-Origin header and the sign-in silently
# fails. Origins match the official page's values-thunder.yaml.
cat > "${BOOTSTRAP_DIR}/95-cors.yaml" <<YAML
resource_type: server_config
name: cors
value:
  allowedOrigins:
    - "${SCHEME}://${OC_DOMAIN}:${CP_PORT}"
    # Backstage's own dev server, on the OPERATOR's machine — a real loopback,
    # so it stays "localhost" whatever this cluster is published as.
    - "http://localhost:7007"
YAML

# ── 3c. ae-install-client — make dev-env's own Thunder admin client ─────────
#
# `aectl platform install` authenticates to Thunder as thunder.admin_client_id
# (skaffold/defaults.yaml: "ae-install-client") to register every OTHER AEP
# OAuth app — see tools/aectl/internal/thunder/client.go's New(), which mints
# a client_credentials token before it can call any admin endpoint. That
# client therefore has to exist, and already hold Thunder's `system` scope
# plus its built-in Administrator role, BEFORE aectl ever runs — a
# chicken-and-egg aectl itself cannot resolve (there is no privileged token
# yet to create the first privileged client). Bootstrapping it here, as one
# more declarative document loaded in-process at chart install, needs no
# auth at all, which is what breaks the cycle.
#
# The role assignment below targets ThunderID's OWN built-in "Administrator"
# role (fixed id, same pattern as deployments/single-cluster/thunder-resources/
# 84-aep-system-role.yaml) rather than a role AEP owns, so ae-install-client
# holds every permission that role carries — not a hand-picked copy of them.
if [ "$WITH_SKAFFOLD_CLIENT" = "1" ]; then
# This client holds Thunder's Administrator role, and Thunder answers on the
# same public gateway as everything else — so on any cluster reachable beyond
# its own host, a secret published in this file is an admin token for anyone
# who reads the repo. It is generated instead, and printed at the end of the
# run for the `aectl platform install` that consumes it.
#
# localhost keeps the fixed literal: `make dev-env` passes exactly that string
# as AEP_THUNDER_ADMIN_CLIENT_SECRET, the cluster is reachable only from the
# machine that built it, and a generated secret there would break the one
# flow that cannot prompt for it. Set AE_INSTALL_CLIENT_SECRET to pin it
# anywhere (re-running an install against an existing Thunder, say).
if [ -z "${AE_INSTALL_CLIENT_SECRET:-}" ]; then
    if [ "$AE_DOMAIN" = "localhost" ]; then
        AE_INSTALL_CLIENT_SECRET="ae-install-client-secret"
    else
        AE_INSTALL_CLIENT_SECRET="$(openssl rand -hex 24)"
        AE_INSTALL_CLIENT_SECRET_GENERATED=1
        # Written the moment it is generated, not only printed at the end of a
        # successful run. ThunderID imports the bundle carrying this secret in
        # its pre-install hook, so the credential goes LIVE long before the
        # script finishes — and a failure anywhere after that point used to
        # lose the only copy, leaving a cluster whose one Administrator client
        # nobody can authenticate as. There is no recovery from inside: a
        # token minted without the resource indicator silently drops the
        # `system` scope, and every other bundled client is too narrow to
        # rotate this one. The file is the difference between re-running a
        # step and rebuilding the cluster.
        if (umask 077 && printf '%s' "$AE_INSTALL_CLIENT_SECRET" > "$AE_INSTALL_CLIENT_SECRET_FILE"); then
            chmod 600 "$AE_INSTALL_CLIENT_SECRET_FILE" 2>/dev/null || true
            echo "   🔑 generated admin client secret, stored at ${AE_INSTALL_CLIENT_SECRET_FILE}"
        else
            echo "❌ could not write ${AE_INSTALL_CLIENT_SECRET_FILE}." >&2
            echo "   Refusing to bootstrap a credential with nowhere to keep it — set" >&2
            echo "   AE_INSTALL_CLIENT_SECRET yourself, or point" >&2
            echo "   AE_INSTALL_CLIENT_SECRET_FILE somewhere writable." >&2
            exit 1
        fi
    fi
fi
cat > "${BOOTSTRAP_DIR}/86-ae-install-client.yaml" <<YAML
resource_type: application
id: ae-install-client
type: m2m
name: "AE Install Client"
description: "Bootstrap admin client for aectl platform install (make dev-env / skaffold/defaults.yaml thunder.admin_client_id)"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "ae-install-client"
      clientSecret: "${AE_INSTALL_CLIENT_SECRET}"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      scopes: ["openid", "profile", "email", "system"]
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
            attributes: ["ouId", "ouHandle"]
YAML

cat > "${BOOTSTRAP_DIR}/87-ae-install-client-admin-role.yaml" <<YAML
# id/name/description/permissions restate ThunderID's built-in Administrator
# role's own fixed values verbatim — the importer REPLACES those fields
# wholesale on update, so restating them keeps this a no-op re-assertion
# rather than an accidental rename. Assignments are additive, so this only
# adds ae-install-client alongside whatever else is already assigned.
resource_type: role
id: "01900000-0000-7000-8000-000000000050"
name: Administrator
description: System administrator role with full permissions
ouHandle: default
permissions:
  - resourceServerId: "01900000-0000-7000-8000-000000000020"
    permissions:
      - system
assignments:
  - id: ae-install-client
    type: app
YAML

fi

# ── 3c-ii. AEP's own client for calling Agent Manager ───────────────────────
# aep-api registers an ai-agent's provider, model binding and keys in Agent
# Manager, and mints for that with this client. Without it the mint answers 401,
# agent governance fails, and aep-api REFUSES the deploy rather than running an
# agent ungoverned — so a build goes green and nothing ever deploys, with the
# reason only in aep-api's log.
#
# Both documents are required and neither is sufficient: the client declares
# what it may ASK for, the role is what ThunderID issues those scopes from.
#
# The role's `amp-resource-server` is Agent Manager's, declared in its frozen
# 60-amp-resource-server.yaml. The numbering is what makes that resolve — 60
# imports before 93 — so these two must stay numbered above Agent Manager's
# documents, and this pair cannot be imported into an IdP that has none of them.
#
# Duplicated from deployments/single-cluster/thunder-resources/92- and 93-.
# That directory is AEP's bundle for the setup-thunder.sh flow, which this one
# replaced; it is no longer read here, and the copies must be kept in step until
# one of the two flows goes. Upstream added those files and this flow silently
# did not pick them up, which is how a green build came to deploy nothing.
cat > "${BOOTSTRAP_DIR}/92-aep-amp-publisher-client.yaml" <<YAML
# THE NAME IS LOAD-BEARING. agent-manager-service's KEY_MANAGER_AUDIENCE is an
# allow-list and the entry admitting this client is the wildcard
# \`amp-publisher-*\`. A client named outside it mints perfectly good tokens that
# amp-api rejects as "invalid jwt" whatever scopes they carry.
#
# ouId (not ouHandle): the importer resolves ouHandle for roles, groups and
# users but NOT for applications, and an application without an OU mints tokens
# carrying no ouId claim — which amp-api's RequireOrgMatch rejects.
resource_type: application
id: amp-publisher-aep
type: m2m
name: "AEP Agent Manager Publisher"
description: "AEP's client for registering its agents, providers and model bindings in Agent Manager"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "amp-publisher-aep"
      clientSecret: "amp-publisher-aep-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_basic"
      pkceRequired: false
      publicClient: false
      scopes: ["amp:org:view","amp:project:create","amp:project:read","amp:agent:create","amp:agent:read","amp:agent:update","amp:llm-provider:create","amp:llm-provider:read","amp:llm-provider:update","amp:llm-provider:api-key-manage","amp:agent:api-key-manage","amp:gateway:read","amp:environment:read"]
      # ouId above sets the OU; \`attributes\` is what puts the claim INTO the
      # token. Without it the mint succeeds and every amp-api call still fails.
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
            attributes: ["ouId", "ouHandle"]
YAML

cat > "${BOOTSTRAP_DIR}/93-aep-amp-publisher-role.yaml" <<YAML
# A NEW role, not an assignment into Agent Manager's amp-role-admin: the
# importer replaces a document wholesale, so editing theirs would make AEP's
# copy the definition of their admin role. Deliberately narrower than it, too —
# AEP registers agents and maintains its own provider, and has no business
# deleting either.
resource_type: role
id: aep-amp-publisher-role
name: AEP Agent Manager Publisher
description: Lets AEP register its agents, providers and model bindings in Agent Manager
ouHandle: default
permissions:
  - resourceServerId: amp-resource-server
    permissions:
      - "amp:org:view"
      - "amp:project:read"
      - "amp:agent:create"
      - "amp:agent:read"
      - "amp:agent:update"
      - "amp:llm-provider:create"
      - "amp:llm-provider:read"
      - "amp:llm-provider:update"
      - "amp:llm-provider:api-key-manage"
      # An AGENT's model-config keys are gated by the agent permission, not the
      # provider one.
      - "amp:agent:api-key-manage"
      # The agent's TRACING token is a third key family again: without this the
      # mint answers 403 and the agent emits no traces, silently.
      - "amp:agent:token-manage"
      - "amp:gateway:read"
      - "amp:environment:read"
assignments:
  - id: amp-publisher-aep
    type: app
YAML

# ── 3d. Agent Manager's documents ───────────────────────────────────────────
# Both products share one IdP (ADR-0027) and ThunderID reads its bootstrap
# folder only once, at install — so Agent Manager's documents have to be in
# this folder now, not added to a running IdP later.
#
# Kept in its own script so it can be deleted whole: that file, the directory
# it copies from, and this call are the entire Agent Manager coupling here.
AMP_DOCS_SCRIPT="${SCRIPT_DIR}/publish-amp-thunder-documents.sh"
if [ -x "$AMP_DOCS_SCRIPT" ]; then
    bash "$AMP_DOCS_SCRIPT" "$BOOTSTRAP_DIR"
else
    echo "⏭️  ${AMP_DOCS_SCRIPT} not present — installing the IdP with AEP's documents only"
fi

BOOTSTRAP_CM="openchoreo-thunderid-bootstrap"
kubectl create namespace thunder --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n thunder create configmap "${BOOTSTRAP_CM}" \
    --from-file="${BOOTSTRAP_DIR}" --dry-run=client -o yaml | kubectl apply -f -

BOOTSTRAP_FILES_JSON="$(python3 -c "import json,os; print(json.dumps(sorted(os.listdir('${BOOTSTRAP_DIR}'))))")"

# Database: 4 logical DBs in this chart (config / runtime_transient / entity /
# runtime_persistent), vs. the old chart's 3 (config / runtime / user). Each
# `.sqlite.path`/`.sqlite.options` already default to sensible per-DB values
# in the chart — only `.type` needs overriding to sqlite for a single-writer
# k3d pod, matching the official page's single-replica SQLite setup.
helm upgrade --install thunder "${THUNDER_CHART}" \
    --version "${THUNDER_VERSION}" \
    --namespace thunder --create-namespace \
    --set-string "fullnameOverride=thunder" \
    --set "deployment.replicaCount=1" \
    --set "hpa.enabled=false" \
    --set "ingress.enabled=false" \
    --set "httproute.enabled=true" \
    --set "httproute.parentRefs[0].name=gateway-default" \
    --set "httproute.parentRefs[0].namespace=openchoreo-control-plane" \
    --set "httproute.hostnames[0]=thunder.${OC_DOMAIN}" \
    --set "configuration.server.httpOnly=true" \
    --set "configuration.server.publicUrl=${SCHEME}://thunder.${OC_DOMAIN}:${CP_PORT}" \
    --set "configuration.jwt.issuer=${SCHEME}://thunder.${OC_DOMAIN}:${CP_PORT}" \
    --set "configuration.database.config.type=sqlite" \
    --set "configuration.database.runtime_transient.type=sqlite" \
    --set "configuration.database.entity.type=sqlite" \
    --set "configuration.database.runtime_persistent.type=sqlite" \
    --set "configuration.passkey.allowedOrigins[0]=${SCHEME}://${OC_DOMAIN}:${CP_PORT}" \
    --set "persistence.enabled=true" \
    --set "setup.enabled=true" \
    --set-string "setup.admin.username=${THUNDER_ADMIN_USER}" \
    --set-string "setup.admin.password=${THUNDER_ADMIN_PASSWORD}" \
    --set "bootstrap.configMap.name=${BOOTSTRAP_CM}" \
    --set-json "bootstrap.configMap.files=${BOOTSTRAP_FILES_JSON}" \
    --wait --timeout 10m

echo "⏳ Waiting for ThunderID..."
kubectl wait -n thunder --for=condition=available --timeout=300s deployment -l app.kubernetes.io/name=thunderid

echo "✅ ThunderID ready at ${SCHEME}://thunder.${OC_DOMAIN}:${CP_PORT} (${THUNDER_ADMIN_USER} / ${THUNDER_ADMIN_PASSWORD})"

# ── 3c. Entitlement claim: sub -> client_id ──────────────────────────────
# ThunderID 1.0.0 puts a client_credentials token's subject in the `client_id`
# claim, not `sub`. openchoreo-control-plane's chart-shipped
# ClusterAuthzRoleBindings for service accounts (mcp-tryout-client-binding,
# backstage-catalog-reader-binding, finops-agent-binding, rca-agent-binding,
# workload-publisher-binding, observer-resource-reader-binding) are keyed on
# claim: sub, and so is openchoreo-api-config's own entitlement mapping. Left
# as `sub`, every one of those service accounts 403s. The four human-login
# bindings (admin-binding, developer-binding, platform-engineer-binding,
# sre-binding) key on claim: groups and are NOT touched — that claim is
# unaffected.
#
# This has to run AFTER the control-plane install below creates these objects,
# so it is applied there, not here. See the "openchoreo-control-plane" section.

echo ""
echo "   Control Plane"
# WITH_OC_PORTAL=0 leaves OpenChoreo's Backstage portal out of the control
# plane. Nothing in AEP reads it — aep-api talks to openchoreo-api, and the
# platform ships its own console — so it is one more deployment to schedule
# and wait for, on the plane whose readiness gates every later step.
#
# It is also the one component here that does not reliably come up. Its
# liveness probe allows 30s + 3 x 10s before the first kill with a 1s
# per-check timeout, and plugin init (auth, catalog, scaffolder) has not
# finished by then on a cold cluster — so kubelet SIGKILLs it mid-startup,
# the restart begins again, and the release never satisfies `--wait`. A longer
# timeout here does not help: the loop is deterministic, not slow.
# The local https listener the SRE agent reaches aep-api's handoff through (see
# DEV_GATEWAY_TLS_* above). WITH_TLS=1 adds its own "https" listener to
# gateway-default further down, for the public domain, and the handoff route
# attaches to that one instead — two listeners may not share the name.
DEV_GATEWAY_TLS_SET=()
if [ "$WITH_TLS" != "1" ]; then
    DEV_GATEWAY_TLS_SET=(--set gateway.tls.enabled=true
        --set "gateway.tls.hostname=${DEV_GATEWAY_TLS_HOSTNAME}"
        --set-json "gateway.tls.certificateRefs=[{\"name\":\"${DEV_GATEWAY_TLS_SECRET}\"}]")
fi

OC_PORTAL_SET=()
if [ "${WITH_OC_PORTAL:-1}" != "1" ]; then
    OC_PORTAL_SET=(--set backstage.enabled=false)
    echo "   ⏭️  OpenChoreo portal (Backstage) disabled (WITH_OC_PORTAL=0)"
fi

helm upgrade --install openchoreo-control-plane \
    oci://ghcr.io/openchoreo/helm-charts/openchoreo-control-plane \
    --version "${OC_VERSION}" \
    --namespace openchoreo-control-plane --create-namespace \
    --values "$(oc_values single-cluster/values-cp.yaml)" \
    ${DEV_GATEWAY_TLS_SET[@]+"${DEV_GATEWAY_TLS_SET[@]}"} \
    ${OC_PORTAL_SET[@]+"${OC_PORTAL_SET[@]}"} \
    --wait --timeout 600s

echo "⏳ Waiting for Control Plane..."
kubectl wait -n openchoreo-control-plane --for=condition=available --timeout=300s deployment --all

echo "🔧 Switching the service-account entitlement claim to client_id..."
patched_api_config="$(kubectl get configmap openchoreo-api-config -n openchoreo-control-plane -o yaml \
    | sed -E "s/claim:[[:space:]]*['\"]?sub['\"]?/claim: client_id/g")"
echo "$patched_api_config" | kubectl apply --server-side --field-manager=helm --force-conflicts -f - >/dev/null
kubectl rollout restart deployment/openchoreo-api -n openchoreo-control-plane
kubectl rollout status deployment/openchoreo-api -n openchoreo-control-plane --timeout=120s

for binding in $(kubectl get clusterauthzrolebindings.openchoreo.dev -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
    claim="$(kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" -o jsonpath='{.spec.entitlement.claim}' 2>/dev/null)"
    [ "$claim" = "sub" ] || continue
    kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" -o yaml \
        | sed -E "s/claim:[[:space:]]*['\"]?sub['\"]?/claim: client_id/g" \
        | kubectl apply --server-side --field-manager=helm --force-conflicts -f - >/dev/null 2>&1 || true
    now="$(kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" -o jsonpath='{.spec.entitlement.claim}' 2>/dev/null)"
    if [ "$now" != "client_id" ]; then
        echo "❌ ClusterAuthzRoleBinding ${binding} is still on claim '${now}' — service accounts through it will 403." >&2
        exit 1
    fi
    echo "   ✓ ${binding} -> client_id"
done
echo "✅ Entitlement claim is client_id"

# ── Dev gateway https listener cert ─────────────────────────────────────────
# The Gateway CR above already declares the https listener's certificateRefs
# (${DEV_GATEWAY_TLS_SECRET}); cert-manager issues into that Secret name once
# this Certificate exists. Reuses the chart's own "cluster-gateway-ca" CA via
# its "cluster-gateway-selfsigned-issuer" Issuer (both created unconditionally
# by the control-plane chart above) rather than standing up a second CA.
if [ "$WITH_TLS" != "1" ]; then
echo "🔐 Issuing the dev gateway's https cert (${DEV_GATEWAY_TLS_SECRET})..."
kubectl wait -n openchoreo-control-plane --for=condition=Ready certificate/cluster-gateway-ca --timeout=120s
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ${DEV_GATEWAY_TLS_SECRET}
  namespace: openchoreo-control-plane
spec:
  secretName: ${DEV_GATEWAY_TLS_SECRET}
  issuerRef:
    kind: Issuer
    name: ${DEV_GATEWAY_CA_ISSUER}
  dnsNames:
    - "${DEV_GATEWAY_TLS_HOSTNAME}"
EOF
kubectl wait -n openchoreo-control-plane --for=condition=Ready "certificate/${DEV_GATEWAY_TLS_SECRET}" --timeout=120s
echo "✅ Dev gateway https cert ready"
fi

echo "✅ Control Plane ready"

# ── TLS for the control-plane gateway ───────────────────────────────────────
#
# Runs here because gateway-default is created by the control-plane chart: the
# Certificate's HTTP-01 solver routes through that Gateway, and the HTTPS
# listener is added to it.
#
# The listener is patched in rather than shipped by the chart, which offers no
# value for a second one. `sectionName` is deliberately absent from the
# HTTPRoutes the platform creates, so they attach to every listener whose
# hostname matches — adding the listener is enough to serve the same routes
# over TLS, with no change to any route.
if [ "$WITH_TLS" = "1" ]; then
    echo ""
    echo "   TLS — Let's Encrypt via cert-manager"

    # A listener on 80, before anything is ordered. cert-manager attaches its
    # solver HTTPRoutes to this Gateway, but ACME fetches the challenge on
    # port 80 and the Gateway only listens on 8080 — so the route exists,
    # k3d forwards host:80 to the node, and nothing answers. The challenge
    # sits pending with "self check ... EOF", which reads like a network
    # problem rather than a missing listener.
    #
    # It stays after issuance: renewals run the same challenge, and this is
    # where an HTTP->HTTPS redirect would attach.
    kubectl -n openchoreo-control-plane patch gateway gateway-default --type=json -p '
[{"op":"add","path":"/spec/listeners/-","value":{
  "name":"acme",
  "port":80,
  "protocol":"HTTP",
  "allowedRoutes":{"namespaces":{"from":"All"}}
}}]' 2>/dev/null || echo "   (acme listener already present)"
    echo "   + port 80 listener for ACME challenges"

    ACME_EMAIL_FIELD=""
    [ -n "$ACME_EMAIL" ] && ACME_EMAIL_FIELD="  email: ${ACME_EMAIL}"

    kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: ae-letsencrypt
spec:
  acme:
    server: ${ACME_SERVER}
${ACME_EMAIL_FIELD}
    privateKeySecretRef:
      name: ae-letsencrypt-account
    solvers:
      - http01:
          gatewayHTTPRoute:
            parentRefs:
              - name: gateway-default
                namespace: openchoreo-control-plane
                kind: Gateway
EOF

    # One certificate, every public name. A single order costs one of the
    # 50-per-week the CA allows per registered domain — and sslip.io is not on
    # the Public Suffix List, so that bucket is shared with every sslip.io user
    # on the internet. Asking once for all names is the difference between one
    # slot and eleven.
    kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ae-public-tls
  namespace: openchoreo-control-plane
spec:
  secretName: ${TLS_SECRET_NAME}
  issuerRef:
    name: ae-letsencrypt
    kind: ClusterIssuer
  dnsNames:
    - console.${AE_CONSOLE_DOMAIN}
    - tryit.${AE_CONSOLE_DOMAIN}
    - thunder.${OC_DOMAIN}
    - api.${OC_DOMAIN}
    - observer.${OC_DOMAIN}
    - ${OC_ENV_IDP_HOST}
    - console.amp.${AE_DOMAIN}
    - api.amp.${AE_DOMAIN}
EOF

    echo "   ⏳ waiting for the certificate (ACME HTTP-01 over port 80)..."
    if ! kubectl wait -n openchoreo-control-plane --for=condition=Ready \
        certificate/ae-public-tls --timeout=300s; then
        echo "❌ certificate was not issued." >&2
        echo "   Check:  kubectl -n openchoreo-control-plane describe certificate ae-public-tls" >&2
        echo "           kubectl get challenges -A" >&2
        echo "   A 'too many certificates already issued' order means the shared" >&2
        echo "   sslip.io rate limit is exhausted — retry later, or use a domain" >&2
        echo "   you control. ACME_SERVER can point at the staging CA to iterate." >&2
        exit 1
    fi

    kubectl -n openchoreo-control-plane patch gateway gateway-default --type=json -p "$(cat <<EOF
[{"op":"add","path":"/spec/listeners/-","value":{
  "name":"https",
  "port":${CP_PORT},
  "protocol":"HTTPS",
  "allowedRoutes":{"namespaces":{"from":"All"}},
  "tls":{"mode":"Terminate","certificateRefs":[{"name":"${TLS_SECRET_NAME}","kind":"Secret"}]}
}}]
EOF
)"
    echo "✅ HTTPS listener on gateway-default:${CP_PORT}"
fi

# ============================================================================
# Step 4: Default resources (official page, Step 4 — unchanged)
# ============================================================================
echo ""
echo "4️⃣  Default resources"
kubectl label namespace default openchoreo.dev/control-plane=true --overwrite
kubectl apply -f "${RAW}/samples/getting-started/all.yaml"

# ============================================================================
# Step 5: Data Plane (official page, Step 5 — unchanged)
# ============================================================================
echo ""
echo "5️⃣  Data Plane"
kubectl create namespace openchoreo-data-plane --dry-run=client -o yaml | kubectl apply -f -
kubectl wait -n openchoreo-control-plane --for=condition=Ready certificate/cluster-gateway-ca --timeout=120s
CA_CRT=$(kubectl get secret cluster-gateway-ca -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
kubectl create configmap cluster-gateway-ca --from-literal=ca.crt="$CA_CRT" \
    -n openchoreo-data-plane --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install openchoreo-data-plane oci://ghcr.io/openchoreo/helm-charts/openchoreo-data-plane \
  --version "${OC_VERSION}" --namespace openchoreo-data-plane --create-namespace \
  --values "${RAW}/install/k3d/single-cluster/values-dp.yaml"

kubectl wait -n openchoreo-data-plane --for=condition=Ready certificate/cluster-agent-dataplane-tls --timeout=120s
AGENT_CA=$(kubectl get secret cluster-agent-tls -n openchoreo-data-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
kubectl apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterDataPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      value: |
$(echo "$AGENT_CA" | sed 's/^/        /')
  secretStoreRef:
    name: default
  gateway:
    ingress:
      external:
        http:
          host: ${DP_INGRESS_HOST}
          listenerName: ${DP_LISTENER}
          port: ${DP_PORT}
        name: gateway-default
        namespace: openchoreo-data-plane
EOF

# ── TLS for the data-plane gateway ──────────────────────────────────────────
#
# A second Certificate rather than a reference to the control plane's: a
# Gateway's certificateRefs are namespace-local, so sharing one would need a
# ReferenceGrant across planes. Two self-contained certificates are simpler to
# reason about and to delete, at the cost of one more ACME order.
#
# Two names live here, and both are browser-facing for a generated app: the
# host its own endpoints are published on, and the vhost it calls its API
# through. A page served over https cannot call a plain-http API, so if the
# control plane has TLS this plane needs it too.
if [ "$WITH_TLS" = "1" ]; then
    echo ""
    echo "   TLS — data-plane gateway"

    kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: ae-dataplane-tls
  namespace: openchoreo-data-plane
spec:
  secretName: ${TLS_SECRET_NAME}
  issuerRef:
    name: ae-letsencrypt
    kind: ClusterIssuer
  dnsNames:
    - ${AE_ENV}-${AE_ORG}.${DP_INGRESS_HOST}
    - ${AE_ENV}-${AE_ORG}.gateway.${AE_DOMAIN}
EOF

    echo "   ⏳ waiting for the data-plane certificate..."
    if ! kubectl wait -n openchoreo-data-plane --for=condition=Ready \
        certificate/ae-dataplane-tls --timeout=300s; then
        echo "❌ data-plane certificate was not issued." >&2
        echo "   kubectl -n openchoreo-data-plane describe certificate ae-dataplane-tls" >&2
        exit 1
    fi

    kubectl -n openchoreo-data-plane patch gateway gateway-default --type=json -p "$(cat <<EOF
[{"op":"add","path":"/spec/listeners/-","value":{
  "name":"https",
  "port":${DP_PORT},
  "protocol":"HTTPS",
  "allowedRoutes":{"namespaces":{"from":"All"}},
  "tls":{"mode":"Terminate","certificateRefs":[{"name":"${TLS_SECRET_NAME}","kind":"Secret"}]}
}}]
EOF
)"
    echo "✅ HTTPS listener on the data-plane gateway:${DP_PORT}"
fi

echo "✅ Data Plane ready"

# ============================================================================
# Step 6: Workflow Plane — optional (official page, Step 6 — unchanged)
# ============================================================================
if [ "$WITH_BUILD" = "1" ]; then
    echo ""
    echo "6️⃣  Workflow Plane"
    kubectl create namespace openchoreo-workflow-plane --dry-run=client -o yaml | kubectl apply -f -
    CA_CRT=$(kubectl get secret cluster-gateway-ca -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl create configmap cluster-gateway-ca --from-literal=ca.crt="$CA_CRT" \
        -n openchoreo-workflow-plane --dry-run=client -o yaml | kubectl apply -f -

    helm repo add twuni https://twuni.github.io/docker-registry.helm >/dev/null 2>&1 || true
    helm repo update twuni >/dev/null
    helm upgrade --install registry twuni/docker-registry \
      --namespace openchoreo-workflow-plane --create-namespace \
      --values "${RAW}/install/k3d/single-cluster/values-registry.yaml"

    helm upgrade --install openchoreo-workflow-plane oci://ghcr.io/openchoreo/helm-charts/openchoreo-workflow-plane \
      --version "${OC_VERSION}" --namespace openchoreo-workflow-plane \
      --values "${RAW}/install/k3d/single-cluster/values-wp.yaml"

    # Re-domained on the way in rather than applied by URL — see oc_manifest.
    kubectl apply \
      -f "$(oc_manifest samples/getting-started/workflow-templates/checkout-source.yaml)" \
      -f "$(oc_manifest samples/getting-started/workflow-templates.yaml)" \
      -f "$(oc_manifest samples/getting-started/workflow-templates/publish-image-k3d.yaml)" \
      -f "$(oc_manifest samples/getting-started/workflow-templates/generate-workload-k3d.yaml)"

    kubectl wait -n openchoreo-workflow-plane --for=condition=Ready certificate/cluster-agent-workflowplane-tls --timeout=120s
    AGENT_CA=$(kubectl get secret cluster-agent-tls -n openchoreo-workflow-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterWorkflowPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      value: |
$(echo "$AGENT_CA" | sed 's/^/        /')
  secretStoreRef:
    name: default
EOF
    echo "✅ Workflow Plane ready"
else
    echo "⏭️  Skipping Workflow Plane (WITH_BUILD=0)"
fi

# ============================================================================
# Step 7: Observability Plane — optional (official page, Step 7 — unchanged)
# ============================================================================
if [ "$WITH_OBSERVABILITY" = "1" ]; then
    echo ""
    echo "7️⃣  Observability Plane"
    kubectl create namespace openchoreo-observability-plane --dry-run=client -o yaml | kubectl apply -f -
    CA_CRT=$(kubectl get secret cluster-gateway-ca -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl create configmap cluster-gateway-ca --from-literal=ca.crt="$CA_CRT" \
        -n openchoreo-observability-plane --dry-run=client -o yaml | kubectl apply -f -

    kubectl apply -f - <<EOF
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: opensearch-admin-credentials
  namespace: openchoreo-observability-plane
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: opensearch-admin-credentials
  data:
  - secretKey: username
    remoteRef:
      key: opensearch-username
      property: value
  - secretKey: password
    remoteRef:
      key: opensearch-password
      property: value
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: observer-secret
  namespace: openchoreo-observability-plane
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: observer-secret
  data:
  - secretKey: UID_RESOLVER_OAUTH_CLIENT_SECRET
    remoteRef:
      key: observer-oauth-client-secret
      property: value
EOF
    kubectl wait -n openchoreo-observability-plane --for=condition=Ready \
        externalsecret/opensearch-admin-credentials externalsecret/observer-secret --timeout=60s

    docker exec "k3d-${CLUSTER_NAME}-server-0" sh -c \
        "cat /proc/sys/kernel/random/uuid | tr -d '-' > /etc/machine-id"

    helm upgrade --install openchoreo-observability-plane oci://ghcr.io/openchoreo/helm-charts/openchoreo-observability-plane \
      --version "${OC_VERSION}" --namespace openchoreo-observability-plane \
      --values "$(oc_values single-cluster/values-op.yaml)" --timeout 25m

    helm upgrade --install observability-logs-opensearch \
      oci://ghcr.io/openchoreo/helm-charts/observability-logs-opensearch \
      --create-namespace --namespace openchoreo-observability-plane --version 0.5.3 \
      --set openSearchSetup.openSearchSecretName="opensearch-admin-credentials" \
      --set adapter.openSearchSecretName="opensearch-admin-credentials"

    helm upgrade --install observability-traces-opensearch \
      oci://ghcr.io/openchoreo/helm-charts/observability-tracing-opensearch \
      --create-namespace --namespace openchoreo-observability-plane --version 0.6.0 \
      --set openSearch.enabled=false \
      --set openSearchSetup.openSearchSecretName="opensearch-admin-credentials"

    # The chart ships prometheus-operator at 40m CPU / 60Mi, which is under what
    # it needs to reach a steady state on a cold cluster. It starts while the
    # rest of the install is still loading the node, gets throttled to
    # GOMAXPROCS=1, and its alertmanager ConfigMap informer misses the cache-sync
    # deadline — the operator exits 1 about 19s in with "failed to sync cache for
    # ConfigMap informer". Nothing then creates the Prometheus StatefulSet, so
    # metrics-adapter crash-loops too on connection-refused, pointing at a
    # Prometheus that was never built. Both look like distinct failures and
    # neither names the limit.
    helm upgrade --install observability-metrics-prometheus \
      oci://ghcr.io/openchoreo/helm-charts/observability-metrics-prometheus \
      --create-namespace --namespace openchoreo-observability-plane --version 0.6.1 \
      --set kube-prometheus-stack.prometheusOperator.resources.requests.cpu=100m \
      --set kube-prometheus-stack.prometheusOperator.resources.requests.memory=128Mi \
      --set kube-prometheus-stack.prometheusOperator.resources.limits.cpu=500m \
      --set kube-prometheus-stack.prometheusOperator.resources.limits.memory=256Mi

    helm upgrade observability-logs-opensearch \
      oci://ghcr.io/openchoreo/helm-charts/observability-logs-opensearch \
      --namespace openchoreo-observability-plane --version 0.5.3 --reuse-values \
      --set fluent-bit.enabled=true

    kubectl wait -n openchoreo-observability-plane --for=condition=Ready \
        certificate/cluster-agent-observabilityplane-tls --timeout=120s
    AGENT_CA=$(kubectl get secret cluster-agent-tls -n openchoreo-observability-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterObservabilityPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      value: |
$(echo "$AGENT_CA" | sed 's/^/        /')
  observerURL: http://observer.${OC_DOMAIN}:11080
EOF
    kubectl patch clusterdataplane default --type merge \
        -p '{"spec":{"observabilityPlaneRef":{"kind":"ClusterObservabilityPlane","name":"default"}}}'
    if [ "$WITH_BUILD" = "1" ]; then
        kubectl patch clusterworkflowplane default --type merge \
            -p '{"spec":{"observabilityPlaneRef":{"kind":"ClusterObservabilityPlane","name":"default"}}}'
    fi
    echo "✅ Observability Plane ready"
else
    echo "⏭️  Skipping Observability Plane (WITH_OBSERVABILITY=0)"
fi

echo ""
echo "============================================"
echo "  ✅ Setup complete"
echo "============================================"
# Grouped by what is actually serving. This script builds the cluster and the
# shared identity provider; the AEP and Agent Manager consoles are installed by
# the steps `make dev-env` runs after it, so listing all four flat would send
# someone to a URL that is not answering yet and read as a broken install.
echo "  Serving now"
# That hostname is the Backstage portal's route, so with the portal skipped it
# answers nothing — and a URL in a success summary that does not load is read
# as a broken install, which is the thing this block exists to avoid.
if [ "${WITH_OC_PORTAL:-1}" = "1" ]; then
    printf "    %-16s%-50s(%s / %s)\n" "OpenChoreo" "${SCHEME}://${OC_DOMAIN}:${CP_PORT}" "${THUNDER_ADMIN_USER}" "${THUNDER_ADMIN_PASSWORD}"
fi
printf "    %-16s%-50s(%s / %s)\n" "ThunderID" "${SCHEME}://thunder.${OC_DOMAIN}:${CP_PORT}/console" "${THUNDER_ADMIN_USER}" "${THUNDER_ADMIN_PASSWORD}"
echo ""
echo "  Still to install — \`make dev-env\` runs both of these next"
printf "    %-16s%-50s%s\n" "AEP" "${SCHEME}://console.${AE_CONSOLE_DOMAIN}:${CP_PORT}" "aectl platform install"
printf "    %-16s%-50s%s\n" "Agent Manager" "${SCHEME}://console.amp.${AE_DOMAIN}:${CP_PORT}" "setup-agent-manager.sh"

# aectl reads its own config file, which AE_DOMAIN cannot reach — so the same
# suffix lives in seven keys there as well. Printing them already composed is
# what keeps the two from disagreeing: a cluster on one domain and a config on
# another installs without complaint and fails in a browser.
#
# tls.enabled rides along for the same reason the hostnames do. It is what
# moves the ENVIRONMENT tier's scheme and ports (envidp's Thunder and API
# gateway), and aectl composes those from config, not from anything this script
# sets. Omitted on a TLS cluster, the environment tier is addressed over plain
# HTTP on the wrong ports — which installs without complaint and fails at the
# first generated app's login.
if [ "$WITH_TLS" = "1" ]; then TLS_ENABLED_YAML=true; else TLS_ENABLED_YAML=false; fi
if [ "$AE_DOMAIN" != "localhost" ]; then
    echo ""
    echo "  Put these in the aectl config you import next"
    echo "  (\`aectl platform config import --config <file>\`):"
    cat <<EOF

    console:
      public_url: "${SCHEME}://console.${AE_CONSOLE_DOMAIN}:${CP_PORT}"
    tryit:
      public_url: "${SCHEME}://tryit.${AE_CONSOLE_DOMAIN}:${CP_PORT}"
    gateway:
      hostname: "${DP_INGRESS_HOST}"
    environment:
      idp_base_domain: "${OC_DOMAIN}"
      gateway_base_domain: "gateway.${AE_DOMAIN}"
    tls:
      enabled: ${TLS_ENABLED_YAML}
    thunder:
      public_url: "${SCHEME}://thunder.${OC_DOMAIN}:${CP_PORT}"
EOF
    echo ""
    echo "  And pass the same suffix to Agent Manager:  AE_DOMAIN=${AE_DOMAIN}"
fi

# Printed here AND written to AE_INSTALL_CLIENT_SECRET_FILE at the moment it is
# generated (step 3c). It is the Administrator credential for an IdP on a
# reachable gateway, so the file is root-only — but it has to exist: ThunderID
# imports the bundle carrying this secret from a pre-install hook, so the
# credential goes live long before this script finishes, and a failure after
# that point used to leave a cluster whose one Administrator client nobody
# could authenticate as. `aectl platform install` reads it from the
# environment and seeds it into OpenBao itself.
if [ "${AE_INSTALL_CLIENT_SECRET_GENERATED:-0}" = "1" ]; then
    echo ""
    echo "  ⚠️  Generated admin client secret:"
    echo ""
    echo "    export AEP_THUNDER_ADMIN_CLIENT_SECRET=${AE_INSTALL_CLIENT_SECRET}"
    echo ""
    echo "  Also stored at ${AE_INSTALL_CLIENT_SECRET_FILE} (mode 600), so a"
    echo "  lost terminal is not a lost cluster."
    echo ""
    echo "  Pin it with AE_INSTALL_CLIENT_SECRET=... if you re-run this script"
    echo "  against the same Thunder; the bundle is imported once and the"
    echo "  secret cannot be re-derived afterwards."
fi

echo ""
echo "  Cleanup:  k3d cluster delete ${CLUSTER_NAME}"
