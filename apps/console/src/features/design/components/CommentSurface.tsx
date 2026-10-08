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

import { useLayoutEffect, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { Box } from "@wso2/oxygen-ui";
import type { CommentAnchor, CommentStatus } from "../api/designModel";
import { anchorAt, placePin, pointIn, type PinPlace } from "../commentAnchor";

/** A pin drawn over the artifact: a comment's number on the element it was made on. */
export interface Pin {
  key: string;
  n: number | null;
  status: CommentStatus | "draft";
  anchor: Pick<CommentAnchor, "element" | "x" | "y">;
  title: string;
}

/**
 * How long the artifact's markup must hold still before a pin whose element
 * is missing counts as gone. The viewers draw in steps (a lazy canvas, a
 * diagram laid out after it is measured, hotspots placed a frame after the
 * canvas fits), so an element can be missing for a moment while they do.
 */
const SETTLE_MS = 400;

const PIN_TONE: Record<Pin["status"], string> = {
  open: "info.main",
  draft: "info.main",
  addressed: "success.main",
  resolved: "text.disabled",
};

function PinMark({ pin, left, top }: { pin: Pin; left: number; top: number }) {
  return (
    <Box
      data-pin
      title={pin.title}
      sx={{
        position: "absolute",
        left,
        top,
        transform: "translate(-2px, -100%)",
        minWidth: 20,
        height: 20,
        px: 0.5,
        display: "grid",
        placeItems: "center",
        borderRadius: "10px 10px 10px 2px",
        bgcolor: PIN_TONE[pin.status],
        color: "background.paper",
        fontFamily: "monospace",
        fontSize: "0.6875rem",
        fontWeight: 600,
        boxShadow: 2,
        opacity: pin.status === "draft" ? 0.7 : 1,
      }}
    >
      {pin.n ?? "+"}
    </Box>
  );
}

function samePlaces(a: Map<string, PinPlace>, b: Map<string, PinPlace>): boolean {
  if (a.size !== b.size) return false;
  for (const [key, place] of a) {
    const other = b.get(key);
    if (!other || other.kind !== place.kind) return false;
    if (place.kind === "at" && other.kind === "at" && (place.left !== other.left || place.top !== other.top)) return false;
  }
  return true;
}

/**
 * Keep each pin on its element: placed from the element's box now, and again
 * whenever the artifact is laid out anew (resized, redrawn, scrolled or
 * panned inside a viewer). Once the markup settles, the pins whose element
 * is not there are reported gone.
 */
function usePinPlaces(
  surface: HTMLElement | null,
  content: HTMLElement | null,
  pins: Pin[],
  onGone: (keys: string[]) => void,
): Map<string, PinPlace> {
  const [places, setPlaces] = useState<Map<string, PinPlace>>(() => new Map());
  useLayoutEffect(() => {
    if (!surface || !content) return;
    let frame = 0;
    let settle: ReturnType<typeof setTimeout> | undefined;
    const place = () => {
      frame = 0;
      const box = surface.getBoundingClientRect();
      const next = new Map(pins.map((pin) => [pin.key, placePin(content, box, pin.anchor)] as const));
      setPlaces((prev) => (samePlaces(prev, next) ? prev : next));
      return next;
    };
    const settled = () => {
      const gone = [...place()].filter(([, at]) => at.kind === "gone").map(([key]) => key);
      onGone(gone);
    };
    const relayout = () => {
      if (!frame) frame = requestAnimationFrame(place);
      clearTimeout(settle);
      settle = setTimeout(settled, SETTLE_MS);
    };
    place();
    relayout();
    const resized = new ResizeObserver(relayout);
    resized.observe(surface);
    resized.observe(content);
    const redrawn = new MutationObserver(relayout);
    redrawn.observe(content, { subtree: true, childList: true, attributes: true, characterData: true });
    // Capture: a viewer's own scroll areas do not bubble their scroll.
    content.addEventListener("scroll", relayout, true);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(settle);
      resized.disconnect();
      redrawn.disconnect();
      content.removeEventListener("scroll", relayout, true);
    };
  }, [surface, content, pins, onGone]);
  return places;
}

/**
 * The artifact, with its comments pinned over it. In Comment mode a clear
 * layer lies over the viewer: a click there does not reach the viewer, it
 * pins a comment on the element under it (see commentAnchor.ts), at the point
 * clicked on that element. The element is outlined as the pointer moves, so
 * the user sees what they are about to comment on.
 */
export function CommentSurface({
  commenting,
  pins,
  fallbackLabel,
  onPick,
  onGone,
  children,
}: {
  commenting: boolean;
  pins: Pin[];
  /** What a comment on something nameless is called: "the ClaimDetail screen". */
  fallbackLabel: string;
  onPick: (anchor: Omit<CommentAnchor, "view">) => void;
  /** The pins whose element is gone, once the artifact has settled. Kept stable by the caller. */
  onGone: (keys: string[]) => void;
  children: ReactNode;
}) {
  const [surface, setSurface] = useState<HTMLDivElement | null>(null);
  const [content, setContent] = useState<HTMLDivElement | null>(null);
  const layerRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<{ left: number; top: number; width: number; height: number } | null>(null);
  const places = usePinPlaces(surface, content, pins, onGone);

  const under = (e: MouseEvent): Element | null => {
    const layer = layerRef.current;
    if (!content || !layer) return null;
    return (
      document
        .elementsFromPoint(e.clientX, e.clientY)
        .find((el) => el !== layer && !layer.contains(el) && content.contains(el) && !el.closest("[data-pin]")) ?? null
    );
  };

  const onMove = (e: MouseEvent) => {
    if (!surface || !content) return;
    const el = anchorAt(under(e), content)?.element ?? content;
    const box = surface.getBoundingClientRect();
    const rect = el.getBoundingClientRect();
    setHover({ left: rect.left - box.left, top: rect.top - box.top, width: rect.width, height: rect.height });
  };

  const onClick = (e: MouseEvent) => {
    if (!content) return;
    const at = anchorAt(under(e), content);
    const box = (at?.element ?? content).getBoundingClientRect();
    onPick({ label: at?.label ?? fallbackLabel, element: at?.ref ?? null, ...pointIn(box, e.clientX, e.clientY) });
  };

  return (
    <Box ref={setSurface} sx={{ position: "relative", minWidth: 0, maxWidth: "100%" }}>
      <Box ref={setContent}>{children}</Box>
      {commenting && (
        <Box
          ref={layerRef}
          role="button"
          tabIndex={0}
          aria-label="Pin a comment: click the part of the artifact it is about"
          onClick={onClick}
          onKeyDown={(e) => {
            if (e.key !== "Enter" && e.key !== " ") return;
            e.preventDefault();
            onPick({ label: fallbackLabel, element: null, x: 0.5, y: 0.05 });
          }}
          onMouseMove={onMove}
          onMouseLeave={() => setHover(null)}
          sx={{ position: "absolute", inset: 0, zIndex: 3, cursor: "copy" }}
        >
          {hover && (
            <Box
              aria-hidden
              sx={{
                position: "absolute",
                ...hover,
                outline: "2px dashed",
                outlineColor: "info.main",
                outlineOffset: 2,
                borderRadius: 1,
                pointerEvents: "none",
              }}
            />
          )}
        </Box>
      )}
      <Box aria-hidden sx={{ position: "absolute", inset: 0, zIndex: 4, pointerEvents: "none" }}>
        {pins.map((pin) => {
          const at = places.get(pin.key);
          return at?.kind === "at" ? <PinMark key={pin.key} pin={pin} left={at.left} top={at.top} /> : null;
        })}
      </Box>
    </Box>
  );
}
