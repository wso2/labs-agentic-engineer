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
import { buildBody, fixBody, repairOfBody, selectionOfBody } from "./buildSelection";

describe("the build selection on the wire", () => {
  it("rides the contract's selection, with no inputs and no version", () => {
    const body = buildBody({ features: ["F1", "F2"], productWide: ["P5"] });
    expect(body).toEqual({ inputs: [], selection: { features: ["F1", "F2"], productWide: ["P5"] } });
    expect(body).not.toHaveProperty("version");
  });

  it("reads back as the server would", () => {
    const selection = { features: ["F1"], productWide: [] };
    expect(selectionOfBody(buildBody(selection))).toEqual(selection);
  });

  it("reads a body without a selection, or a malformed one, as none", () => {
    expect(selectionOfBody({ inputs: [] })).toBeNull();
    expect(selectionOfBody({ selection: { features: [1], productWide: [] } } as never)).toBeNull();
  });
});

describe("a repair build on the wire", () => {
  it("names the version it fixes, and reads back as the server would", () => {
    const body = fixBody("v1");
    expect(body).toEqual({ repair: { of: "v1" } });
    expect(repairOfBody(body)).toEqual({ of: "v1" });
    expect(selectionOfBody(body)).toBeNull();
  });

  it("reads a body without a repair, or a malformed one, as none", () => {
    expect(repairOfBody(buildBody({ features: ["F1"], productWide: [] }))).toBeNull();
    expect(repairOfBody({ repair: { of: 4 } } as never)).toBeNull();
  });
});
