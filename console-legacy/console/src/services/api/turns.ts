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
 * The committed-truth turn client — the console's entry points to the BFF's
 * resumable generation endpoints (shared-volume-clone D13/D16/D18):
 *
 *   - `startTurn`   POST …/agents/{id}/messages {useCase, instruction,
 *                   target?} → 202 {turnId}. No file content ever leaves the
 *                   browser — the backend snapshots HEAD itself.
 *   - `attachTurnStream`  GET …/turns/{id}/stream?from=N — replay + live tail
 *                   of the raw `StreamPart` frames, folded DISPLAY-ONLY
 *                   (`turnStream.ts`), ending in the authoritative
 *                   `turn-committed`/`turn-failed` terminal event. Reconnects
 *                   on connection loss; falls back to the status GET.
 *   - `getTurn` / `getActiveTurn`  turn status — refresh-mid-generation and
 *                   the second-tab viewer attach hang off these.
 *
 * On `turn-committed` the caller refetches the server tree: reads at HEAD are
 * the complete truth (no local draft, no overlay).
 */

import { API_V1, authHeaders, parseErrorEnvelope } from './http';
import {
  classifyStartTurnError,
  filterTurnSeed,
  runTurnAttachLoop,
  type StartTurnError,
  type TurnHandlers,
  type TurnResult,
  type TurnStatus,
  type TurnStreamConnection,
} from './turnStream';

export type {
  FoldEnd,
  FoldResult,
  StartTurnError,
  StartTurnErrorCode,
  TurnErrorCode,
  TurnFailure,
  TurnFailureReason,
  TurnHandlers,
  TurnOk,
  TurnResult,
  TurnStatus,
} from './turnStream';

export type UseCase =
  | 'requirements-generate'
  | 'requirements-chat'
  | 'design-generate';

export interface StartTurnBody {
  useCase: UseCase;
  instruction: string;
  /** Optional hint — e.g. a target doc type. */
  target?: string;
}

export type StartTurnResult = { ok: true; turnId: string } | StartTurnError;

/** A human-readable message for a failed start or a failed/lost turn. */
export function turnErrorMessage(result: {
  code: string;
  message: string;
  reason?: string;
  paths?: string[];
}): string {
  switch (result.code) {
    case 'turn_in_progress':
      return 'A generation is already running for this project. Please wait for it to finish.';
    case 'missing_org_key':
      return 'No Anthropic API key is configured for this organization. Add one in Organization → Settings → Anthropic.';
    case 'requirements_not_approved':
      return 'Approve (publish) a requirements version before generating the design.';
    case 'upstream':
      return 'The generation service is unavailable. Please try again shortly.';
    case 'turn_failed':
      return turnFailedMessage(result);
    case 'stream_lost':
    case 'not_found':
      return result.message;
    default:
      return result.message || 'The request failed. Please try again.';
  }
}

function turnFailedMessage(result: { message: string; reason?: string; paths?: string[] }): string {
  switch (result.reason) {
    case 'base-moved': {
      const paths = result.paths?.length ? result.paths.join(', ') : 'files in the repository';
      return `The generated changes were NOT committed because these files changed while the generation ran: ${paths}. Regenerate to retry against the latest content.`;
    }
    case 'fold-parity':
      return 'The generation result failed verification and was not committed. Please try again.';
    case 'stream-died':
      return 'The generation ended unexpectedly and nothing was committed. Please try again.';
    case 'dispatch-failed':
      return result.message || 'The generation could not be started. Please try again shortly.';
    default:
      return result.message || 'The generation failed and nothing was committed. Please try again.';
  }
}

/** A valid FE conversation id: uuid v4 satisfies `^[A-Za-z0-9_.-]{1,200}$`, no `--`. */
export function newConversationId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  // Fallback for the rare no-WebCrypto environment; still matches the regex.
  return `c-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

/** One message in the rehydrated conversation (the service's `ModelMessage`). */
export interface ConversationMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string | Array<{ type: string; text?: string }>;
}

/**
 * Rehydrate a chat conversation's history (chat only). Returns `{messages}` —
 * the verbatim LLM thread (no files, correctly). Used on refresh when local
 * display history is empty. 404 (unknown/cross-tenant) resolves to empty.
 */
export async function getConversation(
  projectName: string,
  conversationId: string,
): Promise<ConversationMessage[]> {
  const res = await fetch(
    `${API_V1}/projects/${encodeURIComponent(projectName)}/agents/${encodeURIComponent(conversationId)}/messages`,
    { cache: 'no-store', headers: await authHeaders() },
  );
  if (!res.ok) return [];
  const data = (await res.json().catch(() => null)) as { messages?: ConversationMessage[] } | null;
  return data?.messages ?? [];
}

/**
 * Start one turn. Resolves to `{turnId}` on 202, or a typed error —
 * `turn_in_progress` carries `activeTurnId` so the caller can attach to the
 * running generation as a viewer (D18). Never throws for an expected HTTP
 * error; only transport exceptions bubble.
 */
export async function startTurn(
  projectName: string,
  conversationId: string,
  body: StartTurnBody,
): Promise<StartTurnResult> {
  const res = await fetch(
    `${API_V1}/projects/${encodeURIComponent(projectName)}/agents/${encodeURIComponent(conversationId)}/messages`,
    {
      method: 'POST',
      headers: await authHeaders(),
      body: JSON.stringify(body),
    },
  );
  if (res.ok) {
    const data = (await res.json().catch(() => null)) as { turnId?: string } | null;
    if (data && typeof data.turnId === 'string' && data.turnId) {
      return { ok: true, turnId: data.turnId };
    }
    return {
      ok: false,
      code: 'request_failed',
      message: 'The server accepted the turn but returned no turn id.',
    };
  }
  const raw = await res.text().catch(() => '');
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    parsed = undefined;
  }
  return classifyStartTurnError(res.status, parsed, raw);
}

/** One turn's status row. Resolves to null on 404 (unknown/expired turn). */
export async function getTurn(
  projectName: string,
  turnId: string,
): Promise<TurnStatus | null> {
  const res = await fetch(
    `${API_V1}/projects/${encodeURIComponent(projectName)}/turns/${encodeURIComponent(turnId)}`,
    { cache: 'no-store', headers: await authHeaders() },
  );
  if (res.status === 404) return null;
  if (!res.ok) {
    const { message } = await parseErrorEnvelope(res);
    throw new Error(message || `Turn status failed (HTTP ${res.status}).`);
  }
  return (await res.json()) as TurnStatus;
}

/** The project's active turn, or null when none is running (204). */
export async function getActiveTurn(projectName: string): Promise<TurnStatus | null> {
  const res = await fetch(
    `${API_V1}/projects/${encodeURIComponent(projectName)}/turns/active`,
    { cache: 'no-store', headers: await authHeaders() },
  );
  if (res.status === 204 || res.status === 404) return null;
  if (!res.ok) {
    const { message } = await parseErrorEnvelope(res);
    throw new Error(message || `Active-turn lookup failed (HTTP ${res.status}).`);
  }
  return (await res.json()) as TurnStatus;
}

export interface AttachOptions {
  /** Stream index to resume from (default 0 = full replay). */
  from?: number;
  /**
   * The CURRENT server tree (full `specs/…` keys) the fold replays on top of —
   * a fresh `readTree` result. It is filtered to the agents-side turn-snapshot
   * view internally. During a resumed replay the live preview may briefly
   * differ from the final committed state; the terminal event + refetch
   * reconcile.
   */
  seed: Record<string, string>;
  handlers?: TurnHandlers;
  signal?: AbortSignal;
}

/**
 * Attach to a running (or just-finished) turn's stream and drive it to the
 * authoritative outcome. See `runTurnAttachLoop` for the reconnect/fallback
 * semantics. Throws only on abort.
 */
export async function attachTurnStream(
  projectName: string,
  turnId: string,
  opts: AttachOptions,
): Promise<TurnResult> {
  const connect = async (from: number): Promise<TurnStreamConnection> => {
    const res = await fetch(
      `${API_V1}/projects/${encodeURIComponent(projectName)}/turns/${encodeURIComponent(turnId)}/stream?from=${from}`,
      {
        cache: 'no-store',
        headers: await authHeaders({ Accept: 'text/event-stream' }),
        signal: opts.signal,
      },
    );
    if (res.status === 404) {
      res.body?.cancel().catch(() => {});
      return { ok: false, notFound: true, message: 'The turn stream is no longer available.' };
    }
    if (!res.ok || !res.body) {
      const { message } = await parseErrorEnvelope(res);
      return { ok: false, notFound: false, message: message || `Stream failed (HTTP ${res.status}).` };
    }
    return { ok: true, body: res.body };
  };

  const loopOpts: Parameters<typeof runTurnAttachLoop>[2] = {
    seed: filterTurnSeed(opts.seed),
  };
  if (opts.from !== undefined) loopOpts.from = opts.from;
  if (opts.handlers) loopOpts.handlers = opts.handlers;
  if (opts.signal) loopOpts.signal = opts.signal;
  return runTurnAttachLoop(connect, () => getTurn(projectName, turnId), loopOpts);
}
