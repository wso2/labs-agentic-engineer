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

import assert from "node:assert/strict";
import { test } from "node:test";

import { parseDefaultBearer } from "./env.js";

test("an unset or blank default bearer disables the fallback", () => {
  assert.equal(parseDefaultBearer(undefined), undefined);
  assert.equal(parseDefaultBearer(""), undefined);
  assert.equal(parseDefaultBearer("   "), undefined);
});

test("a bare scheme with no credential disables the fallback", () => {
  assert.equal(parseDefaultBearer("Bearer "), undefined);
  assert.equal(parseDefaultBearer("bearer"), undefined);
});

test("a well-formed bearer is returned trimmed", () => {
  assert.equal(parseDefaultBearer("  Bearer abc123 "), "Bearer abc123");
});

test("a value without the Bearer scheme is rejected", () => {
  assert.throws(() => parseDefaultBearer("abc123"), /AEP_MCP_DEFAULT_BEARER/);
  assert.throws(() => parseDefaultBearer("Basic abc123"), /AEP_MCP_DEFAULT_BEARER/);
});

test("a bearer with control characters or non-token characters is rejected", () => {
  assert.throws(() => parseDefaultBearer("Bearer\nabc123"), /AEP_MCP_DEFAULT_BEARER/);
  assert.throws(() => parseDefaultBearer("Bearer abc\n123"), /AEP_MCP_DEFAULT_BEARER/);
  assert.throws(() => parseDefaultBearer("Bearer\tabc123"), /AEP_MCP_DEFAULT_BEARER/);
  assert.throws(() => parseDefaultBearer("Bearer abc\u0000123"), /AEP_MCP_DEFAULT_BEARER/);
  assert.throws(() => parseDefaultBearer("Bearer abc 123"), /AEP_MCP_DEFAULT_BEARER/);
});

test("a bearer in the RFC 6750 b64token alphabet is accepted", () => {
  assert.equal(parseDefaultBearer("Bearer aB0-._~+/=="), "Bearer aB0-._~+/==");
});
