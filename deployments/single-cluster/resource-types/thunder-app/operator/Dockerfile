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

# Multi-stage build. This module is self-contained (its own go.mod), so the
# build context is the operator directory only — the monorepo root is NOT
# required. Build with:
#   docker build -t thunder-app-operator:local deployments/single-cluster/resource-types/thunder-app/operator

# ── builder ──────────────────────────────────────────────────────────────
FROM golang:1.26 AS builder
WORKDIR /workspace

# Cache dependencies before copying the full source tree.
COPY go.mod go.sum ./
RUN go mod download

COPY main.go main.go
COPY api/ api/
COPY internal/ internal/

# Static, stripped binary for a distroless base (no libc, no shell).
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-s -w" -o manager main.go

# ── runtime ──────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
