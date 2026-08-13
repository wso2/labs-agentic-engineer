#!/bin/sh
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

set -e

echo "AEP Console — Initializing runtime configuration..."

AEP_API_PROXY_URL="${AEP_API_PROXY_URL:-http://localhost:9090}"

# window._env_ keys the SPA reads (src/config/env.ts RuntimeEnv).
# VITE_API_BASE_URL defaults to the nginx-side same-origin proxy path.
# collabWsUrl is forward-wiring for the #86 spec-collab feature (in flight
# on console/sso-91): empty means the SPA falls back to same-origin
# /collab, which the nginx block below proxies.
VITE_API_BASE_URL_VAL="${VITE_API_BASE_URL:-/aep-api-service}"
THUNDER_URL="${VITE_THUNDER_URL:-}"
THUNDER_CLIENT_ID="${VITE_THUNDER_CLIENT_ID:-aep-console-client}"
THUNDER_SCOPES="${VITE_THUNDER_SCOPES:-openid profile email}"
COLLAB_WS_URL="${COLLAB_WS_URL:-}"
# WSO2 Cloud only — set by the ReleaseBinding. Empty locally so the SPA skips
# the first-login billing activation call.
BILLING_API_BASE_URL_VAL="${BILLING_API_BASE_URL:-}"

# env-config.js is generated at start so the SPA can read runtime config.
# The heredoc is unquoted so the values above expand.
NOW="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
cat > /usr/share/nginx/html/env-config.js <<EOF_INNER || echo "env-config.js is read-only, skipping write"
// Runtime environment configuration
// Generated at: ${NOW}

window._env_ = {
  VITE_API_BASE_URL: "${VITE_API_BASE_URL_VAL}",
  VITE_THUNDER_URL: "${THUNDER_URL}",
  VITE_THUNDER_CLIENT_ID: "${THUNDER_CLIENT_ID}",
  VITE_THUNDER_SCOPES: "${THUNDER_SCOPES}",
  collabWsUrl: "${COLLAB_WS_URL}",
  BILLING_API_BASE_URL: "${BILLING_API_BASE_URL_VAL}",
};
EOF_INNER

echo "Runtime configuration generated"

# Substitute the DNS resolver placeholder with nameserver IPs from
# /etc/resolv.conf. Kubelet auto-populates this file with the cluster DNS
# service ClusterIP, so this works regardless of whether the cluster uses
# kube-dns or CoreDNS or what the service is named. Using a hardcoded
# hostname (e.g. kube-dns.kube-system.svc.cluster.local) is a chicken-and-
# egg problem — nginx needs a working resolver to look up its resolver.
DNS_RESOLVERS="$(awk '/^nameserver/ {print $2}' /etc/resolv.conf | tr '\n' ' ' | sed 's/ $//')"
if [ -z "$DNS_RESOLVERS" ]; then
    echo "WARNING: no nameservers in /etc/resolv.conf; variable-based proxy_pass will 502"
    DNS_RESOLVERS="127.0.0.11"
fi
sed -i "s|__DNS_RESOLVERS__|${DNS_RESOLVERS}|g" /etc/nginx/nginx.conf

sed -i "s|__AEP_API_PROXY_URL__|${AEP_API_PROXY_URL}|g" /etc/nginx/nginx.conf

# Extract host:port from the proxy URL for variable-based proxy_pass
# (nginx variable triggers runtime DNS resolution via the resolver,
# avoiding stale cached IPs after upstream pod restarts).
AEP_API_BACKEND="$(echo "${AEP_API_PROXY_URL}" | sed 's|^https\{0,1\}://||' | sed 's|/.*||')"
sed -i "s|__AEP_API_BACKEND__|${AEP_API_BACKEND}|g" /etc/nginx/nginx.conf

# Collab-server upstream. nginx.conf uses `set $collab_backend host:port`
# in the /collab/ block so DNS is resolved at request time (the container
# starts even when the upstream is missing — a /collab/ request will then
# 502 instead of preventing nginx from starting at all). Default upstream
# matches docker-compose's service name; OC overrides via COLLAB_SERVER_URL.
COLLAB_SERVER_URL="${COLLAB_SERVER_URL:-}"
if [ -n "$COLLAB_SERVER_URL" ]; then
    # Strip scheme + trailing slash so we end up with "host:port".
    COLLAB_BACKEND="${COLLAB_SERVER_URL#*://}"
    COLLAB_BACKEND="${COLLAB_BACKEND%/}"
    sed -i "s|set \$collab_backend [^;]*;|set \$collab_backend ${COLLAB_BACKEND};|" /etc/nginx/nginx.conf
fi

echo "Configuration summary:"
echo "  API Proxy:     /aep-api-service/ -> ${AEP_API_PROXY_URL}/"
echo "  Thunder URL:   ${THUNDER_URL:-[NOT SET]}"
echo "  Client ID:     ${THUNDER_CLIENT_ID}"
echo "  Billing API:   ${BILLING_API_BASE_URL_VAL:-[NOT SET — activation skipped]}"
echo "  Collab Server: ${COLLAB_SERVER_URL:-[default: collab-server:3400 via lazy DNS — 502s if upstream missing]}"

echo "Starting nginx on port 3000..."
exec "$@"
