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

# Installs the WSO2 Agent Management Platform beside AEP, on a cluster built by
# setup-env-for-aectl.sh and provisioned by `aectl platform install`.
#
#   bash deployments/scripts/setup-agent-manager.sh
#
# Nothing here is built from a checkout: Agent Manager publishes its whole
# platform as OCI charts and images. What this script adds is everything the
# two products have to agree on, which their charts cannot know about each
# other — see deployments/agent-manager/README.md for the decisions and
# deployments/design/agent-manager-convergence.md for the background.
#
# ── What it assumes the cluster already has ─────────────────────────────────
#
#   * OpenChoreo 1.2.5 with its getting-started samples applied, so
#     DeploymentPipeline/default runs development -> staging -> production and
#     Environment/development exists (setup-env-for-aectl.sh, steps 4-7)
#   * ThunderID 1.0.0 as release AND namespace `thunder`, bootstrapped from the
#     ConfigMap named below, serving on thunder.openchoreo.localhost:8080
#   * `aectl platform install` finished — AEP's platform chart, the environment
#     Thunder for `development`, and that environment's API Platform gateway
#
# ── What it deliberately does NOT do ────────────────────────────────────────
#
#   * install a second Thunder. OpenChoreo's control plane has exactly one
#     OIDC issuer (ADR-0027), so the platform identity provider is shared and
#     Agent Manager publishes into it rather than bringing its own
#   * touch the per-environment Thunder tier. Agent Manager runs one Thunder
#     per environment for AgentID / workload identity; convergence does not
#     reach it (ADR-0029)
#   * register the environment with amp-api. That step is Agent Manager's own
#     (its add-environment-thunder.sh), and the two values it needs from this
#     side are printed at the end
#
# ── Re-running ──────────────────────────────────────────────────────────────
#
# Every step is idempotent: the helm installs are `upgrade --install`, the
# bootstrap merge rewrites the same ConfigMap keys, and the ownership hand-over
# re-annotates objects that already carry the annotations.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
INPUTS_DIR="$REPO_ROOT/deployments/agent-manager"

# ============================================================================
# Versions — Agent Manager's charts move together at AMP_VERSION; the sandbox
# module does not. It is an OpenChoreo community module with its own release
# line, and its `upstream` pin is a third, independent version.
# ============================================================================
AMP_VERSION="${AMP_VERSION:-1.0.0-rc2}"
AMP_REGISTRY="${AMP_REGISTRY:-oci://ghcr.io/wso2}"
AGENT_SANDBOX_MODULE_VERSION="${AGENT_SANDBOX_MODULE_VERSION:-0.1.1}"
AGENT_SANDBOX_UPSTREAM_VERSION="${AGENT_SANDBOX_UPSTREAM_VERSION:-v0.4.6}"
OBSERVABILITY_TRACING_VERSION="0.6.0"
OBSERVABILITY_METRICS_VERSION="0.6.1"

CLUSTER_NAME="${CLUSTER_NAME:-openchoreo}"
CLUSTER_CONTEXT="${CLUSTER_CONTEXT:-k3d-${CLUSTER_NAME}}"

# The cluster's own coordinates. These must match what `aectl platform install`
# ran against — skaffold/defaults.yaml is the source for the first three.
OC_ENV="${OC_ENV:-development}"              # oc.pipeline_source_environment
ORG_NS="${ORG_NS:-default}"                  # oc.default_org_namespace
PUBLIC_THUNDER_URL="${PUBLIC_THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"
THUNDER_NS="${THUNDER_NS:-thunder}"
THUNDER_RELEASE="${THUNDER_RELEASE:-thunder}"
BOOTSTRAP_CM="${BOOTSTRAP_CM:-openchoreo-thunderid-bootstrap}"

AMP_NS="wso2-amp"
OBS_NS="openchoreo-observability-plane"
DP_NS="openchoreo-data-plane"
WP_NS="openchoreo-workflow-plane"

# In-cluster Thunder addresses. A hostname is a name, not an address: the
# public URL is what tokens are issued against and what clients send as the
# OAuth resource indicator, but a pod reaches Thunder through its Service.
THUNDER_SVC_HOST="${THUNDER_RELEASE}.${THUNDER_NS}.svc.cluster.local"
THUNDER_INTERNAL_TOKEN_URL="http://${THUNDER_SVC_HOST}:8090/oauth2/token"
THUNDER_INTERNAL_JWKS_URL="http://${THUNDER_SVC_HOST}:8090/oauth2/jwks"

# What Agent Manager's own environment-registration step needs from this side.
# HTTPS on 8443 is not a preference: ThunderID rejects a plain-http JWKS URL
# for a trusted issuer, the certificate is issued for the public hostname, and
# that hostname reaches the HTTPS gateway from inside the cluster only through
# the CoreDNS rewrite OpenChoreo's own coredns-custom.yaml installs.
PUBLIC_THUNDER_HOST="${PUBLIC_THUNDER_URL#*://}"
PUBLIC_THUNDER_HOST="${PUBLIC_THUNDER_HOST%%:*}"
PLATFORM_THUNDER_JWKS_URL="https://${PUBLIC_THUNDER_HOST}:8443/oauth2/jwks"

kubectl() { command kubectl --context "$CLUSTER_CONTEXT" "$@"; }

echo "============================================"
echo "  WSO2 Agent Manager ${AMP_VERSION}"
echo "============================================"

# ── Preflight ───────────────────────────────────────────────────────────────
# Each check names the step that satisfies it. Failing here costs nothing;
# failing four charts in costs a teardown.
fail() { echo "❌ $1" >&2; [ $# -gt 1 ] && echo "   $2" >&2; exit 1; }

kubectl cluster-info &>/dev/null \
    || fail "Cluster '$CLUSTER_CONTEXT' not reachable." "Run deployments/scripts/setup-env-for-aectl.sh first."
kubectl get ns "$OBS_NS" &>/dev/null \
    || fail "The observability plane is missing." "Agent Manager's console and observer both need it (setup-env-for-aectl.sh step 7)."
kubectl get ns "$WP_NS" &>/dev/null \
    || fail "The workflow plane is missing." "The evaluation extension installs into it (setup-env-for-aectl.sh step 6, WITH_BUILD=1)."
kubectl get environment "$OC_ENV" -n "$ORG_NS" &>/dev/null \
    || fail "Environment/$OC_ENV not found in namespace $ORG_NS." "Set OC_ENV to the environment aectl provisioned (oc.pipeline_source_environment)."
kubectl get cm "$BOOTSTRAP_CM" -n "$THUNDER_NS" &>/dev/null \
    || fail "ConfigMap $BOOTSTRAP_CM not found in namespace $THUNDER_NS." "This script merges Agent Manager's bootstrap documents into it."
[ -f "$INPUTS_DIR/amp-values.yaml" ] \
    || fail "Missing $INPUTS_DIR/amp-values.yaml." "The decided chart values live there; this script does not inline them."
command -v helm >/dev/null || fail "helm not found on PATH."
command -v python3 >/dev/null || fail "python3 not found on PATH." "Used to compose the bootstrap ConfigMap."

echo "✅ Preflight: cluster, planes, Environment/${OC_ENV}, ${BOOTSTRAP_CM}"

# ============================================================================
# Step 1: CoreDNS rewrites for Agent Manager's hostnames
# ============================================================================
# The charts keep their own *.amp.localhost defaults everywhere except the
# Thunder addresses, which move to this deployment's. macOS resolves any
# *.localhost host-side, but IN-CLUSTER callers need these too: the gateway
# extension's bootstrap Job calls api.amp.localhost, and agents resolve their
# own gateway vhost.
#
# Rewritten to host.k3d.internal rather than to a Service, so the request
# hairpins out to the k3d load balancer and back in through the gateway with
# its Host header intact — which is what vhost matching needs. OpenChoreo's own
# coredns-custom.yaml does the same for *.openchoreo.localhost; this adds the
# keys beside it rather than replacing the ConfigMap.
echo ""
echo "1️⃣  CoreDNS rewrites for *.amp.localhost and the agent gateway hosts"

coredns_changed=0
for key in amp agentmanager am-gateway gateway; do
    cm_key="${key//-/}.override"
    host="${key//-/\\-}"
    desired="rewrite stop {
  name regex (.+\\.)?${host}\\.localhost host.k3d.internal
  answer auto
}"
    current="$(kubectl get cm coredns-custom -n kube-system -o jsonpath="{.data.${cm_key}}" 2>/dev/null || true)"
    [ "$current" = "$desired" ] && continue
    kubectl get cm coredns-custom -n kube-system -o json 2>/dev/null \
        | CM_KEY="$cm_key" REWRITE_HOST="$host" python3 -c "
import json, os, sys
cm = json.load(sys.stdin)
cm.setdefault('data', {})[os.environ['CM_KEY']] = (
    'rewrite stop {\n'
    '  name regex (.+\\\\.)?' + os.environ['REWRITE_HOST'] + '\\\\.localhost host.k3d.internal\n'
    '  answer auto\n'
    '}'
)
cm['metadata'] = {'name': cm['metadata']['name'], 'namespace': cm['metadata']['namespace']}
json.dump(cm, sys.stdout)
" | kubectl apply -f - >/dev/null
    coredns_changed=1
done
if [ "$coredns_changed" = 1 ]; then
    kubectl -n kube-system rollout restart deployment/coredns >/dev/null
    kubectl -n kube-system rollout status deployment/coredns --timeout=120s >/dev/null
    echo "   ✅ rewrites applied, CoreDNS restarted"
else
    echo "   ✅ rewrites already present"
fi

# ============================================================================
# Step 2: Observability modules, and the operator ceiling they share
# ============================================================================
# setup-env-for-aectl.sh brings the plane, OpenSearch, Fluent Bit and the logs
# adapter. Agent Manager additionally needs traces and metrics — without them
# its console renders empty trace and metric views.
echo ""
echo "2️⃣  Tracing and metrics modules"

helm upgrade --install observability-traces-opensearch \
    oci://ghcr.io/openchoreo/helm-charts/observability-tracing-opensearch \
    --version "$OBSERVABILITY_TRACING_VERSION" \
    --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set openSearch.enabled=false \
    --set openSearchSetup.openSearchSecretName="opensearch-admin-credentials" \
    --timeout 10m >/dev/null
echo "   ✅ tracing module"

# The metrics chart caps its operator at limits.cpu 40m and gives it a liveness
# probe with timeoutSeconds 1. One platform's worth of CRDs fits inside that;
# two does not. The operator throttles at the ceiling, the probe times out, the
# kubelet restarts the container, and it throttles again — and because the
# container exits 0 the pod reads as Completed rather than crash-looping, so
# nothing in `kubectl get pods` says what is wrong.
#
# Only the limit moves. The 20m request is what the scheduler places on and is
# adequate; the ceiling is what throttles.
helm upgrade --install observability-metrics-prometheus \
    oci://ghcr.io/openchoreo/helm-charts/observability-metrics-prometheus \
    --version "$OBSERVABILITY_METRICS_VERSION" \
    --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set kube-prometheus-stack.prometheusOperator.resources.limits.cpu=300m \
    --timeout 10m >/dev/null

# Helm accepts an unknown --set path silently, so read the value back rather
# than assuming the override landed.
applied_cpu="$(kubectl get deploy prometheus-operator -n "$OBS_NS" \
    -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}' 2>/dev/null || true)"
[ "$applied_cpu" = "300m" ] \
    || fail "Prometheus operator CPU limit is '${applied_cpu:-unset}', not 300m." \
            "The --set path may have moved in chart ${OBSERVABILITY_METRICS_VERSION}; check it before continuing."
echo "   ✅ metrics module, operator ceiling at 300m"

# ============================================================================
# Step 3: Compose the identity provider's bootstrap documents
# ============================================================================
# ThunderID imports its bootstrap folder in filename order and upserts each
# document. Four of the settings involved exist ONCE for the whole server, and
# declaring one replaces its entire value rather than adding to it — so two
# publishers cannot both hold them, and without this step a number in a
# filename decides which wins.
#
# Agent Manager's documents are rendered from the chart HERE rather than copied
# into this repo: they move with the chart, and a frozen copy would drift from
# the version actually being installed. `thunder.enabled=false` is the
# supported way to make it a publisher into an identity provider it did not
# install — the chart's ThunderID dependency is declared `condition:
# thunder.enabled`, so with it false the chart emits documents and no Thunder.
echo ""
echo "3️⃣  Composing ${BOOTSTRAP_CM}"

AMP_RENDER="$(mktemp)"
trap 'rm -f "$AMP_RENDER"' EXIT
helm template amp-thunder "${AMP_REGISTRY}/wso2-amp-thunder-extension" \
    --version "$AMP_VERSION" \
    -f "$INPUTS_DIR/thunder-extension-values.yaml" > "$AMP_RENDER"

# Which of Agent Manager's documents are dropped, and why, is the assembly
# table in deployments/agent-manager/thunder-bootstrap/README.md. Keeping the
# list here rather than in the python below keeps the two readable side by side.
AMP_DROP=(
    71-amp-cors-config.yaml                    # cors — composed
    73-amp-csp-config.yaml                     # csp — composed
    70-fix-thunder-system-rs-identifier.yaml   # System resource server — composed
)
# 69-amp-default-resource-server-config.yaml is NOT dropped: AEP publishes no
# competing document, and both its clients send an explicit OAuth resource
# indicator rather than relying on the server-wide default.
# 67-amp-default-users.yaml renders empty — see thunder-extension-values.yaml.

kubectl get cm "$BOOTSTRAP_CM" -n "$THUNDER_NS" -o json \
    | AMP_RENDER="$AMP_RENDER" COMPOSED_DIR="$INPUTS_DIR/thunder-bootstrap" \
      DROP="${AMP_DROP[*]}" python3 -c '
import json, os, sys, yaml

cm = json.load(sys.stdin)
data = cm.setdefault("data", {})
drop = {d.split("#")[0].strip() for d in os.environ["DROP"].split()}

# Agent Manager s documents, out of the rendered ConfigMap.
added = []
for doc in yaml.safe_load_all(open(os.environ["AMP_RENDER"])):
    if not doc or doc.get("kind") != "ConfigMap":
        continue
    for name, body in (doc.get("data") or {}).items():
        if name in drop or not (body or "").strip():
            continue
        if name in data and data[name] != body:
            print(f"   ⚠️  {name} already present with different content — Agent Manager s copy wins", file=sys.stderr)
        data[name] = body
        added.append(name)

# The composed documents last, so they are the value that lands whatever either
# side numbers its own.
composed = []
for fn in sorted(os.listdir(os.environ["COMPOSED_DIR"])):
    if not fn.endswith(".yaml"):
        continue
    data[fn] = open(os.path.join(os.environ["COMPOSED_DIR"], fn)).read()
    composed.append(fn)

cm["metadata"] = {"name": cm["metadata"]["name"], "namespace": cm["metadata"]["namespace"]}
json.dump(cm, sys.stdout)
print(f"   added {len(added)} Agent Manager document(s), {len(composed)} composed", file=sys.stderr)
' | kubectl apply -f - >/dev/null

# ThunderID imports the folder at startup, so the ConfigMap alone changes
# nothing until the pod re-reads it. A rollout is the whole re-import: every
# document is an upsert, so replaying the folder is idempotent.
kubectl -n "$THUNDER_NS" rollout restart "statefulset/${THUNDER_RELEASE}" 2>/dev/null \
    || kubectl -n "$THUNDER_NS" rollout restart "deployment/${THUNDER_RELEASE}"
kubectl -n "$THUNDER_NS" rollout status "statefulset/${THUNDER_RELEASE}" --timeout=300s 2>/dev/null \
    || kubectl -n "$THUNDER_NS" rollout status "deployment/${THUNDER_RELEASE}" --timeout=300s
echo "   ✅ documents merged and re-imported"

# ============================================================================
# Step 4: Hand the shared objects over to Helm
# ============================================================================
# OpenChoreo's samples create DeploymentPipeline/default and Environment/<env>
# with a client-side `kubectl apply`. Agent Manager's platform-resources chart
# renders both, and Helm will not adopt an object it did not create. Three
# moves are needed and the first two alone are not enough:
#
#   1. the meta.helm.sh ownership annotations and the managed-by label —
#      without them the install fails with "invalid ownership metadata"
#   2. dropping last-applied-configuration — Helm's server-side apply migrates
#      that annotation into a kubectl-client-side-apply field manager which
#      owns .spec, and the install dies on the conflict
#   3. --force-conflicts on the install itself (step 5)
#
# Forcing is not a workaround: taking those fields over is the point. What
# makes it safe here is that the chart, given amp-values.yaml, renders the same
# promotion graph and the same environment already on the cluster — so the
# force is a no-op force rather than a rewrite.
echo ""
echo "4️⃣  Handing DeploymentPipeline/default and Environment/${OC_ENV} to Helm"

for target in "deploymentpipeline/default" "environment/${OC_ENV}"; do
    kubectl annotate -n "$ORG_NS" "$target" \
        meta.helm.sh/release-name=amp-platform-resources \
        meta.helm.sh/release-namespace="$ORG_NS" --overwrite >/dev/null
    kubectl label -n "$ORG_NS" "$target" \
        app.kubernetes.io/managed-by=Helm --overwrite >/dev/null
    kubectl annotate -n "$ORG_NS" "$target" \
        kubectl.kubernetes.io/last-applied-configuration- >/dev/null 2>&1 || true
    echo "   ✅ ${target}"
done

# ============================================================================
# Step 5: Platform resources extension
# ============================================================================
# amp-values.yaml carries the three decisions this chart would otherwise make
# wrongly on a converged cluster: the promotion graph (its default flattens
# OpenChoreo's to a single node, which aep-api then resolves to an environment
# with no Thunder or gateway under it), the environment (its default is
# `default`, which would need a second Thunder and gateway tier), and the
# project name (its default collides with OpenChoreo's sample Project/default).
#
# The gateway host is read from the cluster rather than taken from the file:
# component routes are built from it, and the ClusterDataPlane is where it is
# actually decided.
echo ""
echo "5️⃣  Platform resources extension"

DP_INGRESS_HOST="$(kubectl get clusterdataplane default \
    -o jsonpath='{.spec.gateway.ingress.external.http.host}' 2>/dev/null || true)"
DP_INGRESS_HOST="${DP_INGRESS_HOST:-openchoreoapis.localhost}"

helm upgrade --install amp-platform-resources \
    "${AMP_REGISTRY}/wso2-amp-platform-resources-extension" \
    --version "$AMP_VERSION" \
    --namespace "$ORG_NS" --kube-context "$CLUSTER_CONTEXT" \
    --force-conflicts --reset-values \
    -f "$INPUTS_DIR/amp-values.yaml" \
    --set-string "environment.gateway.http.host=${DP_INGRESS_HOST}" \
    --timeout 10m >/dev/null

# The values are only worth passing if they landed. A flattened pipeline is a
# valid pipeline, so nothing downstream would complain about it.
pipeline_sources="$(kubectl get deploymentpipeline default -n "$ORG_NS" \
    -o jsonpath='{.spec.promotionPaths[*].sourceEnvironmentRef.name}' 2>/dev/null || true)"
case " $pipeline_sources " in
    *" $OC_ENV "*) ;;
    *) fail "DeploymentPipeline/default no longer promotes from ${OC_ENV} (sources: ${pipeline_sources:-none})." \
            "deploymentPipeline.promotionOrder did not take effect; aep-api will resolve a different write-target on its next restart." ;;
esac
echo "   ✅ ProjectType, Environment, ComponentTypes, amp-* workflows, traits"
echo "   ✅ pipeline still promotes from ${OC_ENV}"

# ============================================================================
# Step 6: Agent sandbox module
# ============================================================================
# Agents run as sandboxed pods rendered from the SandboxTemplate and
# SandboxWarmPool CRDs this module provides. It is an OpenChoreo community
# module on its own release line, so neither of its versions follows
# AMP_VERSION.
echo ""
echo "6️⃣  Agent sandbox module"
helm upgrade --install agent-sandbox \
    oci://ghcr.io/openchoreo/helm-charts/agent-sandbox \
    --version "$AGENT_SANDBOX_MODULE_VERSION" \
    --namespace "$DP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --wait \
    --set namespace=openchoreo-control-plane \
    --set dataPlaneNamespace="$DP_NS" \
    --set dataPlaneServiceAccount=cluster-agent-dataplane \
    --set upstream.version="$AGENT_SANDBOX_UPSTREAM_VERSION" \
    --timeout 10m >/dev/null
kubectl wait -n agent-sandbox-system --for=condition=available --timeout=180s \
    deployment/agent-sandbox-controller >/dev/null
echo "   ✅ sandbox controller ready"

# ============================================================================
# Step 7: Agent Manager — amp-api, amp-console, PostgreSQL
# ============================================================================
# Every Thunder address moves off the chart's own thunder.amp.localhost. The
# split matters: issuer and console auth take the PUBLIC url, because that is
# what tokens are signed against and what a browser is redirected to; the
# token and JWKS endpoints take the in-cluster Service, because a pod dials
# them directly. resolveToHost is the same distinction made explicit — the
# base URL stays public (the System resource server identifier is derived from
# it) while the connection resolves to the Service.
#
# The chart runs its own DB-migration and JWT-key-generation Jobs.
echo ""
echo "7️⃣  Agent Manager (amp-api, amp-console, PostgreSQL)"
helm upgrade --install amp "${AMP_REGISTRY}/wso2-agent-manager" \
    --version "$AMP_VERSION" \
    --namespace "$AMP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set "console.config.instrumentationUrl=http://${OC_ENV}-${ORG_NS}.gateway.localhost:19080/otel" \
    --set "agentManagerService.config.amObserverPublicURL=http://traces.amp.localhost:11080" \
    --set "agentManagerService.config.keyManager.issuer=${PUBLIC_THUNDER_URL}" \
    --set "agentManagerService.config.thunder.baseURL=${PUBLIC_THUNDER_URL}" \
    --set "console.config.auth.baseUrl=${PUBLIC_THUNDER_URL}" \
    --set "agentManagerService.config.keyManager.jwksUrl=${THUNDER_INTERNAL_JWKS_URL}" \
    --set "agentManagerService.config.oidc.tokenUrl=${THUNDER_INTERNAL_TOKEN_URL}" \
    --set "agentManagerService.config.thunder.resolveToHost=${THUNDER_SVC_HOST}:8090" \
    --timeout 30m >/dev/null

echo "   ⏳ waiting for Agent Manager..."
kubectl rollout status statefulset/amp-postgresql -n "$AMP_NS" --timeout=600s >/dev/null
kubectl wait -n "$AMP_NS" --for=condition=available --timeout=600s \
    deployment/amp-api deployment/amp-console >/dev/null

# A rendered chart can carry an address no override reached — Helm accepts an
# unknown --set path in silence. Read the live pod spec rather than the render.
if kubectl get deploy amp-api -n "$AMP_NS" -o yaml | grep -q "thunder\.amp\.localhost"; then
    echo "   ⚠️  amp-api still references thunder.amp.localhost — a --set path above may have moved" >&2
fi
echo "   ✅ amp-api + amp-console ready"

# ============================================================================
# Step 8: Observability extension (amp-observer)
# ============================================================================
# Same two in-cluster Thunder addresses as the agent-manager chart, for the
# same reason: the observer mints its own token and fetches signing keys from
# inside the cluster.
echo ""
echo "8️⃣  Observability extension"
helm upgrade --install amp-observability-traces \
    "${AMP_REGISTRY}/wso2-amp-observability-extension" \
    --version "$AMP_VERSION" \
    --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set "amObserver.auth.issuer=${PUBLIC_THUNDER_URL}" \
    --set "amObserver.auth.jwksUrl=${THUNDER_INTERNAL_JWKS_URL}" \
    --set "amObserver.observer.idpTokenUrl=${THUNDER_INTERNAL_TOKEN_URL}" \
    --timeout 15m >/dev/null
if kubectl get deployment amp-observer -n "$OBS_NS" &>/dev/null; then
    kubectl wait -n "$OBS_NS" --for=condition=available --timeout=300s \
        deployment/amp-observer >/dev/null
fi
echo "   ✅ amp-observer"

# ============================================================================
# Step 9: Evaluation extension
# ============================================================================
# The chart's NetworkPolicy targets workflows-<env>, which OpenChoreo only
# creates once a workflow has actually run. Pre-create it so install order does
# not depend on that.
echo ""
echo "9️⃣  Evaluation extension"
kubectl create namespace "workflows-${OC_ENV}" --dry-run=client -o yaml \
    | kubectl apply -f - >/dev/null

# The eval pod runs untrusted evaluator code. Scope its API-server egress to
# the k3d node network rather than taking the chart's RFC1918 default, which
# also spans the pod and service CIDRs.
eval_args=()
node_cidr="$(docker network inspect "k3d-${CLUSTER_NAME}" \
    --format '{{ (index .IPAM.Config 0).Subnet }}' 2>/dev/null || true)"
if [ -n "$node_cidr" ]; then
    eval_args=(--set "networkPolicy.evaluationJob.apiServer.cidrs[0]=${node_cidr}")
else
    echo "   ⚠️  could not read the k3d node CIDR — the chart's RFC1918 default will be used" >&2
fi

# The eval job mints its publisher token at the platform IdP, and the same
# NetworkPolicy that scopes its egress allows the IdP BY NAMESPACE. Both
# default to the chart's own Thunder release. Missing the namespace one is the
# silent failure: the token request is denied egress and the job times out with
# nothing naming the policy.
helm upgrade --install amp-evaluation-extension \
    "${AMP_REGISTRY}/wso2-amp-evaluation-extension" \
    --version "$AMP_VERSION" \
    --namespace "$WP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    ${eval_args[@]+"${eval_args[@]}"} \
    --set "ampEvaluation.publisher.idpTokenUrl=${THUNDER_INTERNAL_TOKEN_URL}" \
    --set "networkPolicy.evaluationJob.idp.namespace=${THUNDER_NS}" \
    --timeout 10m >/dev/null
echo "   ✅ evaluation extension"

# ============================================================================
# Done — and the one step that is Agent Manager's own
# ============================================================================
echo ""
echo "============================================"
echo "  ✅ Agent Manager installed"
echo "============================================"
echo ""
echo "  Console: http://console.amp.localhost:8080"
echo "  API:     http://api.amp.localhost:8080"
echo ""
echo "  Environment registration is NOT done here. Agent Manager keeps its own"
echo "  list of environments it can deploy agents into, and putting one on that"
echo "  list is its own step — deployments/scripts/add-environment-thunder.sh in"
echo "  github.com/wso2/agent-manager, at tag amp/v${AMP_VERSION}."
echo ""
echo "  It cannot derive two values, and its defaults name a hostname this"
echo "  cluster does not publish. Pass these:"
echo ""
echo "    PLATFORM_THUNDER_ISSUER=${PUBLIC_THUNDER_URL}"
echo "    PLATFORM_THUNDER_JWKS_URL=${PLATFORM_THUNDER_JWKS_URL}"
echo ""
echo "  Get them wrong and nothing fails at install time: the environment"
echo "  Thunder is configured to trust an issuer that never signs anything, and"
echo "  it surfaces later as a 401 on an agent's API."
