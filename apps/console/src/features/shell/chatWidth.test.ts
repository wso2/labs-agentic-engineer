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
import { CHAT_MIN_WIDTH, chatWidth } from "./chatWidth";
import { RAIL_WIDTH } from "./layout";

describe("chatWidth", () => {
  it("splits the window in the golden ratio, the rail and chat the shorter part", () => {
    const left = RAIL_WIDTH + chatWidth(1440, null);
    expect((1440 - left) / left).toBeCloseTo((1 + Math.sqrt(5)) / 2, 2);
  });

  it("keeps a saved width as the room changes", () => {
    expect(chatWidth(1440, 400)).toBe(400);
    expect(chatWidth(1920, 400)).toBe(400);
  });

  it("caps the chat at half the room right of the rail, saved or not", () => {
    expect(chatWidth(1052, 700)).toBe(500);
  });

  it("holds the floor where the golden share falls under it", () => {
    expect(chatWidth(861, null)).toBe(CHAT_MIN_WIDTH); // just above phone width
    expect(chatWidth(1440, 200)).toBe(CHAT_MIN_WIDTH);
  });
});
