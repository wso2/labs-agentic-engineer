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
 * The read-only task-plan context, parsed from the turn's `files` snapshot. ONE
 * definition of the convention documented in `contracts/task-tools.ts`: the
 * design agent's accumulator validates against these, and the Go assembler (phase
 * 2) renders `tasks/<issueNumber>.md` to match them. Pure, no I/O.
 */

import { parse as parseYaml } from "yaml";
import { FRONTMATTER_RE, lf } from "./bundle.js";
import type { TaskContextFile, TaskOrigin } from "./contracts/task-tools.js";

/** Matches any file under a design component dir, capturing the component name. */
export const COMPONENT_DIR_RE = /^specs\/design\/components\/([^/]+)\//;

/** Matches an existing-Task rendering path, capturing its issue number. */
export const TASK_CONTEXT_FILE_RE = /^tasks\/(\d+)\.md$/;

const ORIGINS: readonly TaskOrigin[] = ["spec-plan", "incident", "manual"];

/**
 * The known components: the directory names under `specs/design/components/`
 * present in the snapshot, sorted and de-duplicated. A component "needs work"
 * iff it has a design directory — its files are what justify a Task.
 */
export function parseKnownComponents(files: Record<string, string>): string[] {
  const names = new Set<string>();
  for (const path of Object.keys(files)) {
    const m = COMPONENT_DIR_RE.exec(path);
    if (m) names.add(m[1]!);
  }
  return [...names].sort();
}

/**
 * Parse one `tasks/<issueNumber>.md` rendering into a `TaskContextFile`, or
 * `null` when the path is not a task rendering or the frontmatter is unusable
 * (missing the required `component`/`title`, or no parseable issue number). A
 * malformed rendering degrades gracefully: the caller skips it, so it simply
 * isn't addressable — it never crashes the turn.
 */
export function parseTaskContextFile(path: string, content: string): TaskContextFile | null {
  const pathMatch = TASK_CONTEXT_FILE_RE.exec(path);
  if (!pathMatch) return null;

  const text = lf(content);
  const fence = FRONTMATTER_RE.exec(text);
  if (!fence) return null;

  let fm: Record<string, unknown>;
  try {
    const parsed = parseYaml(fence[1]!);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    fm = parsed as Record<string, unknown>;
  } catch {
    return null;
  }

  // issueNumber: prefer the frontmatter value, fall back to the filename number.
  const fmIssue = typeof fm.issueNumber === "number" && Number.isInteger(fm.issueNumber) ? fm.issueNumber : undefined;
  const issueNumber = fmIssue ?? Number(pathMatch[1]);
  const component = str(fm.component);
  const title = str(fm.title);
  if (component === undefined || title === undefined) return null;

  return {
    issueNumber,
    component,
    title,
    dependsOn: strList(fm.dependsOn),
    origin: origin(fm.origin),
    ...opt("specTag", str(fm.specTag)),
    ...opt("designTag", str(fm.designTag)),
    body: text.slice(fence[0]!.length),
  };
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() !== "" ? v : undefined;
}

function strList(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((e): e is string => typeof e === "string") : [];
}

function origin(v: unknown): TaskOrigin {
  return typeof v === "string" && (ORIGINS as readonly string[]).includes(v) ? (v as TaskOrigin) : "spec-plan";
}

function opt<K extends string>(key: K, value: string | undefined): Record<K, string> | Record<string, never> {
  return value === undefined ? {} : ({ [key]: value } as Record<K, string>);
}
