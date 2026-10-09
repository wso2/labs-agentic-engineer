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
 * The pod's Room join: ae-collab's Room socket
 * (`AE_ROOM_SOCKET`, a Unix socket on an emptyDir shared by ae-collab and
 * this container only), the Room `spec-<orgHandle>-<project>`, and the
 * credited user as the `credit` connection parameter. No token: socket
 * access is the agent's identity. The Room socket credits the Room's
 * participant to that user and refuses a credit without a name, so a credit
 * that names no one falls back to the user id.
 */

import { joinRoom, type RoomPeer } from "./room-peer.js";

/** Who a Room join is credited to (the turn's credit). */
export interface RoomCredit {
  userId: string;
  name: string;
  email: string;
}

export interface LocalRoomConfig {
  /** `AE_ROOM_SOCKET`: ae-collab's Room socket. */
  socketPath: string;
  /** `AE_ORG_HANDLE`: the Room ids are the org's. */
  orgHandle: string;
}

/** The `credit` parameter: `{name, email}` with a name (`name || userId`). */
export function creditParameter(credit: RoomCredit): string {
  return JSON.stringify({ name: credit.name.trim() || credit.userId, email: credit.email });
}

/** The ws URL of a Unix socket, in `ws`'s `ws+unix:<socket>:<request path>` form. */
function roomSocketUrl(socketPath: string): string {
  return `ws+unix:${socketPath}:/`;
}

/** Joins a project's Room on the Room socket, as the credited user. */
export function localRoomJoiner(cfg: LocalRoomConfig): (project: string, credit: RoomCredit) => Promise<RoomPeer> {
  const url = roomSocketUrl(cfg.socketPath);
  return (project, credit) =>
    joinRoom({
      url,
      roomId: `spec-${cfg.orgHandle}-${project}`,
      parameters: { credit: creditParameter(credit) },
    });
}
