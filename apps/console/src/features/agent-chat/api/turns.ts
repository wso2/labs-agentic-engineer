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

import type { components } from "../../../generated/aep-api";
import { client } from "../../../api/client";
import { apiErrorMessage } from "../../../api/errors";

export type TurnStatus = components["schemas"]["TurnStatus"];

/**
 * The addressed thread is no longer the project's current one (#430) — a
 * teammate rotated while this client held a resolved id. Recovery is
 * re-resolve + rehydrate, not a retry into the demoted thread.
 */
export class ConversationRotatedError extends Error {
  constructor() {
    super("The conversation was replaced with a new one — your message was not sent.");
    this.name = "ConversationRotatedError";
  }
}

/**
 * Start a room-scoped agent turn (#86 phase 4 / #130): `collab: true` — the
 * agent joins the project's spec room as a live peer and edits the shared
 * doc; the panel only receives narration + tool results.
 */
export async function startCollabTurn(
  projectName: string,
  conversationId: string,
  instruction: string,
): Promise<string> {
  const { data, error, response } = await client.POST(
    "/projects/{projectName}/agents/{conversationId}/messages",
    {
      params: { path: { projectName, conversationId } },
      body: {
        instruction,
        collab: true,
      },
    },
  );
  if (error || data === undefined) {
    if (response.status === 409) {
      // The 409 body is the pinned TurnConflict: turn_in_progress /
      // requirements_missing / conversation_rotated (#430).
      if ((error as { code?: string } | undefined)?.code === "conversation_rotated") {
        throw new ConversationRotatedError();
      }
      throw new Error("An agent turn is already running for this project — wait for it to finish.");
    }
    throw new Error(apiErrorMessage(error, "Failed to start the agent turn"));
  }
  return data.turnId;
}

export interface ConversationMessageAuthor {
  id: string;
  displayName: string;
}

export interface ConversationMessage {
  role: string;
  content: unknown;
  /** Who sent this message (#130 multi-user threads) — absent for the agent
   *  and for logs from before attribution existed. */
  author?: ConversationMessageAuthor;
}

// The rehydrate response's schema is currently untyped in the contract
// (`schema: {}` for GET .../messages in openapi.yaml — no author field
// defined yet). Read it as an optional unknown-extension field instead of
// regenerating the client: `author` first, `user` as a fallback name, so a
// future contract addition is likely to just work without another change
// here. See task-2 report for the contract-gap note.
function mapAuthor(raw: unknown): ConversationMessageAuthor | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const source =
    (raw as { author?: unknown }).author ?? (raw as { user?: unknown }).user;
  if (typeof source !== "object" || source === null) return undefined;
  const s = source as Record<string, unknown>;
  const id = typeof s.id === "string" ? s.id : undefined;
  const displayName =
    typeof s.displayName === "string"
      ? s.displayName
      : typeof s.name === "string"
        ? s.name
        : undefined;
  if (!id || !displayName) return undefined;
  return { id, displayName };
}

/** Maps one raw history entry, dropping a malformed author rather than throwing. */
export function mapConversationMessage(raw: unknown): ConversationMessage | null {
  if (typeof raw !== "object" || raw === null) return null;
  const r = raw as { role?: unknown; content?: unknown };
  if (typeof r.role !== "string") return null;
  const author = mapAuthor(raw);
  return { role: r.role, content: r.content, ...(author ? { author } : {}) };
}

/** Text-only rehydrate of a conversation's server-side history. */
export async function getConversationMessages(
  projectName: string,
  conversationId: string,
): Promise<ConversationMessage[] | null> {
  const { data, error } = await client.GET(
    "/projects/{projectName}/agents/{conversationId}/messages",
    { params: { path: { projectName, conversationId } } },
  );
  // null means FAILURE — keep painting the local cache. "This thread is
  // empty" is not a failure and never arrives as one: the BFF answers a
  // known-but-turn-less thread with 200 {messages: []} (it owns thread
  // existence via project_conversations), reserving 404-class errors for
  // genuinely unknown ids, missing repos, and tenant mismatches — all cases
  // where wiping the cache would destroy information over a transient or
  // config problem.
  if (error || data === undefined) return null;
  const body = data as { messages?: unknown[] };
  if (!body.messages) return null;
  return body.messages
    .map(mapConversationMessage)
    .filter((m): m is ConversationMessage => m !== null);
}

/** The project's running turn, or null (204 / none). */
export async function getActiveTurn(
  projectName: string,
): Promise<TurnStatus | null> {
  const { data, error, response } = await client.GET(
    "/projects/{projectName}/turns/active",
    { params: { path: { projectName } } },
  );
  if (response.status === 204 || error || data === undefined) return null;
  return data;
}

export async function getTurn(
  projectName: string,
  turnId: string,
): Promise<TurnStatus | null> {
  const { data, error } = await client.GET(
    "/projects/{projectName}/turns/{turnId}",
    { params: { path: { projectName, turnId } } },
  );
  if (error || data === undefined) return null;
  return data;
}

/** Thrown when the turn-stream attach HTTP call fails. `status` lets callers
 *  discriminate a pre-stream 404 (buffer not on this replica / not yet minted)
 *  from other failures. */
export class TurnStreamAttachError extends Error {
  readonly status: number;
  constructor(status: number, message = "Failed to attach to the turn stream") {
    super(message);
    this.name = "TurnStreamAttachError";
    this.status = status;
  }
}

export function isTurnStreamNotFound(err: unknown): boolean {
  return err instanceof TurnStreamAttachError && err.status === 404;
}

/**
 * Open the turn's SSE stream as a raw byte stream (replay from `from`, then
 * live tail). The caller iterates it with @aep/agent-stream's parseSseStream.
 */
export async function openTurnStream(
  projectName: string,
  turnId: string,
  from: number,
  signal: AbortSignal,
): Promise<ReadableStream<Uint8Array>> {
  const { data, error, response } = await client.GET(
    "/projects/{projectName}/turns/{turnId}/stream",
    {
      params: { path: { projectName, turnId }, query: { from } },
      parseAs: "stream",
      signal,
    },
  );
  if (error || !data) {
    throw new TurnStreamAttachError(response?.status ?? 0);
  }
  return data as ReadableStream<Uint8Array>;
}
