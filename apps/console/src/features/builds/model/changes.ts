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

import { lineWords } from "../../spec/model/designWork";
import { parseLine, type LineBlock } from "../../spec/model/ids";
import type { BuiltLine, ProjectBuild } from "../api/builds";

// What changed in a feature's spec since it was last built. A build keeps
// each feature's lines as it built them; the picker compares them with the
// live spec. A line with its own ID (a story, "F2.4") is followed by its ID,
// so rewording it is an edit; a line without one (a decision) is known only
// by its words, so rewording it retires the old line and adds a new one.
// Headings are not the spec, and dropping an `*assumed*` tag changes no
// words: neither is a change.

/** A feature file's lines as a build reads them. */
export function builtLines(lines: LineBlock[]): BuiltLine[] {
  return lines
    .filter((l) => l.kind !== "heading")
    .map((l) => ({ id: parseLine(l.text, l.emphasis).lead?.id ?? null, words: lineWords(l) }))
    .filter((l) => l.words.length > 0);
}

export interface LineChanges {
  added: number;
  edited: number;
  retired: number;
}

/** A line with its own ID whose words changed since the build, with the words it was built with. */
export interface EditedLine {
  id: string;
  was: string;
  now: string;
}

/** What changed in a feature's lines since the build, line by line, in file order. */
export interface FeatureChanges {
  added: BuiltLine[];
  edited: EditedLine[];
  retired: BuiltLine[];
}

/** The lines of `a` left over once each line of `b` with the same words has claimed one. */
function unmatched(a: BuiltLine[], b: BuiltLine[]): BuiltLine[] {
  const left = new Map<string, number>();
  for (const l of b) left.set(l.words, (left.get(l.words) ?? 0) + 1);
  return a.filter((l) => {
    const k = left.get(l.words) ?? 0;
    if (k > 0) left.set(l.words, k - 1);
    return k === 0;
  });
}

export function featureChanges(built: BuiltLine[], now: BuiltLine[]): FeatureChanges {
  const then = new Map(built.flatMap((l) => (l.id ? [[l.id, l.words] as const] : [])));
  const current = new Set(now.flatMap((l) => (l.id ? [l.id] : [])));
  const free = (lines: BuiltLine[]) => lines.filter((l) => !l.id);
  const freeAdded = new Set(unmatched(free(now), free(built)));
  const freeRetired = new Set(unmatched(free(built), free(now)));
  const added: BuiltLine[] = [];
  const edited: EditedLine[] = [];
  for (const l of now) {
    if (!l.id) {
      if (freeAdded.has(l)) added.push(l);
      continue;
    }
    const was = then.get(l.id);
    if (was === undefined) added.push(l);
    else if (was !== l.words) edited.push({ id: l.id, was, now: l.words });
  }
  const retired = built.filter((l) => (l.id ? !current.has(l.id) : freeRetired.has(l)));
  return { added, edited, retired };
}

export function lineChanges(built: BuiltLine[], now: BuiltLine[]): LineChanges {
  const changes = featureChanges(built, now);
  return { added: changes.added.length, edited: changes.edited.length, retired: changes.retired.length };
}

/** The build that last carried a feature, and its lines as built; null before its first. */
export function lastBuildOf(builds: ProjectBuild[], featureId: string): { version: string; lines: BuiltLine[] } | null {
  for (const b of [...builds].reverse()) {
    const f = b.features.find((x) => x.id === featureId);
    if (f) return { version: b.version, lines: f.lines };
  }
  return null;
}

/** "1 added, 2 edited"; empty when nothing changed. */
export function changeWords(changes: LineChanges): string {
  return [
    changes.added && `${changes.added} added`,
    changes.edited && `${changes.edited} edited`,
    changes.retired && `${changes.retired} retired`,
  ]
    .filter(Boolean)
    .join(", ");
}
