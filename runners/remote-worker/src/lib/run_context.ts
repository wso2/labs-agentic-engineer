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

// The evidence of what a run's agents were GIVEN, in one shape for both
// runtimes: the skills notice at run start, the exact prompt appendix, and one
// line per session saying which agent it was, whether the appendix reached it,
// and which skills it loaded.
//
// Files beside `runtime.log` (`TaskLog.dir`), never the feed: the appendix is
// tens of kilobytes of skill text. It holds nothing but the mirror's SKILL.md
// bodies and the runtime's constant glossary — no environment value is ever
// interpolated into it — so it is written on every run, not only debug ones.
//
// The OpenCode guard plugin bundles this module, so it imports node built-ins
// only.

import fs from "node:fs";
import path from "node:path";

/** The exact `RuntimePolicy.skills.preloadBodies` a run started with. */
export const PROMPT_APPENDIX_FILE = "prompt-appendix.md";

/** One JSON line per session fact (`SessionContextRecord`). */
export const SESSION_CONTEXT_FILE = "session-context.jsonl";

/**
 * A session was seen: which agent it runs, and whether the appendix was in the
 * system prompt of its first model call.
 */
export interface SessionSeen {
  session: string;
  agent: string;
  appendix: boolean;
}

/** A session asked for a skill through the runtime's skill tool. */
export interface SkillLoaded {
  session: string;
  skill: string;
}

export type SessionContextRecord = SessionSeen | SkillLoaded;

export function writePromptAppendix(logDir: string, appendix: string): string {
  fs.mkdirSync(logDir, { recursive: true });
  const file = path.join(logDir, PROMPT_APPENDIX_FILE);
  fs.writeFileSync(file, appendix);
  return file;
}

/** Best effort: evidence must never fail the call it describes. */
export function appendSessionContext(file: string, record: SessionContextRecord): void {
  try {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.appendFileSync(file, JSON.stringify({ ts: new Date().toISOString(), ...record }) + "\n");
  } catch {
    // a lost evidence line is not worth a failed tool call
  }
}

/** Names are listed while the catalog is this short; past it, the count stands alone. */
const LISTED_AVAILABLE = 24;

/**
 * The run-start line naming what the lead's context was assembled from.
 *
 *   [skills] workflow: aep · pinned: ballerina, openapi-conventions · 19 available: a, b, …
 */
export function skillsNotice(workflow: readonly string[], pinned: readonly string[], available: readonly string[]): string {
  const list = (names: readonly string[]): string => (names.length > 0 ? names.join(", ") : "none");
  const catalog =
    available.length > 0 && available.length <= LISTED_AVAILABLE
      ? `${available.length} available: ${available.join(", ")}`
      : `${available.length} available`;
  return `[skills] workflow: ${list(workflow)} · pinned: ${list(pinned)} · ${catalog}`;
}
