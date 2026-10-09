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
 * Where the frame's elements are in the host page, for a host that draws its
 * own UI by them (a comment bubble, a pin). The frame reports element boxes
 * in its own viewport (`proto:toggle`, `proto:geometry`) and how far its
 * document is scrolled; this adds where the frame element sits in the host's
 * viewport, and keeps that current as the
 * host page scrolls or resizes, or the frame element moves or resizes. The
 * frame re-reports its boxes when the prototype scrolls, resizes or redraws,
 * so together the anchors follow the element. Headless: the host draws.
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import type { FrameBox, FramePoint } from "./bridge.js";

/** The elements' boxes and the document's scroll as the frame last reported them, and the frame element they are drawn in. */
export interface FrameGeometry {
  frame: HTMLElement;
  boxes: Readonly<Record<string, FrameBox>>;
  scroll: FramePoint;
}

/** A rectangle in the host's viewport (CSS pixels, as `getBoundingClientRect()` gives). */
export interface HostRect {
  top: number;
  left: number;
  width: number;
  height: number;
}

export interface FrameAnchors {
  /** Give to `PrototypeFrame`'s `onGeometry`. */
  onGeometry: (geometry: FrameGeometry) => void;
  /**
   * The smallest host rectangle around the drawn elements among `keys`; null
   * when the frame draws none of them. The same object while nothing moved.
   */
  anchor: (keys: readonly string[]) => HostRect | null;
  /**
   * Where the spot `at` of the frame's document (a whole-screen comment's)
   * is in the host, as a rectangle of no size; null until the frame is
   * known. The same object while nothing moved.
   */
  point: (at: FramePoint) => HostRect | null;
}

const NO_BOXES: Readonly<Record<string, FrameBox>> = Object.freeze({});
const NO_SCROLL: FramePoint = Object.freeze({ x: 0, y: 0 });

function sameOrigin(a: { top: number; left: number } | null, b: { top: number; left: number }): boolean {
  return a !== null && a.top === b.top && a.left === b.left;
}

/** The smallest rectangle around the drawn elements among `keys`, moved from the frame's viewport into the host's. */
function around(origin: { top: number; left: number }, boxes: Readonly<Record<string, FrameBox>>, keys: readonly string[]): HostRect | null {
  const drawn = keys.map((k) => (Object.hasOwn(boxes, k) ? boxes[k] : undefined)).filter((b): b is FrameBox => b !== undefined);
  if (drawn.length === 0) return null;
  const left = Math.min(...drawn.map((b) => b.x));
  const top = Math.min(...drawn.map((b) => b.y));
  const right = Math.max(...drawn.map((b) => b.x + b.width));
  const bottom = Math.max(...drawn.map((b) => b.y + b.height));
  return { top: origin.top + top, left: origin.left + left, width: right - left, height: bottom - top };
}

export function useFrameAnchors(): FrameAnchors {
  const [frame, setFrame] = useState<HTMLElement | null>(null);
  const [boxes, setBoxes] = useState(NO_BOXES);
  const [scroll, setScroll] = useState(NO_SCROLL);
  // The frame viewport's top-left corner in the host's viewport.
  const [origin, setOrigin] = useState<{ top: number; left: number } | null>(null);

  const onGeometry = useCallback((geometry: FrameGeometry) => {
    setFrame(geometry.frame);
    setBoxes(geometry.boxes);
    setScroll((s) => (s.x === geometry.scroll.x && s.y === geometry.scroll.y ? s : geometry.scroll));
  }, []);

  useEffect(() => {
    if (!frame) return;
    const measure = () => {
      const r = frame.getBoundingClientRect();
      const next = { top: r.top + frame.clientTop, left: r.left + frame.clientLeft };
      setOrigin((o) => (sameOrigin(o, next) ? o : next));
    };
    measure();
    // Capture: a scroll in any of the host's scrollers can move the frame.
    window.addEventListener("scroll", measure, { capture: true, passive: true });
    window.addEventListener("resize", measure);
    const resized = new ResizeObserver(measure);
    resized.observe(frame);
    return () => {
      window.removeEventListener("scroll", measure, { capture: true });
      window.removeEventListener("resize", measure);
      resized.disconnect();
    };
  }, [frame]);

  // One rectangle per set of keys while nothing moved, so a host can memoise on it.
  const anchor = useMemo(() => {
    const placed = new Map<string, HostRect | null>();
    return (keys: readonly string[]): HostRect | null => {
      const id = keys.join("\n");
      if (!placed.has(id)) placed.set(id, origin && around(origin, boxes, keys));
      return placed.get(id) ?? null;
    };
  }, [origin, boxes]);

  // One rectangle per spot while nothing moved, likewise.
  const point = useMemo(() => {
    const placed = new Map<string, HostRect | null>();
    return (at: FramePoint): HostRect | null => {
      const id = `${at.x},${at.y}`;
      if (!placed.has(id)) placed.set(id, origin && { top: origin.top + at.y - scroll.y, left: origin.left + at.x - scroll.x, width: 0, height: 0 });
      return placed.get(id) ?? null;
    };
  }, [origin, scroll]);

  return useMemo(() => ({ onGeometry, anchor, point }), [onGeometry, anchor, point]);
}
