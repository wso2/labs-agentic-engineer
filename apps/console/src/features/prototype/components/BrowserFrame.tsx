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
 * The application window a prototype renders in: a browser-style tab strip
 * and address bar above the app's own shell, so where the prototype ends and
 * the console begins is never in doubt.
 */

import type { ReactNode } from "react";
import { Box, Stack, Typography } from "@wso2/oxygen-ui";
import { Lock } from "@wso2/oxygen-ui-icons-react";

export function BrowserFrame({ title, address, children }: { title: string; address: string; children: ReactNode }) {
  return (
    <Box
      role="region"
      aria-label={`${title} prototype`}
      sx={{
        display: "flex",
        flexDirection: "column",
        flex: 1,
        minWidth: 0,
        minHeight: 0,
        border: 1,
        borderColor: "divider",
        borderRadius: 2,
        overflow: "hidden",
        bgcolor: "background.paper",
        boxShadow: 4,
      }}
    >
      <Stack
        direction="row"
        alignItems="center"
        spacing={1.5}
        sx={{ px: 1.5, py: 0.75, bgcolor: "action.hover", borderBottom: 1, borderColor: "divider" }}
      >
        <Stack direction="row" spacing={0.75} aria-hidden>
          {(["error.main", "warning.main", "success.main"] as const).map((c) => (
            <Box key={c} sx={{ width: 10, height: 10, borderRadius: "50%", bgcolor: c, opacity: 0.8 }} />
          ))}
        </Stack>
        <Typography variant="caption" sx={{ px: 1.5, py: 0.25, borderRadius: 1, bgcolor: "background.paper" }}>
          {title}
        </Typography>
        <Stack
          direction="row"
          alignItems="center"
          spacing={0.75}
          sx={{ flex: 1, minWidth: 0, px: 1.5, py: 0.25, borderRadius: 1, bgcolor: "background.default", color: "text.secondary" }}
        >
          <Lock size={12} aria-hidden />
          <Typography variant="caption" noWrap sx={{ fontFamily: "monospace" }} aria-label="Address">
            {address}
          </Typography>
        </Stack>
      </Stack>
      <Box sx={{ display: "flex", flexDirection: "column", flex: 1, minHeight: 0 }}>{children}</Box>
    </Box>
  );
}
