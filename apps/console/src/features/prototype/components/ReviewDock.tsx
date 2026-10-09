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

import { Children, Fragment, forwardRef, type ReactNode } from "react";
import { Box, Divider, Paper } from "@wso2/oxygen-ui";

/** Below this width the dock drops the Comment mode hint (the window's tag still says it). */
export const DOCK_COMPACT = "@media (max-width: 1100px)";
/** Below this width the Role and State selects drop their inline labels, keeping the value. */
export const DOCK_NARROW = "@media (max-width: 1000px)";

/** One of the dock's groups, named for assistive technology. */
export function DockGroup({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Box role="group" aria-label={label} sx={{ display: "flex", alignItems: "center", gap: 0.5, minWidth: 0 }}>
      {children}
    </Box>
  );
}

/**
 * The review's one floating dock (#885): every control, in groups split by
 * dividers. It sits below the prototype window in the layout rather than over
 * it, so the space it takes is reserved and it never covers the prototype's
 * last rows; what a group opens (the comment list, the send's notices) grows
 * upward from it, over the prototype. The ref is the dock, which a
 * whole-screen comment's bubble anchors to.
 */
export const ReviewDock = forwardRef<HTMLElement, { children: ReactNode }>(function ReviewDock({ children }, ref) {
  const groups = Children.toArray(children);
  return (
    <Paper
      ref={ref}
      component="section"
      aria-label="Review controls"
      sx={{
        position: "relative",
        alignSelf: "center",
        maxWidth: "100%",
        display: "flex",
        alignItems: "center",
        gap: 0.5,
        p: 0.75,
        borderRadius: 3.5,
        border: 1,
        borderColor: "divider",
        boxShadow: "var(--aep-shell-card-shadow)",
        whiteSpace: "nowrap",
      }}
    >
      {groups.map((group, i) => (
        <Fragment key={i}>
          {i > 0 && <Divider orientation="vertical" flexItem sx={{ mx: 0.5, my: 0.75 }} />}
          {group}
        </Fragment>
      ))}
    </Paper>
  );
});
