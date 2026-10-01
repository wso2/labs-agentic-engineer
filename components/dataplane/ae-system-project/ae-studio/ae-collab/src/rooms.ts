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

// Per-room committer state (#133): the baseline the flush diffs against
// (seeded/last-applied content + blob shas, room-keyed paths) and the
// session's participants for Co-authored-by trailers. Lives for a room's
// loaded lifetime; dropped on unload (git is the durable truth again).

export interface BaselineEntry {
  content: string;
  /** Blob sha backing the next apply's baseSha; "" = not in git yet. */
  sha: string;
}

export interface RoomState {
  projectName: string;
  baseline: Map<string, BaselineEntry>;
  /** Session participants keyed by email — the commit trailers. */
  participants: Map<string, { name: string; email: string }>;
  /** The most recent participant's bearer — the forced unload flush has no
   *  connection context left to read a token from. */
  lastToken: string | null;
}

const rooms = new Map<string, RoomState>();

export function roomState(documentName: string): RoomState | undefined {
  return rooms.get(documentName);
}

export function ensureRoomState(
  documentName: string,
  projectName: string,
): RoomState {
  const existing = rooms.get(documentName);
  if (existing) return existing;
  const fresh: RoomState = {
    projectName,
    baseline: new Map(),
    participants: new Map(),
    lastToken: null,
  };
  rooms.set(documentName, fresh);
  return fresh;
}

export function addParticipant(
  documentName: string,
  user: { name: string; email: string },
): void {
  const state = rooms.get(documentName);
  if (!state) return;
  // Thunder access tokens carry no email claim — derive the platform's
  // stable noreply address (the genai author-identity convention) so the
  // Co-authored-by trailer is always well-formed.
  const name = user.name.trim() || "collab-user";
  const email = user.email.trim() || noreplyEmail(name);
  state.participants.set(email, { name, email });
}

function noreplyEmail(name: string): string {
  const local = name.replace(/[^a-zA-Z0-9._-]/g, "") || "aep-user";
  return `${local}@users.noreply.aep.dev`;
}

export function dropRoomState(documentName: string): void {
  rooms.delete(documentName);
}
