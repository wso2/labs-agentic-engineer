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

set -e

echo "Try it — Initializing runtime configuration..."

# window._env_ keys the SPA reads (src/env.ts). The sign-in and the endpoint
# arrive in the LAUNCH URL, from the console; the one thing that must NOT come
# from a URL anyone can write is where a token may be sent, so the gateway
# hosts are the operator's. Space-separated; a host admits its subdomains.
GATEWAY_HOSTS="${TRY_IT_GATEWAY_HOSTS:-openchoreoapis.localhost}"
NOW="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
cat > /usr/share/nginx/html/env-config.js <<EOF_INNER || echo "env-config.js is read-only, skipping write"
// Runtime environment configuration
// Generated at: ${NOW}

window._env_ = {
  TRY_IT_GATEWAY_HOSTS: "${GATEWAY_HOSTS}",
};
EOF_INNER

echo "  Gateway hosts: ${GATEWAY_HOSTS}"
echo "Starting nginx on port 3000..."
exec "$@"
