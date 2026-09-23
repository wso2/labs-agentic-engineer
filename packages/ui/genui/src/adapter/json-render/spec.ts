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
import { genUiComponents, type GenUiComponentName } from "../../catalog/index.js";
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
 * Holds one element's props to its component schema. json-render's own
 * catalog check does not, so this is what rejects, say, a Progress with value
 * 250. Each literal prop is checked against its own field schema (keeping the
 * field's specific error message); a prop given as an expression is skipped.
 */
function propIssues(
  type: GenUiComponentName,
  props: Record<string, unknown>,
): z.core.$ZodIssue[] {
  const issues: z.core.$ZodIssue[] = [];
  for (const [key, field] of Object.entries(genUiComponents[type].props.shape)) {
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

export interface GenUiPromptOptions {
  /** Extra rules for this surface, e.g. "Keep it to one card." */
  customRules?: string[];
}

/**
 * The system-prompt section that teaches a model the catalog and the spec
 * format. It is identical across calls for a given catalog, so it caches well.
 */
export function genUiSystemPrompt(options: GenUiPromptOptions = {}): string {
  return jsonRenderCatalog.prompt(
    options.customRules ? { customRules: options.customRules } : {},
  );
}
