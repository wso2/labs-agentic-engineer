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

# NEW file-mutation agents service (services/agents). Built from the REPO ROOT
# context (docker-compose sets `context: ..`) because @aep/agents imports the
# workspace package @aep/agent-stream — pnpm must see the whole workspace to
# resolve it. @aep/agent-stream in turn depends on @aep/excalidraw-dsl (the
# wireframes layout write-gate), so that package must be installed AND built
# too. (@aep/design-projection stays a playground/evals-only devDependency —
# never imported by src/main.ts, so not built here.)
FROM node:22-bookworm-slim

RUN corepack enable
WORKDIR /repo

# Workspace + package manifests first so the install layer (and the
# workspace-dep builds below) stay cached when only agents source changes; the
# service source is COPY'd LAST. tsconfig.base.json is the shared base
# @aep/agent-stream's tsconfig extends — needed for the tsc build below. The
# pnpm store is a BuildKit cache mount (needs BuildKit; CI's buildx sets it
# automatically; local builds pass DOCKER_BUILDKIT=1).
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
# The workspace packages @aep/agents depends on, plus the agents manifest only.
COPY packages ./packages
COPY services/agents/package.json ./services/agents/

# Install @aep/agents + its workspace deps. This also installs the workspace ROOT
# project's devDependencies (pnpm always installs the root's deps) — so `typescript`
# is present to compile @aep/agent-stream in the next step. (@types/node and tsx
# ride in via @aep/agents' own dependencies.)
RUN --mount=type=cache,id=pnpm-store,target=/pnpm/store \
    pnpm install --frozen-lockfile --store-dir=/pnpm/store --filter @aep/agents...

# @aep/agents runs from TypeScript source via tsx (no build of its own), but it
# imports @aep/agent-stream and @aep/collab-doc as packages whose `exports`
# resolve to ./dist — both MUST be compiled before the service can start.
# @aep/excalidraw-dsl and @aep/prototype-model build FIRST: @aep/agent-stream
# imports both (the layout and prototype write-gates), so their dist must exist
# for agent-stream's tsc to resolve them; @aep/agents imports
# @aep/prototype-model directly too (the turn-snapshot filter).
# Without this step the container crashes at boot with "Cannot find module
# .../@aep/agent-stream/dist" (or the collab-doc analog). These depend only on
# packages/, so an agents-source edit doesn't rerun them.
RUN pnpm --filter @aep/excalidraw-dsl build && pnpm --filter @aep/prototype-model build && pnpm --filter @aep/agent-stream build && pnpm --filter @aep/collab-doc build

# Service source LAST: an agents-only change invalidates only this COPY (install
# + the workspace-dep builds above stay cached). @aep/agents runs via tsx, so
# there is no build step of its own.
COPY services/agents ./services/agents

WORKDIR /repo/services/agents

USER 1000
EXPOSE 4000
# The service listens on PORT (default 4000) and serves GET /healthz.
CMD ["pnpm", "start"]
