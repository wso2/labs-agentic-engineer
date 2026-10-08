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
import { ApiRequestError } from "../../../api/errors";
import { aeStudioUnavailable, refetchWhileAeStudioRestarts } from "./unavailable";

// What a read that aep-api serves through AE Studio says when AE Studio could
// not answer it, by the refusal's code.

const refused = (code: string) => new ApiRequestError({ code, message: "x" }, "x");

describe("aeStudioUnavailable", () => {
  it.each([
    ["ae_studio_unavailable", "restarting"],
    ["github_not_connected", "github"],
    ["ae_studio_misconfigured", "misconfigured"],
  ])("reads %s as %s", (code, kind) => {
    expect(aeStudioUnavailable(refused(code))).toBe(kind);
  });

  it("is null for any other failure", () => {
    expect(aeStudioUnavailable(refused("not_found"))).toBeNull();
    expect(aeStudioUnavailable(new Error("boom"))).toBeNull();
    expect(aeStudioUnavailable(null)).toBeNull();
  });
});

describe("refetchWhileAeStudioRestarts", () => {
  it("reads again every 5 s while the last failure is an AE Studio restart, and not otherwise", () => {
    expect(refetchWhileAeStudioRestarts(refused("ae_studio_unavailable"))).toBe(5_000);
    expect(refetchWhileAeStudioRestarts(refused("github_not_connected"))).toBe(false);
    expect(refetchWhileAeStudioRestarts(null)).toBe(false);
  });
});
