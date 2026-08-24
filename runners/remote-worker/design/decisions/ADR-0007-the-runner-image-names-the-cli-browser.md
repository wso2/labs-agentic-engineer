# ADR-0007 — The runner image names playwright-cli's browser

**Status:** Accepted

`playwright-cli` defaults to the *pair* (engine `chromium`, channel `chrome`), so
its default engine cannot be had without Google's branded build at
`/opt/google/chrome/chrome`. The image bakes only Playwright's own chromium, so
the bare `playwright-cli open <url>` that the `aep-validation` skill instructs
died at launch behind `Daemon process exited with code 1`, and every validation
run spent turns rediscovering it (#570). The image now sets
**`PLAYWRIGHT_MCP_BROWSER=chromium`**, which selects channel
`chrome-for-testing` — the baked build — and with it the sandbox branch that
works as the non-root `aep` user. `runner.ts` already spreads `process.env` into
the agent's env, so the skill's existing instruction becomes correct as written;
an explicit `--browser=` still wins.

## Rejected

- **Installing Chrome.** Channel `chrome` also enables the Chromium sandbox,
  which cannot start unprivileged, so this moves the failure one layer deeper. It
  further needs root plus Google's apt repo, auto-versions against the "pinned as
  a pair" contract, and — proprietary, in an image the platform redistributes —
  raises a licensing question for legal rather than a Dockerfile edit.
- **A `.playwright/cli.config.json`.** Naming `browser.browserName` there leaves
  `channel` undefined and re-enables that sandbox. Recorded because it is the
  obvious-looking simplification of this ADR.

## Consequences

- Exploration and the specs run **different chromium builds**: playwright-cli
  pins its own playwright-core (revision 1229) against `AEP_PLAYWRIGHT_VERSION`'s
  (1228), so a locator captured while exploring is asserted in another build.
  Closing that is a version alignment, outside this ADR.
- `@playwright/test` is untouched; it never forces a channel.

Related: ADR-0006 pins endpoint *resolution*, including for playwright-cli via
`$PLAYWRIGHT_MCP_CONFIG`; this ADR is which binary launches. #570 carries the
diagnosis, #571 the before/after evidence.
