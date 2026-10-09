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

import type { ReactNode } from "react";
import { Box, Button, Tooltip, Typography } from "@wso2/oxygen-ui";
import { MessageCirclePlus, MousePointer2 } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeViewEvent, PrototypeViewState } from "@wso2/prototype-kit/host";
import { DOCK_COMPACT, DockGroup } from "./ReviewDock";

/** A tint of the primary colour, for the active Comment tool and the mode's ring. */
export const PRIMARY_TINT = "rgba(var(--oxygen-palette-primary-mainChannel) / 0.16)";

/**
 * One of the mode tools: labelled, with its shortcut in the tooltip. The
 * active one is pressed: `primary` tints it (Comment), `neutral` greys it (Preview).
 */
function Tool({
  label,
  shortcut,
  icon,
  pressed,
  tone,
  onClick,
}: {
  label: string;
  shortcut: string;
  icon: ReactNode;
  pressed: boolean;
  tone: "neutral" | "primary";
  onClick: () => void;
}) {
  return (
    <Tooltip title={`${label} · ${shortcut}`} describeChild>
      <Button
        size="small"
        color="inherit"
        aria-pressed={pressed}
        startIcon={icon}
        onClick={onClick}
        sx={{
          textTransform: "none",
          fontWeight: 500,
          px: 1.25,
          py: 0.5,
          borderRadius: 1.5,
          color: "text.secondary",
          "&:hover": { bgcolor: "action.hover", color: "text.primary" },
          '&[aria-pressed="true"]': tone === "primary" ? { bgcolor: PRIMARY_TINT, color: "primary.main" } : { bgcolor: "action.selected", color: "text.primary" },
        }}
      >
        {label}
      </Button>
    </Tooltip>
  );
}

/** In Comment mode, what a click does now (dropped on a compact dock: the window's tag still says the mode). */
function CommentHint() {
  return (
    <Typography
      variant="body2"
      color="primary"
      sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, px: 1, fontWeight: 500, [DOCK_COMPACT]: { display: "none" } }}
    >
      <Box
        component="i"
        aria-hidden
        sx={{
          width: 6,
          height: 6,
          borderRadius: "50%",
          bgcolor: "primary.main",
          "@keyframes pulse": { "50%": { opacity: 0.35 } },
          animation: "pulse 1.6s ease-in-out infinite",
          "@media (prefers-reduced-motion: reduce)": { animation: "none" },
        }}
      />
      Click anything to comment
    </Typography>
  );
}

/**
 * The dock's mode group: the Preview · Comment tool pair (Comment is the
 * reviewer's word for the view's Annotate mode; `V` and `C` in the tooltips)
 * and, in Comment mode, the hint that a click comments.
 */
export function ModeTools({ view, dispatch }: { view: PrototypeViewState; dispatch: (event: PrototypeViewEvent) => void }) {
  const commenting = view.mode === "annotate";
  return (
    <DockGroup label="Mode">
      <Box sx={{ display: "inline-flex", gap: 0.25, p: 0.375, border: 1, borderColor: "divider", borderRadius: 2.5 }}>
        <Tool label="Preview" shortcut="V" icon={<MousePointer2 size={16} />} pressed={!commenting} tone="neutral" onClick={() => dispatch({ type: "EXIT_ANNOTATE" })} />
        <Tool label="Comment" shortcut="C" icon={<MessageCirclePlus size={16} />} pressed={commenting} tone="primary" onClick={() => dispatch({ type: "ENTER_ANNOTATE" })} />
      </Box>
      {commenting && <CommentHint />}
    </DockGroup>
  );
}
