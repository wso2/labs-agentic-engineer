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

# The remote-worker runner image — ONE image for both task kinds
# (AEP_TASK_KIND=implementation and =validation). Debian-based because
# Playwright's browser builds are glibc-linked and cannot run on a musl
# base, so a single image is only possible on Debian.
#
# What each layer is for:
#   - git / gh / curl / bash / python3 — workspace provisioning, the git
#     credential helper + `gh` wrapper, and the agent's Bash tool.
#   - Go and Ballerina (pinned archives, not distro packages) + node/npm
#     from the base — the agent's compile-level build verification
#     (`go build`, `bal build`, `tsc --noEmit`, lockfile resolution) before
#     it opens a PR. There is no container runtime here: Dockerfiles are
#     verified by the post-merge OpenChoreo build, not in this pod.
#   - Playwright + a baked chromium — validation runs author and execute
#     e2e specs against the deployed system and must never download a
#     browser at task time.

FROM node:22-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        git \
        curl \
        bash \
        python3 \
        unzip \
        ca-certificates \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Pinned Go tarball. The distro/apk package the retired alpine image used
# was unpinned, so the toolchain the agent verified against could drift
# under it between rebuilds.
ARG GO_VERSION=1.25.5
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" \
    | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

# Pinned Ballerina distribution, same reasoning as the Go tarball above. The
# release zip is the archive to take over the published .deb: it carries the
# same payload, but unpacking a version-pinned archive to a fixed path is the
# shape this image already uses for Go, and it keeps dpkg/apt out of a 300MB
# third-party install.
#
# No JDK is installed alongside it. The distribution bundles its own
# (dependencies/jdk-21.0.5+11-jre) and `bal` resolves that, so `bal version`
# and `bal build` both work on an image with no system `java` — verified on
# this base. Adding a second JDK would be ~180MB of dead weight.
#
# Arch note: the amd64 asset is "…-swan-lake-linux", the arm64 one
# "…-swan-lake-linux-arm" (the .deb spells the latter "linux-arm-x64" — the
# zip does not). Both exist for every release; the GitHub release *page*
# renders only the x64 rows, so read the asset list via the API, not the page.
ARG BALLERINA_VERSION=2201.13.5
RUN case "$(dpkg --print-architecture)" in \
        amd64) BAL_ARCH="linux" ;; \
        arm64) BAL_ARCH="linux-arm" ;; \
        *) echo "no Ballerina distribution for $(dpkg --print-architecture)" >&2; exit 1 ;; \
    esac \
    && BAL_DIR="ballerina-${BALLERINA_VERSION}-swan-lake-${BAL_ARCH}" \
    && curl -fsSL -o /tmp/ballerina.zip \
        "https://github.com/ballerina-platform/ballerina-distribution/releases/download/v${BALLERINA_VERSION}/${BAL_DIR}.zip" \
    && unzip -q /tmp/ballerina.zip -d /usr/lib \
    && mv "/usr/lib/${BAL_DIR}" /usr/lib/ballerina \
    && rm /tmp/ballerina.zip \
    && chmod -R a+rX /usr/lib/ballerina
ENV PATH="/usr/lib/ballerina/bin:${PATH}"

# Playwright toolchain, pinned as a pair: AEP_PLAYWRIGHT_VERSION is the
# contract the aep-validation skill pins tests/e2e/package.json to, so
# the browsers baked here always match the version the specs run with.
ARG PLAYWRIGHT_VERSION=1.61.1
ARG PLAYWRIGHT_CLI_VERSION=0.1.15
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    AEP_PLAYWRIGHT_VERSION=${PLAYWRIGHT_VERSION}
RUN npm install -g "@playwright/test@${PLAYWRIGHT_VERSION}" "@playwright/cli@${PLAYWRIGHT_CLI_VERSION}" \
    && playwright install --with-deps chromium \
    # playwright-cli may bundle a different playwright build; bake its
    # browser too so first use at task time stays offline. Non-fatal:
    # worst case it downloads at runtime.
    && (playwright-cli install-browser || true) \
    && chmod -R a+rX /ms-playwright

RUN groupadd -r aep \
    && useradd -r -g aep -m -d /home/aep aep \
    && mkdir -p /home/aep/aep-workspace \
    && chown -R aep:aep /home/aep

WORKDIR /app

# Install dependencies first so the layer caches across source-only edits.
# --include=optional ensures the platform-native Claude binary is installed.
# The Agent SDK probes the -musl native binary BEFORE the glibc one on
# Linux and only falls through when the probe fails to resolve; npm
# installs both libc variants, so on this Debian image the SDK would
# pick the musl binary and die at spawn (ENOENT: no musl loader).
# Removing the musl variant makes resolution land on the glibc binary.
COPY --chown=aep:aep package.json package-lock.json* ./
RUN npm ci --include=optional \
    && rm -rf node_modules/@anthropic-ai/claude-agent-sdk-linux-*-musl

# .dockerignore excludes node_modules/dist/.git/etc so we never ship a
# duplicate of the dependency tree the previous step already produced.
# --chown writes ownership directly into the layer instead of a trailing
# `chown -R /app` (which would rewrite every file as a fresh layer and
# roughly double the image size).
COPY --chown=aep:aep . .

# The authored skill library. A session's base plugin is ASSEMBLED from it at
# startup (lib/base_plugin.ts selects the runner's skills and applies the mode
# overlay), so the runner reads the same repo-root skills/ the BFF seeds into org
# skills repos — one authored source, two delivery mechanisms. Supplied as a
# BuildKit named build context (`--build-context skills=<repo>/skills`) because
# the library sits outside this image's build context; the dev flow and the
# playground bind-mount their working tree over /app/skills, so a skill edit
# never needs a rebuild.
COPY --from=skills --chown=aep:aep . /app/skills

# The Ballerina Central reader the `ballerina` skill drives by name.
# `@aep/ballerina-central` bundles to ONE dependency-free `.mjs` plus a two-line
# `sh` launcher, so this is a copy — no npm install, no second dependency tree
# beside the runner's own, and nothing that could disagree with it about zod.
#
# Another BuildKit named build context
# (`--build-context balcli=<repo>/packages/ballerina-central/dist`) for the same
# reason as skills: the package is built outside this image's context. Every
# build path has to pass it — `build-runner.sh`, `release.yml`'s matrix row and
# `local/run-local.sh` — and `make build` is what produces the directory.
#
# On PATH rather than symlinked into /usr/local/bin: the playground bind-mounts
# its working-tree build over this directory, which is what makes a CLI edit
# apply to the next docker-mode run with no rebuild — exactly how /app/skills
# above already works. A symlink would resolve through the mount's own path and
# defeat the launcher's `dirname "$0"`.
COPY --from=balcli --chown=aep:aep bal-library bal-library.mjs /opt/ballerina-central/
ENV PATH="/opt/ballerina-central:${PATH}"

USER aep
ENTRYPOINT ["npx", "tsx", "src/oneshot.ts"]
