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

# Fails when a platform manifest and its verbatim copy in the platform Helm
# chart have drifted.
#
#   bash deployments/scripts/check-trait-copies.sh
#
# Two copies exist because the two installation paths do not share a source:
# `deployments/manifests/**` is what setup-*.sh applies to a local cluster with
# kubectl, and `deployments/helm-charts/platform/templates/**` is what a real
# installation renders. A drift between them is invisible until a cluster
# installed one way behaves differently from a cluster installed the other —
# for the api-configuration trait that means a component's API silently losing
# its per-operation scope enforcement on one of the two.
#
# The Helm copy is allowed exactly one thing the manifest does not have: a
# leading block of `#` comment lines saying where it came from. Everything
# after that prefix must be byte-identical to the manifest.
#
# Reached from the repo root as `make manifests-check`, and through that from
# `make test`, which CI runs.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# One private scratch file for the diffs, removed however the script exits.
# `mktemp` rather than a name built from $$: a predictable path in a shared
# /tmp is a file another user can pre-create and this script would then write
# through.
DIFF_FILE="$(mktemp "${TMPDIR:-/tmp}/trait-copy-diff.XXXXXX")"
trap 'rm -f "$DIFF_FILE"' EXIT

# source-of-truth manifest : verbatim helm copy
PAIRS=(
  "deployments/manifests/api-platform/api-configuration-trait.yaml:deployments/helm-charts/platform/templates/openchoreo-org-types/clustertrait-api-configuration.yaml"
)

EXIT_CODE=0

for pair in "${PAIRS[@]}"; do
    src="${REPO_ROOT}/${pair%%:*}"
    copy="${REPO_ROOT}/${pair##*:}"

    if [ ! -f "$src" ]; then
        printf '❌ missing source manifest: %s\n' "${pair%%:*}" >&2
        EXIT_CODE=1
        continue
    fi
    if [ ! -f "$copy" ]; then
        printf '❌ missing helm copy: %s\n' "${pair##*:}" >&2
        EXIT_CODE=1
        continue
    fi

    src_lines=$(wc -l < "$src" | tr -d ' ')
    copy_lines=$(wc -l < "$copy" | tr -d ' ')

    if [ "$copy_lines" -lt "$src_lines" ]; then
        printf '❌ %s is shorter than %s (%s < %s lines) — it cannot contain it verbatim\n' \
            "${pair##*:}" "${pair%%:*}" "$copy_lines" "$src_lines" >&2
        EXIT_CODE=1
        continue
    fi

    # The prefix the helm copy is allowed to add: comment lines only.
    prefix_lines=$(( copy_lines - src_lines ))
    if [ "$prefix_lines" -gt 0 ]; then
        bad_prefix=$(head -n "$prefix_lines" "$copy" | grep -nvE '^(#.*)?$' || true)
        if [ -n "$bad_prefix" ]; then
            printf '❌ %s: the %s line(s) it adds before the copied manifest must be comments:\n%s\n' \
                "${pair##*:}" "$prefix_lines" "$bad_prefix" >&2
            EXIT_CODE=1
            continue
        fi
    fi

    if diff -u "$src" <(tail -n "$src_lines" "$copy") > "$DIFF_FILE" 2>&1; then
        printf '✅ %s == %s (+%s header line(s))\n' "${pair%%:*}" "${pair##*:}" "$prefix_lines"
    else
        printf '❌ %s and %s have drifted:\n' "${pair%%:*}" "${pair##*:}" >&2
        sed -n '1,80p' "$DIFF_FILE" >&2
        printf '   Fix: keep the manifest as the source of truth, then re-copy it under the helm header.\n' >&2
        EXIT_CODE=1
    fi
done

exit "$EXIT_CODE"
