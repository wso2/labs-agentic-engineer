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
# Read-only: the four lists that name a platform image must agree, or
# `make dev-update` rebuilds one image and rolls the pod onto another.
set -euo pipefail
cd "$(dirname "$0")/../.."
want=(aep-api ae-design-agent ae-collab aep-mcp-server console tryit)
skaffold=$(grep -oE 'image: ghcr.io/wso2/aep/[a-z-]+' skaffold.yaml | sed 's#.*/##' | sort -u)
imports=$(sed -n '/^dev-images:/,/--cluster openchoreo/p' Makefile | grep -oE 'ghcr.io/wso2/aep/[a-z-]+:dev-local' | sed 's#.*/##;s#:.*##' | sort -u)
sets=$(sed -n '/^dev-update:/,/rollout restart/p' Makefile | grep -oE 'repository=ghcr.io/wso2/aep/[a-z-]+' | sed 's#.*/##' | sort -u)
values=$(grep -oE 'repository: ghcr.io/wso2/aep/[a-z-]+' deployments/helm-charts/platform/values.yaml | sed 's#.*/##' | sort -u)
expected=$(printf '%s\n' "${want[@]}" | sort -u)
fail=0
for name in skaffold imports sets values; do
  if [ "${!name}" != "$expected" ]; then
    echo "image list '$name' differs from the expected set:"
    diff <(echo "$expected") <(echo "${!name}") || true
    fail=1
  fi
done
exit $fail
