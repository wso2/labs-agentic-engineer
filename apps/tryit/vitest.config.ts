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

// Standalone config (instead of vite.config.ts) so tests skip the react plugin.
// jsdom throughout: the app is small and most of it renders.
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    include: ["src/**/*.test.{ts,tsx}"],
    environment: "jsdom",
    globals: true,
    setupFiles: ["src/test-setup.ts"],
    server: {
      deps: {
        // Oxygen UI re-exports the MUI x-packages, whose ESM imports CSS
        // node cannot load; inlining lets vite transform them, as the console does.
        inline: ["@wso2/oxygen-ui", "prismjs", "@mui/x-data-grid", "@mui/x-date-pickers", "@mui/x-tree-view"],
      },
    },
  },
});
