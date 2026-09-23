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
 * The structural half of the validator: the Zod schema for version 1. It is
 * published as JSON Schema (`./json-schema.ts`) and vendored into the Go save
 * gate, so both gates judge ONE shape. The reference half, which no standalone
 * JSON Schema can express, is `./references.ts`, mirrored rule for rule in Go.
 *
 * Constraints stay inside what the Go interpreter
 * (services/aep-api/internal/platform/jsonschema) implements — no `pattern`,
 * no `format` — because it ignores what it does not implement, and a keyword
 * only one gate enforces is a document only one gate refuses. The vendored
 * schema test fails loudly on any keyword it would ignore.
 *
 * Every object is strict: an unknown key is refused, which is what keeps
 * markup, scripts, and a custom-component escape hatch out mechanically.
 *
 * Key order in each object is the serialized order (`stablePrototypeJson`
 * emits what this schema outputs): `kind` and `id` first, `showIn` last.
 */

import { z } from "zod";
import type {
  PrototypeAction,
  PrototypeBreadcrumb,
  PrototypeButton,
  PrototypeDetailField,
  PrototypeDisplayState,
  PrototypeField,
  PrototypeFlow,
  PrototypeModelV1,
  PrototypeNavigation,
  PrototypeNode,
  PrototypeOverlay,
  PrototypeRole,
  PrototypeRow,
  PrototypeScreen,
  PrototypeTimelineEntry,
} from "./model.js";
import { PROTOTYPE_SCHEMA_VERSION } from "./model.js";
import type { Equal } from "./type-equal.js";

const id = z.string().min(1);
const label = z.string().min(1);
const idList = z.array(id).min(1);
const tone = z.enum(["default", "info", "success", "warning", "error"]);

const actionSchema = z
  .discriminatedUnion("kind", [
    z.strictObject({ kind: z.literal("navigate"), screenId: id }),
    z.strictObject({ kind: z.literal("set-tab"), tabsId: id, tabId: id }),
    z.strictObject({ kind: z.literal("set-step"), stepperId: id, stepId: id }),
    z.strictObject({ kind: z.literal("select-row"), tableId: id, rowId: id }),
    z.strictObject({ kind: z.literal("show-dialog"), dialogId: id }),
    z.strictObject({ kind: z.literal("show-drawer"), drawerId: id }),
    z.strictObject({ kind: z.literal("close-overlay") }),
  ])
  .meta({ id: "PrototypeAction" });

const emphasis = z.enum(["primary", "danger"]);

const buttonSchema = z
  .strictObject({ id, label, emphasis: emphasis.optional(), action: actionSchema })
  .meta({ id: "PrototypeButton" });

const fieldSchema = z
  .strictObject({
    id,
    label,
    type: z.enum(["text", "number", "select", "date", "textarea", "switch"]).optional(),
    value: z.string().optional(),
    options: z.array(label).min(1).optional(),
    error: label.optional(),
    errorIn: idList.optional(),
  })
  .meta({ id: "PrototypeField" });

const rowSchema = z
  .strictObject({ id, values: z.record(z.string(), z.string()), tone: tone.optional() })
  .meta({ id: "PrototypeRow" });

const breadcrumbSchema = z.strictObject({ id, label, action: actionSchema.optional() });
const detailFieldSchema = z.strictObject({ label, value: z.string() });
const timelineEntrySchema = z.strictObject({ id, when: label, who: label, text: label });

// Recursive: containers hold nodes. The explicit annotation on `nodeSchema` is
// what lets a self-referencing schema type-check; `_driftNode` below guards the
// union itself.
function nodeUnion() {
  return z.discriminatedUnion("kind", [
    node("stack", { direction: z.enum(["row", "column"]).optional(), content: nodes }),
    node("grid", { columns: z.number().int().min(1).max(6), content: nodes }),
    node("split", { ratio: z.number().int().min(1).max(11).optional(), left: nodes, right: nodes }),
    node("detail", { title: label.optional(), fields: z.array(detailFieldSchema).min(1) }),
    node("breadcrumbs", { items: z.array(breadcrumbSchema).min(1) }),
    node("tabs", { tabs: z.array(z.strictObject({ id, label, content: nodes })).min(1) }),
    node("stepper", { steps: z.array(z.strictObject({ id, label, content: nodes })).min(1) }),
    node("text", { text: label }),
    node("heading", { text: label, actions: z.array(buttonSchema).min(1).optional() }),
    node("badge", { label, tone: tone.optional() }),
    node("stat", { label, value: label }),
    node("alert", { tone, title: label.optional(), text: label }),
    node("empty-state", { title: label, text: label, action: buttonSchema.optional() }),
    node("button", { label, emphasis: emphasis.optional(), action: actionSchema }),
    node("link", { label, action: actionSchema }),
    node("form", { title: label.optional(), fields: z.array(fieldSchema).min(1), actions: z.array(buttonSchema) }),
    node("validation-summary", { issues: z.array(label).min(1) }),
    node("table", {
      title: label.optional(),
      columns: z.array(label).min(1),
      rows: z.array(rowSchema),
      onRow: actionSchema.optional(),
    }),
    node("filters", { fields: z.array(fieldSchema).min(1) }),
    node("timeline", { entries: z.array(timelineEntrySchema).min(1) }),
    node("approval-panel", { title: label, summary: label, actions: z.array(buttonSchema).min(1) }),
    node("task-queue", {
      title: label,
      columns: z.array(label).min(1),
      rows: z.array(rowSchema),
      onRow: actionSchema.optional(),
    }),
  ]).meta({ id: "PrototypeNode" });
}

const nodeSchema: z.ZodType<PrototypeNode> = z.lazy(nodeUnion);

const nodes = z.array(nodeSchema).meta({ id: "PrototypeNodeList" });

/** One registry node: `kind`, `id`, its own fields, then `showIn`. */
function node<K extends string, S extends z.ZodRawShape>(kind: K, shape: S) {
  return z.strictObject({ kind: z.literal(kind), id, ...shape, showIn: idList.optional() });
}

const overlaySchema = z.discriminatedUnion("kind", [
  z.strictObject({ kind: z.literal("dialog"), id, title: label, content: nodes, actions: z.array(buttonSchema) }),
  z.strictObject({ kind: z.literal("drawer"), id, title: label, content: nodes }),
]);

const navigationSchema = z.strictObject({
  kind: z.enum(["side-nav", "top-nav"]),
  id,
  items: z.array(z.strictObject({ id, label, action: actionSchema, roleIds: idList.optional() })).min(1),
});

const screenSchema = z.strictObject({
  id,
  name: label,
  roleIds: idList,
  navigationId: id.optional(),
  content: nodes,
  overlays: z.array(overlaySchema).optional(),
});

const roleSchema = z.strictObject({ id, name: label });
const stateSchema = z.strictObject({ id, name: label });
const flowSchema = z.strictObject({ id, name: label, roleId: id, screenIds: idList });

export const prototypeModelSchema = z.strictObject({
  schemaVersion: z.literal(PROTOTYPE_SCHEMA_VERSION),
  component: id,
  name: label,
  defaultScreenId: id,
  roles: z.array(roleSchema).min(1),
  states: z.array(stateSchema).min(1),
  flows: z.array(flowSchema),
  screens: z.array(screenSchema).min(1),
  navigation: z.array(navigationSchema),
});

// Compile-time drift guards: schema ⇄ model types.
const _driftModel: Equal<z.infer<typeof prototypeModelSchema>, PrototypeModelV1> = true;
const _driftScreen: Equal<z.infer<typeof screenSchema>, PrototypeScreen> = true;
const _driftOverlay: Equal<z.infer<typeof overlaySchema>, PrototypeOverlay> = true;
const _driftNavigation: Equal<z.infer<typeof navigationSchema>, PrototypeNavigation> = true;
const _driftAction: Equal<z.infer<typeof actionSchema>, PrototypeAction> = true;
const _driftButton: Equal<z.infer<typeof buttonSchema>, PrototypeButton> = true;
const _driftField: Equal<z.infer<typeof fieldSchema>, PrototypeField> = true;
const _driftRow: Equal<z.infer<typeof rowSchema>, PrototypeRow> = true;
const _driftBreadcrumb: Equal<z.infer<typeof breadcrumbSchema>, PrototypeBreadcrumb> = true;
const _driftDetail: Equal<z.infer<typeof detailFieldSchema>, PrototypeDetailField> = true;
const _driftTimeline: Equal<z.infer<typeof timelineEntrySchema>, PrototypeTimelineEntry> = true;
const _driftRole: Equal<z.infer<typeof roleSchema>, PrototypeRole> = true;
const _driftState: Equal<z.infer<typeof stateSchema>, PrototypeDisplayState> = true;
const _driftFlow: Equal<z.infer<typeof flowSchema>, PrototypeFlow> = true;
const _driftNode: Equal<z.infer<ReturnType<typeof nodeUnion>>, PrototypeNode> = true;
void _driftModel;
void _driftScreen;
void _driftOverlay;
void _driftNavigation;
void _driftAction;
void _driftButton;
void _driftField;
void _driftRow;
void _driftBreadcrumb;
void _driftDetail;
void _driftTimeline;
void _driftRole;
void _driftState;
void _driftFlow;
void _driftNode;
