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

// The mock stand-in for a project's collab room and its committer (#817).
//
// In production an agent's room turn edits the room's doc, NOT git; the collab
// committer lands those edits in git later — on its quiet period, on the last
// peer leaving, or at once when a client forces a flush (the path Build and the
// prototype review page await). A mock turn that wrote straight to the files
// would hide exactly the gap the review page must handle: the turn is over and
// git does not have the revision yet.
//
// So a mock room turn writes here; the files API only sees the write once it
// is saved — by `flushMockRoom` (a forced flush), or by the committer's own
// timer if nobody forces one.

import { recordAppliedFiles } from "./fixtures/project";

/** The mock committer's quiet period — short, so a mock session sees it land. */
export const MOCK_COMMIT_DELAY_MS = 10_000;

interface MockRoom {
  writes: Map<string, string>;
  timer: ReturnType<typeof setTimeout> | undefined;
}

const rooms = new Map<string, MockRoom>();

/** An agent's whole-file write into the project's room: live, not yet in git. */
export function writeToMockRoom(projectName: string, path: string, content: string): void {
  const room = rooms.get(projectName) ?? { writes: new Map<string, string>(), timer: undefined };
  rooms.set(projectName, room);
  room.writes.set(path, content);
  clearTimeout(room.timer);
  room.timer = setTimeout(() => flushMockRoom(projectName), MOCK_COMMIT_DELAY_MS);
}

/** Save the room's pending writes to the mock's git now, as one commit. */
export function flushMockRoom(projectName: string): void {
  const room = rooms.get(projectName);
  if (!room) return;
  clearTimeout(room.timer);
  rooms.delete(projectName);
  const writes = [...room.writes].map(([path, content]) => ({ path, content }));
  if (writes.length > 0) recordAppliedFiles(projectName, writes);
}
