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

// PROTOTYPE (throwaway, issue #813). The v1 prototype model as the plan
// sketches it. This is the decision-rich part that carries over into
// @aep/prototype-model; everything else in this folder is disposable.

export type ActionSpec =
  | { kind: "navigate"; screenId: string }
  | { kind: "set-tab"; tabsId: string; tabId: string }
  | { kind: "set-step"; stepperId: string; stepId: string }
  | { kind: "select-row"; tableId: string; rowId: string }
  | { kind: "show-dialog"; dialogId: string }
  | { kind: "show-drawer"; drawerId: string }
  | { kind: "close-overlay" };

interface Base {
  id: string;
  /** Only render in these display states (default: all). */
  showIn?: string[];
}

export type NodeSpec =
  | (Base & { kind: "stack"; direction?: "row" | "column"; content: NodeSpec[] })
  | (Base & { kind: "grid"; columns: number; content: NodeSpec[] })
  | (Base & { kind: "split"; left: NodeSpec[]; right: NodeSpec[]; ratio?: number })
  | (Base & { kind: "breadcrumbs"; items: { id: string; label: string; action?: ActionSpec }[] })
  | (Base & { kind: "tabs"; tabs: { id: string; label: string; content: NodeSpec[] }[] })
  | (Base & { kind: "stepper"; steps: { id: string; label: string; content: NodeSpec[] }[] })
  | (Base & { kind: "text"; text: string })
  | (Base & { kind: "heading"; text: string; actions?: ButtonSpec[] })
  | (Base & { kind: "badge"; label: string; tone?: Tone })
  | (Base & { kind: "stat"; label: string; value: string })
  | (Base & { kind: "alert"; tone: Tone; title?: string; text: string })
  | (Base & { kind: "empty-state"; title: string; text: string; action?: ButtonSpec })
  | (Base & ButtonSpec & { kind: "button" })
  | (Base & { kind: "link"; label: string; action: ActionSpec })
  | (Base & { kind: "form"; title?: string; fields: FieldSpec[]; actions: ButtonSpec[] })
  | (Base & { kind: "validation-summary"; issues: string[] })
  | (Base & { kind: "table"; title?: string; columns: string[]; rows: RowSpec[]; onRow?: ActionSpec })
  | (Base & { kind: "filters"; fields: FieldSpec[] })
  | (Base & { kind: "detail"; title?: string; fields: { label: string; value: string }[] })
  | (Base & { kind: "timeline"; entries: { id: string; when: string; who: string; text: string }[] })
  | (Base & { kind: "approval-panel"; title: string; summary: string; actions: ButtonSpec[] })
  | (Base & { kind: "task-queue"; title: string; columns: string[]; rows: RowSpec[]; onRow?: ActionSpec });

export type Tone = "success" | "error" | "warning" | "info" | "default";

export interface ButtonSpec {
  id: string;
  label: string;
  primary?: boolean;
  danger?: boolean;
  action: ActionSpec;
}

export interface FieldSpec {
  id: string;
  label: string;
  type?: "text" | "select" | "date" | "textarea" | "switch";
  value?: string;
  options?: string[];
  error?: string;
  /** Only show the error in these display states. */
  errorIn?: string[];
}

export interface RowSpec {
  id: string;
  values: Record<string, string>;
  tone?: Tone;
}

export type OverlaySpec =
  | { kind: "dialog"; id: string; title: string; content: NodeSpec[]; actions: ButtonSpec[] }
  | { kind: "drawer"; id: string; title: string; content: NodeSpec[] };

export interface NavigationSpec {
  id: string;
  kind: "side-nav";
  items: { id: string; label: string; action: ActionSpec; roleIds?: string[] }[];
}

export interface ScreenSpec {
  id: string;
  name: string;
  roleIds: string[];
  navigationId?: string;
  content: NodeSpec[];
  overlays?: OverlaySpec[];
}

export interface PrototypeModelV1 {
  schemaVersion: 1;
  component: string;
  name: string;
  defaultScreenId: string;
  roles: { id: string; name: string }[];
  states: { id: string; name: string }[];
  flows: { id: string; name: string; roleId: string; screenIds: string[] }[];
  screens: ScreenSpec[];
  navigation: NavigationSpec[];
}

/** Everything a reviewer can point at in Annotate: nodes, buttons, fields, rows, nav items. */
export function labelOf(model: PrototypeModelV1, screenId: string, componentId: string): string {
  const screen = model.screens.find((s) => s.id === screenId);
  const found = screen ? findLabel(screen.content, componentId) : null;
  if (found) return found;
  for (const nav of model.navigation) {
    const item = nav.items.find((i) => i.id === componentId);
    if (item) return item.label;
  }
  for (const o of screen?.overlays ?? []) {
    if (o.id === componentId) return o.title;
    const inner = findLabel(o.content, componentId);
    if (inner) return inner;
  }
  return componentId;
}

function findLabel(nodes: NodeSpec[], id: string): string | null {
  for (const n of nodes) {
    if (n.id === id) return nodeLabel(n);
    const inner = childrenOf(n);
    for (const c of inner) {
      const hit = findLabel(c, id);
      if (hit) return hit;
    }
    const leaf = leafLabel(n, id);
    if (leaf) return leaf;
  }
  return null;
}

function nodeLabel(n: NodeSpec): string {
  switch (n.kind) {
    case "heading":
    case "text":
      return n.text.length > 32 ? `${n.text.slice(0, 32)}…` : n.text;
    case "badge":
    case "stat":
    case "button":
    case "link":
      return n.label;
    case "alert":
      return n.title ?? n.text;
    case "empty-state":
      return n.title;
    case "form":
    case "table":
    case "detail":
      return n.title ?? n.kind;
    case "task-queue":
    case "approval-panel":
      return n.title;
    default:
      return n.kind;
  }
}

function leafLabel(n: NodeSpec, id: string): string | null {
  if ((n.kind === "form" || n.kind === "filters") && n.fields.some((f) => f.id === id))
    return n.fields.find((f) => f.id === id)!.label;
  if ((n.kind === "form" || n.kind === "approval-panel" || n.kind === "heading") && n.actions?.some((a) => a.id === id))
    return n.actions.find((a) => a.id === id)!.label;
  if ((n.kind === "table" || n.kind === "task-queue") && n.rows.some((r) => r.id === id)) {
    const row = n.rows.find((r) => r.id === id)!;
    return Object.values(row.values)[0] ?? row.id;
  }
  if (n.kind === "tabs") {
    const t = n.tabs.find((t) => t.id === id);
    if (t) return t.label;
  }
  if (n.kind === "stepper") {
    const s = n.steps.find((s) => s.id === id);
    if (s) return s.label;
  }
  if (n.kind === "empty-state" && n.action?.id === id) return n.action.label;
  return null;
}

function childrenOf(n: NodeSpec): NodeSpec[][] {
  switch (n.kind) {
    case "stack":
    case "grid":
      return [n.content];
    case "split":
      return [n.left, n.right];
    case "tabs":
      return n.tabs.map((t) => t.content);
    case "stepper":
      return n.steps.map((s) => s.content);
    default:
      return [];
  }
}
