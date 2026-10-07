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

// The window splits in the golden ratio: the rail and the chat together take
// the smaller part, the page the larger. A width the user drags to replaces
// that share and stays put as the window changes; either way the chat keeps a
// usable floor and never grows past half the room right of the rail.

import { RAIL_WIDTH } from "./layout";

const PHI = (1 + Math.sqrt(5)) / 2;

/** The rail and chat's share of the window by default: 1 / (1 + φ) ≈ 0.382. */
export const GOLDEN_SHARE = 1 / (1 + PHI);

export const CHAT_MIN_WIDTH = 320;
const CHAT_MAX_SHARE = 0.5;

/**
 * The chat's width, in px, in a `windowWidth` px window: the `saved` width if
 * the user dragged one, else the golden share less the rail. The floor wins
 * over the cap on a room under twice the floor, which only phone width
 * reaches, and there the chat is an overlay.
 */
export function chatWidth(windowWidth: number, saved: number | null): number {
  const room = windowWidth - RAIL_WIDTH;
  const wanted = saved ?? windowWidth * GOLDEN_SHARE - RAIL_WIDTH;
  return Math.round(Math.max(CHAT_MIN_WIDTH, Math.min(room * CHAT_MAX_SHARE, wanted)));
}
