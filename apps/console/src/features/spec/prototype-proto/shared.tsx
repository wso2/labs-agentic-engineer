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

// PROTOTYPE (throwaway, issue #813). Small controls the variants share:
// selectors, a queued-request card, the composer. Variants own the layout.

import { useState } from "react";
import {
  Button,
  Card,
  CardContent,
  Chip,
  IconButton,
  MenuItem,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from "@wso2/oxygen-ui";
import { Eye, MessageSquarePlus, X } from "@wso2/oxygen-ui-icons-react";
import { labelOf, type PrototypeModelV1 } from "./model";
import type { Annotation, ViewEvent, ViewState } from "./viewState";

export interface VariantProps {
  model: PrototypeModelV1;
  state: ViewState;
  dispatch: (e: ViewEvent) => void;
}

export function ModeToggle({ state, dispatch, size = "small" }: VariantProps & { size?: "small" | "medium" }) {
  return (
    <ToggleButtonGroup
      exclusive
      size={size}
      value={state.mode}
      onChange={(_, v: "preview" | "annotate" | null) => {
        if (v === "annotate") dispatch({ type: "ENTER_ANNOTATE" });
        if (v === "preview") dispatch({ type: "EXIT_ANNOTATE" });
      }}
    >
      <ToggleButton value="preview" aria-label="Preview">
        <Eye size={16} />
        <Typography variant="button" sx={{ ml: 1 }}>Preview</Typography>
      </ToggleButton>
      <ToggleButton value="annotate" aria-label="Annotate">
        <MessageSquarePlus size={16} />
        <Typography variant="button" sx={{ ml: 1 }}>Annotate</Typography>
      </ToggleButton>
    </ToggleButtonGroup>
  );
}

export function RoleSelect({ model, state, dispatch }: VariantProps) {
  return (
    <TextField select size="small" label="Role" value={state.roleId} sx={{ minWidth: 150 }}
      onChange={(e) => {
        const roleId = e.target.value;
        const first = model.screens.find((s) => s.roleIds.includes(roleId)) ?? model.screens[0]!;
        dispatch({ type: "SET_ROLE", roleId });
        dispatch({ type: "NAVIGATE", screenId: first.id });
      }}>
      {model.roles.map((r) => <MenuItem key={r.id} value={r.id}>{r.name}</MenuItem>)}
    </TextField>
  );
}

export function FlowSelect({ model, state, dispatch }: VariantProps) {
  const flows = model.flows.filter((f) => f.roleId === state.roleId);
  return (
    <TextField select size="small" label="Flow" value={state.flowId ?? ""} sx={{ minWidth: 190 }}
      onChange={(e) => {
        const f = flows.find((x) => x.id === e.target.value);
        dispatch({ type: "SET_FLOW", flowId: f?.id ?? null, screenId: f?.screenIds[0] ?? state.screenId });
      }}>
      <MenuItem value="">Free navigation</MenuItem>
      {flows.map((f) => <MenuItem key={f.id} value={f.id}>{f.name}</MenuItem>)}
    </TextField>
  );
}

export function StateSelect({ model, state, dispatch }: VariantProps) {
  return (
    <TextField select size="small" label="Display state" value={state.stateId} sx={{ minWidth: 170 }}
      onChange={(e) => dispatch({ type: "SET_STATE", stateId: e.target.value })}>
      {model.states.map((s) => <MenuItem key={s.id} value={s.id}>{s.name}</MenuItem>)}
    </TextField>
  );
}

export function ScreenSelect({ model, state, dispatch }: VariantProps) {
  const screens = model.screens.filter((s) => s.roleIds.includes(state.roleId));
  return (
    <TextField select size="small" label="Screen" value={state.screenId} sx={{ minWidth: 170 }}
      onChange={(e) => dispatch({ type: "NAVIGATE", screenId: e.target.value })}>
      {screens.map((s) => <MenuItem key={s.id} value={s.id}>{s.name}</MenuItem>)}
    </TextField>
  );
}

export function SelectionChips({ model, state, dispatch }: VariantProps) {
  if (state.selectedComponentIds.length === 0) {
    return <Chip size="small" variant="outlined" label="Whole screen" />;
  }
  return (
    <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
      {state.selectedComponentIds.map((id) => (
        <Chip key={id} size="small" color="primary" label={labelOf(model, state.screenId, id)}
          onDelete={() => dispatch({ type: "TOGGLE_SELECTION", componentId: id })} />
      ))}
    </Stack>
  );
}

export function Composer({ model, state, dispatch, autoFocus }: VariantProps & { autoFocus?: boolean }) {
  const [text, setText] = useState("");
  const add = () => {
    dispatch({ type: "QUEUE", request: text });
    setText("");
  };
  return (
    <Stack spacing={1.5}>
      <SelectionChips model={model} state={state} dispatch={dispatch} />
      <TextField
        multiline
        minRows={2}
        size="small"
        autoFocus={!!autoFocus}
        placeholder={state.selectedComponentIds.length ? "What should change here?" : "What should change on this screen?"}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) add();
        }}
      />
      <Stack direction="row" justifyContent="flex-end" spacing={1}>
        {state.selectedComponentIds.length > 0 && (
          <Button size="small" onClick={() => dispatch({ type: "CLEAR_SELECTION" })}>Clear selection</Button>
        )}
        <Button size="small" variant="contained" disabled={!text.trim()} onClick={add}>Add request</Button>
      </Stack>
    </Stack>
  );
}

export function QueuedCard({ a, model, dispatch, onJump }: { a: Annotation; model: PrototypeModelV1; dispatch: (e: ViewEvent) => void; onJump?: () => void }) {
  const screen = model.screens.find((s) => s.id === a.screenId)?.name ?? a.screenId;
  return (
    <Card variant="outlined">
      <CardContent sx={{ "&:last-child": { pb: 1.5 }, pt: 1.5 }}>
        <Stack direction="row" alignItems="flex-start" spacing={1}>
          <Stack spacing={0.5} sx={{ flex: 1, minWidth: 0 }}>
            <Stack direction="row" spacing={0.5} alignItems="center" flexWrap="wrap" useFlexGap>
              <Chip size="small" variant="outlined" label={screen} onClick={onJump} />
              {a.componentIds.length === 0 ? (
                <Typography variant="caption" color="text.secondary">whole screen</Typography>
              ) : (
                a.componentIds.map((id) => <Chip key={id} size="small" label={labelOf(model, a.screenId, id)} />)
              )}
            </Stack>
            <Typography variant="body2">{a.request}</Typography>
          </Stack>
          <IconButton size="small" aria-label="Remove request" onClick={() => dispatch({ type: "REMOVE", annotationId: a.id })}>
            <X size={14} />
          </IconButton>
        </Stack>
      </CardContent>
    </Card>
  );
}

export function SendAll({ state, dispatch, busy, setBusy }: VariantProps & { busy: boolean; setBusy: (b: boolean) => void }) {
  const n = state.queue.length;
  return (
    <Button
      variant="contained"
      disabled={n === 0 || busy}
      onClick={() => {
        setBusy(true);
        window.setTimeout(() => {
          dispatch({ type: "SENT" });
          setBusy(false);
        }, 1200);
      }}
    >
      {busy ? "Sending…" : n === 0 ? "Send all" : `Send all (${n})`}
    </Button>
  );
}
