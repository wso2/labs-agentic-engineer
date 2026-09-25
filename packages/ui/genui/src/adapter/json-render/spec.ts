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

import { validateSpec, type Spec } from "@json-render/core";
import { z } from "zod";
import {
  genUiComponents,
  type GenUiComponentDef,
  type GenUiComponentName,
} from "../../catalog/index.js";
import { jsonRenderCatalog } from "./catalog.js";

/**
 * A generated UI, in the wire format of the renderer library underneath
 * (json-render's flat element map today). Treat it as opaque outside the
 * adapter: produce it with a model prompted by genUiSystemPrompt(), check it
 * with validateGenUiSpec(), render it with <GenUiView>.
 */
export type GenUiSpec = Spec;

export type GenUiValidation =
  | { ok: true; spec: GenUiSpec }
  | { ok: false; issues: string[] };

// A json-render dynamic value ({ "$state": … }, { "$template": … }, …). Its
// resolved value is only known at render time, where GenUiView re-checks it.
function isExpression(value: unknown): boolean {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value) &&
    Object.keys(value).some((key) => key.startsWith("$"))
  );
}

/**
 * The object schema an element's props are held to. For a component with
 * kinds (a discriminated union), the kind must be written out literally, not
 * bound, so the right variant is known before render time. Takes the schema
 * as a parameter so its declared type is not narrowed to today's catalog.
 */
function propsShapeFor(
  type: GenUiComponentName,
  schema: GenUiComponentDef["props"],
  props: Record<string, unknown>,
): { shape: z.ZodRawShape } | { issues: z.core.$ZodIssue[] } {
  if (!(schema instanceof z.ZodDiscriminatedUnion)) return { shape: schema.shape };
  const key = schema.def.discriminator;
  if (isExpression(props[key])) {
    return {
      issues: [
        {
          code: "custom",
          path: [key],
          message: `must be written out, not bound, so the kind of ${type} is known`,
          input: props[key],
        },
      ],
    };
  }
  const variant = schema.options.find(
    (option) => (option.shape[key] as z.ZodType).safeParse(props[key]).success,
  );
  if (variant) return { shape: variant.shape };
  // No variant matches the kind: the union's own error names the valid kinds.
  const parsed = schema.safeParse({ [key]: props[key] });
  return { issues: parsed.success ? [] : parsed.error.issues };
}

/**
 * Holds one element's props to its component schema. json-render's own
 * catalog check does not, so this is what rejects, say, a Progress with value
 * 250. Each literal prop is checked against its own field schema (keeping the
 * field's specific error message); a prop given as an expression is skipped.
 */
function propIssues(
  type: GenUiComponentName,
  props: Record<string, unknown>,
): z.core.$ZodIssue[] {
  const resolved = propsShapeFor(type, genUiComponents[type].props, props);
  if ("issues" in resolved) return resolved.issues;
  const issues: z.core.$ZodIssue[] = [];
  for (const [key, field] of Object.entries(resolved.shape)) {
    const value = props[key];
    if (isExpression(value)) continue;
    const result = (field as z.ZodType).safeParse(value);
    if (!result.success) {
      issues.push(
        ...result.error.issues.map((issue) => ({ ...issue, path: [key, ...issue.path] })),
      );
    }
  }
  return issues;
}

const formatPath = (path: PropertyKey[]) => path.map(String).join(".") || "(root)";

/**
 * The save/render gate for model output. A spec passes when it has the spec
 * shape and only catalog component types, every element's props match that
 * component's schema, and it is structurally sound (root exists, every child
 * reference resolves).
 */
export function validateGenUiSpec(input: unknown): GenUiValidation {
  const shape = jsonRenderCatalog.validate(input);
  if (!shape.success) {
    return {
      ok: false,
      issues: (shape.error?.issues ?? []).map(
        (issue) => `${formatPath(issue.path)}: ${issue.message}`,
      ),
    };
  }
  // The caller's object, not shape.data: the parsed copy is only a check, and
  // the spec handed back must be exactly what was validated.
  const spec = input as GenUiSpec;

  const issues: string[] = [];
  for (const [key, element] of Object.entries(spec.elements)) {
    // Unknown types were already rejected by the catalog check above.
    for (const issue of propIssues(element.type as GenUiComponentName, element.props)) {
      issues.push(`${key}.props.${formatPath(issue.path)}: ${issue.message}`);
    }
  }
  for (const issue of validateSpec(spec).issues) {
    if (issue.severity === "error") {
      issues.push(issue.elementKey ? `${issue.elementKey}: ${issue.message}` : issue.message);
    }
  }
  return issues.length > 0 ? { ok: false, issues } : { ok: true, spec };
}

// How a spec learns what happened to an action: the view writes it to state
// (see GenUiActionState), so these rules hold for every generated UI.
const ACTION_STATE_RULES = [
  'Each action\'s progress is in state at /actions/<actionName>: status is "pending", "success" or "error"; message says what went wrong; fieldErrors maps each param name to its problem.',
  "For a form, bind every Field value with $bindState to /<form>/<param>, read those paths with $state in the submit Button's action params, and bind each Field error to /actions/<actionName>/fieldErrors/<param>.",
  'Show the result with element-level visible conditions, e.g. { "$state": "/actions/<actionName>/status", "eq": "success" } for a confirmation Alert and "eq": "error" for an Alert whose message is { "$state": "/actions/<actionName>/message" }. Disable the submit Button while status is "pending".',
];

export interface GenUiPromptOptions {
  /** Extra rules for this surface, e.g. "Keep it to one card." */
  customRules?: string[];
}

/**
 * The system-prompt section that teaches a model the catalog and the spec
 * format. It is identical across calls for a given catalog, so it caches well.
 */
export function genUiSystemPrompt(options: GenUiPromptOptions = {}): string {
  return jsonRenderCatalog.prompt({
    customRules: [...ACTION_STATE_RULES, ...(options.customRules ?? [])],
  });
}
