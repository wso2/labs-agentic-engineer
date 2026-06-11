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

FROM node:22-alpine

# Runtime dependencies for workspace provisioning and agent operations:
# - git: clone repo, commit, push
# - github-cli (gh): issue/PR management by the agent
# - curl + bash + python3: credential helper scripts and Bash tool
# - go: program-level build verification (the agent runs `go mod tidy`
#   + `go build -o /dev/null .` against the component it just wrote
#   BEFORE opening its PR, so lockfile-hash mismatches, missing imports,
#   syntax errors, etc. are caught up-front). npm + tsc come for free
#   from the node:22-alpine base.
RUN apk add --no-cache \
        git \
        github-cli \
        curl \
        python3 \
        bash \
        go \
    && addgroup -S asdlc \
    && adduser -S -G asdlc asdlc \
    && mkdir -p /home/asdlc/asdlc-workspace \
    && chown -R asdlc:asdlc /home/asdlc

WORKDIR /app

# Install dependencies first so the layer caches across source-only edits.
# --include=optional ensures the platform-native Claude binary is installed.
COPY --chown=asdlc:asdlc package.json package-lock.json* ./
RUN npm ci --include=optional

# Copy the rest. .dockerignore excludes node_modules/dist/.git/etc so we
# never ship a duplicate of the dependency tree the previous step already
# produced. --chown writes ownership directly into the layer instead of a
# trailing `chown -R /app` (which would rewrite every file as a fresh
# layer and roughly double the image size).
COPY --chown=asdlc:asdlc . .

USER asdlc
ENTRYPOINT ["npx", "tsx", "src/oneshot.ts"]
