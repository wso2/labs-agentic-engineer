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

// PROTOTYPE (throwaway, issue #813). Variant F — "Storyboard": full takeover.
// Flows are tabs, screens are a numbered filmstrip under them (with queued
// counts), the app fills the rest. Annotate opens a comment strip under the
// app: the composer plus this screen's queued requests as horizontal cards.

import { useState } from "react";
import { Box, Chip, Paper, Stack, Tab, Tabs, Typography } from "@wso2/oxygen-ui";
import { Composer, ModeToggle, QueuedCard, RoleSelect, SendAll, StateSelect, type VariantProps } from "./shared";
import { BrowserFrame, ExitButton, Takeover } from "./takeover";

export const name = "Storyboard: flow tabs + filmstrip + comment strip";

export function VariantF(props: VariantProps) {
  const { model, state, dispatch } = props;
  const [busy, setBusy] = useState(false);
  const annotate = state.mode === "annotate";
  const flows = model.flows.filter((f) => f.roleId === state.roleId);
  const flow = model.flows.find((f) => f.id === state.flowId) ?? flows[0]!;
  const countFor = (id: string) => state.queue.filter((a) => a.screenId === id).length;
  const here = state.queue.filter((a) => a.screenId === state.screenId);
  return (
    <Takeover>
      <Paper square elevation={0} sx={{ borderBottom: 1, borderColor: "divider" }}>
        <Stack direction="row" alignItems="center" spacing={2} sx={{ px: 2, pt: 1 }}>
          <ExitButton />
          <Typography variant="subtitle2">{model.name}</Typography>
          <Tabs value={flow.id} onChange={(_, v: string) => { const f = model.flows.find((x) => x.id === v)!; dispatch({ type: "SET_FLOW", flowId: f.id, screenId: f.screenIds[0]! }); }} sx={{ minHeight: 40 }}>
            {flows.map((f) => <Tab key={f.id} value={f.id} label={f.name} sx={{ minHeight: 40 }} />)}
          </Tabs>
          <Box sx={{ flex: 1 }} />
          <RoleSelect {...props} />
          <StateSelect {...props} />
          <ModeToggle {...props} />
        </Stack>
        <Stack direction="row" spacing={1} sx={{ px: 2, py: 1 }}>
          {flow.screenIds.map((sid, i) => {
            const s = model.screens.find((x) => x.id === sid)!;
            const n = countFor(sid);
            const active = sid === state.screenId;
            return (
              <Chip
                key={sid}
                label={`${i + 1} · ${s.name}${n ? ` (${n})` : ""}`}
                color={active ? "primary" : n ? "warning" : "default"}
                variant={active ? "filled" : "outlined"}
                onClick={() => dispatch({ type: "SET_FLOW", flowId: flow.id, screenId: sid })}
              />
            );
          })}
        </Stack>
      </Paper>
      <Box sx={{ flex: 1, minHeight: 0, p: 2, display: "flex" }}>
        <BrowserFrame {...props} sx={{ flex: 1 }} />
      </Box>
      {annotate && (
        <Paper square elevation={8} sx={{ borderTop: 1, borderColor: "divider", p: 2 }}>
          <Stack direction="row" spacing={2} alignItems="stretch">
            <Box sx={{ width: 380, flexShrink: 0 }}>
              <Composer {...props} />
            </Box>
            <Box sx={{ flex: 1, overflowX: "auto" }}>
              <Stack direction="row" spacing={1.5} sx={{ minHeight: 100 }}>
                {here.length === 0 && (
                  <Typography variant="body2" color="text.secondary" sx={{ alignSelf: "center" }}>
                    No requests on this screen yet. {state.queue.length > 0 && `${state.queue.length} on other screens.`}
                  </Typography>
                )}
                {here.map((a) => (
                  <Box key={a.id} sx={{ width: 300, flexShrink: 0 }}>
                    <QueuedCard a={a} model={model} dispatch={dispatch} />
                  </Box>
                ))}
              </Stack>
            </Box>
            <Stack justifyContent="flex-end">
              <SendAll {...props} busy={busy} setBusy={setBusy} />
            </Stack>
          </Stack>
        </Paper>
      )}
    </Takeover>
  );
}
