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
#   - agent-browser — the browser CLI a CODING run drives a page with. It
#     adds a binary, not a browser: it is pointed at the chromium the line
#     above already baked.

# ─── Stage 1: the `bal library` tool jar ────────────────────────────────────
# BUILT here rather than committed. The tool's source is in this monorepo
# (packages/bal-library-tool), reached as a NAMED BUILD CONTEXT because this
# image's context is runners/remote-worker — the same mechanism the skill
# library uses a few lines down. Building it here is what makes a stale tool
# impossible: there is no artifact to refresh and no ordering to remember, so
# the jar in the image is always the one this commit's source produces.
# See design/decisions/ADR-0008-the-bal-library-tool-is-built-in-the-image.md.
#
# PINNED TO $BUILDPLATFORM. The jar is architecture-independent, and this image
# is released for linux/amd64 AND linux/arm64 — without the pin Gradle would run
# twice, the second time under QEMU, on an image whose arm64 half is already
# measured at 28 minutes.
FROM --platform=$BUILDPLATFORM eclipse-temurin:21-jdk AS tool-build
WORKDIR /tool

# The build files arrive first and the SOURCE last, so editing the tool's code
# reuses the layer that downloaded Gradle and resolved the dependency graph and
# only re-runs the compile. Without the split every source edit would re-resolve
# from scratch on a cold cache.
COPY --from=bal-library-tool gradle ./gradle
COPY --from=bal-library-tool gradlew settings.gradle build.gradle gradle.properties ./
COPY --from=bal-library-tool native/build.gradle ./native/build.gradle
COPY --from=bal-library-tool Ballerina.toml make-dist.sh ./
COPY --from=bal-library-tool release ./release

# Resolve the classpath before the source exists, so the download is its own
# cached layer. `:native:dependencies` touches no sources and fails on nothing.
RUN --mount=type=secret,id=packagePAT,required=true \
    --mount=type=cache,target=/root/.gradle,sharing=locked \
    packageUser=x packagePAT="$(cat /run/secrets/packagePAT)" \
    ./gradlew :native:dependencies --configuration compileClasspath --no-daemon > /dev/null

COPY --from=bal-library-tool native/src ./native/src

# A SECRET MOUNT and never an ARG or ENV, because `release.yml` builds this image
# with `cache-to mode=max` — the builder stages are published to a public GHCR
# buildcache, so a token in a build arg or a layer is published with them. A
# secret mount is never committed to a layer. Why a token is needed at all, and
# why no secret has to be provisioned for it: ADR-0008.
#
# `make-dist.sh` rather than `gradlew :native:jar` directly: it is the ONE place
# that decides what a distribution contains, so the image and a release zip
# cannot disagree about it.
# `packageUser` is any non-empty string: GitHub Packages authenticates on the
# token alone and ignores the username, which is why CI can pass `github.actor`
# — a value that varies per event and per fork — and still resolve.
RUN --mount=type=secret,id=packagePAT,required=true \
    --mount=type=cache,target=/root/.gradle,sharing=locked \
    packageUser=x packagePAT="$(cat /run/secrets/packagePAT)" \
    ./make-dist.sh

# ─── Stage 2: the runner ────────────────────────────────────────────────────
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
# PLAYWRIGHT_MCP_BROWSER names the browser TYPE for playwright-cli, whose own
# default is the pair (chromium, channel `chrome`) — one branch sets both, so
# the default engine cannot be had without Google's branded build at the system
# path /opt/google/chrome/chrome, which this image neither carries (Chrome is a
# vendor install, not a Playwright download) nor may redistribute. Naming the
# type selects the chromium baked below, and with it the sandbox default that
# can start as the non-root `aep` user. A `--browser=` flag still wins, so this
# is the default rather than a lid.
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    AEP_PLAYWRIGHT_VERSION=${PLAYWRIGHT_VERSION} \
    PLAYWRIGHT_MCP_BROWSER=chromium
RUN npm install -g "@playwright/test@${PLAYWRIGHT_VERSION}" "@playwright/cli@${PLAYWRIGHT_CLI_VERSION}" \
    && playwright install --with-deps chromium \
    # playwright-cli bundles a different playwright build, and browser
    # revisions are pinned per build (it wants chromium 1229 where
    # @playwright/test above wants 1228), so this second install is not
    # redundant — it is what keeps the CLI's first use at task time offline.
    # Named explicitly: with no argument it also fetches firefox and webkit,
    # ~546MB whose system dependencies the chromium-only line above never
    # installs, so they could be downloaded but never launched. Non-fatal:
    # worst case it downloads at runtime.
    && (playwright-cli install-browser chromium || true) \
    && chmod -R a+rX /ms-playwright

RUN groupadd -r aep \
    && useradd -r -g aep -m -d /home/aep aep \
    && mkdir -p /home/aep/aep-workspace \
    && chown -R aep:aep /home/aep

# The agent-browser CLI — what a CODING run opens a page with and reads the
# accessibility tree of, driven by the `agent-browser` platform skill. Pinned
# exactly, like the Playwright pair above: the skill tells the agent to take its
# verbs and output formats from `agent-browser skills get core`, so an unpinned
# CLI would move that contract under it between rebuilds. The npm tarball ships
# every platform's prebuilt binary, so this install fetches nothing further.
#
# npm warns EBADENGINE here (the package declares node >=24, this image is node
# 22) and that is left alone rather than worked around: the published bin is a
# static Rust binary and postinstall repoints npm's symlink straight at it, so
# no node ever runs the CLI. The engines field describes the project's own
# toolchain, not its runtime — verified: `agent-browser --version` answers.
#
# NO THIRD BROWSER IS DOWNLOADED. The CLI's own `agent-browser install` would
# fetch one; instead AGENT_BROWSER_EXECUTABLE_PATH names a chromium that is
# already in this image — the image deciding which binary launches, for the same
# reason ADR-0007 has the image and not the skill name playwright-cli's browser.
#
# WHICH of the two baked revisions: @playwright/test's, not playwright-cli's.
# Three reasons that agree — it is the only chromium this build REQUIRES (the
# `playwright-cli install-browser` above is `|| true`, so naming its revision
# would make this variable a promise the build does not keep); it is the one
# whose system libraries `--with-deps` installed; and it is the build the e2e
# specs are later asserted in, so a locator the agent reads while coding is read
# in the chromium that will assert it. Left unset the CLI's own discovery picks
# the OTHER one — it takes the highest version it finds under /ms-playwright —
# which is the drift this line closes.
#
# Resolved FROM playwright through a fixed symlink rather than spelled out: the
# revision is decided by PLAYWRIGHT_VERSION above, and a literal `chromium-1228`
# here would be a second copy of that number to keep in step by hand.
#
# ASKED OF playwright, not assembled from an install location plus a guessed
# directory name: the layout under a chromium revision is PER-ARCH, because
# playwright ships Google's Chrome-for-Testing build on linux-x64
# (`chrome-linux64/chrome`) and its own build on linux-arm64
# (`chrome-linux/chrome`). Either name hardcoded here builds green on one half
# of the release matrix and red on the other, and `executablePath()` is the
# same call playwright launches through. The `test -d` pins WHOSE core answers
# — `require.resolve` walks up to the global root, so a renamed
# @playwright/test would silently hand back another package's copy at another
# revision — and the SYMLINK IS THE DIRECTORY, so the binary keeps the resource
# files (icudtl.dat, the .pak set, the crashpad handler) beside it.
ARG AGENT_BROWSER_VERSION=0.35.2
ENV AGENT_BROWSER_EXECUTABLE_PATH=/usr/local/share/aep-chromium/chrome
# BuildKit predefines both; a stage has to redeclare them to read them in a RUN.
# Equal on a native build, different on the emulated half of the release matrix.
ARG BUILDPLATFORM
ARG TARGETPLATFORM
RUN npm install -g "agent-browser@${AGENT_BROWSER_VERSION}" \
    && test -d "$(npm root -g)/@playwright/test" \
    && CHROME="$(node -p "require(require.resolve('playwright-core', { paths: ['$(npm root -g)/@playwright/test'] })).chromium.executablePath()")" \
    && ln -sfn "$(dirname "$CHROME")" /usr/local/share/aep-chromium \
    && test -x "${AGENT_BROWSER_EXECUTABLE_PATH}" \
    # A smoke test, earning its line the way `bal library --help` does at the
    # bottom of this file: it drives a real page through the variable set
    # above, so a missing CLI, a symlink resolving to nothing, and a chromium
    # that cannot start all fail THIS BUILD rather than a customer's run.
    # `--version` would not — it answers from a binary that never launches a
    # browser. Opening and then READING the page is the round trip: the
    # launch, the CDP connection and the render all have to work for `ok` to
    # come back.
    #
    # NOT `agent-browser doctor`, which is the obvious-looking choice: its
    # launch test is the only check that touches the browser, and it ignores
    # both AGENT_BROWSER_ARGS (so it cannot be made to pass here, see below)
    # and — measured — AGENT_BROWSER_EXECUTABLE_PATH in its "Chrome" line,
    # which reports whatever the CLI's own discovery finds. `doctor --quick`
    # passes with this variable pointing at nothing at all.
    #
    # `--no-sandbox` is a BUILD-TIME concession and deliberately NOT an ENV:
    # BuildKit's RUN container denies the unprivileged user namespace
    # chromium's zygote needs ("No usable sandbox!"), while a `docker run` of
    # this image launches the same chromium sandboxed, as `aep`, unchanged —
    # measured both ways. Exporting the flag would silently drop the sandbox
    # for every run in order to make a build step pass.
    #
    # Run as `aep` because that is who runs a task: a chromium that starts for
    # root and not for an unprivileged user is the class of bug ADR-0007
    # records. `su` without `-` keeps the environment, so the variable set
    # above is the one under test.
    #
    # NATIVE BUILDS ONLY. The release builds both platforms on one amd64
    # runner, so its arm64 half runs under QEMU user-mode emulation, and
    # chromium does not run there: the browser process dies at launch and the
    # CLI reports "CDP response channel closed". Nothing about that half of the
    # IMAGE is wrong — the same layer, built natively on an arm64 host, passes
    # this test — so the launch is asserted on every native build (the
    # release's amd64 half, every developer's host-arch `docker build`) and
    # skipped on any cross-platform one. The gate is the platform pair, not a
    # probe for emulation, so a foreign-arch build that COULD launch (amd64
    # under Rosetta on a Mac) skips it too; that costs nothing the native
    # builds do not already cover. The skipped half keeps everything above the
    # `if`: the install, the symlink and the `test -x` need no browser process.
    #
    # The alternative is a release that builds its arm64 half on an arm64
    # runner, where this test would run natively on both halves. That is a
    # release-workflow change (per-platform jobs, a manifest merge) and is
    # not what fixes this; the gate stays correct under it, since a native
    # build takes the `then` branch.
    && if [ "$TARGETPLATFORM" = "$BUILDPLATFORM" ]; then \
        printf '<h1>agent-browser-ok</h1>' > /tmp/ab-smoke.html \
        && chmod a+r /tmp/ab-smoke.html \
        && su aep -s /bin/bash -c 'set -e; \
            export AGENT_BROWSER_ARGS=--no-sandbox; \
            agent-browser open file:///tmp/ab-smoke.html; \
            agent-browser get text h1 | grep agent-browser-ok; \
            agent-browser close --all' \
        # The smoke test leaves a state directory behind; a socket and pid file
        # baked into the image would be stale in every pod started from it.
        && rm -rf /tmp/ab-smoke.html /home/aep/.agent-browser; \
    else \
        echo "agent-browser smoke test skipped: ${TARGETPLATFORM} is being built on ${BUILDPLATFORM}, and chromium does not run under emulation"; \
    fi

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

# The Ballerina Central reader the `ballerina` skill drives by name — `bal
# library overview <org/name>` and its four sibling verbs.
#
# NOT a command on PATH: it is a Ballerina CLI tool, so it installs into the
# local bala repository under the user's home, registers itself in
# `~/.ballerina/.config/bal-tools.toml`, and `bal` dispatches `library` to it.
# No directory joins PATH here and no launcher script is involved.
#
# The distribution comes from the `tool-build` stage at the top of this file,
# built from this commit's source. There is no checked-in artifact to go stale
# and no refresh step to forget — see ADR-0008.
#
# Installed by RUNNING the tool's own offline installer — the same script a
# released zip carries — rather than by copying a prebuilt bala tree in. The
# installer stamps the bala's `package.json` with the distribution the `bal`
# beside it reports, and `bal` REFUSES a tool stamped newer than the
# distribution running it, so the install has to happen where the tool will run.
# That is also what ties the tool to this image's pinned Ballerina.
#
# As `aep` rather than as root: the tool resolves out of that user's own home,
# and `bal` keeps writing there at task time — its central cache, and `bal tool
# pull openapi` appending to the very `bal-tools.toml` this install writes. A
# root install would leave the whole tree needing a recursive chown to keep both
# of those working.
#
# Last in the file, so a tool change rebuilds two small layers instead of
# invalidating `npm ci`.
USER aep
COPY --from=tool-build --chown=aep:aep /tool/dist /tmp/bal-library-tool
# The `--help` call is a smoke test, and it earns its line: install.sh writes a
# bala tree and a `bal-tools.toml` entry and exits 0 without ever asking `bal`
# whether the result is loadable. Everything that can break between those two
# facts — the jar's name disagreeing with the registered version, a tool stamped
# for a distribution this image does not run, a missing service registration —
# is invisible until a task tries `bal library` inside a pod. One command here
# turns all of it into a failed build.
RUN HOME=/home/aep bash /tmp/bal-library-tool/install.sh \
    && bal library --help > /dev/null \
    && rm -rf /tmp/bal-library-tool

ENTRYPOINT ["npx", "tsx", "src/oneshot.ts"]
