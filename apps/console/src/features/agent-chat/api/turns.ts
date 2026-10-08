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

// A project turn's transport on the org's design agent (ae-design-agent `/v1`
// in the AE Studio pod): start one, find the running one, read one's status,
// and open its SSE stream. The pod parses `/<command>` instructions itself, so
// the body goes as the chat built it. Chat attachments (multipart) and
// anchors are not in this app yet.

import type { components } from "../../../generated/ae-design-agent";
import { designAgentCall, PodRequestError } from "../../../api/aeStudio";
import { retryAfterMs } from "../../../api/errors";
import type { TurnBody } from "../turnScope";

export type TurnStatus = components["schemas"]["TurnStatus"];
type TurnConflict = components["schemas"]["TurnConflict"];

/**
 * A turn's status read: the status, `"gone"` when the pod no longer holds the
 * turn (404, past its retention or lost with a restart: no later read will
 * find it), or null when it could not be read now (a 503, no answer, AE Studio
 * not ready), which a later read may get past.
 */
export type TurnRead = TurnStatus | "gone" | null;

const UNREACHABLE = "Couldn't reach the agent";

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

/** A failed pod answer as the chat says it: the pod's words, or AE Studio restarting for a 503. */
export function podFailure(error: unknown, response: Response, fallback: string): PodRequestError {
  return new PodRequestError(error, fallback, { status: response.status, retryAfterMs: retryAfterMs(response) });
}

/** Start a turn in the project's conversation; resolves with its id (202). */
export async function startTurn(projectName: string, conversationId: string, body: TurnBody): Promise<string> {
  const { data, error, response } = await designAgentCall(UNREACHABLE, (agent) =>
    agent.POST("/projects/{projectName}/conversations/{conversationId}/turns", {
      params: { path: { projectName, conversationId } },
      body,
    }),
  );
  if (data !== undefined) return data.turnId;
  if (response.status === 409) {
    // The plain TurnConflict (turn_in_progress / conversation_rotated, #430);
    // any other 409 is a problem (no_default_key) and reads by its words.
    const conflict = error as Partial<TurnConflict> | undefined;
    if (conflict?.code === "conversation_rotated") throw new ConversationRotatedError();
    if (conflict?.code === "turn_in_progress") throw new TurnInProgressError(conflict.activeTurnId);
  }
  throw podFailure(error, response, UNREACHABLE);
}

/** The project's running turn, or null (204, or the read failed). */
export async function getActiveTurn(projectName: string): Promise<TurnStatus | null> {
  try {
    const { data } = await designAgentCall(UNREACHABLE, (agent) =>
      agent.GET("/projects/{projectName}/turns/active", { params: { path: { projectName } } }),
    );
    return data ?? null;
  } catch {
    return null; // not ready, or no answer: no running turn to show from here
  }
}

/** One turn's status (see TurnRead). */
export async function getTurn(projectName: string, turnId: string): Promise<TurnRead> {
  try {
    const { data, response } = await designAgentCall(UNREACHABLE, (agent) =>
      agent.GET("/projects/{projectName}/turns/{turnId}", { params: { path: { projectName, turnId } } }),
    );
    if (data !== undefined) return data;
    return response.status === 404 ? "gone" : null;
  } catch {
    return null;
  }
}

/**
 * The turn-stream attach failed before any byte arrived. `status` tells a 404
 * (the pod does not stream that turn) from a 409 `replay_truncated` (a running
 * turn that overflowed its replay buffer) and any other failure (0: no answer).
 */
export class TurnStreamAttachError extends Error {
  readonly status: number;
  readonly code: string | undefined;
  constructor(status: number, code?: string) {
    super("Couldn't attach to the agent's stream");
    this.name = "TurnStreamAttachError";
    this.status = status;
    this.code = code;
  }
}

export function isTurnStreamNotFound(err: unknown): boolean {
  return err instanceof TurnStreamAttachError && err.status === 404;
}

/** The pod refused a replay of a running turn as truncated: read its status until it ends instead. */
export function isTurnStreamReplayTruncated(err: unknown): boolean {
  return err instanceof TurnStreamAttachError && err.status === 409 && err.code === "replay_truncated";
}

/**
 * Open a turn's SSE stream as raw bytes: a replay from frame `from`, then the
 * live tail. The caller reads it with @aep/agent-stream's parseSseFrames.
 */
export async function openTurnStream(
  projectName: string,
  turnId: string,
  from: number,
  signal: AbortSignal,
): Promise<ReadableStream<Uint8Array>> {
  let result;
  try {
    result = await designAgentCall(UNREACHABLE, (agent) =>
      agent.GET("/projects/{projectName}/turns/{turnId}/stream", {
        params: { path: { projectName, turnId }, query: { from } },
        parseAs: "stream",
        signal,
      }),
    );
  } catch (err) {
    if (signal.aborted) throw err;
    throw new TurnStreamAttachError(0);
  }
  const { data, error, response } = result;
  if (response.ok && data) return data as ReadableStream<Uint8Array>;
  const code = error && typeof error === "object" ? (error as { code?: unknown }).code : undefined;
  throw new TurnStreamAttachError(response.status, typeof code === "string" ? code : undefined);
}
