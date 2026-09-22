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

// PROTOTYPE (throwaway, issue #813). Variant E — "Stage": dark backdrop, the
// app as a lit window in the middle, and a presenter-style dock at the bottom
// (exit · prev/next screen · role · state · mode · queued). Annotate turns the
// dock into the composer; queued requests appear as numbered pins on the app
// and the dock's count opens the list.

import { useState } from "react";
import { Box, Chip, Divider, IconButton, Paper, Popover, Stack, Typography } from "@wso2/oxygen-ui";
import { ChevronLeft, ChevronRight } from "@wso2/oxygen-ui-icons-react";
import { Composer, ModeToggle, QueuedCard, RoleSelect, SendAll, StateSelect, type VariantProps } from "./shared";
import { BrowserFrame, ExitButton, Takeover } from "./takeover";

export const name = "Stage: dark backdrop + presenter dock";

export function VariantE(props: VariantProps) {
  const { model, state, dispatch } = props;
  const [busy, setBusy] = useState(false);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const annotate = state.mode === "annotate";
  const flow = model.flows.find((f) => f.id === state.flowId) ?? model.flows.find((f) => f.roleId === state.roleId)!;
  const idx = flow.screenIds.indexOf(state.screenId);
  const step = (d: number) => {
    const next = flow.screenIds[idx + d];
    if (next) dispatch({ type: "SET_FLOW", flowId: flow.id, screenId: next });
  };
  const screen = model.screens.find((s) => s.id === state.screenId)!;
  return (
    <Takeover dark>
      <Stack direction="row" alignItems="center" sx={{ px: 2, py: 1 }}>
        <ExitButton dark />
        <Box sx={{ flex: 1 }} />
        <Typography variant="caption" sx={{ color: "grey.500" }}>{model.name} · prototype · nothing here is live</Typography>
      </Stack>
      <Box sx={{ flex: 1, minHeight: 0, px: 6, pb: annotate ? 34 : 14, display: "flex" }}>
        <BrowserFrame {...props} sx={{ flex: 1 }} />
      </Box>

      <Paper elevation={16} sx={{ position: "absolute", left: "50%", bottom: 56, transform: "translateX(-50%)", borderRadius: 4, px: 2, py: 1.5, minWidth: 760, maxWidth: 900, bgcolor: "background.paper" }}>
        <Stack spacing={1.5}>
          <Stack direction="row" alignItems="center" spacing={1.5}>
            <IconButton size="small" aria-label="Previous screen" disabled={idx <= 0} onClick={() => step(-1)}><ChevronLeft size={18} /></IconButton>
            <Stack sx={{ minWidth: 200 }}>
              <Typography variant="caption" color="text.secondary">{flow.name} · {idx + 1} of {flow.screenIds.length}</Typography>
              <Typography variant="subtitle2">{screen.name}</Typography>
            </Stack>
            <IconButton size="small" aria-label="Next screen" disabled={idx >= flow.screenIds.length - 1} onClick={() => step(1)}><ChevronRight size={18} /></IconButton>
            <Divider orientation="vertical" flexItem />
            <RoleSelect {...props} />
            <StateSelect {...props} />
            <Divider orientation="vertical" flexItem />
            <ModeToggle {...props} />
            <Chip
              size="small"
              color={state.queue.length ? "primary" : "default"}
              label={`${state.queue.length} queued`}
              onClick={(e) => setAnchor(e.currentTarget)}
            />
          </Stack>
          {annotate && (
            <>
              <Divider />
              <Stack direction="row" spacing={2} alignItems="flex-start">
                <Box sx={{ flex: 1 }}><Composer {...props} /></Box>
                <SendAll {...props} busy={busy} setBusy={setBusy} />
              </Stack>
            </>
          )}
        </Stack>
      </Paper>

      <Popover open={!!anchor} anchorEl={anchor} onClose={() => setAnchor(null)} anchorOrigin={{ vertical: "top", horizontal: "center" }} transformOrigin={{ vertical: "bottom", horizontal: "center" }}>
        <Box sx={{ p: 2, width: 420, maxHeight: 400, overflow: "auto" }}>
          <Stack spacing={1.5}>
            <Typography variant="subtitle2">Queued requests</Typography>
            {state.queue.length === 0 && <Typography variant="body2" color="text.secondary">Nothing queued yet.</Typography>}
            {state.queue.map((a) => (
              <QueuedCard key={a.id} a={a} model={model} dispatch={dispatch} onJump={() => { dispatch({ type: "NAVIGATE", screenId: a.screenId }); setAnchor(null); }} />
            ))}
          </Stack>
        </Box>
      </Popover>
    </Takeover>
  );
}
