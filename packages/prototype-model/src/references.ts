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
 * The reference half of the validator: the rules a standalone JSON Schema
 * cannot express. Run only on a document that already has the v1 shape.
 *
 * Two passes, in this order:
 *
 *  1. **Index.** Every `id` in the document goes into ONE namespace (roles,
 *     states, flows, screens, nodes, buttons, fields, rows, tabs, steps,
 *     breadcrumbs, timeline entries, overlays, navigation, navigation items).
 *     A repeat is `DUPLICATE_ID`, and any duplicate ends validation there: a
 *     reference into an ambiguous namespace has no single target to check.
 *  2. **Resolve.** Every reference must name an entry of the right kind —
 *     and, for targets that live on a screen (tabs, steppers, tables,
 *     dialogs, drawers), an entry on the screen the action runs on. A
 *     navigation item runs on every screen that shows its navigation.
 *
 * Both passes walk the document in its serialized key order, so issues come
 * out in document order. The Go twin
 * (services/aep-api/internal/platform/prototypespec/references.go) walks the
 * same order and reports the same codes at the same paths.
 */

import type { PrototypeValidationIssue } from "./issues.js";
import type {
  PrototypeAction,
  PrototypeButton,
  PrototypeField,
  PrototypeModelV1,
  PrototypeNode,
} from "./model.js";

/** What an id names. Node kinds are used as-is; the rest are named here. */
type EntryKind =
  | PrototypeNode["kind"]
  | "role"
  | "state"
  | "flow"
  | "screen"
  | "navigation"
  | "navigation-item"
  | "field"
  | "row"
  | "tab"
  | "step"
  | "breadcrumb"
  | "timeline-entry"
  | "dialog"
  | "drawer";

interface Entry {
  kind: EntryKind;
  path: string;
  /** The screen an on-screen entry belongs to. */
  screenId?: string;
  /** The tabs, stepper, or table a tab, step, or row belongs to. */
  parentId?: string;
}

type Index = Map<string, Entry>;

/**
 * The reference findings for a structurally valid model; empty when every id
 * is unique and every reference resolves. `component`, when given, is the
 * directory the file is written to.
 */
export function prototypeReferenceIssues(
  model: PrototypeModelV1,
  component?: string,
): PrototypeValidationIssue[] {
  const { index, duplicates } = buildIndex(model);
  if (duplicates.length > 0) return duplicates;
  return resolve(model, index, component);
}

// -- Pass 1: the global id index ---------------------------------------------

function buildIndex(model: PrototypeModelV1): { index: Index; duplicates: PrototypeValidationIssue[] } {
  const index: Index = new Map();
  const duplicates: PrototypeValidationIssue[] = [];

  const add = (id: string, path: string, entry: Omit<Entry, "path">): void => {
    const first = index.get(id);
    if (first) {
      duplicates.push({
        code: "DUPLICATE_ID",
        path,
        message: `id ${JSON.stringify(id)} is already used at ${first.path}; ids are unique across the whole prototype`,
      });
      return;
    }
    index.set(id, { ...entry, path });
  };

  const buttons = (list: PrototypeButton[], path: string, screenId: string): void =>
    list.forEach((b, i) => add(b.id, `${path}[${i}].id`, { kind: "button", screenId }));
  const fields = (list: PrototypeField[], path: string, screenId: string): void =>
    list.forEach((f, i) => add(f.id, `${path}[${i}].id`, { kind: "field", screenId }));
  const nodes = (list: PrototypeNode[], path: string, screenId: string): void =>
    list.forEach((n, i) => node(n, `${path}[${i}]`, screenId));

  function node(n: PrototypeNode, path: string, screenId: string): void {
    add(n.id, `${path}.id`, { kind: n.kind, screenId });
    switch (n.kind) {
      case "stack":
      case "grid":
        return nodes(n.content, `${path}.content`, screenId);
      case "split":
        nodes(n.left, `${path}.left`, screenId);
        return nodes(n.right, `${path}.right`, screenId);
      case "breadcrumbs":
        return n.items.forEach((c, i) => add(c.id, `${path}.items[${i}].id`, { kind: "breadcrumb", screenId }));
      case "tabs":
        return n.tabs.forEach((t, i) => {
          add(t.id, `${path}.tabs[${i}].id`, { kind: "tab", screenId, parentId: n.id });
          nodes(t.content, `${path}.tabs[${i}].content`, screenId);
        });
      case "stepper":
        return n.steps.forEach((s, i) => {
          add(s.id, `${path}.steps[${i}].id`, { kind: "step", screenId, parentId: n.id });
          nodes(s.content, `${path}.steps[${i}].content`, screenId);
        });
      case "heading":
        return buttons(n.actions ?? [], `${path}.actions`, screenId);
      case "empty-state":
        if (n.action) add(n.action.id, `${path}.action.id`, { kind: "button", screenId });
        return;
      case "form":
        fields(n.fields, `${path}.fields`, screenId);
        return buttons(n.actions, `${path}.actions`, screenId);
      case "filters":
        return fields(n.fields, `${path}.fields`, screenId);
      case "table":
      case "task-queue":
        return n.rows.forEach((r, i) => add(r.id, `${path}.rows[${i}].id`, { kind: "row", screenId, parentId: n.id }));
      case "timeline":
        return n.entries.forEach((e, i) => add(e.id, `${path}.entries[${i}].id`, { kind: "timeline-entry", screenId }));
      case "approval-panel":
        return buttons(n.actions, `${path}.actions`, screenId);
      default:
        return;
    }
  }

  model.roles.forEach((r, i) => add(r.id, `roles[${i}].id`, { kind: "role" }));
  model.states.forEach((s, i) => add(s.id, `states[${i}].id`, { kind: "state" }));
  model.flows.forEach((f, i) => add(f.id, `flows[${i}].id`, { kind: "flow" }));
  model.screens.forEach((s, i) => {
    const path = `screens[${i}]`;
    add(s.id, `${path}.id`, { kind: "screen" });
    nodes(s.content, `${path}.content`, s.id);
    (s.overlays ?? []).forEach((o, j) => {
      const at = `${path}.overlays[${j}]`;
      add(o.id, `${at}.id`, { kind: o.kind, screenId: s.id });
      nodes(o.content, `${at}.content`, s.id);
      if (o.kind === "dialog") buttons(o.actions, `${at}.actions`, s.id);
    });
  });
  model.navigation.forEach((n, i) => {
    add(n.id, `navigation[${i}].id`, { kind: "navigation" });
    n.items.forEach((item, j) => add(item.id, `navigation[${i}].items[${j}].id`, { kind: "navigation-item" }));
  });

  return { index, duplicates };
}

// -- Pass 2: resolve every reference -----------------------------------------

function resolve(model: PrototypeModelV1, index: Index, component?: string): PrototypeValidationIssue[] {
  const issues: PrototypeValidationIssue[] = [];

  const unknown = (path: string, value: string, what: string): void => {
    issues.push({ code: "UNKNOWN_REFERENCE", path, message: `${JSON.stringify(value)} does not name ${what}` });
  };
  const expect = (value: string, path: string, kind: EntryKind, what: string): void => {
    if (index.get(value)?.kind !== kind) unknown(path, value, what);
  };
  const states = (list: string[] | undefined, path: string): void =>
    list?.forEach((s, i) => expect(s, `${path}[${i}]`, "state", "a declared display state"));
  const roles = (list: string[] | undefined, path: string): void =>
    list?.forEach((r, i) => expect(r, `${path}[${i}]`, "role", "a declared role"));

  /**
   * An on-screen target: an entry of one of `kinds` on every screen in
   * `screens` (the screens the action can run on).
   */
  const onScreen = (value: string, screens: string[], kinds: EntryKind[]): Entry | undefined => {
    const entry = index.get(value);
    if (!entry || !kinds.includes(entry.kind) || screens.length === 0) return undefined;
    return screens.every((s) => s === entry.screenId) ? entry : undefined;
  };

  /** Two-part targets: the container on this screen, then a member of that container. */
  const member = (
    containerId: string,
    memberId: string,
    path: string,
    keys: [string, string],
    screens: string[],
    containerKinds: EntryKind[],
    memberKind: EntryKind,
    containerWhat: string,
    memberWhat: string,
  ): void => {
    if (!onScreen(containerId, screens, containerKinds)) {
      unknown(`${path}.${keys[0]}`, containerId, containerWhat);
      return;
    }
    const m = index.get(memberId);
    if (m?.kind !== memberKind || m.parentId !== containerId) {
      unknown(`${path}.${keys[1]}`, memberId, `${memberWhat} of ${JSON.stringify(containerId)}`);
    }
  };

  function action(a: PrototypeAction | undefined, path: string, screens: string[]): void {
    if (!a) return;
    switch (a.kind) {
      case "navigate":
        return expect(a.screenId, `${path}.screenId`, "screen", "a screen");
      case "set-tab":
        return member(a.tabsId, a.tabId, path, ["tabsId", "tabId"], screens, ["tabs"], "tab",
          "a tabs node on this screen", "a tab");
      case "set-step":
        return member(a.stepperId, a.stepId, path, ["stepperId", "stepId"], screens, ["stepper"], "step",
          "a stepper on this screen", "a step");
      case "select-row":
        return member(a.tableId, a.rowId, path, ["tableId", "rowId"], screens, ["table", "task-queue"], "row",
          "a table or task queue on this screen", "a row");
      case "show-dialog":
        if (!onScreen(a.dialogId, screens, ["dialog"])) unknown(`${path}.dialogId`, a.dialogId, "a dialog of this screen");
        return;
      case "show-drawer":
        if (!onScreen(a.drawerId, screens, ["drawer"])) unknown(`${path}.drawerId`, a.drawerId, "a drawer of this screen");
        return;
      case "close-overlay":
        return;
    }
  }

  const buttons = (list: PrototypeButton[] | undefined, path: string, screens: string[]): void =>
    list?.forEach((b, i) => action(b.action, `${path}[${i}].action`, screens));
  const fields = (list: PrototypeField[], path: string): void =>
    list.forEach((f, i) => states(f.errorIn, `${path}[${i}].errorIn`));
  const nodes = (list: PrototypeNode[], path: string, screens: string[]): void =>
    list.forEach((n, i) => node(n, `${path}[${i}]`, screens));

  function node(n: PrototypeNode, path: string, screens: string[]): void {
    switch (n.kind) {
      case "stack":
      case "grid":
        nodes(n.content, `${path}.content`, screens);
        break;
      case "split":
        nodes(n.left, `${path}.left`, screens);
        nodes(n.right, `${path}.right`, screens);
        break;
      case "breadcrumbs":
        n.items.forEach((c, i) => action(c.action, `${path}.items[${i}].action`, screens));
        break;
      case "tabs":
        n.tabs.forEach((t, i) => nodes(t.content, `${path}.tabs[${i}].content`, screens));
        break;
      case "stepper":
        n.steps.forEach((s, i) => nodes(s.content, `${path}.steps[${i}].content`, screens));
        break;
      case "heading":
        buttons(n.actions, `${path}.actions`, screens);
        break;
      case "empty-state":
        if (n.action) action(n.action.action, `${path}.action.action`, screens);
        break;
      case "button":
      case "link":
        action(n.action, `${path}.action`, screens);
        break;
      case "form":
        fields(n.fields, `${path}.fields`);
        buttons(n.actions, `${path}.actions`, screens);
        break;
      case "filters":
        fields(n.fields, `${path}.fields`);
        break;
      case "table":
      case "task-queue":
        action(n.onRow, `${path}.onRow`, screens);
        break;
      case "approval-panel":
        buttons(n.actions, `${path}.actions`, screens);
        break;
      default:
        break;
    }
    states(n.showIn, `${path}.showIn`);
  }

  if (component !== undefined && model.component !== component) {
    issues.push({
      code: "PROTOTYPE_COMPONENT_MISMATCH",
      path: "component",
      message: `component ${JSON.stringify(model.component)} does not match its directory ${JSON.stringify(component)}`,
    });
  }
  expect(model.defaultScreenId, "defaultScreenId", "screen", "a screen");
  model.flows.forEach((f, i) => {
    expect(f.roleId, `flows[${i}].roleId`, "role", "a declared role");
    f.screenIds.forEach((s, j) => expect(s, `flows[${i}].screenIds[${j}]`, "screen", "a screen"));
  });
  model.screens.forEach((s, i) => {
    const path = `screens[${i}]`;
    roles(s.roleIds, `${path}.roleIds`);
    if (s.navigationId !== undefined) expect(s.navigationId, `${path}.navigationId`, "navigation", "a navigation");
    nodes(s.content, `${path}.content`, [s.id]);
    (s.overlays ?? []).forEach((o, j) => {
      const at = `${path}.overlays[${j}]`;
      nodes(o.content, `${at}.content`, [s.id]);
      if (o.kind === "dialog") buttons(o.actions, `${at}.actions`, [s.id]);
    });
  });
  model.navigation.forEach((n, i) => {
    const shownOn = model.screens.filter((s) => s.navigationId === n.id).map((s) => s.id);
    n.items.forEach((item, j) => {
      const path = `navigation[${i}].items[${j}]`;
      action(item.action, `${path}.action`, shownOn);
      roles(item.roleIds, `${path}.roleIds`);
    });
  });

  return issues;
}
