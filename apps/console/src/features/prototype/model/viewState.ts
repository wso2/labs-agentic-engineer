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
 * The prototype review's view state: one pure reducer owns everything a
 * reviewer can change on the page — mode, role, screen, flow, display state,
 * the open overlay, the active tab, step and row per node, and the Annotate
 * selection.
 *
 * The rule the whole page leans on is `ACTIVATE`: Preview acts, Annotate only
 * selects. A click anywhere in the rendered application is one `ACTIVATE`, so
 * nothing in the registry decides what a click means.
 *
 * Screen, flow, display-state, role, model-replaced and exit-Annotate events
 * clear the selection, so a request never silently refers to something that
 * is no longer on screen.
 *
 * The reducer takes the model as its first argument: every event is checked
 * against it, so the state stays reachable (a screen the role can see, a flow
 * that exists) whatever the event says.
 */

import type { PrototypeAction, PrototypeModelV1 } from "@aep/prototype-model";
import { screensForRole } from "./visibility";

export type PrototypeMode = "preview" | "annotate";

export const PROTOTYPE_MODES: readonly PrototypeMode[] = ["preview", "annotate"];

export interface PrototypeViewState {
  mode: PrototypeMode;
  roleId: string;
  screenId: string;
  flowId: string | null;
  stateId: string;
  openOverlayId: string | null;
  /** Active tab per tabs node. A tabs node without an entry shows its first tab. */
  activeTabIds: Record<string, string>;
  /** Active step per stepper node. A stepper without an entry shows its first step. */
  activeStepIds: Record<string, string>;
  /** Highlighted row per table or task queue (`select-row`). */
  selectedRowIds: Record<string, string>;
  /** Selected component IDs in the order they were selected (Annotate only). */
  selectedComponentIds: string[];
}

export type PrototypeViewEvent =
  | { type: "ENTER_ANNOTATE" }
  | { type: "EXIT_ANNOTATE" }
  | { type: "CLEAR_SELECTION" }
  | { type: "TOGGLE_SELECTION"; componentId: string }
  | { type: "ACTIVATE"; componentId: string; action: PrototypeAction }
  | { type: "NAVIGATE"; screenId: string }
  | { type: "SET_ROLE"; roleId: string }
  | { type: "SET_FLOW"; flowId: string | null }
  | { type: "SET_STATE"; stateId: string }
  | { type: "CLOSE_OVERLAY" }
  | { type: "MODEL_REPLACED"; model: PrototypeModelV1 };

/** What the URL carries of the view: the part of it a shared link restores. */
export interface PrototypeViewRequest {
  screen?: string;
  flow?: string;
  state?: string;
  mode?: PrototypeMode;
}

export function reducePrototypeView(
  model: PrototypeModelV1,
  s: PrototypeViewState,
  e: PrototypeViewEvent,
): PrototypeViewState {
  switch (e.type) {
    case "ENTER_ANNOTATE":
      return { ...s, mode: "annotate", selectedComponentIds: [] };
    case "EXIT_ANNOTATE":
      return { ...s, mode: "preview", selectedComponentIds: [] };
    case "CLEAR_SELECTION":
      return { ...s, selectedComponentIds: [] };
    case "TOGGLE_SELECTION":
      return toggleSelection(s, e.componentId);
    case "ACTIVATE":
      // Annotate selects, Preview acts.
      if (s.mode === "annotate") return toggleSelection(s, e.componentId);
      return applyAction(model, s, e.action);
    case "NAVIGATE":
      return navigate(model, s, e.screenId);
    case "SET_ROLE": {
      if (!model.roles.some((r) => r.id === e.roleId)) return s;
      const reachable = screensForRole(model, e.roleId);
      const keepScreen = reachable.some((x) => x.id === s.screenId);
      const flow = model.flows.find((f) => f.id === s.flowId);
      return {
        ...s,
        roleId: e.roleId,
        flowId: flow?.roleId === e.roleId ? s.flowId : null,
        screenId: keepScreen ? s.screenId : (reachable[0]?.id ?? s.screenId),
        openOverlayId: keepScreen ? s.openOverlayId : null,
        selectedComponentIds: [],
      };
    }
    case "SET_FLOW": {
      if (e.flowId === null) return { ...s, flowId: null, selectedComponentIds: [] };
      const flow = model.flows.find((f) => f.id === e.flowId);
      if (!flow) return s;
      return {
        ...s,
        flowId: flow.id,
        roleId: flow.roleId,
        screenId: flow.screenIds[0] ?? s.screenId,
        openOverlayId: null,
        selectedComponentIds: [],
      };
    }
    case "SET_STATE":
      if (!model.states.some((x) => x.id === e.stateId)) return s;
      return { ...s, stateId: e.stateId, selectedComponentIds: [] };
    case "CLOSE_OVERLAY":
      return { ...s, openOverlayId: null };
    case "MODEL_REPLACED": {
      const next = initialPrototypeView(e.model, viewRequestOf(s));
      const roleKept =
        e.model.roles.some((r) => r.id === s.roleId) &&
        screensForRole(e.model, s.roleId).some((x) => x.id === next.screenId);
      const overlayKept = e.model.screens
        .find((x) => x.id === next.screenId)
        ?.overlays?.some((o) => o.id === s.openOverlayId);
      return {
        ...next,
        roleId: roleKept && next.flowId === null ? s.roleId : next.roleId,
        openOverlayId: overlayKept ? s.openOverlayId : null,
        activeTabIds: s.activeTabIds,
        activeStepIds: s.activeStepIds,
        selectedRowIds: s.selectedRowIds,
      };
    }
    default: {
      const unhandled: never = e;
      return unhandled;
    }
  }
}

function toggleSelection(s: PrototypeViewState, componentId: string): PrototypeViewState {
  if (s.mode !== "annotate") return s;
  const selected = s.selectedComponentIds.includes(componentId);
  return {
    ...s,
    selectedComponentIds: selected
      ? s.selectedComponentIds.filter((id) => id !== componentId)
      : [...s.selectedComponentIds, componentId],
  };
}

function navigate(model: PrototypeModelV1, s: PrototypeViewState, screenId: string): PrototypeViewState {
  if (!model.screens.some((x) => x.id === screenId)) return s;
  return { ...s, screenId, openOverlayId: null, selectedComponentIds: [] };
}

function applyAction(model: PrototypeModelV1, s: PrototypeViewState, a: PrototypeAction): PrototypeViewState {
  switch (a.kind) {
    case "navigate":
      return navigate(model, s, a.screenId);
    case "set-tab":
      return { ...s, activeTabIds: { ...s.activeTabIds, [a.tabsId]: a.tabId } };
    case "set-step":
      return { ...s, activeStepIds: { ...s.activeStepIds, [a.stepperId]: a.stepId } };
    case "select-row":
      return { ...s, selectedRowIds: { ...s.selectedRowIds, [a.tableId]: a.rowId } };
    case "show-dialog":
      return { ...s, openOverlayId: a.dialogId };
    case "show-drawer":
      return { ...s, openOverlayId: a.drawerId };
    case "close-overlay":
      return { ...s, openOverlayId: null };
    default: {
      const unhandled: never = a;
      return unhandled;
    }
  }
}

/**
 * The view a request (a shared link) opens on, repaired against the model: an
 * unknown flow is dropped, an unknown screen falls back to the flow's first
 * screen or the model's default, an unknown display state to the first one.
 * The role is the flow's, else the first role (in model order) that can reach
 * the screen.
 */
export function initialPrototypeView(model: PrototypeModelV1, request: PrototypeViewRequest): PrototypeViewState {
  const flow = model.flows.find((f) => f.id === request.flow);
  const requested = model.screens.find((x) => x.id === request.screen);
  const screenId =
    requested && (!flow || requested.roleIds.includes(flow.roleId))
      ? requested.id
      : (flow?.screenIds[0] ?? model.defaultScreenId);
  const screen = model.screens.find((x) => x.id === screenId);
  const roleId =
    flow?.roleId ??
    model.roles.find((r) => screen?.roleIds.includes(r.id))?.id ??
    model.roles[0]?.id ??
    "";
  const stateId = model.states.some((x) => x.id === request.state)
    ? (request.state as string)
    : (model.states[0]?.id ?? "");
  return {
    mode: request.mode ?? "preview",
    roleId,
    screenId,
    flowId: flow?.id ?? null,
    stateId,
    openOverlayId: null,
    activeTabIds: {},
    activeStepIds: {},
    selectedRowIds: {},
    selectedComponentIds: [],
  };
}

/** The part of the view a URL carries. Preview is the default mode and is left out. */
export function viewRequestOf(s: PrototypeViewState): PrototypeViewRequest {
  return {
    screen: s.screenId,
    state: s.stateId,
    ...(s.flowId !== null ? { flow: s.flowId } : {}),
    ...(s.mode === "annotate" ? { mode: s.mode } : {}),
  };
}
