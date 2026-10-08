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
 * Overlays: a modal dialog and a side drawer, drawn in place over the screen
 * on Oxygen surfaces. Not MUI's Modal: it portals out of the kit's scene (so
 * Annotate would not reach inside) and draws nothing in the render check.
 */

import { Box, DialogActions, DialogContent, DialogTitle, IconButton, Paper, Typography } from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import type { ThemeDialogProps, ThemeDrawerProps } from "@wso2/prototype-kit";
import type { ReactNode } from "react";

function Backdrop({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  return (
    <Box onClick={onClose} sx={{ position: "fixed", inset: 0, display: "flex", zIndex: "modal", bgcolor: "rgba(15, 23, 42, 0.45)" }}>
      {children}
    </Box>
  );
}

export function Dialog({ title, titleId, open, onClose, children, actions }: ThemeDialogProps) {
  if (!open) return null;
  return (
    <Backdrop onClose={onClose}>
      <Paper
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        elevation={8}
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key !== "Escape") return;
          // Used here, so the frame does not hand it on to the host.
          e.preventDefault();
          onClose();
        }}
        sx={{ m: "auto", width: "min(560px, calc(100% - 32px))", display: "flex", flexDirection: "column" }}
      >
        <DialogTitle id={titleId} component="h2">
          {title}
        </DialogTitle>
        <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2 }}>{children}</DialogContent>
        {actions !== undefined && <DialogActions sx={{ px: 3, pb: 2 }}>{actions}</DialogActions>}
      </Paper>
    </Backdrop>
  );
}

export function Drawer({ title, open, onClose, children }: ThemeDrawerProps) {
  if (!open) return null;
  return (
    <Backdrop onClose={onClose}>
      <Paper
        role="dialog"
        aria-label={title}
        square
        elevation={8}
        onClick={(e) => e.stopPropagation()}
        sx={{ ml: "auto", width: "min(400px, 100%)", height: "100%", overflow: "auto", p: 2.5, display: "flex", flexDirection: "column", gap: 2 }}
      >
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <Typography variant="h6" component="h2">
            {title}
          </Typography>
          <IconButton aria-label="Close" size="small" onClick={onClose}>
            <X size={18} />
          </IconButton>
        </Box>
        <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>{children}</Box>
      </Paper>
    </Backdrop>
  );
}
