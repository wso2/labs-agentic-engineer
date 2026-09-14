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

// The room is the pane's source for a spec file while collab is up, and the
// server reseeds it from git only on load. A write the platform commits
// OUTSIDE the room — the dependency definition view's two (ADR-0028) — leaves
// the room's copy behind, and the pane would keep showing the file as it was.
// So the console that asked for the write brings the room's copy up to date
// from HEAD itself. The committer then sees a doc that matches git: its next
// flush re-applies byte-identical content over the moved base, which the
// Files API answers without a commit.

import type * as Y from "yjs";
import { fetchSpecFileContent } from "../api/queries";
import { applyTextareaValue } from "./textareaBinding";

/**
 * Replace the room's copy of `path` with what git holds at HEAD. Returns
 * false when the room does not carry the path — the pane reads git for
 * those already, so there is nothing to refresh.
 */
export async function refreshRoomCopy(
  projectName: string,
  getFileText: (path: string) => Y.Text | null,
  path: string,
): Promise<boolean> {
  const ytext = getFileText(path);
  if (!ytext) return false;
  const latest = await fetchSpecFileContent(projectName, { path, sha: "" });
  applyTextareaValue(ytext, latest.content);
  return true;
}
