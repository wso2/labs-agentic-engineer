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

// The front-matter block INCLUDING both `---` delimiters, so a reassembled
// document keeps the original's exact opening bytes.
const FRONT_MATTER = /^---\r?\n[\s\S]*?\r?\n---\r?\n/;

/**
 * Put an edited prompt body back into an `agent.afm.md` document.
 *
 * The front matter is carried over from `raw` VERBATIM rather than
 * re-serialised from parsed YAML. Two reasons, and both are load-bearing:
 *
 *  - `raw` is read at SAVE time, so front matter the agent rewired while the
 *    prompt was being edited survives — only the prose is replaced.
 *  - Re-serialising would reformat a block nobody edited, and that block
 *    carries `${env:}` wiring and the tool allow-list, where a formatting
 *    change is indistinguishable from a real one in review.
 */
export function reassembleAfm(raw: string, body: string): string {
  const head = FRONT_MATTER.exec(raw);
  // Throws rather than returning a sentinel: a caller that ignored the result
  // would write a document with no front matter at all, which is a worse
  // outcome than a failed save the author can see and retry.
  if (!head) throw new Error("This document has no front matter to preserve.");
  return `${head[0]}\n${body.trim()}\n`;
}

/** True when `raw` has a front-matter block this can write back into. */
export function hasFrontMatter(raw: string): boolean {
  return FRONT_MATTER.test(raw);
}
