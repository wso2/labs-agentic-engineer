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
/** Where a host draws a bubble by its anchor: below, flipped above, kept inside the window. */

import { describe, expect, it } from "vitest";
import { placeBubble } from "../src/host/bubble-placement.js";

const bubble = { width: 300, height: 100 };
const window = { width: 1000, height: 800 };

describe("placeBubble", () => {
  it("draws the bubble below its anchor, aligned to its left edge", () => {
    expect(placeBubble({ top: 100, left: 200, width: 50, height: 20 }, bubble, window)).toEqual({ top: 128, left: 200 });
  });

  it("flips above the anchor when there is no room below", () => {
    expect(placeBubble({ top: 700, left: 200, width: 50, height: 20 }, bubble, window)).toEqual({ top: 592, left: 200 });
  });

  it("stays below when there is no room above either", () => {
    expect(placeBubble({ top: 40, left: 200, width: 50, height: 740 }, bubble, window)).toEqual({ top: 788, left: 200 });
  });

  it("keeps the bubble inside the window's left and right edges", () => {
    expect(placeBubble({ top: 100, left: 900, width: 50, height: 20 }, bubble, window).left).toBe(692);
    expect(placeBubble({ top: 100, left: -40, width: 50, height: 20 }, bubble, window).left).toBe(8);
  });
});
