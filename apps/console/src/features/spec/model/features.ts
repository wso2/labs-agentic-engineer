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

import type { FeatureStage, SpecFeature } from "../api/specModel";
import type { LineBlock } from "./ids";
import { readRequirements } from "./requirements";

// The product's features, worked out from the live documents (N5): each
// feature file's ID, name and purpose, and where the feature is in its journey.
// What the documents cannot say comes in from outside: which features a design
// has read (`designedFrom`, from the spec state) and which one an interview is
// running for right now (from the chat's turn).

const NEEDS_LINE = /^needs:/i;

/** A feature's purpose: the first line of its Purpose section that is not its `Needs:` line. */
function purposeOf(lines: readonly LineBlock[]): string {
  let inside = false;
  for (const line of lines) {
    if (line.kind === "heading" && (line.level ?? 1) <= 2) {
      inside = line.text.trim().toLowerCase() === "purpose";
      continue;
    }
    const text = line.text.trim();
    if (inside && line.kind === "paragraph" && text && !NEEDS_LINE.test(text)) return text;
  }
  return "";
}

/**
 * Where a feature is: being interviewed while an interview of it runs; else
 * designed once a design has read it; else interviewed once it has stories;
 * else not interviewed.
 */
function stageOf(id: string, stories: number, designed: boolean, interviewing: string | null): FeatureStage {
  if (interviewing === id) return "Interviewing";
  if (stories === 0) return "Not interviewed";
  return designed ? "Designed" : "Interviewed";
}

export function deriveFeatures(
  lines: ReadonlyMap<string, LineBlock[]>,
  designedFrom: Readonly<Record<string, string>>,
  interviewing: string | null,
): SpecFeature[] {
  return readRequirements(lines).features.map((f) => {
    const path = `specs/requirements/${f.path}`;
    return {
      id: f.id,
      name: f.name,
      path,
      purpose: purposeOf(lines.get(path) ?? []),
      stage: stageOf(f.id, f.stories.length, designedFrom[f.id] !== undefined, interviewing),
    };
  });
}
