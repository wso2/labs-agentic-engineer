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

// PROTOTYPE (throwaway, issue #813). Variant A — "Workbench": a review
// toolbar across the top, the app (its own side-nav + screen) in the middle,
// and a persistent feedback panel on the right that becomes the composer in
// Annotate. Everything is visible at once; nothing floats.

import { useState } from "react";
import { Box, Divider, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { Screen, SideNav } from "./Renderer";
import { Composer, FlowSelect, ModeToggle, QueuedCard, RoleSelect, ScreenSelect, SendAll, StateSelect, type VariantProps } from "./shared";

export const name = "Workbench: toolbar + side panel";

export function VariantA(props: VariantProps) {
  const { model, state, dispatch } = props;
  const [busy, setBusy] = useState(false);
  const screen = model.screens.find((s) => s.id === state.screenId)!;
  return (
    <Stack sx={{ flex: 1, minHeight: 0 }}>
      <Paper square elevation={0} sx={{ borderBottom: 1, borderColor: "divider", px: 2, py: 1 }}>
        <Stack direction="row" alignItems="center" spacing={2}>
          <ModeToggle {...props} />
          <Divider orientation="vertical" flexItem />
          <RoleSelect {...props} />
          <FlowSelect {...props} />
          <ScreenSelect {...props} />
          <StateSelect {...props} />
          <Box sx={{ flex: 1 }} />
          <SendAll {...props} busy={busy} setBusy={setBusy} />
        </Stack>
      </Paper>
      <Stack direction="row" sx={{ flex: 1, minHeight: 0 }}>
        <Box sx={{ width: 240, borderRight: 1, borderColor: "divider", bgcolor: "background.paper", overflow: "auto" }}>
          {screen.navigationId && <SideNav navigationId={screen.navigationId} />}
        </Box>
        <Box sx={{ flex: 1, overflow: "auto", p: 4, bgcolor: "background.default" }}>
          <Box sx={{ maxWidth: 1100, mx: "auto" }}>
            <Screen screen={screen} />
          </Box>
        </Box>
        <Paper square elevation={0} sx={{ width: 360, borderLeft: 1, borderColor: "divider", display: "flex", flexDirection: "column", minHeight: 0 }}>
          <Box sx={{ p: 2, borderBottom: 1, borderColor: "divider" }}>
            {state.mode === "annotate" ? (
              <Stack spacing={1}>
                <Typography variant="subtitle2">New request · {screen.name}</Typography>
                <Composer {...props} />
              </Stack>
            ) : (
              <Stack spacing={0.5}>
                <Typography variant="subtitle2">Feedback</Typography>
                <Typography variant="body2" color="text.secondary">
                  Switch to Annotate, click what should change, and describe it. Requests queue here until you send them together.
                </Typography>
              </Stack>
            )}
          </Box>
          <Box sx={{ flex: 1, overflow: "auto", p: 2 }}>
            <Stack spacing={1.5}>
              <Typography variant="overline" color="text.secondary">Queued ({state.queue.length})</Typography>
              {state.queue.length === 0 && (
                <Typography variant="body2" color="text.secondary">Nothing queued yet.</Typography>
              )}
              {state.queue.map((a) => (
                <QueuedCard key={a.id} a={a} model={model} dispatch={dispatch} onJump={() => dispatch({ type: "NAVIGATE", screenId: a.screenId })} />
              ))}
            </Stack>
          </Box>
        </Paper>
      </Stack>
    </Stack>
  );
}
