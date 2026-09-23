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
    // Component tests opt into jsdom per-file with a
    // `// @vitest-environment jsdom` pragma (matches the other packages/ui
    // packages).
    environment: "node",
    include: ["src/**/*.test.{ts,tsx}"],
    // Lets @testing-library/react register its afterEach cleanup.
    globals: true,
    setupFiles: ["src/test-setup.ts"],
    server: {
      // oxygen-ui needs vite's transform pipeline rather than a plain node
      // require (matches design-view and apps/console).
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
