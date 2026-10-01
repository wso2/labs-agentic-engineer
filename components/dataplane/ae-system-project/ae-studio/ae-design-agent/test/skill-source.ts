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

import type { LoadedReference, LoadedSkillBody, SkillAudience, SkillCatalogEntry, SkillSource } from "../src/agents/main/skill-source.js";
import { ALL_AUDIENCES } from "../src/agents/main/skill-source.js";

/** One in-test skill: a resolved SKILL.md (body + optional aux files). */
export interface TestSkill {
  name: string;
  description: string;
  content: string;
  /** aux path (e.g. `references/<file>.md`, `scripts/run.mjs`) → body (the third disclosure level). */
  references?: Record<string, string>;
  /** aux paths that resolve to a binary marker instead of text (for `loadSkillReference` binary-guard tests). */
  binaryReferences?: readonly string[];
  /** Audiences allowed to load it; omitted means every audience, as an unmarked SKILL.md does. */
  audience?: readonly SkillAudience[];
}

/** Wrap test skills into a `SkillSource` (same semantics as the snapshot source). */
export function testSkillSource(skills: readonly TestSkill[]): SkillSource {
  const byName = new Map(skills.map((s) => [s.name, s] as const));
  const entries: SkillCatalogEntry[] = skills.map((s) => ({
    name: s.name,
    description: s.description,
    hasReferences: !!s.references && Object.keys(s.references).length > 0,
    audience: s.audience ?? ALL_AUDIENCES,
  }));
  return {
    catalog: () => entries,
    load: (name): LoadedSkillBody | undefined => {
      const skill = byName.get(name);
      if (skill === undefined) return undefined;
      return { content: skill.content, references: Object.keys(skill.references ?? {}).sort() };
    },
    loadReference: (name, path): LoadedReference => {
      const skill = byName.get(name);
      if (skill?.binaryReferences?.includes(path)) return { binary: true };
      const content = skill?.references?.[path];
      return content === undefined ? undefined : { content };
    },
  };
}
