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
 * Where elements are drawn, measured inside the frame's document, and kept
 * current: whenever a scroll, a resize or a redraw moves them, at most once a
 * frame and only when they changed. The frame reports the boxes of the
 * elements the host anchors its own UI to (`proto:geometry`: the selected,
 * pinned and drafted ones), since the host cannot measure the sandboxed
 * document; the kit places the pins of elements it cannot wrap by them.
 */

import type { FrameBox, FramePoint } from "../host/bridge.js";

/** Where the element drawn for `key` is in the frame's viewport, or undefined when the screen does not draw it. */
export function boxOf(key: string): FrameBox | undefined {
  const el = document.querySelector(`[data-proto-key="${CSS.escape(key)}"]`);
  if (!el) return undefined;
  const r = el.getBoundingClientRect();
  return { x: r.x, y: r.y, width: r.width, height: r.height };
}

/**
 * Calls `report` with the boxes of `keys()` and how far the document is
 * scrolled whenever either changes; `refresh` re-measures now (e.g. the keys
 * changed).
 */
export function watchGeometry(
  keys: () => readonly string[],
  report: (boxes: Record<string, FrameBox>, scroll: FramePoint) => void,
): { refresh: () => void; stop: () => void } {
  let last = "";
  // The animation frame a measure waits for; null when none is pending.
  let pending: number | null = null;
  const measure = () => {
    pending = null;
    const boxes: Record<string, FrameBox> = {};
    for (const key of new Set(keys())) {
      const box = boxOf(key);
      if (box) boxes[key] = box;
    }
    const scroll = { x: window.scrollX, y: window.scrollY };
    const signature = JSON.stringify([boxes, scroll]);
    if (signature === last) return;
    last = signature;
    report(boxes, scroll);
  };
  const schedule = () => {
    if (pending !== null) return;
    pending = requestAnimationFrame(measure);
  };
  // Capture: a scroll inside any scroller of the prototype moves its elements as much as the page's.
  window.addEventListener("scroll", schedule, { capture: true, passive: true });
  window.addEventListener("resize", schedule);
  const resized = new ResizeObserver(schedule);
  resized.observe(document.documentElement);
  const redrawn = new MutationObserver(schedule);
  redrawn.observe(document.body, { subtree: true, childList: true, attributes: true, characterData: true });
  return {
    refresh: schedule,
    stop: () => {
      if (pending !== null) cancelAnimationFrame(pending);
      pending = null;
      window.removeEventListener("scroll", schedule, { capture: true });
      window.removeEventListener("resize", schedule);
      resized.disconnect();
      redrawn.disconnect();
    },
  };
}
