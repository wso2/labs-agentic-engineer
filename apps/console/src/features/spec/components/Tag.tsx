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
import { Box } from "@wso2/oxygen-ui";
import type { ChipTone } from "../model/workspace";

/** A soft wash of a palette colour, from its CSS-variable channel. */
export function soft(tone: ChipTone | "info", alpha = 0.12): string {
  return `rgba(var(--oxygen-palette-${tone}-mainChannel) / ${alpha})`;
}

/** A small state tag: a feature's stage or one of its chips. Muted when it has no tone. */
export function Tag({ tone, children }: { tone: ChipTone | null; children: ReactNode }) {
  return (
    <Box
      component="span"
      sx={{
        display: "inline-block",
        fontFamily: (t) => t.typography.fontFamily,
        fontSize: "0.6875rem",
        lineHeight: 1.6,
        px: 0.875,
        borderRadius: 1.5,
        border: 1,
        whiteSpace: "nowrap",
        borderColor: tone ? `${tone}.main` : "divider",
        color: tone ? `${tone}.main` : "text.secondary",
        bgcolor: tone ? soft(tone) : "transparent",
      }}
    >
      {children}
    </Box>
  );
}
