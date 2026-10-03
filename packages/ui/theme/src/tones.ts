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

// Oxygen's base theme arrives already extended: every palette colour carries
// its light/dark variants and CSS-variable channels, and createOxygenTheme's
// merge keeps whatever the override leaves out. A colour given as `main` alone
// therefore keeps Oxygen's orange variants and channels, so every field is
// spelled out here, derived from the one hex the design names.

function rgb(hex: string): [number, number, number] {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  if (!m) throw new Error(`Expected #rrggbb, got ${hex}`);
  return [parseInt(m[1]!, 16), parseInt(m[2]!, 16), parseInt(m[3]!, 16)];
}

function toHex(parts: number[]): string {
  return `#${parts.map((n) => Math.round(n).toString(16).padStart(2, "0")).join("")}`.toUpperCase();
}

/** The "r g b" form MUI's CSS variables use for alpha blending. */
export function channel(hex: string): string {
  return rgb(hex).join(" ");
}

// The same offsets MUI's augmentColor applies (tonalOffset 0.2 up, 0.3 down).
function lighten(hex: string): string {
  return toHex(rgb(hex).map((c) => c + (255 - c) * 0.2));
}

function darken(hex: string): string {
  return toHex(rgb(hex).map((c) => c * 0.7));
}

/** A complete palette colour, channels included, from its main hex. */
export function tone(main: string, contrastText: string) {
  const light = lighten(main);
  const dark = darken(main);
  return {
    main,
    light,
    dark,
    contrastText,
    mainChannel: channel(main),
    lightChannel: channel(light),
    darkChannel: channel(dark),
    contrastTextChannel: channel(contrastText),
  };
}
