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
 * The popover every comment bubble is drawn in, by its anchor: below it, or
 * above it when there is no room below, kept inside the window and above the
 * bottom of the review's stage (`BubbleBounds`: so it never covers the dock
 * below the stage), over the frame and never inside it. Nothing is drawn until the anchor is known; a
 * click anywhere outside it is a click away. (Clicks inside the prototype's
 * frame never reach the host page: a click there is the frame's to report.)
 */

import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { focusLeftBehind, placeBubble, type HostRect } from "@wso2/prototype-kit/host";

/**
 * Where a bubble points: elements in the frame (the kit's frame anchors, in
 * the host's viewport), or an element of the host's own (the dock, for a
 * whole-screen comment).
 */
export type BubbleAnchor = HostRect | HTMLElement;

/** The review's stage (the prototype window's area), whose bottom a bubble keeps above; none: the window's. */
export const BubbleBounds = createContext<HTMLElement | null>(null);

function rectOf(anchor: BubbleAnchor): HostRect {
  if (!(anchor instanceof HTMLElement)) return anchor;
  const { top, left, width, height } = anchor.getBoundingClientRect();
  return { top, left, width, height };
}

export function AnchoredBubble({
  anchor,
  label,
  onClickAway,
  onKeyDown,
  children,
}: {
  anchor: BubbleAnchor | null;
  /** The bubble's accessible name. */
  label: string;
  /** A click outside it; `refocus` when that click left keyboard focus nowhere (the host puts it back where the bubble pointed). */
  onClickAway: (refocus: boolean) => void;
  onKeyDown?: (e: KeyboardEvent) => void;
  children: ReactNode;
}) {
  const bubble = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState<{ top: number; left: number } | null>(null);
  const bounds = useContext(BubbleBounds);

  useLayoutEffect(() => {
    const el = bubble.current;
    if (!el || !anchor) return;
    const measure = () => {
      const floor = bounds ? Math.min(window.innerHeight, bounds.getBoundingClientRect().bottom) : window.innerHeight;
      const next = placeBubble(rectOf(anchor), { width: el.offsetWidth, height: el.offsetHeight }, { width: window.innerWidth, height: floor });
      setAt((a) => (a && a.top === next.top && a.left === next.left ? a : next));
    };
    measure();
    // The bubble grows as its text does; the host's own anchor (the dock) and the stage change size too.
    const resized = new ResizeObserver(measure);
    resized.observe(el);
    if (anchor instanceof HTMLElement) resized.observe(anchor);
    if (bounds) resized.observe(bounds);
    window.addEventListener("resize", measure);
    return () => {
      resized.disconnect();
      window.removeEventListener("resize", measure);
    };
  }, [anchor, bounds]);

  const latest = useRef(onClickAway);
  useEffect(() => {
    latest.current = onClickAway;
  });
  useEffect(() => {
    // On click, once the click moved focus (or did not), so the host can tell whether to put it back.
    const onClick = (e: MouseEvent) => {
      const el = bubble.current;
      if (el && e.target instanceof Node && !el.contains(e.target)) latest.current(focusLeftBehind(el));
    };
    document.addEventListener("click", onClick, { capture: true });
    return () => document.removeEventListener("click", onClick, { capture: true });
  }, []);

  if (!anchor) return null;
  return (
    <div
      ref={bubble}
      role="dialog"
      aria-label={label}
      className="ph-bubble"
      onKeyDown={onKeyDown}
      // Unplaced, it is laid out (to be measured) but not seen; still focusable, so its input takes focus on opening.
      style={at ? { top: at.top, left: at.left } : { top: 0, left: 0, opacity: 0 }}
    >
      {children}
    </div>
  );
}
