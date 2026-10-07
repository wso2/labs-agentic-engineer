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
import { Box, ButtonBase, Typography } from "@wso2/oxygen-ui";
import { PHONE } from "../../shell/layout";

// The Issues chat, a branch of the main chat, drawn as a sheet over it inside
// the chat panel: a strip back to the main chat (with its last line), the path
// from the project to Issues, then the branch's own thread and composer. No
// motion: it is there, or it is not. At phone width it fills the panel.

export function BranchSheet({
  projectLabel,
  peek,
  hidden,
  onMinimise,
  children,
}: {
  /** The project as the chat's breadcrumb names it. */
  projectLabel: string;
  /** The main chat's last line, on the strip; null when it has none. */
  peek: string | null;
  /** Minimised: kept (its draft too) but out of sight, the main chat in front. */
  hidden: boolean;
  /** Back to the main chat: the sheet folds down to a link at its end. */
  onMinimise: () => void;
  /** The branch's thread and composer. */
  children: ReactNode;
}) {
  return (
    <Box
      component="section"
      aria-label="Issues chat"
      hidden={hidden}
      sx={{
        position: "absolute",
        top: 10,
        left: 6,
        right: 6,
        bottom: 0,
        zIndex: 1,
        display: hidden ? "none" : "flex",
        flexDirection: "column",
        minHeight: 0,
        bgcolor: "var(--aep-shell-chat)",
        border: 1,
        borderBottom: 0,
        borderColor: "divider",
        borderRadius: "14px 14px 0 0",
        boxShadow: "0 -14px 34px rgba(20,24,30,.22), 0 -2px 8px rgba(20,24,30,.08)",
        overflow: "hidden",
        [PHONE]: { top: 0, left: 0, right: 0, borderRadius: 0, border: 0 },
      }}
    >
      <ButtonBase
        onClick={onMinimise}
        sx={{
          display: "flex",
          justifyContent: "flex-start",
          alignItems: "baseline",
          gap: 1,
          px: 1.75,
          py: 1,
          width: "100%",
          fontSize: "0.78125rem",
          textAlign: "left",
          whiteSpace: "nowrap",
          overflow: "hidden",
          borderBottom: 1,
          borderColor: "divider",
          bgcolor: "background.default",
          "&:hover": { bgcolor: "action.hover" },
        }}
      >
        <Box component="span" aria-hidden sx={{ color: "primary.main", fontWeight: 700 }}>
          ↑
        </Box>{" "}
        <Box component="b" sx={{ fontWeight: 600 }}>
          Main chat
        </Box>{" "}
        {peek && (
          <Box component="span" sx={{ color: "text.secondary", overflow: "hidden", textOverflow: "ellipsis", minWidth: 0, flex: 1 }}>
            {peek}
          </Box>
        )}
      </ButtonBase>
      <Box sx={{ px: 1.75, py: 1, borderBottom: 1, borderColor: "divider" }}>
        <Typography data-testid="branch-path" noWrap sx={{ fontSize: "0.8125rem" }}>
          <Box component="span" sx={{ color: "text.secondary" }}>
            {projectLabel}
          </Box>{" "}
          <Box component="span" aria-hidden sx={{ color: "text.secondary" }}>
            └
          </Box>{" "}
          <Box component="b" sx={{ fontWeight: 600 }}>
            Issues
          </Box>
        </Typography>
      </Box>
      {children}
    </Box>
  );
}
