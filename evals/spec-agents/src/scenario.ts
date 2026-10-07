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
 * Scenario artifacts (map #351: prompts and evaluations are DATA, one YAML per
 * eval case). The brief is the sim user's world (#354); the rubric is the
 * evaluation side (#355: weighted mustCover + declared mustNot) — the sim
 * never sees it. Chain scenarios carry one brief + a rubric PER section.
 */

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { parse as parseYaml } from "yaml";
import type { PlanScope } from "@aep/agent-stream";
import { z } from "zod";
import { SCENARIOS_DIR } from "./config.js";

export const briefSchema = z.object({
  name: z.string().regex(/^[a-z0-9-]+$/, "kebab-case"),
  idea: z.string().min(20),
  persona: z.string().min(10),
  /** Facts the sim may reveal — the "hidden" spec (#354). */
  facts: z.array(z.string().min(5)).min(1),
  /** Standing preference for questions the facts don't settle. */
  fallback: z.string().min(5),
  /** Optional behavior styles folded into the sim prompt (#354). */
  traits: z.array(z.string()).optional(),
  /** Per-scenario override of the agent-turn cap (#354). */
  maxTurns: z.number().int().min(1).max(30).optional(),
  /**
   * Documents the user attached (S5), by file name under
   * scenarios/fixtures/references/: copied into the project's
   * specs/requirements/references/ and listed to the kickoff, as the platform
   * overlays them.
   */
  references: z.array(z.string().min(1)).optional(),
});
export type ScenarioBrief = z.infer<typeof briefSchema>;

/** `mustCover` entries: plain prose, or prose + weight (defaults to 1). */
const mustCoverItemSchema = z.union([
  z.string().min(5).transform((item) => ({ item, weight: 1 })),
  z.object({ item: z.string().min(5), weight: z.number().positive().default(1) }),
]);

export const rubricSchema = z.object({
  mustCover: z.array(mustCoverItemSchema).min(1),
  /** Declared anti-requirements; a violation forces at least REVIEW (#355). */
  mustNot: z.array(z.string().min(5)).default([]),
});
export type Rubric = z.infer<typeof rubricSchema>;
export type RubricItem = Rubric["mustCover"][number];

const requirementsScenarioSchema = z.object({ brief: briefSchema, rubric: rubricSchema });
export type RequirementsScenario = z.infer<typeof requirementsScenarioSchema>;

/** Design runs standalone from a captured requirements fixture (#356 pattern). */
const designScenarioSchema = z.object({
  brief: briefSchema,
  /** Directory name under scenarios/fixtures/ holding the input specs/. */
  fixture: z.string().min(1),
  rubric: rubricSchema,
});
export type DesignScenario = z.infer<typeof designScenarioSchema>;

/** Task generation is one-shot — no sim user, so no brief (#356). */
const tasksScenarioSchema = z.object({
  name: z.string().regex(/^[a-z0-9-]+$/),
  fixture: z.string().min(1),
  /**
   * The milestone scope the platform would send for the version (aep-api
   * `planScopeFor`): its stories, features and product-wide items. Absent →
   * a scope-less plan of the whole design.
   */
  scope: z
    .object({
      tag: z.string().min(1),
      stories: z.array(z.object({ id: z.string(), title: z.string().optional(), covered: z.boolean() })),
      features: z
        .array(z.object({ id: z.string(), name: z.string().optional(), needs: z.array(z.string()).optional() }))
        .optional(),
      productWide: z
        .array(z.object({ id: z.string(), text: z.string().optional(), appliesTo: z.array(z.string()).optional() }))
        .optional(),
    })
    // Parsed YAML never carries an explicit undefined, which is the one way
    // zod's optional output differs from the contract's exact optionals.
    .transform((s) => s as PlanScope)
    .optional(),
  rubric: rubricSchema,
});
export type TasksScenario = z.infer<typeof tasksScenarioSchema>;

/** A chain is FIRST-CLASS: one brief, a rubric per section (#357). */
const chainScenarioSchema = z.object({
  brief: briefSchema,
  rubrics: z.object({
    requirements: rubricSchema,
    design: rubricSchema,
    tasks: rubricSchema,
  }),
});
export type ChainScenario = z.infer<typeof chainScenarioSchema>;

const sectionSchemas = {
  requirements: requirementsScenarioSchema,
  design: designScenarioSchema,
  tasks: tasksScenarioSchema,
  chains: chainScenarioSchema,
} as const;

export type ScenarioSection = keyof typeof sectionSchemas;

/** Parse + validate every scenario YAML under `scenarios/<section>/`. */
export function loadScenarios<S extends ScenarioSection>(
  section: S,
): Array<z.infer<(typeof sectionSchemas)[S]>> {
  const dir = join(SCENARIOS_DIR, section);
  let files: string[];
  try {
    files = readdirSync(dir).filter((f) => f.endsWith(".yaml") || f.endsWith(".yml"));
  } catch {
    return [];
  }
  return files.sort().map((f) => {
    const raw: unknown = parseYaml(readFileSync(join(dir, f), "utf8"));
    const parsed = sectionSchemas[section].safeParse(raw);
    if (!parsed.success) {
      throw new Error(`invalid scenario ${section}/${f}: ${parsed.error.issues.map((i) => `${i.path.join(".")}: ${i.message}`).join("; ")}`);
    }
    return parsed.data as z.infer<(typeof sectionSchemas)[S]>;
  });
}
