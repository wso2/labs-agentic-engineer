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
 * The review bar: the one strip of console UI the prototype page keeps.
 * Back to Spec, the application's name, the read-only tag, the role, flow,
 * screen and display-state selectors, and the Preview/Annotate toggle.
 */

import type { Dispatch, ReactElement, ReactNode } from "react";
import {
  Box,
  Chip,
  Divider,
  IconButton,
  MenuItem,
  PageTitle,
  Paper,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronLeft, ChevronRight } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeModelV1 } from "@aep/prototype-model";
import { flowsForRole, screensForRole } from "../model/visibility";
import type { PrototypeViewEvent, PrototypeViewState } from "../model/viewState";
import { ModeToggle } from "./ModeToggle";

export interface ReviewBarProps {
  model: PrototypeModelV1;
  view: PrototypeViewState;
  dispatch: Dispatch<PrototypeViewEvent>;
  /** A router link element back to the Spec view, e.g. `<Link to="/projects/$projectName/spec" />`. */
  backLink: ReactElement<{ children?: ReactNode }>;
  /** Hold the mode where it is — an agent turn is running on the project (#817). */
  modeLocked?: boolean;
}

const NO_FLOW = "";

function Selector({
  label,
  value,
  onChange,
  width,
  children,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  width: number;
  children: ReactNode;
}) {
  return (
    <TextField
      select
      size="small"
      label={label}
      value={value} onChange={(e) => onChange(e.target.value)}
      slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
      sx={{ width }}
    >
      {children}
    </TextField>
  );
}

export function ReviewBar({ model, view, dispatch, backLink, modeLocked = false }: ReviewBarProps) {
  const flows = flowsForRole(model, view.roleId);
  const flow = model.flows.find((f) => f.id === view.flowId);
  const flowStep = flow ? flow.screenIds.indexOf(view.screenId) : -1;
  // Inside a flow the screen list is the flow's path, in order; otherwise it
  // is every screen the role can reach.
  const screens = flow
    ? flow.screenIds.flatMap((id) => model.screens.filter((s) => s.id === id))
    : screensForRole(model, view.roleId);
  const currentScreen = model.screens.find((s) => s.id === view.screenId);
  const screenOptions =
    currentScreen && !screens.includes(currentScreen) ? [...screens, currentScreen] : screens;

  const walk = (delta: number) => {
    const target = flow?.screenIds[flowStep + delta];
    if (target) dispatch({ type: "NAVIGATE", screenId: target });
  };

  return (
    <Paper square elevation={0} component="header" sx={{ px: 2, py: 1, borderBottom: 1, borderColor: "divider" }}>
      <Stack direction="row" alignItems="center" spacing={1} flexWrap="wrap" useFlexGap>
        <Box sx={{ width: "fit-content" }}>
          <PageTitle.BackButton component={backLink}>Back to Spec</PageTitle.BackButton>
        </Box>
        <Divider orientation="vertical" flexItem />
        <Typography variant="subtitle1" component="h1">
          {model.name}
        </Typography>
        <Chip size="small" variant="outlined" label="Read-only" />
        <Box sx={{ flex: 1 }} />
        <Selector label="Role" value={view.roleId} width={130} onChange={(roleId) => dispatch({ type: "SET_ROLE", roleId })}>
          {model.roles.map((r) => (
            <MenuItem key={r.id} value={r.id}>
              {r.name}
            </MenuItem>
          ))}
        </Selector>
        <Selector
          label="Flow"
          value={view.flowId ?? NO_FLOW}
          width={170}
          onChange={(id) => dispatch({ type: "SET_FLOW", flowId: id === NO_FLOW ? null : id })}
        >
          <MenuItem value={NO_FLOW}>Free navigation</MenuItem>
          {flows.map((f) => (
            <MenuItem key={f.id} value={f.id}>
              {f.name}
            </MenuItem>
          ))}
        </Selector>
        <Stack direction="row" alignItems="center" spacing={0.5}>
          {flow && (
            <Tooltip title="Previous screen in flow">
              <span>
                <IconButton size="small" aria-label="Previous screen in flow" disabled={flowStep <= 0} onClick={() => walk(-1)}>
                  <ChevronLeft size={18} />
                </IconButton>
              </span>
            </Tooltip>
          )}
          <Selector label="Screen" value={view.screenId} width={160} onChange={(screenId) => dispatch({ type: "NAVIGATE", screenId })}>
            {screenOptions.map((s) => {
              const step = flow ? flow.screenIds.indexOf(s.id) : -1;
              return (
                <MenuItem key={s.id} value={s.id}>
                  {step >= 0 ? `${step + 1}. ${s.name}` : s.name}
                </MenuItem>
              );
            })}
          </Selector>
          {flow && (
            <Tooltip title="Next screen in flow">
              <span>
                <IconButton
                  size="small"
                  aria-label="Next screen in flow"
                  disabled={flowStep < 0 || flowStep >= flow.screenIds.length - 1}
                  onClick={() => walk(1)}
                >
                  <ChevronRight size={18} />
                </IconButton>
              </span>
            </Tooltip>
          )}
        </Stack>
        <Selector
          label="Display state"
          value={view.stateId}
          width={145}
          onChange={(stateId) => dispatch({ type: "SET_STATE", stateId })}
        >
          {model.states.map((s) => (
            <MenuItem key={s.id} value={s.id}>
              {s.name}
            </MenuItem>
          ))}
        </Selector>
        <Divider orientation="vertical" flexItem />
        <ModeToggle
          mode={view.mode}
          disabled={modeLocked}
          onChange={(mode) => dispatch({ type: mode === "annotate" ? "ENTER_ANNOTATE" : "EXIT_ANNOTATE" })}
        />
      </Stack>
    </Paper>
  );
}
