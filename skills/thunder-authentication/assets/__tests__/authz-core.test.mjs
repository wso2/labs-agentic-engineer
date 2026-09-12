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

// Runner for ./authz-core.cases.mjs.
//
// authz-core.ts is TypeScript, and the repo-wide skill test runner is a bare
// `node --test <files matching *.test.mjs>` — no flags, no transpiler, no
// dependency. The pinned Node needs `--experimental-strip-types` to import a
// .ts module at all, so the cases run in a child process that has the flag and
// this file asserts the child's exit status, surfacing its output on failure.
//
// When Node's default type stripping lands under the version this repo pins,
// fold authz-core.cases.mjs back into this file and delete the spawn.

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));

/**
 * The test runner marks its children with NODE_TEST_CONTEXT, which switches a
 * nested `node --test` to the v8-serialized reporter and leaves this process
 * with an empty stdout to assert on. Drop it.
 */
function childEnv() {
  const env = { ...process.env };
  for (const key of Object.keys(env)) {
    if (key.startsWith("NODE_TEST_")) delete env[key];
  }
  return env;
}

test("authz-core: the pure authorization rules", () => {
  const child = spawnSync(
    process.execPath,
    ["--experimental-strip-types", "--no-warnings", "--test", path.join(here, "authz-core.cases.mjs")],
    { encoding: "utf8", env: childEnv() },
  );
  const output = `${child.stdout ?? ""}${child.stderr ?? ""}`;
  assert.equal(child.status, 0, `authz-core.cases.mjs failed:\n${output}`);
  // Guard against the child silently running nothing at all.
  assert.match(output, /# pass (\d+)/);
  const passed = Number(/# pass (\d+)/.exec(output)[1]);
  assert.ok(passed >= 15, `expected the case file to run its suite, saw ${passed} passing`);
});
