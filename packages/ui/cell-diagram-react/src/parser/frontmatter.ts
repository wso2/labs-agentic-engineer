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
 * Blank out a leading `---` YAML frontmatter block — the platform's
 * `sourceSpec` carrier on the root design.cell — so the statements below it
 * keep their line numbers in diagnostics. Same fence rule as the platform's
 * Go `SplitFrontmatter`: the block opens on the first non-blank line being
 * exactly `---` (surrounding whitespace allowed) and closes on the next line
 * that starts with `---`. An unterminated block is left in place — the
 * grammar surfaces it as unknown statements instead of this helper guessing.
 *
 * Every parser entry point (single-cell `parseCellDsl`, multi-cell
 * `splitCells`) strips through this one function, so both modes agree.
 */
export function stripFrontmatter(source: string): string {
  const lines = source.split(/\r?\n/);
  let first = 0;
  while (first < lines.length && (lines[first] ?? "").trim() === "") first++;
  if (first >= lines.length || (lines[first] ?? "").trim() !== "---") return source;
  for (let end = first + 1; end < lines.length; end++) {
    if ((lines[end] ?? "").startsWith("---")) {
      for (let i = first; i <= end; i++) lines[i] = "";
      return lines.join("\n");
    }
  }
  return source;
}
