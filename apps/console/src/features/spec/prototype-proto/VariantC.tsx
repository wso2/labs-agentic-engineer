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

// PROTOTYPE (throwaway, issue #813). Variant C — "Review board": a review
// rail on the left lists every flow and screen with its queued count, so the
// reviewer works the application screen by screen. The app is framed in the
// centre with its own nav. Requests live in a bottom sheet table.

import { useState } from "react";
import {
  Box,
  Chip,
  Collapse,
  Divider,
  IconButton,
  List,
  ListItemButton,
  ListItemText,
  ListSubheader,
  ListingTable,
  Paper,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronDown, ChevronUp, X } from "@wso2/oxygen-ui-icons-react";
import { labelOf } from "./model";
import { Screen, SideNav } from "./Renderer";
import { Composer, ModeToggle, RoleSelect, SendAll, StateSelect, type VariantProps } from "./shared";

export const name = "Review board: screen rail + request table";

export function VariantC(props: VariantProps) {
  const { model, state, dispatch } = props;
  const [busy, setBusy] = useState(false);
  const [sheetOpen, setSheetOpen] = useState(true);
  const screen = model.screens.find((s) => s.id === state.screenId)!;
  const flows = model.flows.filter((f) => f.roleId === state.roleId);
  const countFor = (screenId: string) => state.queue.filter((a) => a.screenId === screenId).length;
  const annotate = state.mode === "annotate";

  return (
    <Stack direction="row" sx={{ flex: 1, minHeight: 0 }}>
      {/* Review rail */}
      <Paper square elevation={0} sx={{ width: 280, borderRight: 1, borderColor: "divider", display: "flex", flexDirection: "column" }}>
        <Stack spacing={1.5} sx={{ p: 2, borderBottom: 1, borderColor: "divider" }}>
          <Typography variant="subtitle2" color="text.secondary">Reviewing</Typography>
          <Typography variant="h6">{model.name}</Typography>
          <ModeToggle {...props} size="medium" />
        </Stack>
        <Box sx={{ flex: 1, overflow: "auto" }}>
          {flows.map((f) => (
            <List key={f.id} dense subheader={<ListSubheader disableSticky>{f.name}</ListSubheader>}>
              {f.screenIds.map((sid, i) => {
                const s = model.screens.find((x) => x.id === sid)!;
                const n = countFor(sid);
                return (
                  <ListItemButton key={sid} selected={state.screenId === sid && state.flowId === f.id}
                    onClick={() => dispatch({ type: "SET_FLOW", flowId: f.id, screenId: sid })}>
                    <ListItemText primary={`${i + 1}. ${s.name}`} />
                    {n > 0 && <Chip size="small" color="primary" label={n} />}
                  </ListItemButton>
                );
              })}
            </List>
          ))}
          <List dense subheader={<ListSubheader disableSticky>All screens</ListSubheader>}>
            {model.screens.filter((s) => s.roleIds.includes(state.roleId)).map((s) => {
              const n = countFor(s.id);
              return (
                <ListItemButton key={s.id} selected={state.screenId === s.id && state.flowId === null}
                  onClick={() => dispatch({ type: "SET_FLOW", flowId: null, screenId: s.id })}>
                  <ListItemText primary={s.name} />
                  {n > 0 && <Chip size="small" color="primary" label={n} />}
                </ListItemButton>
              );
            })}
          </List>
        </Box>
        <Stack spacing={1.5} sx={{ p: 2, borderTop: 1, borderColor: "divider" }}>
          <RoleSelect {...props} />
          <StateSelect {...props} />
        </Stack>
      </Paper>

      {/* Framed app + bottom sheet */}
      <Stack sx={{ flex: 1, minWidth: 0 }}>
        <Box sx={{ flex: 1, overflow: "auto", p: 3, bgcolor: "background.default" }}>
          <Paper variant="outlined" sx={{ maxWidth: 1280, mx: "auto", minHeight: "100%", overflow: "hidden", ...(annotate && { borderColor: "primary.main", borderWidth: 2 }) }}>
            <Stack direction="row" sx={{ minHeight: "100%" }}>
              <Box sx={{ width: 220, borderRight: 1, borderColor: "divider" }}>
                {screen.navigationId && <SideNav navigationId={screen.navigationId} />}
              </Box>
              <Box sx={{ flex: 1, p: 4 }}>
                <Screen screen={screen} />
              </Box>
            </Stack>
          </Paper>
        </Box>

        <Paper square elevation={4} sx={{ borderTop: 1, borderColor: "divider" }}>
          <Stack direction="row" alignItems="center" spacing={2} sx={{ px: 2, py: 1 }}>
            <IconButton size="small" onClick={() => setSheetOpen((o) => !o)} aria-label="Toggle requests">
              {sheetOpen ? <ChevronDown size={18} /> : <ChevronUp size={18} />}
            </IconButton>
            <Typography variant="subtitle2">Requests</Typography>
            <Chip size="small" label={state.queue.length} />
            <Box sx={{ flex: 1 }} />
            <SendAll {...props} busy={busy} setBusy={setBusy} />
          </Stack>
          <Collapse in={sheetOpen}>
            <Divider />
            <Stack direction="row" sx={{ maxHeight: 280 }}>
              <Box sx={{ width: 420, p: 2, borderRight: 1, borderColor: "divider", overflow: "auto" }}>
                {annotate ? (
                  <Composer {...props} />
                ) : (
                  <Typography variant="body2" color="text.secondary">Switch to Annotate to add a request about {screen.name}.</Typography>
                )}
              </Box>
              <Box sx={{ flex: 1, overflow: "auto" }}>
                <ListingTable.Container disablePaper>
                  <ListingTable density="compact">
                    <ListingTable.Head>
                      <ListingTable.Row>
                        <ListingTable.Cell>Screen</ListingTable.Cell>
                        <ListingTable.Cell>About</ListingTable.Cell>
                        <ListingTable.Cell>Request</ListingTable.Cell>
                        <ListingTable.Cell />
                      </ListingTable.Row>
                    </ListingTable.Head>
                    <ListingTable.Body>
                      {state.queue.map((a) => (
                        <ListingTable.Row key={a.id} clickable onClick={() => dispatch({ type: "NAVIGATE", screenId: a.screenId })}>
                          <ListingTable.Cell>{model.screens.find((s) => s.id === a.screenId)?.name}</ListingTable.Cell>
                          <ListingTable.Cell>
                            {a.componentIds.length === 0 ? "Whole screen" : a.componentIds.map((id) => labelOf(model, a.screenId, id)).join(", ")}
                          </ListingTable.Cell>
                          <ListingTable.Cell>{a.request}</ListingTable.Cell>
                          <ListingTable.Cell>
                            <IconButton size="small" aria-label="Remove" onClick={(e) => { e.stopPropagation(); dispatch({ type: "REMOVE", annotationId: a.id }); }}>
                              <X size={14} />
                            </IconButton>
                          </ListingTable.Cell>
                        </ListingTable.Row>
                      ))}
                    </ListingTable.Body>
                  </ListingTable>
                  {state.queue.length === 0 && <ListingTable.EmptyState title="No requests yet" description="Requests from every screen collect here and are sent together." minHeight={120} />}
                </ListingTable.Container>
              </Box>
            </Stack>
          </Collapse>
        </Paper>
      </Stack>
    </Stack>
  );
}
