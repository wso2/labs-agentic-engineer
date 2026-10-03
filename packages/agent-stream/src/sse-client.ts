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
 * The reference SSE consumer — exactly what a browser fold is. Starts one turn,
 * streams it, and yields each raw `StreamPart` frame until `[DONE]`, buffered
 * across chunk boundaries. The JSON payload is always a single physical line (`data: <json>`),
 * since the SDK `JSON.stringify`s each part (embedded newlines are escaped), but a
 * frame is a multi-line SSE record: an `id: <index>` line (the part's index in
 * the turn's replay buffer, for `?from=` resume) may precede the `data:` line.
 * So a frame is parsed line-by-line — the `data:` line(s) are the payload, the
 * `id:` line its resume index; `event:` metadata and `: keep-alive` comment
 * lines carry neither and are skipped.
 */

import { SSE_DONE, type TurnAim } from "./contracts/sse-events.js";
import type { StreamPart } from "./stream-types.js";

/** A `/v1` turn start body (the design agent's `CreateTurnRequest`, JSON form). */
export interface TurnStartBody {
  /** Verbatim: `/<command>` lines are parsed by the design agent. */
  instruction: string;
  target?: string;
  aim?: TurnAim;
}

/**
 * How a parsed stream ended: `"done"` — the server's `[DONE]` sentinel arrived
 * (a complete stream); `"eof"` — the byte stream ended WITHOUT `[DONE]` (the
 * connection died mid-turn; whatever was folded so far is incomplete).
 */
export type SseStreamEnd = "done" | "eof";

/**
 * One parsed frame: the part, and the frame's `id:` when it carried a valid
 * one. The design agent's turn stream numbers every part by its index in the
 * turn's replay buffer, so a reader whose stream dies resumes with
 * `?from=<last id + 1>` and folds no frame twice.
 */
export interface SseFrame {
  id?: number;
  part: StreamPart;
}

/** A frame's `id:` value as a buffer index, or undefined when it is not a non-negative integer. */
function frameId(lines: string[]): number | undefined {
  const line = lines.filter((l) => l.startsWith("id:")).pop();
  if (line === undefined) return undefined;
  const value = line.slice("id:".length).replace(/^ /, "");
  return /^\d+$/.test(value) ? Number(value) : undefined;
}

/**
 * The raw frame parser, extracted so a caller that owns its own `fetch` (e.g. a
 * browser that must add auth headers, a custom request body shape, and its own
 * pre-stream HTTP-status error mapping) folds the SAME wire through ONE
 * definition instead of reimplementing the buffered `data:`/`[DONE]` loop.
 * Yields each frame (its `StreamPart` and `id:`) until `[DONE]`; skips
 * keep-alive comment frames.
 *
 * The generator's RETURN value reports how the stream ended (`SseStreamEnd`).
 * `for await` consumers ignore it — a caller that must distinguish a complete
 * stream from a mid-turn disconnect iterates manually:
 *
 *   const it = parseSseFrames(body)[Symbol.asyncIterator]();
 *   while (true) {
 *     const r = await it.next();
 *     if (r.done) { const end = r.value; break; }  // "done" | "eof"
 *     fold(r.value.part);
 *   }
 */
export async function* parseSseFrames(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<SseFrame, SseStreamEnd> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  // finally runs on normal end AND on early generator.return() (the consumer
  // breaking out of the loop), so the reader lock is always released — letting
  // the caller cancel() the body to close the connection on a reconnect.
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) return "eof";
      buffer += decoder.decode(value, { stream: true });

      // Frames are delimited by a blank line.
      let sep: number;
      while ((sep = buffer.indexOf("\n\n")) !== -1) {
        const frame = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        // A frame is a multi-line SSE record. Collect its `data:` line(s) —
        // an `id:` line may precede them, and `id:`/`event:`/`: comment`
        // lines carry no payload. Per the SSE spec, multiple data lines join
        // with "\n" (our payload is one line, but stay spec-correct). One
        // optional space after the colon is stripped.
        const lines = frame.split(/\r?\n/);
        const data = lines
          .filter((line) => line.startsWith("data:"))
          .map((line) => line.slice("data:".length).replace(/^ /, ""))
          .join("\n")
          .trim();
        if (data === "") continue; // comment / metadata-only frame — no data line
        if (data === SSE_DONE) return "done";
        // A frame that doesn't parse is a truncation remnant (a proxy closes a
        // partial in-flight frame with a blank line when the upstream dies).
        // Wire frames are single-line JSON, so nothing legitimate is skipped —
        // and the missing terminal (or the "eof" return) carries the failure.
        let part: StreamPart;
        try {
          part = JSON.parse(data) as StreamPart;
        } catch {
          continue;
        }
        const id = frameId(lines);
        yield id === undefined ? { part } : { id, part };
      }
    }
  } finally {
    reader.releaseLock();
  }
}

/**
 * `parseSseFrames` without the ids, for a reader that never resumes. The
 * return value reports how the stream ended, as there.
 */
export async function* parseSseStream(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<StreamPart, SseStreamEnd> {
  const frames = parseSseFrames(body);
  for (;;) {
    const next = await frames.next();
    if (next.done) return next.value;
    yield next.value.part;
  }
}

/**
 * Why `startAndStreamTurn` started no turn: the `/v1` refusal's status and its
 * code (a problem's `code`, or a TurnConflict's, e.g. `conversation_rotated`).
 */
export class TurnRefusedError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    detail: string,
  ) {
    super(`turn refused: HTTP ${status} ${code}${detail ? `: ${detail}` : ""}`);
    this.name = "TurnRefusedError";
  }
}

async function refusal(res: Response): Promise<TurnRefusedError> {
  const text = await res.text().catch(() => "");
  let body: { code?: unknown; detail?: unknown; message?: unknown } = {};
  try {
    body = JSON.parse(text) as typeof body;
  } catch {
    // not JSON: the status alone names the refusal
  }
  const code = typeof body.code === "string" ? body.code : `http_${res.status}`;
  const detail = typeof body.detail === "string" ? body.detail : typeof body.message === "string" ? body.message : "";
  return new TurnRefusedError(res.status, code, detail);
}

/**
 * One browser turn on the design agent's `/v1` edge (the console's flow, for
 * server-side callers such as the playground and the evals): `POST
 * /v1/projects/{project}/conversations/{conversationId}/turns`, then `GET
 * /v1/projects/{project}/turns/{turnId}/stream?from=0`, yielding each part
 * until `[DONE]`. A refused start throws `TurnRefusedError` and opens no
 * stream. `headers` carries the caller's credential; this reader holds none.
 */
export async function* startAndStreamTurn(
  baseUrl: string,
  project: string,
  conversationId: string,
  body: TurnStartBody,
  headers: Record<string, string> = {},
): AsyncGenerator<StreamPart, SseStreamEnd> {
  const projectUrl = `${baseUrl}/v1/projects/${encodeURIComponent(project)}`;
  const started = await fetch(`${projectUrl}/conversations/${encodeURIComponent(conversationId)}/turns`, {
    method: "POST",
    headers: { "content-type": "application/json", ...headers },
    body: JSON.stringify(body),
  });
  if (started.status !== 202) throw await refusal(started);
  const { turnId } = (await started.json()) as { turnId: string };
  const res = await fetch(`${projectUrl}/turns/${encodeURIComponent(turnId)}/stream?from=0`, { headers });
  if (!res.ok || !res.body) throw await refusal(res);
  return yield* parseSseStream(res.body);
}
