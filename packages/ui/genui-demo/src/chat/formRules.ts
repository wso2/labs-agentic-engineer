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

import type { GenUiFieldErrors, GenUiSpec } from "@aep/ui-genui";

/** What a form asked for one answer: read from the Field that collects it. */
export interface FieldRule {
  name: string;
  label: string;
  kind: string;
  required: boolean;
  options?: Array<{ value: string; label: string }>;
}

/**
 * The rules a chat form states about itself. In chat the agent chooses the
 * fields, so there is no fixed schema to check answers against; the form it
 * sent is the schema. Every Field whose value is bound under `root` is one
 * answer, named by the rest of its path.
 */
export function fieldRules(spec: GenUiSpec, root = "/answers"): FieldRule[] {
  const rules: FieldRule[] = [];
  for (const element of Object.values(spec.elements)) {
    if (element.type !== "Field") continue;
    const props = element.props as Record<string, unknown>;
    const binding = props.value as { $bindState?: unknown } | undefined;
    const path = typeof binding?.$bindState === "string" ? binding.$bindState : undefined;
    if (!path?.startsWith(`${root}/`)) continue;
    rules.push({
      name: path.slice(root.length + 1),
      label: String(props.label),
      kind: String(props.type),
      required: props.required === true,
      ...(Array.isArray(props.options)
        ? { options: props.options as Array<{ value: string; label: string }> }
        : {}),
    });
  }
  return rules;
}

/** Field errors for answers that break the form's own rules, keyed by field. */
export function checkAnswers(
  rules: FieldRule[],
  answers: Record<string, string | boolean>,
): GenUiFieldErrors {
  const errors: GenUiFieldErrors = {};
  for (const rule of rules) {
    const value = answers[rule.name];
    const empty = value === undefined || value === "" || value === false;
    if (rule.required && empty) {
      errors[rule.name] =
        rule.kind === "checkbox" ? `Tick "${rule.label}" to continue.` : `Answer "${rule.label}".`;
    } else if (rule.kind === "email" && typeof value === "string" && value !== "") {
      if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(value)) errors[rule.name] = "Enter a valid email address.";
    }
  }
  return errors;
}

/** The answers as a person would read them back, using option labels. */
export function describeAnswers(
  rules: FieldRule[],
  answers: Record<string, string | boolean>,
): Array<{ label: string; value: string }> {
  return rules.map((rule) => {
    const value = answers[rule.name];
    if (typeof value === "boolean") return { label: rule.label, value: value ? "Yes" : "No" };
    const option = rule.options?.find((o) => o.value === value);
    return { label: rule.label, value: option?.label ?? (value ? String(value) : "—") };
  });
}
