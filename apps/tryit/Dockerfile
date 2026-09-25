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

# Stage 0: build the SPA inside the ROOT pnpm workspace (apps/tryit is a
# member; docker-compose sets `context: ..` accordingly). @aep/tryit has no
# workspace deps and no generated sources, so the install and the build are
# the whole story. Pinned to BUILDPLATFORM for the same reason as the console:
# `dist/` is architecture-neutral and one native run serves every platform.
FROM --platform=$BUILDPLATFORM node:22-alpine AS builder

RUN corepack enable
WORKDIR /repo

COPY pnpm-workspace.yaml package.json pnpm-lock.yaml tsconfig.base.json eslint.config.mjs ./
COPY apps/tryit/package.json ./apps/tryit/

RUN --mount=type=cache,id=pnpm-store,target=/pnpm/store \
    pnpm install --frozen-lockfile --store-dir=/pnpm/store --filter @aep/tryit...

COPY apps/tryit ./apps/tryit
RUN --mount=type=cache,id=vite-tryit,target=/repo/apps/tryit/node_modules/.vite \
    pnpm --filter @aep/tryit build

# Stage 1: serve with nginx.
FROM nginx:1.28-alpine

USER root

RUN apk upgrade --no-cache \
      libcrypto3 libssl3 libexpat libpng libxml2 \
      musl musl-utils nghttp2-libs zlib

RUN addgroup -g 10001 -S aepuser && \
    adduser -u 10001 -S aepuser -G aepuser

COPY --chown=10001:10001 apps/tryit/nginx.conf /etc/nginx/nginx.conf
COPY --from=builder --chown=10001:10001 /repo/apps/tryit/dist /usr/share/nginx/html

COPY --chown=10001:10001 apps/tryit/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh

# See apps/console/Dockerfile for why /var/lib/nginx is created and chowned.
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
