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

# Primary sibling address. After copy, the coding agent sets this to the
# UPPER_SNAKE `_URL` of the primary component-kind dependency
# (todo-api → TODO_API_URL). OpenChoreo injects it on the pod.
API_URL="${TODO_API_URL:-}"
API_BACKEND="$(echo "${API_URL}" | sed 's|^https\{0,1\}://||' | sed 's|/.*||')"
if [ -z "$API_BACKEND" ]; then
    echo "aep-api-proxy: primary *_URL unset; /api will 502 until OpenChoreo injects it"
    API_BACKEND="127.0.0.1:9"
fi
sed -i "s|__API_BACKEND__|${API_BACKEND}|g" "$CONF"
