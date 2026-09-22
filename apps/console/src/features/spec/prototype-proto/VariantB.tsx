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

// PROTOTYPE (throwaway, issue #813). Variant B — "Immersive": the app fills
// the viewport as the end user would see it. Review controls live in one
// floating pill (top-right). In Annotate a composer card floats bottom-centre;
// the queue is a bottom drawer opened from the pill's count.

import { useState } from "react";
import { Box, Button, Chip, Divider, Drawer, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { Screen, SideNav } from "./Renderer";
import { Composer, FlowSelect, ModeToggle, QueuedCard, RoleSelect, SendAll, StateSelect, type VariantProps } from "./shared";

export const name = "Immersive: floating controls";

export function VariantB(props: VariantProps) {
  const { model, state, dispatch } = props;
  const [busy, setBusy] = useState(false);
  const [queueOpen, setQueueOpen] = useState(false);
  const [controlsOpen, setControlsOpen] = useState(false);
  const screen = model.screens.find((s) => s.id === state.screenId)!;
  const annotate = state.mode === "annotate";
  return (
    <Box sx={{ flex: 1, minHeight: 0, position: "relative", overflow: "hidden" }}>
      {/* The app, as the user would see it */}
      <Stack direction="row" sx={{ height: "100%" }}>
        <Box sx={{ width: 240, borderRight: 1, borderColor: "divider", bgcolor: "background.paper" }}>
          <Box sx={{ px: 2, py: 2, borderBottom: 1, borderColor: "divider" }}>
            <Typography variant="subtitle1" fontWeight={600}>{model.name}</Typography>
          </Box>
          {screen.navigationId && <SideNav navigationId={screen.navigationId} />}
        </Box>
        <Box sx={{ flex: 1, overflow: "auto", p: 4, pb: annotate ? 28 : 4, bgcolor: "background.default" }}>
          <Box sx={{ maxWidth: 1100, mx: "auto" }}>
            <Screen screen={screen} />
          </Box>
        </Box>
      </Stack>

      {/* Annotate tint so the mode is unmistakable */}
      {annotate && (
        <Box sx={{ position: "absolute", inset: 0, pointerEvents: "none", boxShadow: (t) => `inset 0 0 0 4px ${t.palette.primary.main}` }} />
      )}

      {/* Floating control pill */}
      <Paper elevation={6} sx={{ position: "absolute", top: 16, right: 16, borderRadius: 6, px: 1.5, py: 1 }}>
        <Stack direction="row" alignItems="center" spacing={1.5}>
          <ModeToggle {...props} />
          <Divider orientation="vertical" flexItem />
          <Button size="small" onClick={() => setControlsOpen((o) => !o)}>
            {model.roles.find((r) => r.id === state.roleId)?.name} · {screen.name}
          </Button>
          <Divider orientation="vertical" flexItem />
          <Chip
            size="small"
            color={state.queue.length ? "primary" : "default"}
            label={`${state.queue.length} queued`}
            onClick={() => setQueueOpen(true)}
          />
        </Stack>
        {controlsOpen && (
          <Stack direction="row" spacing={1} sx={{ pt: 1.5 }}>
            <RoleSelect {...props} />
            <FlowSelect {...props} />
            <StateSelect {...props} />
          </Stack>
        )}
      </Paper>

      {/* Floating composer */}
      {annotate && (
        <Paper elevation={8} sx={{ position: "absolute", left: "50%", bottom: 24, transform: "translateX(-50%)", width: 560, p: 2, borderRadius: 3 }}>
          <Stack spacing={1}>
            <Typography variant="caption" color="text.secondary">
              {state.selectedComponentIds.length ? "Request about the selection" : "Click anything to select it, or describe the whole screen"}
            </Typography>
            <Composer {...props} />
          </Stack>
        </Paper>
      )}

      {/* Queue drawer */}
      <Drawer anchor="bottom" open={queueOpen} onClose={() => setQueueOpen(false)}>
        <Box sx={{ p: 3, maxHeight: "60vh", overflow: "auto" }}>
          <Stack direction="row" alignItems="center" justifyContent="space-between" sx={{ mb: 2 }}>
            <Typography variant="h6">Queued requests ({state.queue.length})</Typography>
            <SendAll {...props} busy={busy} setBusy={setBusy} />
          </Stack>
          <Stack spacing={1.5} sx={{ maxWidth: 800 }}>
            {state.queue.length === 0 && <Typography variant="body2" color="text.secondary">Nothing queued yet.</Typography>}
            {state.queue.map((a) => (
              <QueuedCard key={a.id} a={a} model={model} dispatch={dispatch} onJump={() => { dispatch({ type: "NAVIGATE", screenId: a.screenId }); setQueueOpen(false); }} />
            ))}
          </Stack>
        </Box>
      </Drawer>
    </Box>
  );
}
