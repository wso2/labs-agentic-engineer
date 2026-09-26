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
#   * take over OpenChoreo's build templates. Agent Manager's chart forks five
#     of them under OpenChoreo's own names; step 5 renames its copies to
#     `amp-*` on the way in, so AEP keeps building with the templates it
#     already has
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

# Renames Agent Manager's forked build templates during the step 5 install.
# See the file itself for which five move and why the others may not.
FORKED_TEMPLATE_RENAMER="prefix-forked-workflow-templates.py"

AMP_NS="wso2-amp"
OBS_NS="openchoreo-observability-plane"
DP_NS="openchoreo-data-plane"
WP_NS="openchoreo-workflow-plane"

# The in-cluster Thunder addresses are resolved from the cluster after the
# preflight, below — ThunderID's objects are not named after its release.

# What Agent Manager's own environment-registration step needs from this side.
# HTTPS on 8443 is not a preference: ThunderID rejects a plain-http JWKS URL
# for a trusted issuer, the certificate is issued for the public hostname, and
# that hostname reaches the HTTPS gateway from inside the cluster only through
# the CoreDNS rewrite OpenChoreo's own coredns-custom.yaml installs.
# Agent Manager addresses an environment's IdP as "<handle>.<base domain>:8080"
# (ThunderOriginFromHandle, with TLS off) — it stores an origin it COMPOSES, it
# is not told one. aectl has already provisioned this environment's IdP at
# "<env>-idp.openchoreo.localhost", so pointing Agent Manager at that instance
# rather than standing up a second one is a matter of making the value it
# computes come out right: the base domain below, plus the handle registered in
# the final step.
#
# There is no way to hand it the address directly. Later Agent Manager builds
# grew a `url` field on this endpoint; ${AMP_VERSION} has neither the field nor
# a column to store it, and a body carrying one is accepted with 200 and a
# generated handle rather than refused — so composing the right value is the
# only lever, and a `url` that looks like it worked is the trap.
ENV_IDP_BASE_DOMAIN="${ENV_IDP_BASE_DOMAIN:-openchoreo.localhost}"
ENV_IDP_HANDLE="${ENV_IDP_HANDLE:-${OC_ENV}-idp}"
ENV_IDP_RELEASE="thunder-${ORG_NS}-${OC_ENV}"
AMP_API_URL="${AMP_API_URL:-http://api.amp.localhost:8080/api/v1}"

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
# Step 1 edits this ConfigMap in place and pipes it through python3; an absent
# one feeds empty stdin to json.load and, under `set -o pipefail`, ends the run
# on a JSONDecodeError rather than on anything naming the missing object.
# OpenChoreo installs it (install/k3d/common/coredns-custom.yaml).
kubectl get cm coredns-custom -n kube-system &>/dev/null \
    || fail "ConfigMap coredns-custom not found in namespace kube-system." "OpenChoreo installs it; step 1 adds Agent Manager's rewrites beside its keys."
kubectl get cm "$BOOTSTRAP_CM" -n "$THUNDER_NS" &>/dev/null \
    || fail "ConfigMap $BOOTSTRAP_CM not found in namespace $THUNDER_NS." "setup-env-for-aectl.sh publishes it before installing ThunderID."
# Agent Manager's identity configuration is published with AEP's, before the
# IdP installs, and cannot be added afterwards — the bootstrap folder is read
# once, by the chart's pre-install Job. An IdP installed without it needs
# reinstalling, not patching, so this fails here rather than four charts in
# with amp-console unable to authenticate anyone.
kubectl get cm "$BOOTSTRAP_CM" -n "$THUNDER_NS" -o jsonpath='{.data.50-amp-api-client\.yaml}' 2>/dev/null | grep -q . \
    || fail "ThunderID was installed without Agent Manager's documents." \
            "Re-run setup-env-for-aectl.sh (it publishes both products' documents in step 3d) — they cannot be added to a running IdP."
[ -f "$INPUTS_DIR/amp-values.yaml" ] \
    || fail "Missing $INPUTS_DIR/amp-values.yaml." "The decided chart values live there; this script does not inline them."
command -v helm >/dev/null || fail "helm not found on PATH."
command -v python3 >/dev/null || fail "python3 not found on PATH." "Used to compose the bootstrap ConfigMap."
python3 -c 'import yaml' 2>/dev/null \
    || fail "python3 cannot import yaml (PyYAML)." "The bootstrap merge (step 3) and the workflow-template post-renderer (step 5) both parse YAML."
[ -x "$INPUTS_DIR/$FORKED_TEMPLATE_RENAMER" ] \
    || fail "Missing or non-executable $INPUTS_DIR/$FORKED_TEMPLATE_RENAMER." "Step 5 runs it as a Helm post-renderer; chmod +x it."

# ── ThunderID's own object names ────────────────────────────────────────────
# A hostname is a name, not an address: the public URL is what tokens are
# issued against and what clients send as the OAuth resource indicator, but a
# pod reaches Thunder through its Service.
#
# Both the Service and the workload are found by ThunderID's release label
# rather than named. The chart derives them from the release name with its own
# suffixes — `thunder-service`, `thunder-deployment` — so a name built from
# THUNDER_RELEASE alone matches nothing, and a suffix list would break again on
# the next chart that spells them differently.
#
# Getting the Service wrong is the silent half: every address below is passed
# as a --set that Helm cannot validate, so the wrong host installs cleanly and
# surfaces later as a 401 from amp-api with nothing naming the cause.
THUNDER_SELECTOR="app.kubernetes.io/instance=${THUNDER_RELEASE}"

THUNDER_SVC="$(kubectl -n "$THUNDER_NS" get svc -l "$THUNDER_SELECTOR" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
[ -n "$THUNDER_SVC" ] \
    || fail "No Service in namespace ${THUNDER_NS} carries ${THUNDER_SELECTOR}." \
            "Agent Manager reaches the identity provider through it. Set THUNDER_RELEASE to the ThunderID release name."

# 8090 is ThunderID's own service port, the same on every deployment of it.
THUNDER_SVC_HOST="${THUNDER_SVC}.${THUNDER_NS}.svc.cluster.local"
THUNDER_INTERNAL_TOKEN_URL="http://${THUNDER_SVC_HOST}:8090/oauth2/token"
THUNDER_INTERNAL_JWKS_URL="http://${THUNDER_SVC_HOST}:8090/oauth2/jwks"

echo "✅ Preflight: cluster, planes, Environment/${OC_ENV}, ${BOOTSTRAP_CM}"
echo "   Thunder: reachable at ${THUNDER_SVC_HOST}:8090"

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
# The identity provider's documents are NOT published here
# ============================================================================
# Both products' bootstrap documents are published together by
# setup-env-for-aectl.sh, before ThunderID is installed — see its step 3d and
# deployments/agent-manager/thunder-bootstrap/README.md.
#
# They cannot be added from this script. ThunderID reads its bootstrap folder
# exactly once, from the chart's pre-install setup Job; the running server
# mounts no bootstrap volume, so a document written afterwards is never read,
# and restarting the pod re-imports nothing. Publishing both sides up front is
# what makes one install enough.
# ============================================================================
# Step 4: Hand the shared objects over to Helm
# ============================================================================
# OpenChoreo's samples create DeploymentPipeline/default and Environment/<env>
# with a client-side `kubectl apply`. Agent Manager's platform-resources chart
# renders both, and Helm will not adopt an object it did not create.
#
# The meta.helm.sh annotations and the managed-by label are the whole of what
# Helm checks, and are all the hand-over needs: without them the install fails
# with "invalid ownership metadata", and with them it succeeds whether or not
# the object still carries kubectl's own record.
#
# last-applied-configuration is dropped anyway, but not because Helm reads it —
# Helm 3 reconciles with a three-way merge over its OWN stored manifest, the
# new one, and the live object, and never looks at that annotation. It is
# dropped because it is kubectl's record of a spec Helm now authors: it keeps a
# second writer's view of the object alive, and it becomes a conflicting field
# manager rather than a stale note the day this reconciles server-side.
#
# Adoption is safe here rather than a rewrite: the chart, given
# amp-values.yaml, renders the same promotion graph and the same environment
# already on the cluster, so what changes is who owns them and not what they
# say.
#
# The release is deliberately NOT installed with --take-ownership. That flag
# would skip the ownership check for every object in the release, which is the
# one thing standing between a post-renderer that stops renaming and Helm
# quietly adopting OpenChoreo's build templates.
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

# The chart also forks five of OpenChoreo's build templates under OpenChoreo's
# own names. OpenChoreo applies those five client-side, so they carry no Helm
# ownership and this install would stop on the first one. The post-renderer
# prefixes Agent Manager's copies instead of adopting OpenChoreo's, because the
# forks differ: Agent Manager's checkout-source has no ssh-privatekey branch,
# so adopting it would drop SSH git authentication from every AEP build.
helm upgrade --install amp-platform-resources \
    "${AMP_REGISTRY}/wso2-amp-platform-resources-extension" \
    --version "$AMP_VERSION" \
    --namespace "$ORG_NS" --kube-context "$CLUSTER_CONTEXT" \
    --reset-values \
    --post-renderer "$INPUTS_DIR/$FORKED_TEMPLATE_RENAMER" \
    -f "$INPUTS_DIR/amp-values.yaml" \
    --set-string "environment.gateway.http.host=${DP_INGRESS_HOST}" \
    --timeout 10m >/dev/null

# Read both halves of the rename back. The post-renderer fails loudly on a
# shape it does not recognise, but it cannot see what reached the cluster, and
# an adopted template is the silent half: AEP would go on building with Agent
# Manager's fork and the first sign would be a private repo that stops cloning.
for forked in checkout-source publish-image containerfile-build \
              ballerina-buildpack-build gcp-buildpacks-build; do
    kubectl get clusterworkflowtemplate "amp-${forked}" >/dev/null 2>&1 \
        || fail "ClusterWorkflowTemplate/amp-${forked} is missing after the install." \
                "The post-renderer did not reach this release."
    owner="$(kubectl get clusterworkflowtemplate "$forked" \
        -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-name}' 2>/dev/null || true)"
    [ "$owner" = "amp-platform-resources" ] \
        && fail "OpenChoreo's ClusterWorkflowTemplate/${forked} was adopted by amp-platform-resources." \
                "AEP builds would run Agent Manager's fork, which has no SSH git authentication."
done
echo "   ✅ forked build templates installed as amp-*, OpenChoreo's untouched"

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
    --set-string "agentManagerService.config.thunderHostBaseDomain=${ENV_IDP_BASE_DOMAIN}" \
    --set-string "console.config.thunderHostBaseDomain=${ENV_IDP_BASE_DOMAIN}" \
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
# Step 10: Point Agent Manager at the environment IdP aectl already installed
# ============================================================================
# Agent Manager keeps its own record of where each environment's IdP lives and
# what credentials administer it. Its own add-environment-thunder.sh fills that
# record by PROVISIONING a second IdP for the environment; this registers the
# one aectl already built instead, so the environment keeps a single IdP tier
# shared by both products (ADR-0029).
#
# Two calls, no provisioning:
#
#   thunder-system-client  the credentials amp-api administers that IdP with.
#                          aectl's aep-system-client is reused rather than a
#                          second admin client being minted — one environment,
#                          one IdP, one system credential.
#   thunder-url            the handle Agent Manager composes the origin from.
#                          It must spell aectl's hostname exactly, which is why
#                          it is derived from OC_ENV rather than written down.
#
# The record is immutable once written: a different value later is rejected
# with 409 and needs DeleteThunderURL first, so a mismatch here is not
# self-correcting. Both calls are idempotent when the value matches.
echo ""
echo "🔟 Registering ${OC_ENV}'s identity provider with Agent Manager"

env_idp_secret="${ENV_IDP_RELEASE}-aep-system-client"
system_client_id="$(kubectl -n "$ENV_IDP_RELEASE" get secret "$env_idp_secret" \
    -o jsonpath='{.data.client-id}' 2>/dev/null | base64 -d || true)"
system_client_secret="$(kubectl -n "$ENV_IDP_RELEASE" get secret "$env_idp_secret" \
    -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d || true)"
[ -n "$system_client_id" ] && [ -n "$system_client_secret" ] \
    || fail "Secret ${ENV_IDP_RELEASE}/${env_idp_secret} has no client-id/client-secret." \
            "aectl writes it when it provisions the environment IdP — re-run \`aectl platform install\`."

amp_token="$(curl -sf --max-time 30 --retry 5 --retry-delay 5 \
    -X POST "${PUBLIC_THUNDER_URL}/oauth2/token" \
    -u "amp-api-client:amp-api-client-secret" \
    -d "grant_type=client_credentials" \
    --data-urlencode "scope=amp:org:manage-service-account" 2>/dev/null \
    | sed -E 's/.*"access_token":"([^"]+)".*/\1/')"
[ -n "$amp_token" ] \
    || fail "Could not get an amp-api token from ${PUBLIC_THUNDER_URL}." \
            "amp-api-client is published by setup-env-for-aectl.sh's bootstrap; check it imported."

register() {
    local path="$1" body="$2" label="$3"
    local code
    code="$(curl -s -o /tmp/amp-register-$$.json -w '%{http_code}' --max-time 30 \
        -X PUT "${AMP_API_URL}/orgs/${ORG_NS}/environments/${OC_ENV}/${path}" \
        -H "Authorization: Bearer ${amp_token}" \
        -H "Content-Type: application/json" -d "$body")"
    case "$code" in
        200|201|204) echo "   ✅ ${label}" ;;
        409) fail "${label}: already registered with a different value (HTTP 409)." \
                  "Agent Manager treats this record as immutable — DELETE it before re-registering." ;;
        *)   echo "   response: $(head -c 300 /tmp/amp-register-$$.json)" >&2
             fail "${label} failed (HTTP ${code})." ;;
    esac
    rm -f "/tmp/amp-register-$$.json"
}

register "thunder-system-client" \
    "{\"clientId\":\"${system_client_id}\",\"clientSecret\":\"${system_client_secret}\"}" \
    "system client (${system_client_id})"
# amp-api runs a reconciler that generates a RANDOM handle for any environment
# without one, seconds after it boots. That handle composes to a hostname
# nothing answers on, so it is replaced rather than accepted — and since the
# reconciler can win the race between the delete and the write, this retries
# instead of failing on the first 409.
#
# The record is otherwise immutable: a PUT of a different handle is rejected,
# which is why the existing one is deleted rather than overwritten.
env_idp_url="${AMP_API_URL}/orgs/${ORG_NS}/environments/${OC_ENV}/thunder-url"
read_handle() {
    curl -s --max-time 30 "$env_idp_url" -H "Authorization: Bearer ${amp_token}" 2>/dev/null \
        | sed -nE 's/.*"handle":"([^"]+)".*/\1/p'
}

registered=""
for attempt in 1 2 3; do
    registered="$(read_handle)"
    [ "$registered" = "$ENV_IDP_HANDLE" ] && break
    if [ -n "$registered" ]; then
        curl -s -o /dev/null --max-time 30 -X DELETE "$env_idp_url" \
            -H "Authorization: Bearer ${amp_token}"
    fi
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 30 -X PUT "$env_idp_url" \
        -H "Authorization: Bearer ${amp_token}" -H "Content-Type: application/json" \
        -d "{\"handle\":\"${ENV_IDP_HANDLE}\"}")"
    [ "$code" = "200" ] || [ "$code" = "201" ] && { registered="$(read_handle)"; break; }
    sleep 2
done

[ "$registered" = "$ENV_IDP_HANDLE" ] \
    || fail "${OC_ENV}'s IdP handle is '${registered:-<none>}', not ${ENV_IDP_HANDLE}." \
            "amp-api's reconciler keeps reclaiming it; re-run, or delete the record and register by hand."
echo "   ✅ IdP handle (${ENV_IDP_HANDLE})"

# The handle only addresses the right instance if amp-api composes it against
# the same base domain aectl published the IdP on. Helm accepts an unknown
# --set path in silence, so that value is read back off the live ConfigMap
# rather than assumed — a mismatch leaves Agent Manager addressing an IdP that
# does not exist, and nothing says so until an agent's API returns 401.
amp_base_domain="$(kubectl -n "$AMP_NS" get cm amp-api \
    -o jsonpath='{.data.THUNDER_HOST_BASE_DOMAIN}' 2>/dev/null || true)"
[ "$amp_base_domain" = "$ENV_IDP_BASE_DOMAIN" ] \
    || fail "amp-api composes environment IdP hostnames under '${amp_base_domain:-unset}', not ${ENV_IDP_BASE_DOMAIN}." \
            "The thunderHostBaseDomain value did not reach the chart — its path may have moved in ${AMP_VERSION}."

env_idp_host="$(kubectl -n "$ENV_IDP_RELEASE" get httproute \
    -o jsonpath='{.items[0].spec.hostnames[0]}' 2>/dev/null || true)"
[ "$env_idp_host" = "${ENV_IDP_HANDLE}.${ENV_IDP_BASE_DOMAIN}" ] \
    || fail "Agent Manager will address ${ENV_IDP_HANDLE}.${ENV_IDP_BASE_DOMAIN}, but the environment IdP answers on '${env_idp_host:-none}'." \
            "ENV_IDP_HANDLE must match the hostname aectl published (envidp's publicURL)."
echo "   ✅ resolves to http://${env_idp_host}:8080 — the instance aectl installed"

# ============================================================================
# Step 11: The environment's AI gateway
# ============================================================================
# The LLM proxy an AMP-governed agent's model traffic flows through, and the
# Environment annotations aep-api resolves it by. Without it an ai-agent
# deployed here reaches Anthropic directly — nothing fails, there is simply no
# governed path to route onto, and no guardrail can be attached to a running
# agent.
#
# Last, because it needs step 10's registration: it resolves this environment's
# id out of amp-api before it can bind a gateway to it. Kept in its own script
# because a gateway is per-environment — adding a second environment means
# running that script again, not re-running this one.
#
# WITH_AI_GATEWAY=0 skips it, for an install that only needs the two products
# standing up rather than governed model access.
if [ "${WITH_AI_GATEWAY:-1}" = "1" ]; then
    AIGW_SCRIPT="${SCRIPT_DIR}/setup-environment-aigateway.sh"
    [ -x "$AIGW_SCRIPT" ] \
        || fail "Missing or non-executable ${AIGW_SCRIPT}." "chmod +x it, or set WITH_AI_GATEWAY=0 to skip."
    ORG_NS="$ORG_NS" OC_ENV="$OC_ENV" CLUSTER_NAME="$CLUSTER_NAME" \
        PUBLIC_THUNDER_URL="$PUBLIC_THUNDER_URL" THUNDER_NS="$THUNDER_NS" \
        THUNDER_RELEASE="$THUNDER_RELEASE" AMP_NS="$AMP_NS" AMP_API_URL="$AMP_API_URL" \
        bash "$AIGW_SCRIPT" "$ORG_NS" "$OC_ENV"
else
    echo ""
    echo "⏭️  Skipping the AI gateway (WITH_AI_GATEWAY=0) — agents will call their model directly"
fi

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
echo "  ${OC_ENV} is registered against the identity provider aectl installed"
echo "  (${ENV_IDP_RELEASE}), not a second one. Agent Manager's own"
echo "  add-environment-thunder.sh is NOT used and must not be run for this"
echo "  environment — it would provision a parallel IdP for the same"
echo "  environment and both products would stop agreeing on agent identity."
echo ""
echo "  Adding a FURTHER environment is still Agent Manager's own step, and"
echo "  needs these two values, which its defaults name a hostname this"
echo "  cluster does not publish:"
echo ""
echo "    PLATFORM_THUNDER_ISSUER=${PUBLIC_THUNDER_URL}"
echo "    PLATFORM_THUNDER_JWKS_URL=${PLATFORM_THUNDER_JWKS_URL}"
