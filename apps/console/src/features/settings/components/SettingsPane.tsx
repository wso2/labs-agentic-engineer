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
import { Box, Typography } from "@wso2/oxygen-ui";

/** How wide a Settings section reads, whatever it holds. */
export const PANE_MAX_WIDTH = 820;

/** One section of the Settings card: its title and what it is for, an action beside them, then its body. */
export function SettingsPane({
  title,
  intro,
  action,
  children,
}: {
  title: string;
  intro: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Box component="section" aria-label={title} sx={{ display: "flex", flexDirection: "column", gap: 2.5, maxWidth: PANE_MAX_WIDTH }}>
      <Box sx={{ display: "flex", alignItems: "flex-start", flexWrap: "wrap", gap: 2 }}>
        <Box sx={{ flex: 1, minWidth: 240 }}>
          <Typography component="h3" sx={{ fontSize: "1.5rem", fontWeight: 600 }}>
            {title}
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {intro}
          </Typography>
        </Box>
        {action}
      </Box>
      {children}
    </Box>
  );
}
