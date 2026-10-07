# ADR-0005: Host processes drive the pinned agent-browser

**Status:** Accepted (2026-10-05)

## Context

The runner image pins the `agent-browser` CLI (`AGENT_BROWSER_VERSION` in
`runners/remote-worker/Dockerfile`). Two host processes also drive a browser:
`play <dir> code --host`, whose mock walk runs on the developer's machine, and
the `evals/codegen` walker. Both used whatever global `agent-browser` the
developer had installed.

A walker on another version measures a different tool. A host 0.27.0 `click`
landed on a footer that covered a button the image's version scrolls into view,
and the eval scored two working features as failures.

## Decision

**Each package pins `agent-browser` as a devDependency equal to the image's
`AGENT_BROWSER_VERSION`.** A test in each package (`playground/test/agent-browser.test.ts`,
`evals/codegen/test/harness.test.ts`) reads the Dockerfile ARG and fails when
the pin differs, so a version bump moves both in one commit.

**The pinned copy runs through `node_modules/.bin`.** pnpm puts the package's
launcher there, and the launcher runs the native binary that the npm tarball
already ships for every platform. The package therefore needs no install
script.

**Its postinstall is blocked on purpose.** The root `onlyBuiltDependencies`
does not list it. That postinstall re-points the global
`$(npm prefix -g)/bin/agent-browser` at the local install, which would silently
replace the developer's own CLI.

**The pinned copy goes first on PATH**, for `code --host` and for the eval
walker (`withAgentBrowserFirst`). Nothing else on PATH changes. Before anything
is spent, `agentBrowserProblem` refuses the run when the launcher is missing,
because a bare `agent-browser` would otherwise resolve to the global CLI.

**`agent-browser.ts` and `runner-image.ts` are their own modules.** The eval
walker needs the PATH helpers and the provenance needs the runner image name.
Importing them from `coding-run.ts` would load `@aep/ae-design-agent`, whose module
scope merges `deployments/.env` into the importer's environment.

## Consequences

- A host walk and an image walk drive the same `agent-browser` version.
- `make install` is required before `code --host` or an eval walk; a missing
  launcher is reported with that instruction.
- The developer's global CLI is never used or changed by the playground or the
  evals.
- `evals/codegen` imports the two small modules and never imports
  `@aep/ae-design-agent`.
