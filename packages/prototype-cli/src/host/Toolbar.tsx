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

/** The review's controls: screen, flow, role and display-state pickers, Reset data and (in preview) the Preview/Annotate toggle. */

import { screensForRole, type PrototypeManifest, type PrototypeViewEvent, type PrototypeViewState } from "@wso2/prototype-kit/host";

export interface ToolbarProps {
  manifest: PrototypeManifest;
  view: PrototypeViewState;
  dispatch: (event: PrototypeViewEvent) => void;
  onReset: () => void;
  /** Whether Annotate is offered (not in an export). */
  annotate: boolean;
}

const NO_FLOW = "";

export function Toolbar({ manifest, view, dispatch, onReset, annotate }: ToolbarProps) {
  return (
    <header className="ph-toolbar">
      <strong className="ph-name">{manifest.name}</strong>
      <label>
        Screen
        <select value={view.screenId} onChange={(e) => dispatch({ type: "NAVIGATE", screenId: e.target.value })}>
          {screensForRole(manifest, view.roleId).map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </label>
      <label>
        Flow
        <select value={view.flowId ?? NO_FLOW} onChange={(e) => dispatch({ type: "SET_FLOW", flowId: e.target.value === NO_FLOW ? null : e.target.value })}>
          <option value={NO_FLOW}>No flow</option>
          {manifest.flows.map((f) => (
            <option key={f.id} value={f.id}>
              {f.name}
            </option>
          ))}
        </select>
      </label>
      <label>
        Role
        <select value={view.roleId} onChange={(e) => dispatch({ type: "SET_ROLE", roleId: e.target.value })}>
          {manifest.roles.map((r) => (
            <option key={r.id} value={r.id}>
              {r.name}
            </option>
          ))}
        </select>
      </label>
      <label>
        State
        <select value={view.stateId} onChange={(e) => dispatch({ type: "SET_STATE", stateId: e.target.value })}>
          {manifest.states.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </label>
      <button type="button" onClick={onReset}>
        Reset data
      </button>
      {annotate && (
        <span className="ph-modes" role="group" aria-label="Mode">
          <button type="button" aria-pressed={view.mode === "preview"} onClick={() => dispatch({ type: "EXIT_ANNOTATE" })}>
            Preview
          </button>
          <button type="button" aria-pressed={view.mode === "annotate"} onClick={() => dispatch({ type: "ENTER_ANNOTATE" })}>
            Annotate
          </button>
        </span>
      )}
    </header>
  );
}
