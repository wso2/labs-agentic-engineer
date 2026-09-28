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

import { describe, expect, test } from "vitest";

import { hasFrontMatter, reassembleAfm } from "./afmBody";

const DOC = `---
name: "booking-agent"
x-aep:
  tools:
    openapi:
      - component: "hotel-api"
        allow: [listHotels]
---

# Role
You book hotels.
`;

describe("reassembleAfm", () => {
  test("replaces the body and leaves the front matter byte for byte", () => {
    const next = reassembleAfm(DOC, "# Role\nYou book trains.");

    expect(next).toContain('name: "booking-agent"');
    expect(next).toContain("allow: [listHotels]");
    expect(next).toContain("You book trains.");
    expect(next).not.toContain("You book hotels.");
  });

  // The whole point of reading `raw` at save time: an agent that rewired the
  // tools while someone was editing the prompt must not lose that change.
  test("keeps front matter the agent changed mid-edit", () => {
    const rewired = DOC.replace("allow: [listHotels]", "allow: [listHotels, getHotel]");

    const next = reassembleAfm(rewired, "# Role\nYou book trains.");

    expect(next).toContain("allow: [listHotels, getHotel]");
    expect(next).toContain("You book trains.");
  });

  test("normalises the body's edges so a save cannot drift the file's shape", () => {
    const next = reassembleAfm(DOC, "\n\n  # Role\nYou book trains.\n\n\n");

    expect(next.endsWith("You book trains.\n")).toBe(true);
    expect(next).not.toContain("\n\n\n");
  });

  test("refuses a document with no front matter rather than inventing one", () => {
    expect(hasFrontMatter("# Role\njust prose\n")).toBe(false);
    expect(hasFrontMatter(DOC)).toBe(true);
    expect(() => reassembleAfm("# Role\njust prose\n", "# Role\nnew")).toThrow(
      /front matter/i,
    );
  });
});
