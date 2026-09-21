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
import { decodePromoteTarget, encodePromoteTarget } from "./promoteTarget";

describe("promoteTarget", () => {
  it("round-trips a project and name, keeping a slash inside the name", () => {
    const target = { project: "team-expenses", name: "fx/rates" };
    expect(decodePromoteTarget(encodePromoteTarget(target))).toEqual(target);
  });

  it("rejects anything that is not <project>/<name>", () => {
    expect(decodePromoteTarget("fx-rates")).toBeUndefined();
    expect(decodePromoteTarget("/fx-rates")).toBeUndefined();
    expect(decodePromoteTarget("team-expenses/")).toBeUndefined();
    expect(decodePromoteTarget(42)).toBeUndefined();
  });
});
