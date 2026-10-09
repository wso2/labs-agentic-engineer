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

import { test, mock } from "node:test";
import assert from "node:assert/strict";
import { startKeepAlive, KEEP_ALIVE_FRAME } from "../src/shared/keepalive.js";

test("emits a keep-alive frame every interval until stopped", () => {
  mock.timers.enable({ apis: ["setInterval"] });
  try {
    const frames: string[] = [];
    const stop = startKeepAlive((f) => frames.push(f), 15_000);

    mock.timers.tick(45_000); // three intervals
    assert.equal(frames.length, 3);
    assert.deepEqual(new Set(frames), new Set([KEEP_ALIVE_FRAME]));

    stop();
    mock.timers.tick(60_000); // no further emissions after stop
    assert.equal(frames.length, 3);
  } finally {
    mock.timers.reset();
  }
});

test("stop is idempotent", () => {
  mock.timers.enable({ apis: ["setInterval"] });
  try {
    let n = 0;
    const stop = startKeepAlive(() => (n += 1), 1_000);
    mock.timers.tick(2_000);
    assert.equal(n, 2);
    stop();
    stop(); // second call is a no-op, not a throw
    mock.timers.tick(5_000);
    assert.equal(n, 2);
  } finally {
    mock.timers.reset();
  }
});
