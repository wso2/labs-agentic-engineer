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
import { aepTheme } from "../src/aepTheme.js";
import { tone } from "../src/tones.js";

// OxygenTheme's type omits the per-scheme palettes the built theme carries.
type Tone = Record<string, string>;
const { colorSchemes } = aepTheme as unknown as {
  colorSchemes: Record<
    "light" | "dark",
    { palette: { primary: Tone; background: Tone; text: Tone; dividerChannel: string } }
  >;
};

// Components colour themselves through the CSS-variable channels and the
// light/dark variants, not only the main hex. Oxygen's base keeps its own
// orange in every field an override leaves out, so the fields are checked.
describe("aepTheme", () => {
  it("carries the prototype's primary in both schemes, variants and channels included", () => {
    const light = colorSchemes.light.palette.primary;
    const dark = colorSchemes.dark.palette.primary;

    expect(light).toMatchObject({ main: "#E2611B", mainChannel: "226 97 27" });
    expect(light.dark).toBe("#9E4413");
    expect(dark).toMatchObject({ main: "#F07A3A", mainChannel: "240 122 58" });
  });

  it("replaces the base's background, text and divider channels", () => {
    const p = colorSchemes.dark.palette;

    expect(p.background.defaultChannel).toBe("15 18 22");
    expect(p.text.primaryChannel).toBe("231 234 239");
    expect(p.dividerChannel).toBe("38 44 53");
  });
});

describe("tone", () => {
  it("derives variants with MUI's tonal offsets", () => {
    expect(tone("#2F6FB5", "#FFFFFF")).toEqual({
      main: "#2F6FB5",
      light: "#598CC4",
      dark: "#214E7F",
      contrastText: "#FFFFFF",
      mainChannel: "47 111 181",
      lightChannel: "89 140 196",
      darkChannel: "33 78 127",
      contrastTextChannel: "255 255 255",
    });
  });

  it("rejects a colour that is not #rrggbb", () => {
    expect(() => tone("rgb(0, 0, 0)", "#FFFFFF")).toThrow("Expected #rrggbb");
  });
});
