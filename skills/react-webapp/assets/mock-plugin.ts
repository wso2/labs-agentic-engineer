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

// Mock mode — copied verbatim to <app-path>/mock/plugin.ts, never edited.
//
// The API itself is Mock Service Worker's job (./browser.ts, ./handlers.ts).
// This plugin covers the two things a request interceptor structurally cannot,
// plus the one file MSW needs served:
//
//   /env-config.js          window._env_ must be set BEFORE the bundle runs,
//                           and the worker starts inside it
//   src/auth.ts             a module swap, not a request
//   /mockServiceWorker.js   served out of node_modules, so the worker script
//                           never enters public/ and never reaches dist/
//
// It is added under `--mode mock` only and runs in Node at dev-server time, so
// nothing here reaches the production bundle.

import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import type { Plugin } from "vite";
import { mockEnv } from "./env";

export function mockMode(): Plugin {
  let root = process.cwd();
  return {
    name: "aep-mock-mode",
    enforce: "pre",

    configResolved(config) {
      root = config.root;
    },

    // Sign-in without an IDP: every import that resolves to src/auth.ts gets
    // mock/auth.ts instead, so no module under src/ knows mock mode exists.
    async resolveId(source, importer, options) {
      if (!importer || importer.includes(`${path.sep}mock${path.sep}`)) return null;
      const resolved = await this.resolve(source, importer, { ...options, skipSelf: true });
      if (!resolved) return null;
      const file = resolved.id.split("?")[0];
      return file === path.join(root, "src", "auth.ts") ? path.join(root, "mock", "auth.ts") : null;
    },

    configureServer(server) {
      server.middlewares.use("/env-config.js", (_req, res) => {
        res.setHeader("Content-Type", "application/javascript; charset=utf-8");
        res.end(`window._env_ = ${JSON.stringify(mockEnv, null, 2)};\n`);
      });

      // `msw init` would copy this into public/, where it would be committed and
      // then shipped inside every production image. Resolving it from the
      // installed package instead keeps the repo and dist/ clean, and keeps the
      // script in lockstep with the msw version in package-lock.json.
      server.middlewares.use("/mockServiceWorker.js", (_req, res) => {
        const require = createRequire(path.join(root, "package.json"));
        res.setHeader("Content-Type", "application/javascript; charset=utf-8");
        res.setHeader("Service-Worker-Allowed", "/");
        res.end(fs.readFileSync(require.resolve("msw/mockServiceWorker.js"), "utf-8"));
      });
    },
  };
}
