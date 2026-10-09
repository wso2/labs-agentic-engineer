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

import type { ChangeEvent } from "react";
import { Box, IconButton, NativeSelect, Tooltip } from "@wso2/oxygen-ui";
import { RotateCcw } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeManifest, PrototypeViewEvent, PrototypeViewState } from "@wso2/prototype-kit/host";
import { DOCK_NARROW, DockGroup } from "./ReviewDock";

/**
 * A compact select: its name inline before the value (dropped on a narrow
 * dock, where the value alone stays), borderless until hovered.
 */
function Select({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly { id: string; name: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <Box
      component="label"
      sx={{
        display: "inline-flex",
        alignItems: "center",
        gap: 0.75,
        height: 32,
        pl: 1.25,
        borderRadius: 2,
        cursor: "pointer",
        "&:hover, &:focus-within": { bgcolor: "action.hover" },
      }}
    >
      <Box component="span" aria-hidden sx={{ color: "text.disabled", fontSize: "0.8125rem", [DOCK_NARROW]: { display: "none" } }}>
        {label}
      </Box>
      <NativeSelect
        disableUnderline
        value={value}
        onChange={(e: ChangeEvent<HTMLSelectElement>) => onChange(e.target.value)}
        inputProps={{ "aria-label": label }}
        sx={{ fontSize: "0.8125rem", fontWeight: 500, "& select": { py: 0.5, maxWidth: 160, textOverflow: "ellipsis" } }}
      >
        {options.map((o) => (
          <option key={o.id} value={o.id}>
            {o.name}
          </option>
        ))}
      </NativeSelect>
    </Box>
  );
}

/**
 * The dock's view group: the role and display-state selects and Reset data
 * (the prototype's mock data back to its seed). There is no screen or flow
 * picker: the reviewer moves through the prototype by using it. Every change
 * goes through the kit's view reducer, which keeps the view reachable for the role.
 */
export function ViewControls({
  manifest,
  view,
  dispatch,
  onReset,
}: {
  manifest: PrototypeManifest;
  view: PrototypeViewState;
  dispatch: (event: PrototypeViewEvent) => void;
  onReset: () => void;
}) {
  return (
    <DockGroup label="View">
      <Select label="Role" value={view.roleId} options={manifest.roles} onChange={(roleId) => dispatch({ type: "SET_ROLE", roleId })} />
      <Select label="State" value={view.stateId} options={manifest.states} onChange={(stateId) => dispatch({ type: "SET_STATE", stateId })} />
      <Tooltip title="Reset data">
        <IconButton size="small" aria-label="Reset data" onClick={onReset}>
          <RotateCcw size={16} />
        </IconButton>
      </Tooltip>
    </DockGroup>
  );
}
