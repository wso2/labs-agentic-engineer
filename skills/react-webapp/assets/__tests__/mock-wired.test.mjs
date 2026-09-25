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

// Runner for ./mock-wired.cases.mjs — the same spawn-with-the-flag shape as
// thunder-authentication's asset tests, and for the same reason: the case file
// imports TypeScript and the repo-wide runner is a bare `node --test`.

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));

/** Drop NODE_TEST_* so the child uses the TAP reporter and this process can read it. */
function childEnv() {
  const env = { ...process.env };
  for (const key of Object.keys(env)) {
    if (key.startsWith("NODE_TEST_")) delete env[key];
  }
  return env;
}

test("mock/wired.ts: the gateway stand-in refuses, and signs what it forwards", () => {
  const child = spawnSync(
    process.execPath,
    ["--experimental-strip-types", "--no-warnings", "--test", path.join(here, "mock-wired.cases.mjs")],
    { encoding: "utf8", env: childEnv() },
  );
  const output = `${child.stdout ?? ""}${child.stderr ?? ""}`;
  assert.equal(child.status, 0, `mock-wired.cases.mjs failed:\n${output}`);
  assert.match(output, /# pass (\d+)/);
  const passed = Number(/# pass (\d+)/.exec(output)[1]);
  assert.ok(passed >= 14, `expected the case file to run its suite, saw ${passed} passing`);
});
