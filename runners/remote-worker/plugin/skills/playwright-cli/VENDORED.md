# Vendored skill

This skill is vendored verbatim from Microsoft's `@playwright/cli` npm
package, version `0.1.15` (the `skills/playwright-cli/` directory it
ships for agent installation), under the Apache-2.0 license in
`LICENSE`. Do not edit these files — refresh them from the package when
bumping `PLAYWRIGHT_CLI_VERSION` in `Dockerfile.validation`:

```bash
npm pack @playwright/cli@<version>
tar xzf playwright-cli-<version>.tgz
cp -R package/skills/playwright-cli/* plugin/skills/playwright-cli/
cp package/LICENSE plugin/skills/playwright-cli/LICENSE
```

AEP-specific authoring/healing discipline lives in the `aep-validation`
skill's `references/authoring.md` and `references/healing.md`, which
defer all CLI mechanics to this one.

## Why vendored (interim)

The platform has no third-party skill/plugin install channel for the
runner today: the SDK session only loads the programmatic plugins
(`/app/plugin` + the per-task pull), `settingSources: []` deliberately
ignores `.claude/skills/` (where `playwright-cli install --skills`
writes), and dev flows bind-mount `plugin/` over the image so a
build-time copy would be masked. Building that channel for a single
skill isn't warranted. If more third-party skills accumulate, the
natural home is the BFF skills system (org skills repo + per-task
pull) with an upstream-package import source and task-kind-based
attachment — then this vendored copy can be dropped.
