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

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { writeBearerFile } from "./workspace.js";

test("writeBearerFile: concurrent writers do not share a tmp path", async () => {
  const dir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-bearer-"));
  const file = path.join(dir, "bearer");
  try {
    const tokens = Array.from({ length: 20 }, (_, i) => `tok-${i}-${"x".repeat(32)}`);
    await Promise.all(tokens.map((t) => writeBearerFile(file, t)));
    const got = await fs.promises.readFile(file, "utf8");
    assert.ok(tokens.includes(got), `final token ${got} was not one of the writers`);
    const leftovers = (await fs.promises.readdir(dir)).filter((n) => n.includes(".tmp"));
    assert.deepEqual(leftovers, []);
  } finally {
    await fs.promises.rm(dir, { recursive: true, force: true });
  }
});
