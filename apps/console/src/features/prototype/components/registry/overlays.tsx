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

// Overlays: a screen's dialogs and drawers. Open and closed by view state only.

import { Box, Dialog, DialogActions, DialogContent, DialogTitle, Drawer, IconButton, Stack, Typography } from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeOverlay } from "@aep/prototype-model";
import { Selectable, usePrototypeRender } from "../renderContext";
import { ActionButton } from "./content";

export function OverlayView({ overlay, onClose }: { overlay: PrototypeOverlay; onClose: () => void }) {
  const { view, renderNodes } = usePrototypeRender();
  const open = view.openOverlayId === overlay.id;
  if (overlay.kind === "dialog") {
    return (
      <Dialog open={open} onClose={onClose} fullWidth maxWidth="sm" aria-labelledby={`${overlay.id}-title`}>
        <DialogTitle id={`${overlay.id}-title`}>{overlay.title}</DialogTitle>
        <DialogContent>
          <Selectable id={overlay.id}>
            <Stack spacing={2} sx={{ pt: 1 }}>
              {renderNodes(overlay.content)}
            </Stack>
          </Selectable>
        </DialogContent>
        {overlay.actions.length > 0 && (
          <DialogActions>
            {overlay.actions.map((b) => (
              <ActionButton key={b.id} button={b} />
            ))}
          </DialogActions>
        )}
      </Dialog>
    );
  }
  return (
    <Drawer anchor="right" open={open} onClose={onClose}>
      <Box role="dialog" aria-label={overlay.title} sx={{ width: 380, p: 3 }}>
        <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 2 }}>
          <Typography variant="h6" component="h2">
            {overlay.title}
          </Typography>
          <IconButton aria-label="Close" onClick={onClose}>
            <X size={18} />
          </IconButton>
        </Stack>
        <Selectable id={overlay.id}>
          <Stack spacing={2}>{renderNodes(overlay.content)}</Stack>
        </Selectable>
      </Box>
    </Drawer>
  );
}
