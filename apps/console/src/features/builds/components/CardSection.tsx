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

import { useState, type ReactNode } from "react";
import { Box, ButtonBase, Typography } from "@wso2/oxygen-ui";
import { ChevronRight } from "@wso2/oxygen-ui-icons-react";

/**
 * A section of a card that opens and closes: the old console's LogSection.
 * Its body mounts only while open, so a closed log holds no stream open.
 */
export function CardSection({
  title,
  meta,
  defaultOpen = false,
  children,
}: {
  title: string;
  /** A short line beside the title: counts, or how the run ended. */
  meta?: ReactNode;
  defaultOpen?: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <Box component="section" aria-label={title} sx={{ border: 1, borderColor: "divider", borderRadius: 2.5, overflow: "hidden" }}>
      <ButtonBase
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        sx={{
          width: "100%",
          display: "flex",
          justifyContent: "flex-start",
          alignItems: "center",
          gap: 1,
          px: 1.75,
          py: 1.25,
          textAlign: "start",
          "&:hover": { bgcolor: "action.hover" },
        }}
      >
        <ChevronRight
          size={16}
          aria-hidden
          style={{ transition: "transform 0.15s", transform: open ? "rotate(90deg)" : "none", flexShrink: 0 }}
        />
        <Typography component="h3" sx={{ fontSize: "0.9375rem", fontWeight: 600, flex: 1 }}>
          {title}
        </Typography>
        {meta && (
          <Typography component="span" variant="caption" color="text.secondary">
            {meta}
          </Typography>
        )}
      </ButtonBase>
      {open && <Box sx={{ borderTop: 1, borderColor: "divider" }}>{children}</Box>}
    </Box>
  );
}

/** A log's lines, monospace, in a box that scrolls on its own; `empty` when there are none. */
export function LogLines({ lines, empty, maxHeight = 320 }: { lines: string[]; empty: string; maxHeight?: number }) {
  return (
    <Box
      component="pre"
      sx={{
        m: 0,
        px: 1.75,
        py: 1.25,
        maxHeight,
        overflowY: "auto",
        bgcolor: "background.default",
        fontFamily: "monospace",
        fontSize: "0.72rem",
        lineHeight: 1.6,
        color: "text.secondary",
        whiteSpace: "pre-wrap",
        overflowWrap: "anywhere",
      }}
    >
      {lines.length ? lines.join("\n") : empty}
    </Box>
  );
}
