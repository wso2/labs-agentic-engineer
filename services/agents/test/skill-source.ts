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
 * TEST-ONLY scaffolding (never a production code path): wraps a plain in-memory
 * skill list into the `SkillSource` seam so unit tests can drive the catalog +
 * loaders without materializing a `_skills` snapshot on disk. Production turns
 * always read skills through `SnapshotSkillSource` (§12).
 */

import type { LoadedSkillBody, SkillCatalogEntry, SkillSource } from "../src/agents/main/skill-source.js";

/** One in-test skill: a resolved SKILL.md (body + optional reference files). */
export interface TestSkill {
  name: string;
  description: string;
  content: string;
  /** `references/<file>.md` → body (the third disclosure level). */
  references?: Record<string, string>;
}

/** Wrap test skills into a `SkillSource` (same semantics as the snapshot source). */
export function testSkillSource(skills: readonly TestSkill[]): SkillSource {
  const byName = new Map(skills.map((s) => [s.name, s] as const));
  const entries: SkillCatalogEntry[] = skills.map((s) => ({
    name: s.name,
    description: s.description,
    hasReferences: !!s.references && Object.keys(s.references).length > 0,
  }));
  return {
    catalog: () => entries,
    load: (name): LoadedSkillBody | undefined => {
      const skill = byName.get(name);
      if (skill === undefined) return undefined;
      return { content: skill.content, references: Object.keys(skill.references ?? {}) };
    },
    loadReference: (name, path) => byName.get(name)?.references?.[path],
  };
}
