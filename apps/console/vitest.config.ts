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

// Standalone config (instead of vite.config.ts) so tests skip the app's
// router-codegen and react plugins.
//
// `environment` stays "node": most of the suite is pure logic and node is
// the fastest/least-disruptive default. Component tests opt into jsdom
// per-file via a `// @vitest-environment jsdom` pragma at the top of the file.
import { configDefaults, defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // `test/` holds the few node-side tests that cannot live under src/ — see the
    // header of test/validation-fixtures.contract.test.ts for why.
    include: ["src/**/*.test.{ts,tsx}", "test/**/*.test.{ts,tsx}"],
    // Real-browser tests (`*.browser.test.tsx`) run under vitest.browser.config.ts
    // in headless Chromium — they can't run in this node/jsdom project.
    exclude: [...configDefaults.exclude, "**/*.browser.test.{ts,tsx}"],
    environment: "node",
    globals: true,
    setupFiles: ["src/test-setup.ts"],
    // Above the 5s async-util budget in `src/test-setup.ts`, so a slow CI
    // machine surfaces the real assertion failure instead of a bare test
    // timeout that says nothing about what the component was waiting for.
    testTimeout: 15_000,
    server: {
      deps: {
        inline: [
          "@wso2/oxygen-ui",
          "prismjs",
          "@mui/x-data-grid",
          "@mui/x-date-pickers",
          "@mui/x-tree-view",
        ],
      },
    },
  },
});
