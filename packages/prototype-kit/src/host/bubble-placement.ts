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

/**
 * Where a host draws a bubble by its anchor (a frame element's box in the
 * host's viewport, or the host's own element): below it, or above it when
 * there is no room below, kept inside the window. Host-agnostic and pure; a
 * host with its own popper (the console's) need not use it.
 */

import type { HostRect } from "./useFrameAnchors.js";

/** The gap between the anchor and the bubble. */
const GAP = 8;
/** The least room kept between the bubble and the window's edges. */
const MARGIN = 8;

export interface BubbleSize {
  width: number;
  height: number;
}

/** The bubble's top-left corner for its anchor, its size and the window's size. */
export function placeBubble(anchor: HostRect, bubble: BubbleSize, window: BubbleSize): { top: number; left: number } {
  const below = anchor.top + anchor.height + GAP;
  const above = anchor.top - GAP - bubble.height;
  const fitsBelow = below + bubble.height <= window.height - MARGIN;
  const top = fitsBelow || above < MARGIN ? below : above;
  const left = Math.max(MARGIN, Math.min(anchor.left, window.width - bubble.width - MARGIN));
  return { top: Math.max(MARGIN, top), left };
}
