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

import { describe, expect, it } from "vitest";
import { versionNameError } from "./versionName";

describe("versionNameError", () => {
  const taken = ["v1", "v2", "payments-v2"];

  it("accepts the shapes a version is actually named", () => {
    for (const name of ["v3", "payments-v3", "beta_launch", "1.4.0", "hotfix"]) {
      expect(versionNameError(name, taken)).toBeNull();
    }
  });

  it("refuses an empty name", () => {
    expect(versionNameError("   ", taken)).toBe("Name the version.");
  });

  it("refuses what a tag cannot hold", () => {
    for (const name of ["my version", "feature/x", "v3~1", "v3?"]) {
      expect(versionNameError(name, taken)).toMatch(/letters, digits/i);
    }
  });

  it("refuses the edges git itself refuses", () => {
    expect(versionNameError("-v3", taken)).toMatch(/start with/i);
    expect(versionNameError(".v3", taken)).toMatch(/start with/i);
    expect(versionNameError("v3.", taken)).toMatch(/dot or with .lock/i);
    expect(versionNameError("v3.lock", taken)).toMatch(/dot or with .lock/i);
    expect(versionNameError("v3..1", taken)).toMatch(/two dots/i);
  });

  it("names the collision rather than repairing it", () => {
    expect(versionNameError("payments-v2", taken)).toBe(
      "A version named payments-v2 already exists.",
    );
  });

  // Refs are case-sensitive, so calling these a collision would refuse a name
  // that works.
  it("treats a different case as a different name", () => {
    expect(versionNameError("V2", taken)).toBeNull();
  });

  it("ignores the whitespace a user leaves around a name", () => {
    expect(versionNameError("  v3  ", taken)).toBeNull();
    expect(versionNameError("  v1  ", taken)).toMatch(/already exists/);
  });
});
