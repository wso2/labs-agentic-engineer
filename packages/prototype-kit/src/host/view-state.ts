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
 * The review's view state: one pure reducer owns everything the host's
 * pickers and Annotate control — mode, role, screen, flow, display state and
 * the selection. What happens inside the running app (an open dialog, a
 * typed field, changed mock data) is the app's own state in its frame.
 *
 * The rule the review leans on: Preview acts, Annotate only selects. The
 * frame navigates only in Preview and toggles only in Annotate, and the
 * reducer refuses a toggle while previewing besides.
 *
 * Screen, flow, state, role, manifest-replaced and mode changes clear the
 * selection, so a request never refers to something no longer on screen.
 * Every event is checked against the manifest (a navigation also against the
 * current role's screens), so the state stays reachable.
 */

import { screensForRole } from "../manifest/screens.js";
import type { PrototypeManifest } from "../manifest/types.js";
import type { FrameScreenPin, FrameView } from "./bridge.js";

export type PrototypeMode = "preview" | "annotate";

/** The longest element id a selection takes. */
const MAX_ELEMENT_KEY = 200;

export interface PrototypeViewState {
  mode: PrototypeMode;
  roleId: string;
  screenId: string;
  flowId: string | null;
  stateId: string;
  /** Selected element ids in the order they were selected (Annotate only). */
  selectedKeys: string[];
}

export type PrototypeViewEvent =
  | { type: "ENTER_ANNOTATE" }
  | { type: "EXIT_ANNOTATE" }
  | { type: "CLEAR_SELECTION" }
  /** A Shift-click in Annotate: add the element to the selection, or take it out. */
  | { type: "TOGGLE_SELECTION"; elementKey: string }
  /** A plain click in Annotate: the element alone is selected (a new comment on it). */
  | { type: "SELECT_ONLY"; elementKey: string }
  /** The host selects these elements (to reopen a comment it holds on them, a draft): Annotate, with them selected. */
  | { type: "SELECT_ELEMENTS"; elementKeys: string[] }
  | { type: "NAVIGATE"; screenId: string }
  | { type: "SET_ROLE"; roleId: string }
  | { type: "SET_FLOW"; flowId: string | null }
  | { type: "SET_STATE"; stateId: string }
  | { type: "MANIFEST_REPLACED"; manifest: PrototypeManifest };

/** A view to open on (e.g. carried over from a previous manifest). Every field is repaired against the manifest. */
export interface PrototypeViewRequest {
  screen?: string | undefined;
  flow?: string | undefined;
  state?: string | undefined;
  mode?: PrototypeMode | undefined;
  role?: string | undefined;
}

export function reducePrototypeView(manifest: PrototypeManifest, s: PrototypeViewState, e: PrototypeViewEvent): PrototypeViewState {
  switch (e.type) {
    case "ENTER_ANNOTATE":
      return { ...s, mode: "annotate", selectedKeys: [] };
    case "EXIT_ANNOTATE":
      return { ...s, mode: "preview", selectedKeys: [] };
    case "CLEAR_SELECTION":
      return { ...s, selectedKeys: [] };
    case "TOGGLE_SELECTION":
      return toggleSelection(s, e.elementKey);
    case "SELECT_ONLY":
      return selects(s, e.elementKey) ? { ...s, selectedKeys: [e.elementKey] } : s;
    case "SELECT_ELEMENTS": {
      const annotating: PrototypeViewState = { ...s, mode: "annotate" };
      const keys = [...new Set(e.elementKeys)];
      return keys.length > 0 && keys.every((k) => selects(annotating, k)) ? { ...annotating, selectedKeys: keys } : s;
    }
    case "NAVIGATE":
      if (e.screenId === s.screenId || !screensForRole(manifest, s.roleId).some((x) => x.id === e.screenId)) return s;
      return { ...s, screenId: e.screenId, selectedKeys: [] };
    case "SET_ROLE": {
      if (!manifest.roles.some((r) => r.id === e.roleId)) return s;
      const reachable = screensForRole(manifest, e.roleId);
      const keepScreen = reachable.some((x) => x.id === s.screenId);
      const flow = manifest.flows.find((f) => f.id === s.flowId);
      return {
        ...s,
        roleId: e.roleId,
        flowId: flow?.roleId === e.roleId ? s.flowId : null,
        screenId: keepScreen ? s.screenId : (reachable[0]?.id ?? s.screenId),
        selectedKeys: [],
      };
    }
    case "SET_FLOW": {
      if (e.flowId === null) return { ...s, flowId: null, selectedKeys: [] };
      const flow = manifest.flows.find((f) => f.id === e.flowId);
      if (!flow) return s;
      return { ...s, flowId: flow.id, roleId: flow.roleId, screenId: flow.screenIds[0] ?? s.screenId, selectedKeys: [] };
    }
    case "SET_STATE":
      if (!manifest.states.some((x) => x.id === e.stateId)) return s;
      return { ...s, stateId: e.stateId, selectedKeys: [] };
    case "MANIFEST_REPLACED": {
      const next = initialPrototypeView(e.manifest, requestOf(s));
      const roleKept = e.manifest.roles.some((r) => r.id === s.roleId) && screensForRole(e.manifest, s.roleId).some((x) => x.id === next.screenId);
      return { ...next, roleId: roleKept && next.flowId === null ? s.roleId : next.roleId };
    }
    default: {
      const unhandled: never = e;
      return unhandled;
    }
  }
}

/** Whether a click the frame reported on `key` selects: only while annotating, and only an id of sane length. */
function selects(s: PrototypeViewState, key: string): boolean {
  return s.mode === "annotate" && key.trim() !== "" && key.length <= MAX_ELEMENT_KEY;
}

/** Select or deselect an element the frame reported a click on. */
function toggleSelection(s: PrototypeViewState, key: string): PrototypeViewState {
  if (!selects(s, key)) return s;
  const selected = s.selectedKeys.includes(key);
  return { ...s, selectedKeys: selected ? s.selectedKeys.filter((k) => k !== key) : [...s.selectedKeys, key] };
}

/**
 * The view a request opens on, repaired against the manifest: an unknown flow
 * is dropped, an unknown screen falls back to the flow's first screen or the
 * entry screen, an unknown display state to the first one. The role is the
 * flow's; else the requested role when it reaches some screen; else the
 * first role that reaches the screen.
 */
export function initialPrototypeView(manifest: PrototypeManifest, request: PrototypeViewRequest = {}): PrototypeViewState {
  const flow = manifest.flows.find((f) => f.id === request.flow);
  const requested = manifest.screens.find((x) => x.id === request.screen);
  const role = flow ? undefined : manifest.roles.find((r) => r.id === request.role);
  const roleScreens = role ? screensForRole(manifest, role.id) : [];
  const roleScreen = roleScreens.find((x) => x.id === requested?.id) ?? roleScreens.find((x) => x.id === manifest.entryScreen) ?? roleScreens[0];
  const screenId =
    roleScreen?.id ?? (requested && (!flow || requested.roleIds.includes(flow.roleId)) ? requested.id : (flow?.screenIds[0] ?? manifest.entryScreen));
  const screen = manifest.screens.find((x) => x.id === screenId);
  const roleId = flow?.roleId ?? (roleScreen ? role?.id : undefined) ?? manifest.roles.find((r) => screen?.roleIds.includes(r.id))?.id ?? manifest.roles[0]?.id ?? "";
  const stateId = manifest.states.some((x) => x.id === request.state) ? (request.state as string) : (manifest.states[0]?.id ?? "");
  return { mode: request.mode ?? "preview", roleId, screenId, flowId: flow?.id ?? null, stateId, selectedKeys: [] };
}

function requestOf(s: PrototypeViewState): PrototypeViewRequest {
  return { screen: s.screenId, role: s.roleId, state: s.stateId, flow: s.flowId ?? undefined, mode: s.mode };
}

/** The view as the frame draws it, with the current screen's comment pins, the elements holding a draft and its whole-screen comments' pins. */
export function frameViewOf(s: PrototypeViewState, pins: Record<string, number[]> = {}, drafts: string[] = [], screenPins: FrameScreenPin[] = []): FrameView {
  return {
    mode: s.mode,
    roleId: s.roleId,
    stateId: s.stateId,
    screenId: s.screenId,
    selectedKeys: s.selectedKeys,
    pins,
    ...(drafts.length > 0 ? { drafts } : {}),
    ...(screenPins.length > 0 ? { screenPins } : {}),
  };
}
