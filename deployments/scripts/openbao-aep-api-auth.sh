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

# aep-api's OpenBao access: the write-only policy aep-api-writer and the
# Kubernetes-auth role aep-api that binds it to service account aep-api in
# wso2-aep. aep-api holds no static token; it logs in with its projected
# service-account token and can then only create/update user-app secrets,
# delete their metadata, and read the environment Thunder bindings.
#
# Local only. The k3d OpenBao runs in dev mode (in memory), so every openbao-0
# restart forgets this policy and role while the upstream values re-create only
# OpenChoreo's own roles. That is why this is its own idempotent script, run by
# setup-env-for-aectl.sh after the ClusterSecretStore, by `make dev-update`
# before the helm upgrade, and by the "Local OpenBao wipe recovery" runbook in
# deployments/README.md. Re-running it overwrites the policy and role with the
# same values.
#
# The dev root token is the OpenBao chart's own public dev value. It is piped
# on stdin, never on argv. Override with OPENBAO_ROOT_TOKEN.
set -euo pipefail

echo "   OpenBao: aep-api write-only policy and Kubernetes-auth role"

printf '%s' "${OPENBAO_ROOT_TOKEN:-root}" | kubectl -n openbao exec -i openbao-0 -- bao login -no-print -

# The upstream values enable auth/kubernetes from the pod's start-up script; a
# freshly restarted pod can be Ready a moment before that mount exists. The
# role write below fails loudly if it never appears.
for _ in $(seq 1 30); do
  if kubectl -n openbao exec openbao-0 -- bao auth list -format=json 2>/dev/null | grep -q '"kubernetes/"'; then
    break
  fi
  sleep 2
done

kubectl -n openbao exec -i openbao-0 -- bao policy write aep-api-writer - <<'POLICY'
path "secret/data/user-app-secrets/*"     { capabilities = ["create", "update"] }
path "secret/metadata/user-app-secrets/*" { capabilities = ["delete"] }
path "secret/data/aep/thunder/*"          { capabilities = ["read"] }
POLICY
kubectl -n openbao exec openbao-0 -- bao write auth/kubernetes/role/aep-api \
  bound_service_account_names=aep-api bound_service_account_namespaces=wso2-aep \
  policies=aep-api-writer ttl=1h
