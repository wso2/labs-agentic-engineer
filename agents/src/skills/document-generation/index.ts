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

import type { DocumentGenerationSkill } from "./types.js";
import { requirementsFromPrompt } from "./requirements-from-prompt.js";
import { functionalRequirements } from "./functional-requirements.js";
import { nonFunctionalRequirements } from "./non-functional-requirements.js";
import { userStories } from "./user-stories.js";
import { wireframes } from "./wireframes.js";
import { domainModel } from "./domain-model.js";
import { componentDesign } from "./component-design.js";
import { componentOpenApi } from "./component-openapi.js";

const SKILLS: DocumentGenerationSkill[] = [
  requirementsFromPrompt,
  functionalRequirements,
  nonFunctionalRequirements,
  userStories,
  wireframes,
  domainModel,
  componentDesign,
  componentOpenApi,
];

const SKILLS_BY_ID = new Map<string, DocumentGenerationSkill>(
  SKILLS.map((s) => [s.id, s]),
);

export function getDocumentGenerationSkill(
  id: string,
): DocumentGenerationSkill | undefined {
  return SKILLS_BY_ID.get(id);
}

export function listDocumentGenerationSkills(): DocumentGenerationSkill[] {
  return [...SKILLS];
}

export type { DocumentGenerationSkill, SkillInput } from "./types.js";
