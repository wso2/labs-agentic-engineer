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

/** The host run's `agent-browser` is the image's version (its PATH: `coding-run.test.ts`). */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { delimiter, join } from "node:path";
import { agentBrowserBinDir, agentBrowserProblem, withAgentBrowserFirst } from "../src/engine/agent-browser.js";
import { REPO_ROOT } from "../src/paths.js";

const PACKAGE_ROOT = join(REPO_ROOT, "playground");

test("the pinned agent-browser is the runner image's version", () => {
  const dockerfile = readFileSync(join(REPO_ROOT, "runners", "remote-worker", "Dockerfile"), "utf8");
  const imageVersion = /^ARG AGENT_BROWSER_VERSION=(\S+)$/m.exec(dockerfile)?.[1];
  assert.ok(imageVersion, "the Dockerfile lost its AGENT_BROWSER_VERSION ARG");
  const pkg = JSON.parse(readFileSync(join(PACKAGE_ROOT, "package.json"), "utf8")) as {
    devDependencies?: Record<string, string>;
  };
  assert.equal(pkg.devDependencies?.["agent-browser"], imageVersion);
});

test("`make install` puts the pinned agent-browser where a host run looks for it", () => {
  assert.equal(agentBrowserProblem(agentBrowserBinDir(PACKAGE_ROOT)), undefined);
});

test("PATH keeps everything it had, after the pin", () => {
  assert.equal(withAgentBrowserFirst({ PATH: "/usr/bin" }, "/pin").PATH, `/pin${delimiter}/usr/bin`);
  assert.equal(withAgentBrowserFirst({}, "/pin").PATH, "/pin");
  assert.match(agentBrowserProblem("/nowhere") ?? "", /make install/);
});
