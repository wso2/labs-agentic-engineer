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

# Runs from official nginx:alpine /docker-entrypoint.d/ *before* nginx starts.
# Do not exec nginx here; the image ENTRYPOINT does that.

set -e

CONF=/etc/nginx/conf.d/default.conf

DNS_RESOLVERS="$(awk '/^nameserver/ {print $2}' /etc/resolv.conf | tr '\n' ' ' | sed 's/ $//')"
if [ -z "$DNS_RESOLVERS" ]; then
    echo "aep-api-proxy: no nameservers in /etc/resolv.conf; /api will 502"
    DNS_RESOLVERS="127.0.0.11"
fi
sed -i "s|__DNS_RESOLVERS__|${DNS_RESOLVERS}|g" "$CONF"

# Primary sibling address. After copy, the coding agent renames BOTH variables
# below to the UPPER_SNAKE of the primary component-kind dependency
# (todo-api → TODO_API_GATEWAY_URL / TODO_API_URL). OpenChoreo and the platform
# inject them on the pod.
#
# Two lanes exist and they are NOT interchangeable:
#
#   <DEP>_GATEWAY_URL  the API gateway. It validates the caller's bearer token
#                      and injects the X-User-* identity headers the backend
#                      authorizes on. Set by the platform for any sibling whose
#                      design declares `exposesAPI.auth`. Carries a context
#                      path prefix.
#   <DEP>_URL          the project Service, reached directly. Nothing validates
#                      a token and nothing injects identity.
#
# Always prefer the gateway when the platform offers it. Browser traffic is
# untrusted, and this proxy is the one hop that would otherwise carry it into
# the project's trusted lane with no authentication in between.
API_URL="${TODO_API_GATEWAY_URL:-}"
API_LANE="gateway (token validated, identity injected)"
if [ -z "$API_URL" ]; then
    API_URL="${TODO_API_URL:-}"
    API_LANE="direct Service (NO token validation)"
fi

# Split the injected address into host:port and the context path prefix.
API_BACKEND="$(echo "${API_URL}" | sed -e 's|^https\{0,1\}://||' -e 's|/.*$||')"
API_CONTEXT="$(echo "${API_URL}" | sed -e 's|^https\{0,1\}://[^/]*||' -e 's|/$||')"

if [ -z "$API_BACKEND" ]; then
    echo "aep-api-proxy: no sibling API address injected; /api will 502 until one is"
    API_BACKEND="127.0.0.1:9"
    API_CONTEXT=""
fi

echo "aep-api-proxy: /api -> ${API_BACKEND}${API_CONTEXT}  [${API_LANE}]"
sed -i "s|__API_BACKEND__|${API_BACKEND}|g" "$CONF"
sed -i "s|__API_CONTEXT__|${API_CONTEXT}|g" "$CONF"
