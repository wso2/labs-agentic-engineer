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

/** What the user pointed at, and what they want done with it (#666). */
export type TurnAnchor = components["schemas"]["TurnAnchor"];
export type TurnIntent = NonNullable<components["schemas"]["TurnInputBody"]["intent"]>;

/**
 * The aiming half of a send. Both fields travel together or not at all: an
 * intent with nothing to point at says nothing, and an anchor with no intent
 * leaves the service guessing which preamble to render.
 */
export interface TurnAiming {
  anchor: TurnAnchor;
  intent: TurnIntent;
}

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
 * The multipart body for a send that carries attachments. `collab` goes over the
 * wire as the string "true" because a form field has no other representation —
 * the server parses it, and the JSON path is unaffected.
 */
function turnFormData(
  instruction: string,
  files: File[],
  collab: boolean,
  aiming?: TurnAiming | undefined,
): FormData {
  const form = new FormData();
  form.append("instruction", instruction);
  if (collab) form.append("collab", "true");
  // The anchor is a nested object, which a form field cannot carry as a scalar —
  // the contract declares this part `application/json` for exactly that reason.
  if (aiming) {
    form.append("anchor", new Blob([JSON.stringify(aiming.anchor)], { type: "application/json" }));
    form.append("intent", aiming.intent);
  }
  for (const file of files) form.append("files", file);
  return form;
}

/** JSON body for StartTurn. Register chat omits collab — there is no spec room. */
export function startTurnBody(
  instruction: string,
  collab: boolean,
  aiming?: TurnAiming | undefined,
): components["schemas"]["TurnInputBody"] {
  return {
    instruction,
    ...(collab ? { collab: true } : {}),
    ...(aiming ? { anchor: aiming.anchor, intent: aiming.intent } : {}),
  };
}

/**
 * Start a room-scoped agent turn (#86 phase 4 / #130): `collab: true` — the
 * agent joins the project's spec room as a live peer and edits the shared
 * doc; the panel only receives narration + tool results.
 *
 * `files` (#428) are chat attachments: conversation-scoped model content that
 * rides THIS message and is never stored server-side or committed (ADR-0019).
 * With none attached the request is the same JSON body as before — the
 * multipart form is built only when there is something to put in it, so the
 * overwhelmingly common send is byte-identical to the pre-feature one.
 */
export async function startCollabTurn(
  projectName: string,
  conversationId: string,
  instruction: string,
  files: File[] = [],
  collab = true,
  aiming?: TurnAiming | undefined,
): Promise<string> {
  const { data, error, response } = await client.POST(
    "/projects/{projectName}/agents/{conversationId}/messages",
    {
      params: { path: { projectName, conversationId } },
      ...(files.length > 0
        ? {
            // Raw bytes, not base64-in-JSON: base64 inflates ~33% and would
            // silently shave the real 15 MB budget the composer screens against
            // (ADR-0017 decision 6 made the same call for references).
            //
            // Same cast as useUploadReferences: openapi-fetch passes FormData
            // through its default bodySerializer untouched (the browser sets the
            // multipart boundary), but the generated request type describes the
            // JSON Schema shape, not the wire.
            body: turnFormData(instruction, files, collab, aiming) as unknown as {
              instruction: string;
            },
          }
        : {
            body: startTurnBody(instruction, collab, aiming),
          }),
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
  /** File NAMES attached to this message (#428), from the turn journal — never
   *  bytes (ADR-0019). Absent for every message without attachments, and for
   *  history from before the journal carried them. */
  attachments?: string[];
  /** What the user aimed this message at (#666), from the turn journal. Absent
   *  for every ordinary chat message, and for history from before the journal
   *  carried it. */
  anchor?: TurnAnchor;
}

// `author` predates the response schema being typed at all (#666 closed that
// gap), and the field it arrives under was never pinned: read `author` first,
// `user` as a fallback name, so history written either way still attributes.
// A malformed author drops rather than throwing — this is the rehydrate path,
// and one bad row must not cost the user their whole log.
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
  const attachments = mapAttachments(raw);
  const anchor = mapAnchor(raw);
  return {
    role: r.role,
    content: r.content,
    ...(author ? { author } : {}),
    ...(attachments ? { attachments } : {}),
    ...(anchor ? { anchor } : {}),
  };
}

/**
 * Attachment NAMES off a rehydrated message (#428), from the turn journal.
 *
 * Filtered rather than trusted. The contract now describes this field (#666),
 * but a contract describes what the server SHOULD send — this is the rehydrate
 * path, and a malformed entry must drop out instead of reaching the UI as a
 * blank chip. Returns null — not [] — when there is nothing, so the caller can
 * omit the property entirely under `exactOptionalPropertyTypes` and a message
 * without attachments keeps the row shape it had before this feature.
 */
function mapAttachments(raw: unknown): string[] | null {
  const value = (raw as { attachments?: unknown }).attachments;
  if (!Array.isArray(value)) return null;
  const names = value.filter((n): n is string => typeof n === "string" && n.trim() !== "");
  return names.length > 0 ? names : null;
}

/**
 * The anchor off a rehydrated message (#666) — what the user pointed at when
 * they aimed this turn at part of a spec document.
 *
 * Read defensively for the same reason as attachments, and dropped WHOLE when
 * any part of it is malformed. A half-read anchor would render a tag naming
 * fewer nodes than the user selected, which is a quieter and worse failure than
 * no tag: the transcript is the record of what was aimed at, so it either says
 * so correctly or says nothing.
 */
function mapAnchor(raw: unknown): TurnAnchor | null {
  const value = (raw as { anchor?: unknown }).anchor;
  if (typeof value !== "object" || value === null) return null;
  const { file, nodes } = value as { file?: unknown; nodes?: unknown };
  if (typeof file !== "string" || file === "" || !Array.isArray(nodes)) return null;
  const mapped: TurnAnchor["nodes"] = [];
  for (const node of nodes) {
    if (typeof node !== "object" || node === null) return null;
    const { name, kind, context } = node as {
      name?: unknown;
      kind?: unknown;
      context?: unknown;
    };
    if (typeof name !== "string" || name === "" || typeof kind !== "string") return null;
    mapped.push({ name, kind, ...(typeof context === "string" && context ? { context } : {}) });
  }
  return mapped.length > 0 ? { file, nodes: mapped } : null;
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
