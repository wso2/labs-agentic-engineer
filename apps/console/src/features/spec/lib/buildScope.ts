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

// Display-only reads of the build scope for the "Cut version" ceremony
// (#369/#372). The BACKEND is the authority — it computes the real scope at
// the tag it cuts and its gate refuses an incomplete design; these parsers
// only preview the same facts from the draft so the drawer can say what the
// click will do.

/** The PRD's story numbers from its "## User Stories" numbered list
 *  ([] when unparsable). Mirrors the backend's parsePRDStories — the drawer
 *  must preview exactly the scope the gate will compute. */
export function parsePrdStories(prd: string): number[] {
  const out = new Set<number>();
  let inSection = false;
  for (const raw of prd.split(/\r?\n/)) {
    const line = raw.trim();
    if (line.startsWith("## ")) {
      inSection = /^##\s+user stories\s*$/i.test(line);
      continue;
    }
    if (!inSection) continue;
    const m = /^(\d+)\.\s+\S/.exec(line);
    if (m) {
      const n = Number(m[1]);
      if (Number.isInteger(n) && n > 0) out.add(n);
    }
  }
  return [...out].sort((a, b) => a - b);
}

/** The predictive next version label: v<latest+1>, or v1 with no tags yet.
 *  The backend assigns the real number at cut time — display only. */
export function nextVersionLabel(latestTag: string | undefined | null): string {
  const m = latestTag ? /^v(\d+)$/.exec(latestTag) : null;
  return m ? `v${Number(m[1]) + 1}` : "v1";
}
