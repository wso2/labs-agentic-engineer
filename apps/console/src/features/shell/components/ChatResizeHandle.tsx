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

import { useRef, useState, type PointerEvent } from "react";
import { Box, Tooltip } from "@wso2/oxygen-ui";
import { PHONE } from "../layout";

/**
 * The chat's right border, made draggable: a thin strip over the border that
 * takes the accent on hover and while dragging. Double-click resets. Mouse
 * only; at phone width the chat is an overlay and has none.
 */
export function ChatResizeHandle({
  width,
  onResize,
  onReset,
}: {
  width: number;
  onResize: (px: number) => void;
  onReset: () => void;
}) {
  // The pointer is captured for the drag, so it keeps arriving here over the
  // page, its editors and canvases, and past the window's edge.
  const drag = useRef<{ startX: number; startWidth: number } | null>(null);
  const [dragging, setDragging] = useState(false);

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault(); // no text selection while dragging
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { startX: e.clientX, startWidth: width };
    setDragging(true);
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    // A click that does not move must not pin the width it happens to be at.
    if (d && e.clientX !== d.startX) onResize(d.startWidth + e.clientX - d.startX);
  };
  const endDrag = () => {
    drag.current = null;
    setDragging(false);
  };

  return (
    <Tooltip title={dragging ? "" : "Drag to resize · Double-click to reset"} placement="right" followCursor>
      <Box
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize chat"
        data-dragging={dragging || undefined}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onDoubleClick={onReset}
        sx={{
          position: "absolute",
          top: 0,
          bottom: 0,
          right: -3,
          width: 6,
          zIndex: 1,
          cursor: "col-resize",
          touchAction: "none",
          "&::after": {
            content: '""',
            position: "absolute",
            top: 0,
            bottom: 0,
            left: 2,
            width: 2,
            bgcolor: "primary.main",
            opacity: 0,
            transition: "opacity 120ms",
          },
          "&:hover::after, &[data-dragging]::after": { opacity: 1 },
          [PHONE]: { display: "none" },
        }}
      />
    </Tooltip>
  );
}
