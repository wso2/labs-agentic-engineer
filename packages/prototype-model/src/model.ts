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
 * Prototype model, version 1: the shape of
 * `specs/design/components/<component>/prototype.json`, the read-only
 * prototype of one `web-application` component.
 *
 * The model is a CONTROLLED registry, not a UI language. Every node is one of
 * the kinds below and every interaction is one of the actions below; there is
 * no custom-component escape hatch, no markup, no script. That is what lets
 * one renderer draw any prototype and one gate refuse a malformed one.
 *
 * The registry's 28 entries map onto the model like this:
 *
 *  - content nodes (`PrototypeNode`): stack, grid, split, detail, breadcrumbs,
 *    tabs, stepper, text, heading, badge, stat, alert, empty-state, button,
 *    link, form, validation-summary, table, filters, timeline, approval-panel,
 *    task-queue;
 *  - `field`: an entry of a form's or filters' `fields`;
 *  - `dialog`, `drawer`: a screen's `overlays`;
 *  - `side-nav`, `top-nav`: a `navigation` entry's `kind`;
 *  - `app-shell`: the frame a screen renders in when it names a
 *    `navigationId` — the application's chrome around that navigation.
 *
 * Every entry that can be pointed at carries an `id`. IDs share ONE namespace
 * across the whole document, are stable across revisions, and are the only
 * identities: labels and display values are not.
 *
 * The Zod schema in `./schema.ts` is drift-guarded against these types.
 */

/** The only schema version this package reads or writes. */
export const PROTOTYPE_SCHEMA_VERSION = 1;

/** Presentation tone of a badge, alert, or row. */
export type PrototypeTone = "default" | "info" | "success" | "warning" | "error";

/**
 * What activating a button, link, row, breadcrumb, or navigation item does.
 * Every action changes VIEW state only — the prototype never submits, mutates,
 * or calls an integration.
 */
export type PrototypeAction =
  | { kind: "navigate"; screenId: string }
  | { kind: "set-tab"; tabsId: string; tabId: string }
  | { kind: "set-step"; stepperId: string; stepId: string }
  | { kind: "select-row"; tableId: string; rowId: string }
  | { kind: "show-dialog"; dialogId: string }
  | { kind: "show-drawer"; drawerId: string }
  | { kind: "close-overlay" };

/** A button wherever one appears: a node, a heading's or form's actions, an overlay's footer. */
export interface PrototypeButton {
  id: string;
  label: string;
  emphasis?: "primary" | "danger" | undefined;
  action: PrototypeAction;
}

/** One input of a form or filter bar. Read-only: `value` is what it displays. */
export interface PrototypeField {
  id: string;
  label: string;
  type?: "text" | "number" | "select" | "date" | "textarea" | "switch" | undefined;
  value?: string | undefined;
  options?: string[] | undefined;
  /** The validation message the field shows… */
  error?: string | undefined;
  /** …and the display states it shows it in (all states when absent). */
  errorIn?: string[] | undefined;
}

/** One mock record of a table or task queue, keyed by column label. */
export interface PrototypeRow {
  id: string;
  values: Record<string, string>;
  tone?: PrototypeTone | undefined;
}

export interface PrototypeTab {
  id: string;
  label: string;
  content: PrototypeNode[];
}

export interface PrototypeStep {
  id: string;
  label: string;
  content: PrototypeNode[];
}

export interface PrototypeBreadcrumb {
  id: string;
  label: string;
  action?: PrototypeAction | undefined;
}

export interface PrototypeDetailField {
  label: string;
  value: string;
}

export interface PrototypeTimelineEntry {
  id: string;
  when: string;
  who: string;
  text: string;
}

/**
 * Fields every content node carries. `showIn` limits the node to those display
 * states (all states when absent), which is how one screen shows its empty,
 * failed, or invalid presentation without a second screen.
 */
interface NodeBase {
  id: string;
  showIn?: string[] | undefined;
}

export interface PrototypeStackNode extends NodeBase {
  kind: "stack";
  direction?: "row" | "column" | undefined;
  content: PrototypeNode[];
}
export interface PrototypeGridNode extends NodeBase { kind: "grid"; columns: number; content: PrototypeNode[] }
/** Two panes; `ratio` is the left pane's share of 12 columns. */
export interface PrototypeSplitNode extends NodeBase {
  kind: "split";
  ratio?: number | undefined;
  left: PrototypeNode[];
  right: PrototypeNode[];
}
export interface PrototypeDetailNode extends NodeBase {
  kind: "detail";
  title?: string | undefined;
  fields: PrototypeDetailField[];
}
export interface PrototypeBreadcrumbsNode extends NodeBase { kind: "breadcrumbs"; items: PrototypeBreadcrumb[] }
export interface PrototypeTabsNode extends NodeBase { kind: "tabs"; tabs: PrototypeTab[] }
export interface PrototypeStepperNode extends NodeBase { kind: "stepper"; steps: PrototypeStep[] }
export interface PrototypeTextNode extends NodeBase { kind: "text"; text: string }
export interface PrototypeHeadingNode extends NodeBase {
  kind: "heading";
  text: string;
  actions?: PrototypeButton[] | undefined;
}
export interface PrototypeBadgeNode extends NodeBase { kind: "badge"; label: string; tone?: PrototypeTone | undefined }
export interface PrototypeStatNode extends NodeBase { kind: "stat"; label: string; value: string }
export interface PrototypeAlertNode extends NodeBase {
  kind: "alert";
  tone: PrototypeTone;
  title?: string | undefined;
  text: string;
}
export interface PrototypeEmptyStateNode extends NodeBase {
  kind: "empty-state";
  title: string;
  text: string;
  action?: PrototypeButton | undefined;
}
export interface PrototypeButtonNode extends NodeBase {
  kind: "button";
  label: string;
  emphasis?: "primary" | "danger" | undefined;
  action: PrototypeAction;
}
export interface PrototypeLinkNode extends NodeBase { kind: "link"; label: string; action: PrototypeAction }
export interface PrototypeFormNode extends NodeBase {
  kind: "form";
  title?: string | undefined;
  fields: PrototypeField[];
  actions: PrototypeButton[];
}
export interface PrototypeValidationSummaryNode extends NodeBase { kind: "validation-summary"; issues: string[] }
export interface PrototypeTableNode extends NodeBase {
  kind: "table";
  title?: string | undefined;
  columns: string[];
  rows: PrototypeRow[];
  onRow?: PrototypeAction | undefined;
}
export interface PrototypeFiltersNode extends NodeBase { kind: "filters"; fields: PrototypeField[] }
export interface PrototypeTimelineNode extends NodeBase { kind: "timeline"; entries: PrototypeTimelineEntry[] }
export interface PrototypeApprovalPanelNode extends NodeBase {
  kind: "approval-panel";
  title: string;
  summary: string;
  actions: PrototypeButton[];
}
export interface PrototypeTaskQueueNode extends NodeBase {
  kind: "task-queue";
  title: string;
  columns: string[];
  rows: PrototypeRow[];
  onRow?: PrototypeAction | undefined;
}

/** Every content node of the v1 registry. */
export type PrototypeNode =
  | PrototypeStackNode
  | PrototypeGridNode
  | PrototypeSplitNode
  | PrototypeDetailNode
  | PrototypeBreadcrumbsNode
  | PrototypeTabsNode
  | PrototypeStepperNode
  | PrototypeTextNode
  | PrototypeHeadingNode
  | PrototypeBadgeNode
  | PrototypeStatNode
  | PrototypeAlertNode
  | PrototypeEmptyStateNode
  | PrototypeButtonNode
  | PrototypeLinkNode
  | PrototypeFormNode
  | PrototypeValidationSummaryNode
  | PrototypeTableNode
  | PrototypeFiltersNode
  | PrototypeTimelineNode
  | PrototypeApprovalPanelNode
  | PrototypeTaskQueueNode;

export type PrototypeNodeKind = PrototypeNode["kind"];

/** A modal over the screen it belongs to. */
export interface PrototypeDialog {
  kind: "dialog";
  id: string;
  title: string;
  content: PrototypeNode[];
  actions: PrototypeButton[];
}

/** A side panel over the screen it belongs to. */
export interface PrototypeDrawer {
  kind: "drawer";
  id: string;
  title: string;
  content: PrototypeNode[];
}

export type PrototypeOverlay = PrototypeDialog | PrototypeDrawer;

export interface PrototypeNavigationItem {
  id: string;
  label: string;
  action: PrototypeAction;
  /** The roles that see this item (every role when absent). */
  roleIds?: string[] | undefined;
}

export interface PrototypeNavigation {
  id: string;
  kind: "side-nav" | "top-nav";
  items: PrototypeNavigationItem[];
}

export interface PrototypeScreen {
  id: string;
  name: string;
  /** The roles that can reach this screen. */
  roleIds: string[];
  /** The navigation this screen's app shell shows; no shell when absent. */
  navigationId?: string | undefined;
  content: PrototypeNode[];
  overlays?: PrototypeOverlay[] | undefined;
}

/** A role the reviewer can view the prototype as — `security.json`'s role when that file exists. */
export interface PrototypeRole {
  id: string;
  name: string;
}

/** A named display state (default, validation errors, failed, …) the reviewer can switch to. */
export interface PrototypeDisplayState {
  id: string;
  name: string;
}

/** A named path through the application, walked as one role. */
export interface PrototypeFlow {
  id: string;
  name: string;
  roleId: string;
  screenIds: string[];
}

export interface PrototypeModelV1 {
  schemaVersion: typeof PROTOTYPE_SCHEMA_VERSION;
  /** The web-application component this prototype belongs to — its directory name. */
  component: string;
  name: string;
  defaultScreenId: string;
  roles: PrototypeRole[];
  states: PrototypeDisplayState[];
  flows: PrototypeFlow[];
  screens: PrototypeScreen[];
  navigation: PrototypeNavigation[];
}
