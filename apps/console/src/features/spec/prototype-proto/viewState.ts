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

// PROTOTYPE (throwaway, issue #813). The pure Preview/Annotate reducer the
// plan sketches. Decision-rich: carries over into features/prototype/model.

import type { ActionSpec } from "./model";

export interface Annotation {
  id: string;
  screenId: string;
  flowId: string | null;
  stateId: string;
  componentIds: string[];
  request: string;
}

export interface ViewState {
  mode: "preview" | "annotate";
  roleId: string;
  screenId: string;
  flowId: string | null;
  stateId: string;
  openOverlayId: string | null;
  activeTabIds: Record<string, string>;
  activeStepIds: Record<string, string>;
  selectedComponentIds: string[];
  queue: Annotation[];
}

export type ViewEvent =
  | { type: "ENTER_ANNOTATE" }
  | { type: "EXIT_ANNOTATE" }
  | { type: "CLEAR_SELECTION" }
  | { type: "TOGGLE_SELECTION"; componentId: string }
  | { type: "ACTIVATE"; componentId: string; action: ActionSpec }
  | { type: "NAVIGATE"; screenId: string }
  | { type: "SET_ROLE"; roleId: string }
  | { type: "SET_FLOW"; flowId: string | null; screenId: string }
  | { type: "SET_STATE"; stateId: string }
  | { type: "CLOSE_OVERLAY" }
  | { type: "QUEUE"; request: string }
  | { type: "REMOVE"; annotationId: string }
  | { type: "SENT" };

let seq = 0;

export function reduce(s: ViewState, e: ViewEvent): ViewState {
  switch (e.type) {
    case "ENTER_ANNOTATE":
      return { ...s, mode: "annotate", openOverlayId: s.openOverlayId, selectedComponentIds: [] };
    case "EXIT_ANNOTATE":
      return { ...s, mode: "preview", selectedComponentIds: [] };
    case "CLEAR_SELECTION":
      return { ...s, selectedComponentIds: [] };
    case "TOGGLE_SELECTION": {
      if (s.mode !== "annotate") return s;
      const has = s.selectedComponentIds.includes(e.componentId);
      return {
        ...s,
        selectedComponentIds: has
          ? s.selectedComponentIds.filter((id) => id !== e.componentId)
          : [...s.selectedComponentIds, e.componentId],
      };
    }
    case "ACTIVATE": {
      // The one rule that matters: Annotate selects, Preview acts.
      if (s.mode === "annotate") return reduce(s, { type: "TOGGLE_SELECTION", componentId: e.componentId });
      return applyAction(s, e.action);
    }
    case "NAVIGATE":
      return { ...s, screenId: e.screenId, openOverlayId: null, selectedComponentIds: [] };
    case "SET_ROLE":
      return { ...s, roleId: e.roleId, flowId: null, openOverlayId: null, selectedComponentIds: [] };
    case "SET_FLOW":
      return { ...s, flowId: e.flowId, screenId: e.screenId, openOverlayId: null, selectedComponentIds: [] };
    case "SET_STATE":
      return { ...s, stateId: e.stateId, selectedComponentIds: [] };
    case "CLOSE_OVERLAY":
      return { ...s, openOverlayId: null };
    case "QUEUE": {
      if (!e.request.trim()) return s;
      seq += 1;
      const a: Annotation = {
        id: `a${seq}`,
        screenId: s.screenId,
        flowId: s.flowId,
        stateId: s.stateId,
        componentIds: s.selectedComponentIds,
        request: e.request.trim(),
      };
      return { ...s, queue: [...s.queue, a], selectedComponentIds: [] };
    }
    case "REMOVE":
      return { ...s, queue: s.queue.filter((a) => a.id !== e.annotationId) };
    case "SENT":
      return { ...s, queue: [], selectedComponentIds: [] };
    default: {
      const never: never = e;
      return never;
    }
  }
}

function applyAction(s: ViewState, a: ActionSpec): ViewState {
  switch (a.kind) {
    case "navigate":
      return { ...s, screenId: a.screenId, openOverlayId: null };
    case "set-tab":
      return { ...s, activeTabIds: { ...s.activeTabIds, [a.tabsId]: a.tabId } };
    case "set-step":
      return { ...s, activeStepIds: { ...s.activeStepIds, [a.stepperId]: a.stepId } };
    case "select-row":
      return s;
    case "show-dialog":
      return { ...s, openOverlayId: a.dialogId };
    case "show-drawer":
      return { ...s, openOverlayId: a.drawerId };
    case "close-overlay":
      return { ...s, openOverlayId: null };
  }
}

export function initialState(roleId: string, screenId: string, stateId: string): ViewState {
  return {
    mode: "preview",
    roleId,
    screenId,
    flowId: null,
    stateId,
    openOverlayId: null,
    activeTabIds: {},
    activeStepIds: {},
    selectedComponentIds: [],
    queue: [],
  };
}
