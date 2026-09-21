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
 * Playground project state under `<project>/.aep-playground/` (a dot-dir, so
 * it is invisible to engineering-agent turns by construction), plus the
 * global recent-projects list under the user's home dir.
 *
 * `project.json` holds what a fresh process needs to resume: the conversation
 * uuid (ONE `general` conversation per project — console parity), the issue
 * counter (task 6), and the last-folded spec hash that drives production's
 * D20 `filesChangedExternally` signal.
 */

import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, sep } from "node:path";
import { randomUUID } from "node:crypto";
import { projectDirError } from "../paths.js";

export const STATE_DIR = ".aep-playground";

export interface ProjectState {
  /** Fence-valid slug (from the directory name at init time). */
  slug: string;
  /** The uuid segment of the project's single `general` conversation. */
  conversationUuid: string;
  /** Next issue file number to allocate (tasks phase). */
  nextIssueNumber: number;
  /** Content hash of the spec files map after the last fold (D20 signal). */
  lastFoldedHash?: string;
  /** The user confirmed coding runs may write this directory (§12 first-run confirm). */
  codingConfirmed?: boolean;
  /** The user confirmed `wire` may build and run this project's images locally. */
  wireConfirmed?: boolean;
}

export function stateDir(projectDir: string): string {
  return join(projectDir, STATE_DIR);
}

export function conversationsDir(projectDir: string): string {
  return join(stateDir(projectDir), "conversations");
}

function stateFile(projectDir: string): string {
  return join(stateDir(projectDir), "project.json");
}

/** Load the project state, initializing it (and the dot-dir) on first touch. */
export function loadProjectState(projectDir: string, slug: string): ProjectState {
  const file = stateFile(projectDir);
  if (existsSync(file)) {
    const raw = JSON.parse(readFileSync(file, "utf8")) as Partial<ProjectState>;
    return {
      slug: raw.slug ?? slug,
      conversationUuid: raw.conversationUuid ?? randomUUID(),
      nextIssueNumber: raw.nextIssueNumber ?? 1,
      ...(raw.lastFoldedHash ? { lastFoldedHash: raw.lastFoldedHash } : {}),
      ...(raw.codingConfirmed ? { codingConfirmed: true } : {}),
      ...(raw.wireConfirmed ? { wireConfirmed: true } : {}),
    };
  }
  return { slug, conversationUuid: randomUUID(), nextIssueNumber: 1 };
}

export function saveProjectState(projectDir: string, state: ProjectState): void {
  mkdirSync(stateDir(projectDir), { recursive: true });
  const file = stateFile(projectDir);
  const tmp = `${file}.tmp`;
  writeFileSync(tmp, JSON.stringify(state, null, 2), "utf8");
  renameSync(tmp, file);
}

// --- Recent projects (global, ~/.aep-playground/recents.json) ---------------

const RECENTS_LIMIT = 10;

function recentsFile(): string {
  return join(homedir(), ".aep-playground", "recents.json");
}

export function listRecentProjects(): string[] {
  const file = recentsFile();
  if (!existsSync(file)) return [];
  try {
    const raw = JSON.parse(readFileSync(file, "utf8")) as unknown;
    if (!Array.isArray(raw)) return [];
    // Prune vanished dirs AND fence-illegal entries (a pre-fence run could
    // have remembered the repo checkout itself) — the next rememberProject
    // write persists the pruned list.
    return raw.filter((p): p is string => typeof p === "string" && existsSync(p) && projectDirError(p) === null);
  } catch {
    return [];
  }
}

export function rememberProject(projectDir: string): void {
  // Temp-dir projects (mock-model tests, throwaway probes) are not recents —
  // and tests must never rewrite the user's real list.
  if (projectDir.startsWith(tmpdir() + sep)) return;
  const list = [projectDir, ...listRecentProjects().filter((p) => p !== projectDir)].slice(0, RECENTS_LIMIT);
  mkdirSync(join(homedir(), ".aep-playground"), { recursive: true });
  writeFileSync(recentsFile(), JSON.stringify(list, null, 2), "utf8");
}
