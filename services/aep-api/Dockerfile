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

# Built from the REPO ROOT context (docker-compose sets `context: ..`) because
# go.mod has a relative `replace` to the unpublished sibling module
# packages/contracts/orchestration (ADR-0001: no `replace` is the default;
# this is the documented deviation for an intra-repo module go.work covers at
# dev time but a container build must see directly — mirrors
# services/orchestrator/Dockerfile). Preserving the repo's real directory
# layout inside the image keeps that relative path valid.
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY packages/contracts/orchestration ./packages/contracts/orchestration
COPY services/aep-api ./services/aep-api
WORKDIR /app/services/aep-api
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/aep-api ./cmd/aep-api

FROM alpine:3.21

# git at runtime: the binary reads/writes/tags project repos via local git
# plumbing over bare mirrors on the shared /workspaces volume
# (docs/design/shared-volume-clone-architecture.md) — clone/fetch/push/tag
# shell out to the git CLI. GitHub REST remains only for repo admin, issues,
# webhooks, and App installs (§9). aep-api is the sole writer of the mount;
# the agents service consumes read-only per-SHA snapshots from it.
RUN apk add --no-cache git
RUN addgroup -g 1000 appgroup && adduser -S -u 1000 -G appgroup appuser

WORKDIR /app
COPY --from=builder /app/bin/aep-api .

USER appuser

EXPOSE 8080

ENTRYPOINT ["./aep-api"]
