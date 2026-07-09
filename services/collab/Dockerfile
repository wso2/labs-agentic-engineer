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

# Collab service (services/collab) — Yjs/Hocuspocus rooms with BFF-oracle auth
# and Files-API seeding (#86 / #114). Built from the REPO ROOT context
# (docker-compose sets `context: ..`) so pnpm resolves the workspace lockfile;
# @aep/collab has no workspace-package dependencies, so only its own tree is
# copied. Runs from TypeScript source via tsx, like @aep/agents.
FROM node:22-bookworm-slim

RUN corepack enable
WORKDIR /repo

# Workspace manifests first for install-layer caching.
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
COPY services/collab ./services/collab

RUN pnpm install --frozen-lockfile --filter @aep/collab...

WORKDIR /repo/services/collab

USER 1000
EXPOSE 3400
# The server listens on COLLAB_PORT (compose sets 3400, matching the console
# nginx's default `collab-server:3400` upstream).
CMD ["pnpm", "start"]
