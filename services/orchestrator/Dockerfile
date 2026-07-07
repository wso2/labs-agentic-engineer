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

# The orchestrator worker (services/orchestrator). Built from the REPO ROOT
# context (docker-compose sets `context: ..`) because its go.mod has a relative
# `replace` to the unpublished sibling module packages/contracts/orchestration
# (ADR-0001: no `replace` is the default; this is the documented deviation for
# an intra-repo module go.work covers at dev time but a container build must see
# directly). Preserving the repo's real directory layout inside the image keeps
# that relative path valid.
FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY packages/contracts/orchestration ./packages/contracts/orchestration
COPY services/orchestrator ./services/orchestrator
WORKDIR /app/services/orchestrator
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/orchestrator ./cmd/worker

FROM alpine:3.21

RUN addgroup -g 1000 appgroup && adduser -S -u 1000 -G appgroup appuser

WORKDIR /app
COPY --from=builder /app/bin/orchestrator .

USER appuser

ENTRYPOINT ["./orchestrator"]
