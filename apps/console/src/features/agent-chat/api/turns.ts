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

import type { components } from "../../../generated/ae-design-agent";
import { designAgent } from "../../../api/aeStudio";
import { apiErrorCode, apiErrorMessage } from "../../../api/errors";

// The chat's turn calls, on the org's design agent (ae-design-agent /v1 in the
// AE Studio pod, reached through designAgent()): start a turn in the
// project's current thread, read its status, attach to its event stream, and
// rehydrate the thread's messages.

export type TurnStatus = components["schemas"]["TurnStatus"];

/** What the user pointed at, and what they want done with it (#666). */
export type TurnAnchor = components["schemas"]["TurnAnchor"];
export type TurnIntent = components["schemas"]["TurnIntent"];

/**
 * The aiming half of a send. Both fields travel together or not at all: an
 * intent with nothing to point at says nothing, and an anchor with no intent
 * leaves the agent guessing which preamble to render.
 */
export interface TurnAiming {
  anchor: TurnAnchor;
  intent: TurnIntent;
}

/** One send: the user's words, the files riding along (#428), what it aims at (#666). */
export interface TurnStartInput {
  instruction: string;
  files?: File[] | undefined;
  aiming?: TurnAiming | undefined;
}

/**
 * A refused turn start: the sentence the chat shows, plus the pod's status
 * (undefined when it did not answer) and code.
 */
export class TurnStartError extends Error {
  readonly status: number | undefined;
  readonly code: string | undefined;

  constructor(message: string, status: number | undefined, code: string | undefined) {
    super(message);
    this.name = "TurnStartError";
    this.status = status;
    this.code = code;
  }
}

/**
 * The addressed thread is no longer the project's current one (#430) — a
 * teammate rotated while this client held a resolved id. Recovery is
 * re-resolve + rehydrate, not a retry into the demoted thread.
 */
export class ConversationRotatedError extends TurnStartError {
  constructor() {
    super("The conversation was replaced with a new one — your message was not sent.", 409, "conversation_rotated");
    this.name = "ConversationRotatedError";
  }
}

/** A turn is already running for the project; `activeTurnId` names it when the pod did. */
export class TurnInProgressError extends TurnStartError {
  readonly activeTurnId: string | undefined;

  constructor(activeTurnId: string | undefined) {
    super("An agent turn is already running for this project — wait for it to finish.", 409, "turn_in_progress");
    this.name = "TurnInProgressError";
    this.activeTurnId = activeTurnId;
  }
}

/**
 * The chat's sentence for a refused start, by the pod's code. A refusal whose
 * cure is somewhere else (Settings, a moment's wait) says so; any other one
 * carries the pod's own sentence.
 */
function refusalMessage(status: number, code: string | undefined, error: unknown): string {
  switch (code) {
    case "no_default_key":
      return "Your organization has no model connection yet — add one in Settings → AI agents.";
    case "tools_unavailable":
      return "AE Studio's tools are not answering — try again in a moment.";
    case "shutting_down":
      return "AE Studio is restarting — try again in a moment.";
  }
  if (status === 503) return "AE Studio is not available right now — try again in a moment.";
  return apiErrorMessage(error, "Failed to start the agent turn");
}

function startRefusal(status: number, error: unknown): TurnStartError {
  const code = apiErrorCode(error);
  if (status === 409 && code === "conversation_rotated") return new ConversationRotatedError();
  if (status === 409 && code === "turn_in_progress") {
    const active = (error as { activeTurnId?: unknown }).activeTurnId;
    return new TurnInProgressError(typeof active === "string" && active ? active : undefined);
  }
  return new TurnStartError(refusalMessage(status, code, error), status, code);
}

/** JSON body of a turn start: the aiming fields ride beside the words, never inside them. */
function turnBody(input: TurnStartInput): components["schemas"]["TurnInputBody"] {
  return {
    instruction: input.instruction,
    ...(input.aiming ? { anchor: input.aiming.anchor, intent: input.aiming.intent } : {}),
  };
}

/** The multipart body of a send that carries attachments. */
function turnFormData(input: TurnStartInput, files: File[]): FormData {
  const form = new FormData();
  form.append("instruction", input.instruction);
  // The anchor is a nested object, which a form field cannot carry as a scalar —
  // the contract declares this part `application/json` for exactly that reason.
  if (input.aiming) {
    form.append("anchor", new Blob([JSON.stringify(input.aiming.anchor)], { type: "application/json" }));
    form.append("intent", input.aiming.intent);
  }
  for (const file of files) form.append("files", file);
  return form;
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

/**
 * Start a turn in the project's conversation. The agent edits the project's
 * spec Room itself; the panel only folds the stream's narration and results.
 *
 * `files` (#428) are chat attachments: conversation-scoped model content that
 * rides THIS message and is never stored or committed (ADR-0019). With none
 * the request is plain JSON; the multipart form is built only when there is
 * something to put in it.
 *
 * Throws TurnStartError (ConversationRotatedError, TurnInProgressError) for a
 * refusal or no answer, AeStudioNotReadyError before AE Studio is `ready`.
 */
export async function startTurn(
  projectName: string,
  conversationId: string,
  input: TurnStartInput,
): Promise<{ turnId: string }> {
  const client = designAgent();
  const files = input.files ?? [];
  const result = await client
    .POST("/projects/{projectName}/conversations/{conversationId}/turns", {
      params: { path: { projectName, conversationId } },
      // Raw bytes, not base64-in-JSON: base64 inflates ~33% and would shave
      // the real 15 MiB budget the composer screens against. openapi-fetch
      // passes FormData through untouched (the browser sets the boundary);
      // the generated request type describes the JSON shape, not the wire.
      body: files.length > 0 ? (turnFormData(input, files) as unknown as { instruction: string }) : turnBody(input),
    })
    .catch((error: unknown) => {
      if (isAbort(error)) throw error;
      throw new TurnStartError("Failed to start the agent turn — AE Studio did not answer.", undefined, undefined);
    });
  const { data, error, response } = result;
  if (error !== undefined || data === undefined) throw startRefusal(response.status, error);
  return { turnId: data.turnId };
}

/** Who sent a message: `id` is the sender's verified token `sub`, the console's "me" id. */
export type ConversationMessageAuthor = components["schemas"]["ConversationMessageAuthor"];

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

// The contract pins `author`; `user` is still read as a fallback name so a
// history row written either way attributes. A malformed author drops rather
// than throwing — this is the rehydrate path, and one bad row must not cost
// the user their whole log.
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

/**
 * Text-only rehydrate of a conversation's history. null means FAILURE — keep
 * painting the local cache. "This thread is empty" is not a failure: the pod
 * answers the current thread before its first turn with `[]`, and keeps 404
 * for unknown ids, where wiping the cache would destroy information.
 */
export async function getConversationMessages(
  projectName: string,
  conversationId: string,
): Promise<ConversationMessage[] | null> {
  const data = await readOrNull(() =>
    designAgent().GET("/projects/{projectName}/conversations/{conversationId}/messages", {
      params: { path: { projectName, conversationId } },
    }),
  );
  if (!data) return null;
  return data.messages
    .map(mapConversationMessage)
    .filter((m): m is ConversationMessage => m !== null);
}

/**
 * A read whose every failure means "unknown": a refusal, no answer, or AE
 * Studio not `ready` (its URLs are dropped while it restarts) all answer
 * null, so the background triggers that make these reads (mount, poll,
 * refocus) keep what they have instead of rejecting. Aborts pass through.
 */
async function readOrNull<T>(
  call: () => Promise<{ data?: T | undefined; error?: unknown; response: Response }>,
): Promise<NonNullable<T> | null> {
  try {
    const { data, error, response } = await call();
    if (error !== undefined || data == null || response.status === 204) return null;
    return data;
  } catch (err) {
    if (isAbort(err)) throw err;
    return null;
  }
}

/**
 * The project's running turn, or null when none runs (204). A failed read
 * throws — a refusal, no answer, or AeStudioNotReadyError before AE Studio is
 * `ready` — so the active-turn query keeps its last answer instead of reading
 * a pod hiccup as "nothing is running".
 */
export async function getActiveTurn(projectName: string): Promise<TurnStatus | null> {
  const { data, error, response } = await designAgent().GET("/projects/{projectName}/turns/active", {
    params: { path: { projectName } },
  });
  if (response.status === 204) return null;
  if (error !== undefined || data === undefined) {
    throw new Error(apiErrorMessage(error, "Failed to read the project's running turn"));
  }
  return data.status === "running" ? data : null;
}

/** One turn's status, or null when the pod no longer holds it (404) or the read failed. */
export async function getTurn(projectName: string, turnId: string): Promise<TurnStatus | null> {
  const read = await readTurnStatus(projectName, turnId);
  return read.kind === "status" ? read.status : null;
}

/**
 * One turn's status read, telling its two failures apart: `gone` — the pod
 * answered 404, it no longer holds the turn and no later read will — from
 * `unavailable` — any other refusal, no answer, or AE Studio not `ready`,
 * which a later read may get past. Aborts pass through.
 */
export type TurnStatusRead =
  | { kind: "status"; status: TurnStatus }
  | { kind: "gone" }
  | { kind: "unavailable" };

export async function readTurnStatus(projectName: string, turnId: string): Promise<TurnStatusRead> {
  try {
    const { data, response } = await designAgent().GET("/projects/{projectName}/turns/{turnId}", {
      params: { path: { projectName, turnId } },
    });
    if (data !== undefined) return { kind: "status", status: data };
    return response.status === 404 ? { kind: "gone" } : { kind: "unavailable" };
  } catch (err) {
    if (isAbort(err)) throw err;
    return { kind: "unavailable" };
  }
}

/**
 * Thrown when the turn-stream attach fails before a byte of the stream:
 * `status` (0 when the pod did not answer) and the problem's `code` tell a
 * turn past its retention (404) from a replay with a gap (409
 * replay_truncated) and from everything else.
 */
export class TurnStreamAttachError extends Error {
  readonly status: number;
  readonly code: string | undefined;
  constructor(status: number, code?: string | undefined) {
    super("Failed to attach to the turn stream");
    this.name = "TurnStreamAttachError";
    this.status = status;
    this.code = code;
  }
}

export function isTurnStreamNotFound(err: unknown): boolean {
  return err instanceof TurnStreamAttachError && err.status === 404;
}

/**
 * The running turn overflowed its replay buffer, so a replay would have a
 * gap: the pod says to attach again after the turn ends.
 */
export function isTurnStreamReplayTruncated(err: unknown): boolean {
  return err instanceof TurnStreamAttachError && err.status === 409 && err.code === "replay_truncated";
}

/**
 * Open the turn's SSE stream as a raw byte stream (replay from frame `from`,
 * then live tail). The caller iterates it with @aep/agent-stream's
 * parseSseStream.
 */
export async function openTurnStream(
  projectName: string,
  turnId: string,
  from: number,
  signal: AbortSignal,
): Promise<ReadableStream<Uint8Array>> {
  const { data, error, response } = await designAgent().GET("/projects/{projectName}/turns/{turnId}/stream", {
    params: { path: { projectName, turnId }, query: { from } },
    parseAs: "stream",
    signal,
  });
  if (error !== undefined || !data) {
    throw new TurnStreamAttachError(response.status, apiErrorCode(error));
  }
  return data as ReadableStream<Uint8Array>;
}
