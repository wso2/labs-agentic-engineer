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
 * The progressive-disclosure skill loaders (ADR-0002), shared by EVERY tool set
 * (file tools and task-plan tools alike): the catalog at the end of the prompt is
 * toolset-agnostic, so a skill body reaches the model the same way regardless of
 * which domain tools are registered. Extracted here so both `files.ts` and
 * `task-plan.ts` register identical `loadSkill`(+`loadSkillReference`) behavior.
 *
 * The tools read through the `SkillSource` seam (`../skill-source.ts`): the
 * turn's `_skills` snapshot supplies the catalog, and bodies are read from disk
 * at call time.
 *
 * No skills → returns `{}`, so a skill-free turn is byte-identical to today.
 */

import { tool } from "ai";
import type { Tool } from "ai";
import { z } from "zod";
import type {
  Equal,
  LoadSkillInput,
  LoadSkillResult,
  LoadedSkill,
  LoadSkillReferenceInput,
  LoadSkillReferenceResult,
} from "@aep/agent-stream";
import { EMPTY_SKILL_SOURCE, SkillReadError, type SkillSource } from "../skill-source.js";

/** Progressive-disclosure skill loader — registered only when skills are supplied. */
export const LOAD_SKILL = "loadSkill" as const;
/** Third disclosure level — registered only when some skill carries `references`. */
export const LOAD_SKILL_REFERENCE = "loadSkillReference" as const;

export const loadSkillInputSchema = z.object({
  names: z
    .array(z.string())
    .min(1)
    .describe(
      "ALL the skill names to load, exactly as listed in the Skills catalog. " +
        "Batch every skill the task needs into ONE call.",
    ),
});

export const loadSkillReferenceInputSchema = z.object({
  name: z.string().describe("The skill whose reference to read, exactly as listed in the Skills catalog."),
  path: z
    .string()
    .describe(
      "A path exactly as listed by loadSkill — docs, scripts, or assets — " +
        'e.g. "references/schema.md" or "scripts/run.mjs".',
    ),
});

// --- Drift guard: Zod schema ⇄ sse-events wire type -------------------------
const _drift: [
  Equal<z.infer<typeof loadSkillInputSchema>, LoadSkillInput>,
  Equal<z.infer<typeof loadSkillReferenceInputSchema>, LoadSkillReferenceInput>,
] = [true, true];
void _drift;

/**
 * The skill loaders bound to one turn's `SkillSource`. Empty when no skills are
 * supplied (the catalog is likewise omitted from the prompt).
 * `loadSkillReference` is registered only when some skill actually carries
 * references, so a references-free library keeps today's exact tool set.
 */
export function buildSkillTools(skills?: SkillSource): Record<string, Tool> {
  const source = skills ?? EMPTY_SKILL_SOURCE;
  const entries = source.catalog();
  if (entries.length === 0) return {};
  const available = entries.map((e) => e.name);

  const tools: Record<string, Tool> = {
    [LOAD_SKILL]: tool({
      description:
        "Read the full guidance bodies of skills by name (names are listed in the Skills catalog at the end of your " +
        "instructions). Call this ONCE with every skill the task needs, before relying on any of them. Returns each " +
        "skill's content; unknown names come back in `missing` with the available catalog — re-call for the corrected " +
        "missing names only.",
      inputSchema: loadSkillInputSchema,
      execute: async ({ names }): Promise<LoadSkillResult> => {
        const skills: LoadedSkill[] = [];
        const missing: string[] = [];
        const refused: string[] = [];
        for (const name of names) {
          let loaded;
          try {
            loaded = source.load(name);
          } catch (err) {
            if (err instanceof SkillReadError) {
              // I/O fault — do NOT list as unknown skills; fail loudly.
              return {
                ok: false,
                error: `could not read skill ${name}`,
                skills,
                missing: [],
                available,
              };
            }
            throw err;
          }
          if (loaded === undefined) {
            missing.push(name);
            continue;
          }
          if ("refused" in loaded) {
            refused.push(name);
            continue;
          }
          skills.push({
            name,
            content: loaded.content,
            ...(loaded.references.length > 0 ? { references: loaded.references } : {}),
          });
        }
        if (missing.length > 0 || refused.length > 0) {
          // Two different faults, phrased differently on purpose: an unknown name is
          // a mistake to correct, a refusal is a redirect to the right mechanism.
          const parts: string[] = [];
          if (refused.length > 0) {
            parts.push(
              `not yours to load: ${refused.join(", ")} — these are the coding agent's guidance. ` +
                "If a component's build needs one, add it to that component's skillsPinned in its design.json.",
            );
          }
          if (missing.length > 0) parts.push(`unknown skills: ${missing.join(", ")}`);
          return {
            ok: false,
            error: parts.join(" "),
            skills,
            missing,
            available,
            ...(refused.length > 0 ? { refused } : {}),
          };
        }
        return { ok: true, skills };
      },
    }),
  };

  const refSkillNames = entries.filter((e) => e.hasReferences).map((e) => e.name);
  if (refSkillNames.length > 0) {
    tools[LOAD_SKILL_REFERENCE] = tool({
      description:
        "Read ONE reference file of a loaded skill (loadSkill lists each skill's reference paths). Call it only " +
        "when the skill's body points you at that reference. On a miss the error lists what is addressable — " +
        "re-call with one of those.",
      inputSchema: loadSkillReferenceInputSchema,
      execute: async ({ name, path }): Promise<LoadSkillReferenceResult> => {
        let loaded;
        try {
          loaded = source.load(name);
        } catch (err) {
          if (err instanceof SkillReadError) {
            return { ok: false, name, path, error: `could not read skill ${name}`, available: refSkillNames };
          }
          throw err;
        }
        // A refusal is treated like a miss HERE only: a reference belongs to a
        // skill this agent may not load at all, so there is nothing to redirect
        // it to — unlike loadSkill, where the redirect to pinning is the point.
        if (loaded === undefined || "refused" in loaded || loaded.references.length === 0) {
          return { ok: false, name, path, error: `unknown skill: ${name}`, available: refSkillNames };
        }
        let loadedRef;
        try {
          loadedRef = source.loadReference(name, path);
        } catch (err) {
          if (err instanceof SkillReadError) {
            return {
              ok: false,
              name,
              path,
              error: `could not read skill ${name} reference ${path}`,
              available: loaded.references,
            };
          }
          throw err;
        }
        if (loadedRef === undefined) {
          return { ok: false, name, path, error: `unknown reference: ${path}`, available: loaded.references };
        }
        if ("binary" in loadedRef) {
          return {
            ok: false,
            name,
            path,
            error: `${path} is a binary file — it cannot be loaded into context`,
            available: loaded.references,
          };
        }
        return { ok: true, name, path, content: loadedRef.content };
      },
    });
  }

  return tools;
}
