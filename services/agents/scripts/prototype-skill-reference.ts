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
 * The `prototype` skill's kit reference is not authored: it is the kit's own
 * `reference.md` (`@wso2/prototype-kit`, rendered from the kit's types), pasted
 * between two marker comments in `skills/prototype/SKILL.md`. The skill library
 * ships as plain files an org's repo is seeded from, so the block is written
 * into the file rather than composed at run time. This module is the one
 * reading of where it sits and how the reference is fitted under the skill's
 * own headings, shared by the generator (`generate-prototype-skill.ts`, this
 * package's `gen`) and the freshness test (`test/prototype-skill.test.ts`).
 */

import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** The kit's generated reference (`pnpm --filter @wso2/prototype-kit gen`). */
export const KIT_REFERENCE = createRequire(import.meta.url).resolve("@wso2/prototype-kit/reference.md");

// scripts/ → services/agents → services → the repo's skills/ library.
export const PROTOTYPE_SKILL_PATH = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "skills",
  "prototype",
  "SKILL.md",
);

const START = "<!-- kit:start -->\n";
const END = "<!-- kit:end -->";

/**
 * The reference as the skill carries it: its title and "do not edit" line
 * dropped (the skill has its own), and every heading one level deeper, so the
 * kit's sections nest under the skill's `## The kit`.
 */
export function kitBlock(reference: string): string {
  const lines = reference.split("\n");
  const body = lines.slice(lines.findIndex((l) => l.startsWith("## ")));
  const intro = lines.slice(1).find((l) => l.trim() !== "" && !l.startsWith("Generated from"));
  const demoted = body.map((l) => (l.startsWith("#") ? `#${l}` : l));
  return `${intro ?? ""}\n\n${demoted.join("\n")}`;
}

/** Where the block sits in `skill`: the offsets just inside the two markers. */
function blockBounds(skill: string): { from: number; to: number } {
  const start = skill.indexOf(START);
  const end = skill.indexOf(END);
  if (start < 0 || end < start || skill.indexOf(START, start + 1) >= 0) {
    throw new Error(`the prototype skill must carry exactly one "${START.trim()}" … "${END}" block, in that order`);
  }
  return { from: start + START.length, to: end };
}

/** The kit block as the skill currently carries it. */
export function kitBlockOf(skill: string): string {
  const { from, to } = blockBounds(skill);
  return skill.slice(from, to);
}

/** `skill` with its kit block replaced by the one made from `reference`. */
export function withKitReference(skill: string, reference: string): string {
  const { from, to } = blockBounds(skill);
  return skill.slice(0, from) + kitBlock(reference) + skill.slice(to);
}
