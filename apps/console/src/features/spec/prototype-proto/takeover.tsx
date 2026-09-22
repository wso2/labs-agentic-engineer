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

// PROTOTYPE (throwaway, issue #813). Variants D–F escape the console shell:
// a fixed full-viewport layer above the AE chrome, so the prototype reads as
// its own application with a clear boundary. Shared: the takeover layer, an
// exit affordance, and a browser-window frame around the app.

import type { ReactNode } from "react";
import { Box, Button, Stack, Typography } from "@wso2/oxygen-ui";
import { ArrowLeft, Lock } from "@wso2/oxygen-ui-icons-react";
import { Screen, SideNav } from "./Renderer";
import type { VariantProps } from "./shared";

/** Full-viewport layer over the console. Dialogs/drawers (modal z) still sit above it. */
export function Takeover({ children, dark }: { children: ReactNode; dark?: boolean }) {
  return (
    <Box
      sx={{
        position: "fixed",
        inset: 0,
        zIndex: (t) => t.zIndex.drawer,
        display: "flex",
        flexDirection: "column",
        bgcolor: dark ? "grey.900" : "background.default",
        color: dark ? "grey.100" : "text.primary",
      }}
    >
      {children}
    </Box>
  );
}

export function ExitButton({ dark }: { dark?: boolean }) {
  return (
    <Button size="small" startIcon={<ArrowLeft size={16} />} {...(dark ? { sx: { color: "grey.300" } } : {})} onClick={() => window.history.back()}>
      Back to Spec
    </Button>
  );
}

/** The app inside a browser window: tab strip, address bar, then nav + screen. */
export function BrowserFrame({ model, state, children, sx }: VariantProps & { children?: ReactNode; sx?: object }) {
  const screen = model.screens.find((s) => s.id === state.screenId)!;
  const path = "/" + screen.id.replace("screen.", "").replace(/\./g, "/");
  return (
    <Box sx={{ display: "flex", flexDirection: "column", borderRadius: 2, overflow: "hidden", border: 1, borderColor: "divider", bgcolor: "background.paper", color: "text.primary", boxShadow: 6, ...sx }}>
      <Stack direction="row" alignItems="center" spacing={1} sx={{ px: 1.5, py: 0.75, bgcolor: "grey.200", color: "grey.800" }}>
        <Stack direction="row" spacing={0.75} sx={{ mr: 1 }}>
          {["error.main", "warning.main", "success.main"].map((c) => (
            <Box key={c} sx={{ width: 10, height: 10, borderRadius: "50%", bgcolor: c, opacity: 0.8 }} />
          ))}
        </Stack>
        <Box sx={{ px: 1.5, py: 0.25, borderRadius: 1, bgcolor: "background.paper", fontSize: 12 }}>{model.name}</Box>
        <Stack direction="row" alignItems="center" spacing={0.75} sx={{ flex: 1, ml: 1, px: 1.5, py: 0.25, borderRadius: 1, bgcolor: "grey.50", fontSize: 12, color: "grey.600" }}>
          <Lock size={11} />
          <Typography variant="caption" sx={{ fontFamily: "monospace" }}>{model.component}.example.com{path}</Typography>
        </Stack>
      </Stack>
      <Stack direction="row" sx={{ flex: 1, minHeight: 0 }}>
        <Box sx={{ width: 220, borderRight: 1, borderColor: "divider", bgcolor: "background.paper", overflow: "hidden" }}>
          <Box sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: "divider" }}>
            <Typography variant="subtitle2">{model.name}</Typography>
          </Box>
          {screen.navigationId && <SideNav navigationId={screen.navigationId} />}
        </Box>
        <Box sx={{ flex: 1, overflow: "auto", p: 4, bgcolor: "background.default", position: "relative" }}>
          <Box sx={{ maxWidth: 1100, mx: "auto" }}>
            <Screen screen={screen} />
          </Box>
          {children}
        </Box>
      </Stack>
    </Box>
  );
}
