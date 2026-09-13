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
    // parse.test.ts is pure logic and node is the fastest default;
    // OpenApiView.test.tsx opts into jsdom per-file via a
    // `// @vitest-environment jsdom` pragma, mirroring packages/ui/design-view.
    environment: "node",
    // Source only. `build` compiles into dist/, and vitest's default glob would
    // collect those stale copies and run them against yesterday's source.
    include: ["src/**/*.test.{ts,tsx}"],
    // @testing-library/react's auto-cleanup only registers when a global
    // `afterEach` exists; without it the render tests leak DOM into each other.
    globals: true,
    setupFiles: ["src/test-setup.ts"],
    server: {
      // oxygen-ui ships in a form that needs vite's transform pipeline rather
      // than a plain node require (matches packages/ui/design-view).
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
