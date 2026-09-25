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
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

# Local-dev bridge: make the SRE agent consume the same AE-managed org
# Anthropic key the user saves from the Console. This does NOT copy a separate
# SRE key. It authors an ExternalSecret in the observability namespace that
# watches AE's OpenBao path and materializes:
#
#   openchoreo-observability-plane/rca-agent-anthropic-secret
#     RCA_LLM_API_KEY
#
# When the Console later saves or rotates the default org Anthropic key, AE's
# credential flow mirrors it to OpenBao and ESO refreshes the SRE Secret
# automatically. The SRE pod requires this Secret and therefore waits instead
# of accepting the first alert without credentials.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$SCRIPT_DIR/env.sh"

NS="${SRE_AGENT_NAMESPACE:-openchoreo-observability-plane}"
SECRET_NAME="${SRE_ANTHROPIC_SECRET_NAME:-rca-agent-anthropic-secret}"
SECRET_KEY="${SRE_ANTHROPIC_SECRET_KEY:-RCA_LLM_API_KEY}"
ORG_ID="${SRE_ANTHROPIC_ORG_ID:-default}"
DB_CONTAINER="${AEP_DB_CONTAINER:-aep-db}"
SECRET_REF_NAME="${SRE_ANTHROPIC_SECRET_REF_NAME:-anthropic-secrets}"
REMOTE_PROPERTY="${SRE_ANTHROPIC_REMOTE_PROPERTY:-api-key}"

CURRENT_CTX="$(kubectl config current-context 2>/dev/null || true)"
if [ -z "$CURRENT_CTX" ] || [ "$CURRENT_CTX" != "$CLUSTER_CONTEXT" ]; then
    echo "⚠️  reconcile-sre-anthropic-externalsecret: current kubectl context ($CURRENT_CTX) != $CLUSTER_CONTEXT — refusing to run."
    exit 1
fi

if ! kubectl --context "$CLUSTER_CONTEXT" get namespace "$NS" >/dev/null 2>&1; then
    echo "ℹ️  $NS does not exist yet — skipping SRE Anthropic ExternalSecret."
    exit 0
fi

remote_key=""
if docker ps --format '{{.Names}}' | grep -qx "$DB_CONTAINER"; then
    remote_key="$(docker exec "$DB_CONTAINER" psql -U aep -d aep -At \
        -c "select coalesce(secret_ref_kv_path, '') from org_anthropic_credentials where oc_org_id='${ORG_ID}' and role='default' limit 1;" 2>/dev/null || true)"
fi

if [ -z "$remote_key" ]; then
    # The Console may not have saved the key yet. We can still author the
    # ExternalSecret before the secret exists because the default org base
    # namespace is created during setup and the default role's SecretRef name is
    # deterministic (`anthropic-secrets`). Once the Console write happens, ESO
    # will refresh this same path.
    wc_namespaces="$(kubectl --context "$CLUSTER_CONTEXT" get ns -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | grep '^wc-' || true)"
    wc_count="$(printf '%s\n' "$wc_namespaces" | sed '/^$/d' | wc -l | tr -d ' ')"
    if [ "$wc_count" != "1" ]; then
        echo "ℹ️  Cannot derive default org OpenBao namespace yet (found $wc_count wc-* namespaces)."
        echo "   Save the Anthropic key in the AE Console, then rerun start.sh."
        exit 0
    fi
    org_base_ns="$(printf '%s\n' "$wc_namespaces" | sed '/^$/d' | head -1)"
    remote_key="user-app-secrets/${org_base_ns}/${SECRET_REF_NAME}"
fi

kubectl --context "$CLUSTER_CONTEXT" apply -f - <<EOF >/dev/null
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: ${SECRET_NAME}
  namespace: ${NS}
  labels:
    app.kubernetes.io/managed-by: aep-local-setup
    aep.wso2.com/source: ae-org-anthropic
spec:
  refreshInterval: 15s
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: ${SECRET_NAME}
    creationPolicy: Owner
  data:
    - secretKey: ${SECRET_KEY}
      remoteRef:
        key: ${remote_key}
        property: ${REMOTE_PROPERTY}
EOF

kubectl --context "$CLUSTER_CONTEXT" -n "$NS" annotate externalsecret "$SECRET_NAME" \
    "force-sync=$(date +%s)" --overwrite >/dev/null 2>&1 || true

echo "✅ SRE Anthropic ExternalSecret reconciled: $NS/$SECRET_NAME <- $remote_key#$REMOTE_PROPERTY"
