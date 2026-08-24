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
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const src = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "oneshot.ts"), "utf8");

test("oneshot requires publisher CC and does not inject a BFF MCP token", () => {
  assert.match(src, /PUBLISHER_CLIENT_ID\/SECRET\/TOKEN_URL required/);
  assert.doesNotMatch(src, /preferPublisherMcpToken/);
  assert.doesNotMatch(src, /process\.env\.AEP_MCP_TOKEN/);
  assert.doesNotMatch(src, /process\.env\.AEP_BEARER/);
});
