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

// The browser lane (Seam 1, `preview` and `export`): Vitest browser mode with
// the Playwright provider. The tests run in the browser and drive a spawned
// `prototype preview` (or an exported file) in pages of the same browser
// through the node-side commands in test/browser/commands.ts. Kept out of the
// default `test` run, like the console's browser lane: run it with
// `pnpm --filter @wso2/prototype-cli test:browser`.
import { defineConfig } from "vitest/config";
import { playwright } from "@vitest/browser-playwright";
import { commands } from "./test/browser/commands.ts";

export default defineConfig({
  test: {
    include: ["test/browser/**/*.browser.test.ts"],
    testTimeout: 60_000,
    hookTimeout: 60_000,
    fileParallelism: false,
    browser: {
      enabled: true,
      provider: playwright(),
      headless: true,
      instances: [{ browser: "chromium" }],
      commands,
    },
  },
});
