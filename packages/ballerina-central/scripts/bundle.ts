/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Emit the command as ONE dependency-free file plus a launcher.
 *
 * Bundling is what makes delivery a copy: the runner image gets a directory on
 * `PATH` and nothing else — no `npm install`, no second dependency tree next to
 * the runner's own, no chance of the two disagreeing about zod.
 *
 * Two files, and the split is deliberate:
 *
 *   `bal-library.mjs`  the bundle. The extension is load-bearing — it is what
 *                      tells node this is ESM no matter which directory the
 *                      file ends up in. A `.js` here would take its module
 *                      system from the nearest `package.json`, which is how the
 *                      predecessor script died inside a project that declared
 *                      `"type": "module"`.
 *   `bal-library`      a two-line `sh` launcher, because the thing that has to
 *                      be on `PATH` is a command named `bal-library`, and a
 *                      shell script has no module system to get wrong.
 */

import { chmodSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const packageDir = dirname(dirname(fileURLToPath(import.meta.url)));
const distDir = join(packageDir, "dist");

await build({
  entryPoints: [join(packageDir, "src", "main.ts")],
  outfile: join(distDir, "bal-library.mjs"),
  bundle: true,
  platform: "node",
  format: "esm",
  // The floor is the runner image's node and this repo's `engines.node`.
  target: "node22",
  // Not minified on purpose: the file is read when a lookup misbehaves inside a
  // container, and a few hundred KB costs nothing next to a multi-GB image.
  minify: false,
  banner: { js: "#!/usr/bin/env node" },
});

// `$0` is the launcher's own path even when it was found through `PATH`, so the
// bundle is located relative to it rather than to the caller's cwd.
const launcher = `#!/bin/sh
exec node "$(dirname "$0")/bal-library.mjs" "$@"
`;
const launcherPath = join(distDir, "bal-library");
writeFileSync(launcherPath, launcher);
chmodSync(launcherPath, 0o755);
chmodSync(join(distDir, "bal-library.mjs"), 0o755);

process.stdout.write(`bundled ${launcherPath}\n`);
