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

# Stage 0: build the SPA inside the ROOT pnpm workspace (apps/console is a
# member; docker-compose sets `context: ..` accordingly). @aep/console has no
# workspace runtime deps — the only cross-package input is
# packages/contracts/api/v1/openapi.yaml, consumed by `pnpm gen`, which MUST
# run here because src/generated/* is gitignored and absent from a fresh
# checkout. corepack picks the pnpm version from the root `packageManager`
# field once package.json is copied in.
#
# Pinned to BUILDPLATFORM: this stage emits `dist/` — JS, CSS and static assets,
# none of it architecture-specific — so one native run serves BOTH published
# platforms and buildx reuses the identical stage across them. Only the nginx
# stage below is genuinely per-arch, and it takes nothing from here but `dist`.
# Remove the pin and the entire toolchain (pnpm install, six workspace builds,
# vite) runs under QEMU for the arm64 image at ~6x the cost — the vite build
# alone ~7x. Native modules installed here resolve for the BUILDPLATFORM, which
# is what we want: they execute at build time and never reach the runtime image.
FROM --platform=$BUILDPLATFORM node:22-alpine AS builder

RUN corepack enable
WORKDIR /repo

# Workspace manifests + package manifests first, so the install layer (and the
# workspace-dep builds below) stay cached when only app source changes. The app
# source itself is COPY'd LAST — see the note before `COPY apps/console`.
# The pnpm content-addressable store is a BuildKit cache mount, so a cache miss
# on install relinks from the store instead of re-downloading (needs BuildKit;
# CI's buildx sets it automatically; local builds pass DOCKER_BUILDKIT=1).
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json ./
COPY packages ./packages
COPY apps/console/package.json ./apps/console/

RUN --mount=type=cache,id=pnpm-store,target=/pnpm/store \
    pnpm install --frozen-lockfile --store-dir=/pnpm/store --filter @aep/console...

# Workspace deps whose exports resolve to ./dist must be compiled first
# (@aep/prototype-model — the prototype.json model, read by the prototype
# review page and imported by @aep/agent-stream, so built before it; @aep/agent-stream — the agent-chat panel's SSE
# contracts, #130; @aep/collab-doc — the AgentInsertion mark shared with the
# editor, #86 ph6; @aep/design-projection — the rich design view's projection,
# #149; @aep/progress-view — the ONE rendering of the run progress feed, shared
# with the playground TUI; @aep/ui-cell-diagram-react — the vendored xyflow
# renderer, which ships types: dist/index.d.ts so consumers read its pre-built
# declarations instead of re-typechecking its source). The
# @aep/ui-cell-diagram-view package resolves to its src/ (types: src/index.ts)
# so tsc reads it directly — no pre-build. These depend only on packages/, so an
# app-source edit doesn't rerun them.
#
# This list is hand-maintained and drifts silently: a new dist-resolved
# workspace dep typechecks locally (its dist/ exists from an earlier build) and
# fails ONLY here, as TS2307 plus a cascade of implicit-any and
# undefined-narrowing errors in files that never mention it. Adding a dep to
# apps/console/package.json means adding a line below.
RUN pnpm --filter @aep/prototype-model build
RUN pnpm --filter @aep/agent-stream build
RUN pnpm --filter @aep/collab-doc build
RUN pnpm --filter @aep/design-projection build
RUN pnpm --filter @aep/progress-view build
RUN pnpm --filter @aep/ui-cell-diagram-react build

# App source LAST: a console-only change invalidates only this COPY and the
# gen+build below (install + the workspace-dep builds above stay cached).
COPY apps/console ./apps/console
RUN --mount=type=cache,id=vite-console,target=/repo/apps/console/node_modules/.vite \
    pnpm --filter @aep/console gen && pnpm --filter @aep/console build

# Stage 1: serve with nginx.
FROM nginx:1.28-alpine

USER root

# Pull latest patched versions of CVE-flagged packages. Scoped (not blanket
# `apk upgrade`) so alpine-baselayout's pre-upgrade script doesn't try to clean
# /var/run when this image is built inside a pod that bind-mounts it read-only.
RUN apk upgrade --no-cache \
      libcrypto3 libssl3 libexpat libpng libxml2 \
      musl musl-utils nghttp2-libs zlib

RUN addgroup -g 10001 -S aepuser && \
    adduser -u 10001 -S aepuser -G aepuser

COPY --chown=10001:10001 apps/console/nginx.conf /etc/nginx/nginx.conf
COPY --from=builder --chown=10001:10001 /repo/apps/console/dist /usr/share/nginx/html

COPY --chown=10001:10001 apps/console/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# Alpine's packaged nginx (and any `apk upgrade nginx` in cloud overlays) defaults
# temp paths to /var/lib/nginx/tmp/*, owned by the distro `nginx` user. UID 10001
# must own that tree or boot fails with:
#   mkdir() "/var/lib/nginx/tmp/client_body" failed (13: Permission denied)
# Official nginx image temps under /var/cache/nginx — chown both.
#
# /var/lib/nginx is CREATED first because the official `nginx:*-alpine` base does
# not ship it — only the apk package does. Without the mkdir, `chown -R` on a
# missing path fails the build outright, so the overlay this line exists for could
# never be reached. Creating it is correct for both cases: owned by 10001 up front,
# whether or not a later `apk upgrade nginx` populates it.
RUN mkdir -p /var/lib/nginx && \
    chown -R 10001:10001 /var/cache/nginx && \
    chown -R 10001:10001 /var/log/nginx && \
    chown -R 10001:10001 /var/lib/nginx && \
    chown -R 10001:10001 /etc/nginx && \
    chown -R 10001:10001 /usr/share/nginx/html

USER 10001

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:3000/ || exit 1

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
