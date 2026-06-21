// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// Single swap point for where the agents acquire their skills.
//
// The BFF still PUSHES skill bodies in the request payload (the origin of the
// bytes moved to the per-org skills repo — docs/design/skills-repo-storage.md
// §4/§5). The prompt builders read skills through the resolvers here instead
// of off `input.*` directly, so a test can inject a canned skill set via
// `set*Resolver(...)` without constructing a full request or touching the BFF.
//
// Mirrors the module-singleton swap idiom of shared/anthropic-key-resolver.ts.

import type {
  ArchitectInput,
  SkillDescription,
  SkillRecord,
} from "../agents/architect/schema.js";
import type {
  AttachedSkillSummary,
  ResolvedSkill,
  TechLeadDetailItem,
  TechLeadPlanInput,
} from "../agents/tech-lead/schema.js";

// ---- architect -------------------------------------------------------------

export interface ArchitectSkills {
  /** Full-body platform skills, inlined into the prompt. */
  builtinSkills: SkillRecord[];
  /** Manifest-only org skills (name + description). */
  orgSkills: SkillDescription[];
  /** Names of skills currently attached to the project's design. */
  skillsApplied: string[];
}

export type ArchitectSkillsResolver = (input: ArchitectInput) => ArchitectSkills;

const defaultArchitectResolver: ArchitectSkillsResolver = (input) => ({
  builtinSkills: input.builtinSkills ?? [],
  orgSkills: input.orgSkills ?? [],
  skillsApplied: input.skillsApplied ?? [],
});

let architectResolver: ArchitectSkillsResolver = defaultArchitectResolver;

export function resolveArchitectSkills(input: ArchitectInput): ArchitectSkills {
  return architectResolver(input);
}
/** Test-only: override the architect skill source. */
export function setArchitectSkillsResolver(fn: ArchitectSkillsResolver): void {
  architectResolver = fn;
}
/** Test-only: restore the default (read-from-request) source. */
export function resetArchitectSkillsResolver(): void {
  architectResolver = defaultArchitectResolver;
}

// ---- tech-lead: planner phase ----------------------------------------------

export type TechLeadPlanSkillsResolver = (
  input: TechLeadPlanInput,
) => AttachedSkillSummary[];

const defaultPlanResolver: TechLeadPlanSkillsResolver = (input) =>
  input.attachedSkills ?? [];

let planResolver: TechLeadPlanSkillsResolver = defaultPlanResolver;

export function resolveTechLeadPlanSkills(
  input: TechLeadPlanInput,
): AttachedSkillSummary[] {
  return planResolver(input);
}
export function setTechLeadPlanSkillsResolver(
  fn: TechLeadPlanSkillsResolver,
): void {
  planResolver = fn;
}
export function resetTechLeadPlanSkillsResolver(): void {
  planResolver = defaultPlanResolver;
}

// ---- tech-lead: detail phase -----------------------------------------------

export type TechLeadDetailSkillsResolver = (
  item: TechLeadDetailItem,
) => ResolvedSkill[];

const defaultDetailResolver: TechLeadDetailSkillsResolver = (item) =>
  item.skillsResolved ?? [];

let detailResolver: TechLeadDetailSkillsResolver = defaultDetailResolver;

export function resolveTechLeadDetailSkills(
  item: TechLeadDetailItem,
): ResolvedSkill[] {
  return detailResolver(item);
}
export function setTechLeadDetailSkillsResolver(
  fn: TechLeadDetailSkillsResolver,
): void {
  detailResolver = fn;
}
export function resetTechLeadDetailSkillsResolver(): void {
  detailResolver = defaultDetailResolver;
}
