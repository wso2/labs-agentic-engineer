#!/usr/bin/env bash
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
#
# The closing summary of `make dev-env`: every console the finished cluster
# serves, with the login for each.
#
# Hostnames are READ FROM THE LIVE HTTPRoutes, never hard-coded. A URL in a
# success banner is believed, so a wrong one costs more than a missing one, and
# a hostname written here is a second place to remember when one changes.
# Deriving them means this file cannot disagree with what the cluster actually
# publishes; it can only fail to find a route, which it says plainly.
#
# Runs LAST, when all four are answering. setup-env-for-aectl.sh has its own
# summary for the point where only two of them are, and labels the other two as
# still to install — that one is a plan, this one is a result.
#
# Usage: bash deployments/scripts/print-consoles.sh

set -uo pipefail

# Everything reaches the cluster through the one gateway k3d maps to 8080.
PORT="${GATEWAY_PORT:-8080}"

# host <namespace> <httproute-name> — the first hostname that route publishes,
# or empty when the route is absent (its component was skipped, or failed).
host() {
    kubectl get httproute "$2" -n "$1" -o jsonpath='{.spec.hostnames[0]}' 2>/dev/null
}

# Rows are collected first and printed once the widest URL is known, rather
# than padded to a guessed column: ThunderID's is the longest and already
# overran a fixed width, running its login into the URL with no space. A
# computed width cannot be outgrown by a longer hostname later.
ROWS=()

# row <label> <url> [credential] — records a console that is serving.
row() { ROWS+=("$1"$'\t'"$2"$'\t'"${3:-}"); }

print_rows() {
    local width=0 label url cred
    for r in "${ROWS[@]}"; do
        url="$(printf '%s' "$r" | cut -f2)"
        [ "${#url}" -gt "$width" ] && width="${#url}"
    done
    for r in "${ROWS[@]}"; do
        label="$(printf '%s' "$r" | cut -f1)"
        url="$(printf '%s' "$r" | cut -f2)"
        cred="$(printf '%s' "$r" | cut -f3)"
        if [ -n "$cred" ]; then
            printf "    %-16s%-*s  %s\n" "$label" "$width" "$url" "$cred"
        else
            printf "    %-16s%s\n" "$label" "$url"
        fi
    done
}

OC_HOST="$(host openchoreo-control-plane backstage)"
THUNDER_HOST="$(host thunder thunder-httproute)"
AEP_HOST="$(host wso2-aep aep-console)"
AMP_HOST="$(host openchoreo-control-plane amp-console)"

# Kept in step with setup-env-for-aectl.sh's THUNDER_ADMIN_* and the Makefile's
# AEP_AE_ADMIN_PASSWORD, which are the values `make dev-env` installs with. An
# install that chose its own password prints the wrong thing here, which is why
# both are overridable rather than literals.
OC_LOGIN="${THUNDER_ADMIN_USER:-admin} / ${THUNDER_ADMIN_PASSWORD:-Admin@123}"
AE_LOGIN="aeadmin / ${AEP_AE_ADMIN_PASSWORD:-admin}"

echo ""
echo "============================================"
echo "  ✅ Cluster ready"
echo "============================================"
echo ""
echo "  Consoles"

[ -n "$OC_HOST" ]      && row "OpenChoreo"    "http://${OC_HOST}:${PORT}"              "$OC_LOGIN"
# ThunderID's console is at /console, not at the root — the root is the
# OAuth/OIDC surface, and sending someone there looks like a broken install.
[ -n "$THUNDER_HOST" ] && row "ThunderID"     "http://${THUNDER_HOST}:${PORT}/console" "$OC_LOGIN"
[ -n "$AEP_HOST" ]     && row "AEP"           "http://${AEP_HOST}:${PORT}"             "$AE_LOGIN"
[ -n "$AMP_HOST" ]     && row "Agent Manager" "http://${AMP_HOST}:${PORT}"             "$OC_LOGIN"
print_rows

# A console that was meant to install and did not is worth a line of its own:
# the alternative is a summary that looks complete because the missing row is
# simply absent.
MISSING=()
[ -z "$OC_HOST" ]      && MISSING+=("OpenChoreo")
[ -z "$THUNDER_HOST" ] && MISSING+=("ThunderID")
[ -z "$AEP_HOST" ]     && MISSING+=("AEP")
[ -z "$AMP_HOST" ]     && MISSING+=("Agent Manager")
if [ "${#MISSING[@]}" -gt 0 ]; then
    echo ""
    printf "  Not serving — %s" "${MISSING[0]}"
    for m in "${MISSING[@]:1}"; do printf ", %s" "$m"; done
    printf "\n"
    echo "    No HTTPRoute found. Skipped by a WITH_* flag, or its install did not finish."
fi

# Pods that never came up. Every step above can report success while a pod sits
# in ImagePullBackOff behind it — no step's own checks look past its own
# rollout — so a banner that says "ready" has to answer for the namespace as a
# whole or it is the most confident thing on screen and the least informed.
NOT_READY="$(kubectl get pods -n wso2-aep --no-headers 2>/dev/null \
    | awk '$3 != "Running" && $3 != "Completed" { print "    " $1 "  " $3 }')"
if [ -n "$NOT_READY" ]; then
    echo ""
    echo "  ⚠️  Pods not running in wso2-aep"
    echo "$NOT_READY"
    echo ""
    # Every one of these is a real fault. `make dev-env` builds and imports all
    # six service images before installing and pins the release to them
    # (dev-images, --image-tag=dev-local), so an ImagePullBackOff here is a
    # missing local build rather than an unpublished release — which is why
    # rebuilding is the first thing to try rather than waiting for a registry.
    echo "    kubectl -n wso2-aep describe pod <name>    says why"
    echo "    make dev-images                            rebuilds and re-imports all six"
fi

echo ""
