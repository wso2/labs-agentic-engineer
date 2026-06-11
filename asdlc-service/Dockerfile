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

FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/asdlc-api ./cmd/asdlc-api

FROM alpine:3.21

# git is still needed because the merged binary owns the repo working-tree
# operations the standalone git-service used to handle. WS0.4 will swap
# shell-git for the GitHub Git Data API and let us drop this dependency.
RUN apk add --no-cache git
RUN addgroup -g 1000 appgroup && adduser -S -u 1000 -G appgroup appuser
RUN mkdir -p /data/repos && chown -R appuser:appgroup /data/repos
# Allow git to operate in bind-mounted directories regardless of ownership
# (on macOS/Colima the host user maps to root inside the VM, but container
# runs as UID 1000).
RUN mkdir -p /home/appuser && \
    printf '[safe]\n\tdirectory = *\n' > /home/appuser/.gitconfig && \
    chown -R appuser:appgroup /home/appuser

WORKDIR /app
COPY --from=builder /app/bin/asdlc-api .

USER appuser

RUN git config --global --add safe.directory '*'

EXPOSE 8080

ENTRYPOINT ["./asdlc-api"]
