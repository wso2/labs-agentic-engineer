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

// PROTOTYPE (throwaway, issue #813). Variant D — "Inspector": full takeover.
// One slim review bar on top; the app in a browser window below, edge to
// edge. Preview shows nothing else. Annotate slides an inspector in from the
// right (composer + queue) and the app window shrinks to make room.

import { useState } from "react";
import { Box, Chip, Divider, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { Composer, FlowSelect, ModeToggle, QueuedCard, RoleSelect, SendAll, StateSelect, type VariantProps } from "./shared";
import { BrowserFrame, ExitButton, Takeover } from "./takeover";

export const name = "Inspector: takeover + side inspector on Annotate";

export function VariantD(props: VariantProps) {
  const { model, state, dispatch } = props;
  const [busy, setBusy] = useState(false);
  const annotate = state.mode === "annotate";
  const screen = model.screens.find((s) => s.id === state.screenId)!;
  return (
    <Takeover>
      <Paper square elevation={0} sx={{ px: 2, py: 1, borderBottom: 1, borderColor: "divider" }}>
        <Stack direction="row" alignItems="center" spacing={2}>
          <ExitButton />
          <Divider orientation="vertical" flexItem />
          <Typography variant="subtitle2">{model.name}</Typography>
          <Chip size="small" variant="outlined" label="Prototype · read-only" />
          <Box sx={{ flex: 1 }} />
          <RoleSelect {...props} />
          <FlowSelect {...props} />
          <StateSelect {...props} />
          <Divider orientation="vertical" flexItem />
          <ModeToggle {...props} />
        </Stack>
      </Paper>
      <Stack direction="row" sx={{ flex: 1, minHeight: 0 }}>
        <Box sx={{ flex: 1, minWidth: 0, p: 2, display: "flex" }}>
          <BrowserFrame {...props} sx={{ flex: 1 }} />
        </Box>
        {annotate && (
          <Paper square elevation={0} sx={{ width: 380, borderLeft: 1, borderColor: "divider", display: "flex", flexDirection: "column", minHeight: 0 }}>
            <Stack direction="row" alignItems="center" sx={{ px: 2, py: 1.5, borderBottom: 1, borderColor: "divider" }}>
              <Typography variant="subtitle2" sx={{ flex: 1 }}>Feedback</Typography>
              <SendAll {...props} busy={busy} setBusy={setBusy} />
            </Stack>
            <Box sx={{ p: 2, borderBottom: 1, borderColor: "divider" }}>
              <Stack spacing={1}>
                <Typography variant="caption" color="text.secondary">On {screen.name}</Typography>
                <Composer {...props} />
              </Stack>
            </Box>
            <Box sx={{ flex: 1, overflow: "auto", p: 2 }}>
              <Stack spacing={1.5}>
                {state.queue.length === 0 ? (
                  <Typography variant="body2" color="text.secondary">Click anything in the app to select it, then describe the change.</Typography>
                ) : (
                  state.queue.map((a) => (
                    <QueuedCard key={a.id} a={a} model={model} dispatch={dispatch} onJump={() => dispatch({ type: "NAVIGATE", screenId: a.screenId })} />
                  ))
                )}
              </Stack>
            </Box>
          </Paper>
        )}
      </Stack>
    </Takeover>
  );
}
