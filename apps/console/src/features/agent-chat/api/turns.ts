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

// A project turn's transport: start one, find the running one, read one's
// status, and open its SSE stream. Copied from the old console's agent-chat
// (api/turns.ts) down to what the chat store uses; the old console's multipart
// attachments and anchors are not in this app yet.

import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";
import type { TurnBody } from "../turnScope";

export type TurnStatus = components["schemas"]["TurnStatus"];
type TurnConflict = components["schemas"]["TurnConflict"];

/**
 * The server refused the turn because one is already running for the project
 * (409 `turn_in_progress`): one turn at a time is the server's rule. It names
 * the running turn when it can, so the chat can show it instead.
 */
export class TurnInProgressError extends Error {
  readonly activeTurnId: string | undefined;
  constructor(activeTurnId: string | undefined) {
    super("The agent is already working on this project. Your message wasn't sent.");
    this.name = "TurnInProgressError";
    this.activeTurnId = activeTurnId;
  }
}

/**
 * The addressed thread is no longer the project's current one (#430): a
 * teammate started a new one while this client held the old id. Recovery is
 * to resolve the thread again, not to retry into the old one.
 */
export class ConversationRotatedError extends Error {
  constructor() {
    super("The conversation was replaced with a new one. Your message wasn't sent.");
    this.name = "ConversationRotatedError";
  }
}

/** Start a turn in the project's conversation; resolves with its id (202). */
export async function startTurn(projectName: string, conversationId: string, body: TurnBody): Promise<string> {
  const { data, error, response } = await client.POST("/projects/{projectName}/agents/{conversationId}/messages", {
    params: { path: { projectName, conversationId } },
    body,
  });
  if (error || data === undefined) {
    if (response.status === 409) {
      // The pinned TurnConflict: turn_in_progress / requirements_missing /
      // conversation_rotated (#430).
      const conflict = error as Partial<TurnConflict> | undefined;
      if (conflict?.code === "conversation_rotated") throw new ConversationRotatedError();
      if (conflict?.code === "turn_in_progress") throw new TurnInProgressError(conflict.activeTurnId);
    }
    throw new Error(apiErrorMessage(error, "Couldn't reach the agent"));
  }
  return data.turnId;
}

/** The project's running turn, or null (204, or the read failed). */
export async function getActiveTurn(projectName: string): Promise<TurnStatus | null> {
  const { data, error, response } = await client.GET("/projects/{projectName}/turns/active", {
    params: { path: { projectName } },
  });
  if (response.status === 204 || error || data === undefined) return null;
  return data;
}

/** One turn's status, or null when it cannot be read. */
export async function getTurn(projectName: string, turnId: string): Promise<TurnStatus | null> {
  const { data, error } = await client.GET("/projects/{projectName}/turns/{turnId}", {
    params: { path: { projectName, turnId } },
  });
  if (error || data === undefined) return null;
  return data;
}

/**
 * The turn-stream attach failed before any byte arrived. `status` tells a 404
 * (the buffer is not on this replica, or not minted yet) from any other
 * failure.
 */
export class TurnStreamAttachError extends Error {
  readonly status: number;
  constructor(status: number) {
    super("Couldn't attach to the agent's stream");
    this.name = "TurnStreamAttachError";
    this.status = status;
  }
}

export function isTurnStreamNotFound(err: unknown): boolean {
  return err instanceof TurnStreamAttachError && err.status === 404;
}

/**
 * Open a turn's SSE stream as raw bytes: a replay from `from`, then the live
 * tail. The caller reads it with @aep/agent-stream's parseSseStream.
 */
export async function openTurnStream(
  projectName: string,
  turnId: string,
  from: number,
  signal: AbortSignal,
): Promise<ReadableStream<Uint8Array>> {
  const { data, error, response } = await client.GET("/projects/{projectName}/turns/{turnId}/stream", {
    params: { path: { projectName, turnId }, query: { from } },
    parseAs: "stream",
    signal,
  });
  if (error || !data) throw new TurnStreamAttachError(response?.status ?? 0);
  return data as ReadableStream<Uint8Array>;
}
