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

# Build context is this module folder (no in-repo replace directives). Pinned to
# BUILDPLATFORM and cross-compiled via GOARCH for the same reason as
# services/aep-api/Dockerfile: CGO_ENABLED=0 needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src
# go.sum* because the module has no dependencies yet (no go.sum to copy).
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -o /out/ae-studio-tools ./cmd/ae-studio-tools

# Distroless has no shell or git. Phase 2 adds the git engine and switches the
# base then. UID 10001 matches the AE Studio pod security context.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ae-studio-tools /ae-studio-tools
USER 10001:10001
ENTRYPOINT ["/ae-studio-tools"]
