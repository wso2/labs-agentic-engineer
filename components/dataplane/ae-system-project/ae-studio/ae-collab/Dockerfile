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

# The AE Studio Room (ae-collab) — Yjs/Hocuspocus rooms that verify Platform
# IdP tokens and seed from / commit through the pod's Files socket (#86).
# Built from the REPO ROOT context
# (docker-compose sets `context: ..`) because @aep/ae-collab depends on the
# workspace package @aep/collab-doc (the shared doc model, #86 phase 4).
# Runs from TypeScript source via tsx, like @aep/ae-design-agent.
FROM node:22-bookworm-slim

# Corepack fetches the pnpm release named by the root package.json's
# packageManager field into COREPACK_HOME, which defaults to the invoking
# user's home. The build runs as root and the container runs as UID 1000, so
# the copy fetched below lands in /root/.cache, where the runtime user is
# denied — and corepack then re-downloads pnpm from registry.npmjs.org on every
# container start. That makes booting depend on the public registry being
# reachable and fast, and leaves the package manager unpinned by the image
# digest even though packageManager names a version. Pointing COREPACK_HOME at
# a shared path makes the build's download the one the runtime uses.
ENV COREPACK_HOME=/opt/corepack
RUN corepack enable
WORKDIR /repo

# Workspace manifests first for install-layer caching.
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
COPY packages ./packages
COPY components/dataplane/ae-system-project/ae-studio/ae-collab ./components/dataplane/ae-system-project/ae-studio/ae-collab

RUN pnpm install --frozen-lockfile --filter @aep/ae-collab...

# @aep/collab-doc and @aep/platform-idp-auth (the pod-mode /v1 gate) resolve
# through their dist exports — build both before boot, in either mode: main.ts
# imports the pod listeners unconditionally.
RUN pnpm --filter @aep/collab-doc build && pnpm --filter @aep/platform-idp-auth build

# Readable by the unprivileged runtime user, so corepack resolves pnpm from
# this cache instead of the network. Without it the ENV above changes only
# where the build writes, not what UID 1000 can read.
RUN chmod -R a+rX "$COREPACK_HOME"

WORKDIR /repo/components/dataplane/ae-system-project/ae-studio/ae-collab

USER 1000
# Pod mode: public room listener 8081, health 9081 (the local listener is
# 127.0.0.1:8091, inside the pod only).
EXPOSE 8081 9081
CMD ["pnpm", "start"]
