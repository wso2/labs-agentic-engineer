# ADR-0016 — The browser comes from Debian

**Status:** Accepted. Supersedes
[ADR-0007](ADR-0007-the-runner-image-names-the-cli-browser.md), and replaces the
Playwright clause of
[ADR-0012](../../../../docs/decisions/ADR-0012-one-debian-runner-image-for-both-task-kinds.md).

The image installed `@playwright/test` to obtain one thing: a chromium for
`agent-browser` to launch. Nothing ran `playwright test` — ADR-0029 retired the
compiled-spec path — so the package was a browser delivery mechanism and the
Dockerfile said so. It is a poor one. It put 961 MB on disk to deliver a 624 MB
binary, and it delivered a **different browser on each architecture**: Playwright
ships Google's Chrome for Testing on linux-x64 and its own Chromium build on
linux-arm64, so the two halves of every release carried different browsers at
different versions (arm64 ran Chromium 149.0.7827.0) behind an
`executablePath()` call that hid the fact.

**The image now installs Debian's `chromium` package.** One package, both
architectures, the same version on each.

## Why not `agent-browser install --with-deps`

This was the obvious replacement and the reason the work started: the CLI has its
own installer, and `--with-deps` supplies the ~35 Debian libraries chromium links
against. **It is linux-x64 only.** The arm64 build of the CLI contains no
download path, no apt list and no `--with-deps` implementation at all; it prints

> Chrome for Testing does not provide Linux ARM64 builds.
>   Install Chromium from your system package manager instead:
>     sudo apt install chromium-browser   # Debian/Ubuntu
>   Then use: agent-browser --executable-path /usr/bin/chromium

and exits. Checked against the pinned 0.35.2 and against 0.38.1, the current
release. This ADR is that instruction taken.

The message is out of date on its own premise: Chrome for Testing *does* publish
a `linux-arm64` build today (`chrome-linux-arm64.zip`, 412 MB unpacked, confirmed
on the CDN at the time of writing). Neither version of the CLI reads it. So a
later release may make the installer viable on both arches — at which point the
choice below has to be argued again, not assumed.

## Why chromium and not Chrome

Nothing needs Chrome. Every verb the skills use is plain CDP — `snapshot -i`,
`click`, `wait`, `open`, `fill`, `get`, `network requests`, `console`,
`close --all`. No screenshots, no `--headed`, no extensions, no React DevTools.
Chrome's differentiators over Chromium are proprietary codecs, Widevine and
Google service integration, and none of them is exercised by an agent reading an
accessibility tree off a dev server on loopback.

Debian's chromium is also open source and redistributable in an image the
platform ships. ADR-0007 raised the licensing question against Google's branded
build and sent it to legal rather than to a Dockerfile edit; nothing here
re-opens it.

## Why unpinned, when Go and Ballerina are pinned

The rule this Dockerfile already follows is **archives are pinned, apt is not**.
Go, Ballerina and `agent-browser` each carry an `ARG`; `git`, `curl`, `python3`,
`gh`, the two base images and the browser's own ~35 system libraries never did.
The Go pin's stated reason — "the toolchain the agent verified against could
drift under it between rebuilds" — is about a compiler, where a version change
alters what the agent verifies. A browser reading an accessibility tree is not
that.

Pinning would also cost more than it buys: bookworm-security drops a superseded
chromium within weeks while go.dev keeps its tarballs forever, so an `ARG` here
breaks the build roughly monthly, on a schedule nobody chose. Against that, apt
means Debian's security team patches the largest attack surface in the image on
every rebuild, for free.

What is kept instead is **attribution**: the install layer prints
`dpkg-query -W -f='${Version}' chromium`, so a run's browser is recoverable from
the build log of the image that dispatched it. `dpkg-query` and not
`chromium --version`, because that layer also builds for the foreign
architecture under emulation, where the browser cannot run.

## Why `/usr/lib/chromium/chromium` and not `/usr/bin/chromium`

`/usr/bin/chromium` is Debian's `#!/bin/sh` launcher. Both paths start and both
pass the image's smoke test — measured, in a container, as the `aep` user. The
launcher sources `/etc/chromium.d/*` first, and what those fragments add has no
place in a pod:

- `--enable-remote-extensions`, and a `--load-extension=` that is **empty** here
  because the directory it globs does not exist;
- `--enable-gpu-rasterization`, on a machine with no GPU;
- `GOOGLE_API_KEY` and `GOOGLE_DEFAULT_CLIENT_SECRET` exported into the
  environment of a run working inside a customer's repository.

Their contents are also Debian's to change in a security update, which would
silently change how every run's browser starts. The image names the real binary
and inherits none of it — the same reason ADR-0007 had the image and not a skill
name the browser.

`chromium-sandbox` is a `Recommends`, so `--no-install-recommends` leaves it out.
That is deliberate and load-bearing: the whole `AGENT_BROWSER_ARGS=--no-sandbox`
argument rests on this image having no setuid helper.

## Rejected

- **`agent-browser install --with-deps` on x64, apt on arm64.** The literal task,
  made to work. It ships a different browser at a different version in each half
  of one image — the divergence `.github/workflows/images.yml` exists to catch.
  It also shells out to `sudo`, which this image does not have.
- **Chrome for Testing fetched by the Dockerfile.** Viable on both arches and
  45 MB smaller. The image would own a downloader, a per-arch unpack-directory
  `case` (`chrome-linux64` vs `chrome-linux-arm64` — the exact hazard that
  reached a release once), and the ~35-package apt list `--with-deps` maintains
  today. Its version would move only when someone edited an `ARG`, which for a
  browser means CVEs wait for a person.
- **`chromium-shell`.** Debian's "minimal shell", 143 MB cheaper and free of the
  desktop stack, which made it look like the better answer. Its file list settles
  it: `content_shell.pak`, `shell_resources.pak`, `libtest_trace_processor.so`.
  This is Chromium's **content_shell** layout-test harness, not Chrome's
  `chrome-headless-shell`. Different binary, different CDP surface; not something
  to hand an agent. Recorded so the 143 MB does not invite the question again.
- **Keeping Playwright and adding `--no-shell`.** Worth 334 MB for one flag —
  `chromium_headless_shell` is installed and launched by nothing — but subsumed:
  Playwright leaving takes the headless shell with it.

## Consequences

- **The image loses ~600 MB.** Measured on arm64, before against after: the
  filesystem goes 3.2 GB → 2.6 GB and the layer sum 3.67 GB → 2.96 GB. The
  amd64 image lands at the same 2.6 GB filesystem. Out:
  `/ms-playwright` (961 MB — chromium 624 MB, `chromium_headless_shell` 334 MB,
  `ffmpeg` 3 MB) and 18 MB of global `@playwright`. In: `/usr/lib/chromium`
  (350 MB) and the rest of its dependency closure.
- **A headless image gains GTK3, adwaita-icon-theme and systemd**, because
  Debian's `chromium` hard-depends on the desktop stack. `systemd` here is a
  package, not an init. Inelegant, and the price of apt owning the dependency
  graph.
- **The per-arch layout problem is gone.** One path on both architectures, so the
  `executablePath()` probe, the `/usr/local/share/aep-chromium` symlink and the
  twelve lines explaining `chrome-linux64` versus `chrome-linux` all leave the
  Dockerfile with the package that needed them.
- **A rebuild months apart gets a different browser.** Accepted; the version line
  in the build log is the mitigation.
- **The browser is newer** — Chromium 153 against Playwright's 149 on arm64. What
  an agent acts on is the accessibility tree `snapshot -i` returns, so this is
  proven by a real coding run and a real validation run, not by the build.
