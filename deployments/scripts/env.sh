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

# Shared cluster environment variables — sourced by all scripts in this directory.
#
# OPENCHOREO_VERSION history:
#   1.0.1-hotfix.1 -> 1.1.1  the Resource model (ResourceType/Resource/
#     ResourceReleaseBinding/ClusterResourceType) that the postgres-cnpg
#     platform-resource sample depends on first ships in OC v1.1.0.
#   1.1.1 -> 1.2.0  convergence with Agent Manager on one cluster (see
#     docs/design/agent-manager-convergence.md). Agent Manager's platform
#     charts require ProjectType, which lands in OC 1.2.0, and it cannot go
#     backwards — so AEP moves forward. Pinned to 1.2.0 (not the newer 1.2.2)
#     to match the version Agent Manager's own charts are validated against.
OPENCHOREO_VERSION="1.2.0"
CNPG_VERSION="0.29.0"
CLUSTER_NAME="openchoreo"
CLUSTER_CONTEXT="k3d-${CLUSTER_NAME}"

# ── WSO2 API Platform gateway-operator ───────────────────────────────────────
# Bumped 0.6.0 -> 0.11.0 for the OC 1.2.0 / Agent Manager convergence. The
# operator upgrades in place but does NOT downgrade, so this pin is
# unconditional — it is not behind ENABLE_AGENT_MANAGER.
#
# The chart trails the images: 1.2.1 gateway-controller/gateway-runtime images
# are published but no 1.2.1 gateway chart is. Pin the newest chart (1.2.2) and
# carry the runtime to 1.2.1 via the image-tag overrides in
# manifests/api-platform/operator-values.yaml.
GATEWAY_OPERATOR_VERSION="0.11.0"
GATEWAY_CHART_VERSION="1.2.2"
GATEWAY_IMAGE_VERSION="1.2.1"

# ── Agent Manager (optional, opt-in) ─────────────────────────────────────────
# ENABLE_AGENT_MANAGER=1 installs the Agent Management Platform alongside AEP
# on this same cluster, from WSO2's published OCI charts. Off by default: it
# adds ~22 pods / ~4-5 GB of RAM and forces the observability plane on.
# See docs/design/agent-manager-convergence.md.
ENABLE_AGENT_MANAGER="${ENABLE_AGENT_MANAGER:-0}"
AMP_VERSION="${AMP_VERSION:-1.0.0-rc2}"
AMP_REGISTRY="${AMP_REGISTRY:-oci://ghcr.io/wso2}"
# Namespace of the ONE platform Thunder both products authenticate against.
# The release is wso2-amp-thunder-extension (a thin wrapper around ThunderID),
# installed unconditionally — see setup-openchoreo.sh step 3.
THUNDER_NS="${THUNDER_NS:-amp-thunder}"
THUNDER_RELEASE="${THUNDER_RELEASE:-amp-thunder-extension}"
# In-cluster address of that Thunder. Every jwks/token URL in this repo derives
# from these two, so moving Thunder is a one-line change here.
THUNDER_SVC_HOST="${THUNDER_RELEASE}-service.${THUNDER_NS}.svc.cluster.local"
THUNDER_INTERNAL_URL="http://${THUNDER_SVC_HOST}:8090"

# Community observability modules compatible with OpenChoreo 1.2.0.
# Tracing MUST be >= 0.6.0: the 1.2.0 observer returns span status as an object
# ({code,message}) but adapters below 0.6.0 still return a string, so
# span-details 500 and traces are dropped.
OBSERVABILITY_LOGS_VERSION="0.5.3"
OBSERVABILITY_TRACING_VERSION="0.6.0"
OBSERVABILITY_METRICS_VERSION="0.6.1"

# Agent Sandbox community module (openchoreo registry, versioned independently
# of AMP). Only installed when ENABLE_AGENT_MANAGER=1.
AGENT_SANDBOX_MODULE_VERSION="${AGENT_SANDBOX_MODULE_VERSION:-0.1.1}"
AGENT_SANDBOX_UPSTREAM_VERSION="${AGENT_SANDBOX_UPSTREAM_VERSION:-v0.4.6}"
