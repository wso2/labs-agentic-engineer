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
import { validationIsLive } from "./lifecycle";

describe("validationIsLive", () => {
  it("is the two lifecycle states and nothing else", () => {
    expect(validationIsLive("running")).toBe(true);
    expect(validationIsLive("awaiting-fix")).toBe(true);
  });

  // The ledger's poll-stop asks this. `none` is the resting state of every
  // version a run never reached, so counting it as live left the ledger of any
  // project holding one polling every 5s for ever.
  it("does not count a version nothing has judged", () => {
    expect(validationIsLive("none")).toBe(false);
  });

  it("does not count a settled verdict", () => {
    for (const settled of ["passed", "partial", "failed", "inconclusive", "unreported", "skipped", "cancelled"]) {
      expect(validationIsLive(settled)).toBe(false);
    }
  });
});
