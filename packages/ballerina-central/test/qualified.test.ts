/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * The branded coordinates, and the one class of input that used to pass.
 *
 * `.` and `..` satisfy both patterns and are legal path traversal. Nothing
 * derived a filesystem path from these values until the docs cache did, so this
 * is the guard that keeps the branded type itself from ever holding one.
 */

import assert from "node:assert/strict";
import { test } from "node:test";
import { formatQualifiedName, parseQualifiedName, parseVersion } from "../src/qualified.js";

test("a well-formed package name round-trips", () => {
  const parsed = parseQualifiedName("ballerinax/googleapis.gmail");
  assert.ok(parsed.ok);
  assert.equal(formatQualifiedName(parsed.value), "ballerinax/googleapis.gmail");
});

test("a traversal segment is rejected in either position", () => {
  for (const input of ["../..", "./.", "ballerinax/..", "../github", "./github", "ballerinax/."]) {
    const parsed = parseQualifiedName(input);
    assert.equal(parsed.ok, false, `${input} must not parse`);
    if (parsed.ok) continue;
    assert.equal(parsed.error.kind, "validation");
  }
});

test("a traversal version is rejected, while a real version with dots is not", () => {
  assert.equal(parseVersion("..").ok, false);
  assert.equal(parseVersion(".").ok, false);
  assert.equal(parseVersion("2.16.6").ok, true);
  assert.equal(parseVersion("1.0.0-alpha.1+build.7").ok, true);
});

test("a dotted name that is not a traversal still parses, because packages really are named that way", () => {
  assert.equal(parseQualifiedName("ballerinax/client.config").ok, true);
  assert.equal(parseQualifiedName("ballerina/lang.value").ok, true);
});
