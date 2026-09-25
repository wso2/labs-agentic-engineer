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

// Real-browser test project (`*.browser.test.tsx`), separate from the default
// node/jsdom suite. Some behaviors — auto-scroll driven by ResizeObserver, real
// layout/overflow — can only be verified against a real layout engine, which
// jsdom does not have. These run in headless Chromium via Playwright (browsers
// come from the machine's Playwright cache; no per-run download).
//
// Kept out of the default `test` glob so `make test` stays node-only and fast;
// invoke explicitly with `pnpm --filter @aep/console test:browser`.
import { defineConfig } from "vitest/config";
import { searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react-swc";
import { playwright } from "@vitest/browser-playwright";

export default defineConfig({
  plugins: [react()],
  // `page.screenshot({ path })` is written through Vite's fs layer, which
  // refuses any path outside the project root. Shots a human is meant to look
  // at do not belong in the source tree (`**/__screenshots__/` is ignored, and
  // ignored files are easy to never see), so the lane is allowed to write to
  // the machine's scratch directory as well.
  server: { fs: { allow: [searchForWorkspaceRoot(process.cwd()), "/tmp"] } },
  test: {
    include: ["src/**/*.browser.test.{ts,tsx}"],
    globals: true,
    setupFiles: ["src/test-setup.ts"],
    browser: {
      enabled: true,
      provider: playwright(),
      headless: true,
      instances: [{ browser: "chromium" }],
    },
  },
});
