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

import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // parse/report/counts/shortId are pure logic and node is the fastest
    // default; ValidationView.test.tsx opts into jsdom per-file via a
    // `// @vitest-environment jsdom` pragma, mirroring design-view and
    // apps/console.
    environment: "node",
    // Source only. `build` compiles tests into dist/ alongside the library, and
    // vitest's default glob collects those stale copies and runs them against
    // whatever the source was at build time (matches design-view).
    include: ["src/**/*.test.{ts,tsx}"],
    // Needed so @testing-library/react's auto-cleanup-between-tests effect
    // detects a global `afterEach` and actually registers — it silently no-ops
    // without a global test-framework hook, and ValidationView.test.tsx renders
    // many times in one file.
    globals: true,
    setupFiles: ["src/test-setup.ts"],
    server: {
      // oxygen-ui ships in a form that needs vite's transform pipeline rather
      // than a plain node require (matches design-view and apps/console).
      deps: {
        inline: [
          "@wso2/oxygen-ui",
          "@mui/x-data-grid",
          "@mui/x-date-pickers",
          "@mui/x-tree-view",
        ],
      },
    },
  },
});
