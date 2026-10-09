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
 * The dock's view group: the role and display-state selects and Reset data
 * (the mock data back to its seed). There is no screen or flow picker: the
 * reviewer moves through the prototype by using it. Each select names itself
 * inline before its value; a narrow dock drops that name and keeps the value.
 */

import type { PrototypeManifest, PrototypeViewEvent, PrototypeViewState } from "@wso2/prototype-kit/host";
import { DockGroup } from "./Dock.js";
import { Icon, RESET } from "./icons.js";

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
    <label className="ph-select">
      <span className="ph-select-label" aria-hidden>
        {label}
      </span>
      <select aria-label={label} value={value} onChange={(e) => onChange(e.target.value)}>
        {options.map((o) => (
          <option key={o.id} value={o.id}>
            {o.name}
          </option>
        ))}
      </select>
    </label>
  );
}

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
      <button type="button" className="ph-icon-button" aria-label="Reset data" title="Reset data" onClick={onReset}>
        <Icon d={RESET} />
      </button>
    </DockGroup>
  );
}
