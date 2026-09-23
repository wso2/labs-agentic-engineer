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

import { z } from "zod";

/** One row of a props table: a prop, its type in words, and whether it is required. */
export interface PropRow {
  name: string;
  type: string;
  required: boolean;
}

type JsonSchema = {
  type?: string;
  enum?: unknown[];
  anyOf?: JsonSchema[];
  items?: JsonSchema;
  properties?: Record<string, JsonSchema>;
  required?: string[];
  format?: string;
  minimum?: number;
  maximum?: number;
};

function describeType(schema: JsonSchema): string {
  if (schema.enum) return schema.enum.map((value) => JSON.stringify(value)).join(" | ");
  if (schema.anyOf) return schema.anyOf.map(describeType).join(" | ");
  switch (schema.type) {
    case "array":
      return schema.items ? `${describeType(schema.items)}[]` : "array";
    case "object": {
      const required = new Set(schema.required ?? []);
      const fields = Object.entries(schema.properties ?? {}).map(
        ([name, field]) => `${name}${required.has(name) ? "" : "?"}: ${describeType(field)}`,
      );
      return fields.length > 0 ? `{ ${fields.join("; ")} }` : "{}";
    }
    case "string":
      return schema.format === "uri" ? "URL (http or https)" : "string";
    case "number":
    case "integer": {
      const range =
        schema.minimum !== undefined && schema.maximum !== undefined
          ? ` ${schema.minimum}–${schema.maximum}`
          : "";
      return `${schema.type}${range}`;
    }
    default:
      return schema.type ?? "any";
  }
}

/**
 * The props table for a catalog schema, derived from the schema itself so it
 * cannot drift from what validation enforces.
 */
export function propRows(schema: z.ZodType): PropRow[] {
  const json = z.toJSONSchema(schema, { io: "input", unrepresentable: "any" }) as JsonSchema;
  const required = new Set(json.required ?? []);
  return Object.entries(json.properties ?? {}).map(([name, field]) => ({
    name,
    type: describeType(field),
    required: required.has(name),
  }));
}
