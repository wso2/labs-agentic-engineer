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
 * What a reviewer calls a component: the name it shows on screen. Selection
 * chips, the outline's label and queued-request cards read it; the request
 * itself carries the stable ID, never this — labels are display, not identity.
 */

import type {
  PrototypeButton,
  PrototypeField,
  PrototypeModelV1,
  PrototypeNode,
  PrototypeRow,
} from "@aep/prototype-model";

const EXCERPT = 32;

function excerpt(text: string): string {
  return text.length > EXCERPT ? `${text.slice(0, EXCERPT)}…` : text;
}

/** A row reads as its first column's value. */
function rowLabel(row: PrototypeRow): string {
  return Object.values(row.values)[0] ?? row.id;
}

function nodeLabel(n: PrototypeNode): string {
  switch (n.kind) {
    case "heading":
    case "text":
      return excerpt(n.text);
    case "badge":
    case "stat":
    case "button":
    case "link":
      return n.label;
    case "alert":
      return n.title ?? excerpt(n.text);
    case "empty-state":
    case "approval-panel":
    case "task-queue":
      return n.title;
    case "form":
    case "table":
    case "detail":
      return n.title ?? n.kind;
    default:
      return n.kind;
  }
}

/**
 * Every labelled ID on one screen — its nodes and what they hold (buttons,
 * fields, rows, tabs, steps, crumbs, timeline entries), its navigation and its
 * overlays.
 */
function screenLabels(model: PrototypeModelV1, screenId: string): Map<string, string> {
  const labels = new Map<string, string>();
  const buttons = (bs: PrototypeButton[] | undefined) => bs?.forEach((b) => labels.set(b.id, b.label));
  const fields = (fs: PrototypeField[]) => fs.forEach((f) => labels.set(f.id, f.label));
  const walk = (nodes: PrototypeNode[]): void => {
    for (const n of nodes) {
      labels.set(n.id, nodeLabel(n));
      switch (n.kind) {
        case "stack":
        case "grid":
          walk(n.content);
          break;
        case "split":
          walk(n.left);
          walk(n.right);
          break;
        case "tabs":
          n.tabs.forEach((t) => labels.set(t.id, t.label));
          n.tabs.forEach((t) => walk(t.content));
          break;
        case "stepper":
          n.steps.forEach((s) => labels.set(s.id, s.label));
          n.steps.forEach((s) => walk(s.content));
          break;
        case "breadcrumbs":
          n.items.forEach((i) => labels.set(i.id, i.label));
          break;
        case "heading":
          buttons(n.actions);
          break;
        case "form":
          fields(n.fields);
          buttons(n.actions);
          break;
        case "filters":
          fields(n.fields);
          break;
        case "approval-panel":
          buttons(n.actions);
          break;
        case "empty-state":
          if (n.action) buttons([n.action]);
          break;
        case "table":
        case "task-queue":
          n.rows.forEach((r) => labels.set(r.id, rowLabel(r)));
          break;
        case "timeline":
          n.entries.forEach((e) => labels.set(e.id, excerpt(e.text)));
          break;
        default:
          break;
      }
    }
  };
  const screen = model.screens.find((s) => s.id === screenId);
  if (!screen) return labels;
  const nav = model.navigation.find((n) => n.id === screen.navigationId);
  nav?.items.forEach((i) => labels.set(i.id, i.label));
  walk(screen.content);
  for (const o of screen.overlays ?? []) {
    labels.set(o.id, o.title);
    walk(o.content);
    if (o.kind === "dialog") buttons(o.actions);
  }
  return labels;
}

/** The on-screen name of a component on a screen; its ID when it has none there. */
export function labelOf(model: PrototypeModelV1, screenId: string, componentId: string): string {
  return screenLabels(model, screenId).get(componentId) ?? componentId;
}
