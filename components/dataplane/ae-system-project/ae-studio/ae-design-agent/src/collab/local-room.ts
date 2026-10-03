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
 * The pod's Room join (07 §9): ae-collab's local listener
 * (`AE_COLLAB_LOCAL_URL`, `ws://127.0.0.1:8091`), a room token minted by
 * ae-studio-tools for the `ae-studio-<org>` client (`POST /room-token` on the
 * MCP socket) and asked again at every connect, the Room
 * `spec-<orgHandle>-<project>`, and the credited user as the `credit`
 * connection parameter. The local listener credits the Room's participant
 * to that user and refuses a credit without a name, so a credit that names
 * no one falls back to the user id.
 */

import type { ToolsSocket } from "../tools-socket/client.js";
import { joinRoom, type RoomPeer } from "./room-peer.js";

/** Who a Room join is credited to (the turn's credit). */
export interface RoomCredit {
  userId: string;
  name: string;
  email: string;
}

export interface LocalRoomConfig {
  /** `AE_COLLAB_LOCAL_URL`. */
  url: string;
  /** `AE_ORG_HANDLE`: the Room ids are the org's. */
  orgHandle: string;
  tools: Pick<ToolsSocket, "roomToken">;
}

/** The `credit` parameter: `{name, email}` with a name (`name || userId`). */
export function creditParameter(credit: RoomCredit): string {
  return JSON.stringify({ name: credit.name.trim() || credit.userId, email: credit.email });
}

/** Joins a project's Room on the local listener, as the credited user. */
export function localRoomJoiner(cfg: LocalRoomConfig): (project: string, credit: RoomCredit) => Promise<RoomPeer> {
  return (project, credit) =>
    joinRoom({
      url: cfg.url,
      roomId: `spec-${cfg.orgHandle}-${project}`,
      token: () => cfg.tools.roomToken(),
      parameters: { credit: creditParameter(credit) },
    });
}
