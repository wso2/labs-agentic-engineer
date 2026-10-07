/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Bundle this theme's two runtimes into dist/ with the kit's build helper:
 * `frame-runtime.js` (the sandboxed frame) and `check-runtime.js` (the
 * isolated render check). Runs after `tsc`, from dist/index.js.
 */

import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { buildThemeRuntimes } from "@wso2/prototype-kit/build";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const files = await buildThemeRuntimes({
  theme: join(root, "dist", "index.js"),
  resolveDir: root,
  outDir: join(root, "dist"),
  // Oxygen UI's single-module bundle evaluates Prism's language files, which
  // read the bare `Prism` that Prism's core publishes on Node's `global`. The
  // frame has `window` for it; the render check's bare context has neither, so
  // the bundle names the global object directly, as browser bundlers do.
  define: { global: "globalThis" },
});
process.stdout.write(`built ${files.frameRuntime} and ${files.checkRuntime}\n`);
