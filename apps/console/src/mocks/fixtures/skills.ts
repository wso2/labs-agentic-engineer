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

import type { components } from "../../generated/aep-api";

type SkillUpdate = components["schemas"]["SkillUpdate"];

// The org's skill library in mock mode: one of each thing the Skills Page
// tells apart. The platform's own (one the runs need), org skills the
// platform still tracks (one with an update to take, one both changed), one
// the org wrote, and an imported one that is disabled.

export interface MockSkill {
  kind: "platform" | "org" | "imported";
  skillMd: string;
  references: Record<string, string>;
  enabled: boolean;
  required: boolean;
}

/** A SKILL.md: its name and description, then the body. */
export function md(name: string, description: string, body: string): string {
  return `---\nname: ${name}\ndescription: ${description}\n---\n\n${body}`;
}

export const seedSkills: Record<string, MockSkill> = {
  aep: {
    kind: "platform",
    required: true,
    enabled: true,
    references: {},
    skillMd: md(
      "aep",
      "The coding run's workflow: take a task, build it, verify it, open the pull request.",
      "# The coding run\n\nYou build one task at a time.\n\n## Steps\n\n1. Read the task and the design it points to.\n2. Build it, with tests.\n3. Verify it against the acceptance criteria.\n4. Open the pull request.\n",
    ),
  },
  architecture: {
    kind: "platform",
    required: false,
    enabled: true,
    references: {},
    skillMd: md(
      "architecture",
      "How the design agent splits a product into components.",
      "# Architecture\n\nSplit only where the parts deploy and evolve apart.\n\n| Signal | Means |\n|---|---|\n| Different users and lifecycles | two web applications |\n| A long-running job | a worker beside the API |\n",
    ),
  },
  go: {
    kind: "org",
    required: false,
    enabled: true,
    references: { "references/layout.md": "# Layout\n\ncmd/, internal/.\n" },
    skillMd: md(
      "go",
      "Go services, the house way: the standard library first.",
      "# Go\n\nUse the standard library before reaching for a framework.\n\n## Errors\n\n- Wrap with `%w`.\n- Never panic in a handler.\n",
    ),
  },
  "react-webapp": {
    kind: "org",
    required: false,
    enabled: true,
    references: {},
    skillMd: md(
      "react-webapp",
      "React web apps on Vite, with the organization's design system.",
      "# React web apps\n\nVite, TypeScript, and Oxygen UI for every component.\n\n**Our rule:** no other component kit.\n",
    ),
  },
  "expense-policy": {
    kind: "org",
    required: false,
    enabled: true,
    references: {},
    skillMd: md(
      "expense-policy",
      "Acme's expense policy, for any product that handles claims.",
      "# Expense policy\n\n- Receipts over $25 need a photo.\n- A manager approves every claim.\n",
    ),
  },
  "pdf-tools": {
    kind: "imported",
    required: false,
    enabled: false,
    references: {},
    skillMd: md("pdf-tools", "Read and write PDF files.", "# PDF tools\n\nUse `pdf-lib` to fill forms.\n"),
  },
};

/** What the platform reports against the seed: one to take, one both changed. */
export const seedSkillUpdates: SkillUpdate[] = [
  { name: "go", state: "update" },
  { name: "react-webapp", state: "conflict" },
];
