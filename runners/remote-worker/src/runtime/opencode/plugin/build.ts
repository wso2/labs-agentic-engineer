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

// Builds the guard plugin as the package DIRECTORY OpenCode loads:
//
//   <outDir>/package.json   {"name":"aep-guard","type":"module","main":"index.js"}
//   <outDir>/index.js       aep-guard.ts and everything it imports, one ESM file
//
// Why a directory and why one file are in `aep-guard.ts`. The image runs this
// (`npx tsx src/runtime/opencode/plugin/build.ts /app/runtime/opencode/aep-guard`),
// and so do the tests, so the bytes a test loads are the bytes a pod loads.
//
// usage: tsx src/runtime/opencode/plugin/build.ts <outDir>

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";

const ENTRY = path.join(path.dirname(fileURLToPath(import.meta.url)), "aep-guard.ts");

/** The `package.json` that makes the directory a plugin OpenCode will load. */
export const GUARD_PACKAGE_JSON = { name: "aep-guard", private: true, type: "module", main: "index.js" };

/** Bundle the plugin into `outDir`, creating it. Resolves to the directory. */
export async function buildGuardPlugin(outDir: string): Promise<string> {
  fs.mkdirSync(outDir, { recursive: true });
  await build({
    entryPoints: [ENTRY],
    outfile: path.join(outDir, "index.js"),
    bundle: true,
    // Node built-ins stay imports: the binary's Bun provides `node:fs`/`node:path`/
    // `node:os`, and they are the only things the bundle does not carry.
    platform: "node",
    format: "esm",
    target: "node20",
    legalComments: "inline",
    logLevel: "warning",
  });
  fs.writeFileSync(path.join(outDir, "package.json"), JSON.stringify(GUARD_PACKAGE_JSON, null, 2) + "\n");
  return outDir;
}

// Run as a script: build into the directory named on the command line.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const outDir = process.argv[2];
  if (!outDir) {
    console.error("usage: tsx src/runtime/opencode/plugin/build.ts <outDir>");
    process.exit(2);
  }
  await buildGuardPlugin(path.resolve(outDir));
}
