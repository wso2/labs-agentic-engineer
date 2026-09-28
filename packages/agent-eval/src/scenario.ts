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

const rubricItem = z.object({
  id: z.string().min(1),
  must: z.string().min(1),
  weight: z.number().positive().default(1),
});
const mustNotItem = z.object({ id: z.string().min(1), mustNot: z.string().min(1) });

const rubric = z
  .object({ mustCover: z.array(rubricItem).default([]), mustNot: z.array(mustNotItem).default([]) })
  // A rubric with nothing in it cannot fail, so a scenario carrying one would
  // report green forever while testing nothing.
  .refine((r) => r.mustCover.length + r.mustNot.length > 0, {
    message: "rubric: needs at least one mustCover or mustNot entry",
  });

const scenario = z.object({
  id: z.string().min(1),
  criteria: z.array(z.string()).default([]),
  brief: z.object({
    goal: z.string().min(1),
    facts: z.record(z.string(), z.unknown()).default({}),
    // What the sim user KNOWS but will not volunteer — this is what turns
    // "asks for what it needs" into something observable rather than asserted.
    withholds: z.array(z.string()).default([]),
  }),
  rubric,
});

export const scenarioFileSchema = z.object({
  version: z.literal(1),
  component: z.string().min(1),
  scenarios: z.array(scenario).min(1),
});

export type ScenarioFile = z.infer<typeof scenarioFileSchema>;
export type Scenario = ScenarioFile["scenarios"][number];
export type Rubric = Scenario["rubric"];
export type RubricItem = Rubric["mustCover"][number];

export class ScenarioError extends Error {}

// Renders a zod issue path as `scenarios[0].brief` rather than `scenarios.0.brief`
// — array indices read as indices, not as another property hop, matching how
// callers already cite scenario positions in fix-loop reasoning and reports.
function formatPath(path: readonly PropertyKey[]): string {
  let out = "";
  for (const segment of path) {
    if (typeof segment === "number") {
      out += `[${segment}]`;
    } else {
      out += out ? `.${String(segment)}` : String(segment);
    }
  }
  return out || "<root>";
}

export function parseScenarios(input: unknown): ScenarioFile {
  const result = scenarioFileSchema.safeParse(input);
  if (!result.success) {
    const first = result.error.issues[0]!;
    throw new ScenarioError(`${formatPath(first.path)}: ${first.message}`);
  }
  const seen = new Set<string>();
  for (const s of result.data.scenarios) {
    if (seen.has(s.id)) throw new ScenarioError(`duplicate scenario id: ${s.id}`);
    seen.add(s.id);

    // A goal that states a withheld fact was never actually withheld — that
    // is an authoring mistake, not something the sim user can paper over at
    // runtime. Catching it once here, where the author can see and fix the
    // scenario, beats trying to filter the leak out of free text on every
    // turn of every run.
    const goal = s.brief.goal.toLowerCase();
    for (const name of s.brief.withholds) {
      const value = s.brief.facts[name];
      if (value === undefined) continue;
      if (goal.includes(String(value).toLowerCase())) {
        throw new ScenarioError(
          `${s.id}: brief.goal states the withheld fact "${name}" (${String(value)})`,
        );
      }
    }
  }
  return result.data;
}
