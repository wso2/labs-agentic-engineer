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

# Pinned to BUILDPLATFORM and cross-compiling via GOARCH, so the arm64 image is
# never compiled under QEMU. Removing the pin makes this the most expensive step
# in the release: emulating the Go toolchain costs ~8x here (measured on a cold
# release: ~19 min against ~2 min native) and buys nothing, because CGO_ENABLED=0
# cross-compiles to a static binary for any GOARCH.
#
# TARGETARCH comes from buildx and is the arch being built for, so a local
# single-arch build resolves it to the host's. BuildKit is required — the legacy
# builder leaves $BUILDPLATFORM unset and fails to parse the empty --platform.
# That was already true here because of the cache mounts below, and BuildKit has
# been Docker's default since 23.0.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# Cache the Go module + build caches (BuildKit) so incremental rebuilds
# reuse compiled packages instead of recompiling from scratch. One shared mount
# serves both target arches: Go's build cache is content-addressed and already
# namespaces entries by GOARCH.
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o bin/aep-api ./cmd/aep-api

FROM alpine:3.21

# git at runtime: the binary reads/writes/tags project repos via local git
# plumbing over bare mirrors on the shared /workspaces volume
# (services/aep-api/design/shared-workspace-volume.md) — clone/fetch/push/tag
# shell out to the git CLI. GitHub REST remains only for repo admin, issues,
# webhooks, and App installs. aep-api is the sole writer of the mount;
# the agents service consumes read-only per-SHA snapshots from it.
RUN apk add --no-cache git tini
RUN addgroup -g 1000 appgroup && adduser -S -u 1000 -G appgroup appuser

WORKDIR /app
COPY --from=builder /app/bin/aep-api .

# The platform skill library, read at runtime and seeded into each org's
# skills repo (config SkillsDir, default /app/skills). Supplied as a BuildKit
# named build context (`--build-context skills=<repo>/skills`) so the single
# authored source at repo-root skills/ is never vendored/copied into the module.
COPY --from=skills . /app/skills

USER appuser

EXPOSE 8080

# tini as PID 1, because aep-api would otherwise hold that slot itself. git
# leaves helpers behind it (a background `gc --auto`, a remote helper outliving
# the fetch that spawned it); an orphan re-parents to PID 1, and no process
# implicitly reaps children it did not spawn, so they accumulate as zombies
# (~6/min observed on a local stack). tini reaps them and forwards signals
# through unchanged, so shutdown behaves as before. Here rather than on a
# runtime flag so every path gets it (compose, skaffold/helm, cloud).
ENTRYPOINT ["/sbin/tini", "--", "./aep-api"]
