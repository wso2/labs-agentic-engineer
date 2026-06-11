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

import type { SkillKind } from '../../services/api/orgSkills';

/** Chip color per skill kind — matches the Oxygen UI Chip color enum. */
export function kindChipColor(kind: SkillKind): 'primary' | 'success' | 'info' {
  switch (kind) {
    case 'builtin':
      return 'primary';
    case 'custom':
      return 'success';
    case 'imported':
      return 'info';
  }
}

/** Human label per kind. */
export function kindLabel(kind: SkillKind): string {
  switch (kind) {
    case 'builtin':
      return 'Built-in';
    case 'custom':
      return 'Custom';
    case 'imported':
      return 'Imported';
  }
}

/** Starter SKILL.md template prefilled into the editor for a new custom skill. */
export function newSkillTemplate(name: string): string {
  const slug = name || 'my-custom-skill';
  return `---
name: ${slug}
description: One-line summary of what this skill does and when an agent should apply it.
metadata:
  asdlc.version: "1"
---

# ${slug}

## What this skill does

Describe the capability and when agents should apply it.

## Recommended practice

(Architect)
- ...

(Tech-lead — issue body bullets)
- ...

(Coding agent — implementation)
- ...
`;
}
