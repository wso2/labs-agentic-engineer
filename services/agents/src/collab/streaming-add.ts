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
 * StreamingDocWriter (#86 follow-up) — live-stream an `addFile` body into the
 * collab doc AS THE MODEL TYPES IT, instead of one write when the tool call
 * closes. It is a pure OPTIMISTIC PREVIEW layered over the parts the turn loop
 * already forwards; the tool's `execute()` (DocFileBundle → FileBundle) stays the
 * authority for validation and the D14 manifest, so this writes nothing the
 * bundle wouldn't, and the final flush reconciles to the same content (an
 * idempotent no-op once `execute` runs).
 *
 * Mechanism:
 *  - AI SDK v7 streams tool input as `tool-input-start` → `tool-input-delta` (a
 *    growing, JSON-escaped args buffer) → `tool-input-end`, correlated by `id`
 *    (== the later `toolCallId`).
 *  - `parsePartialJson` (from `ai`) decodes the partial buffer; `path` resolves
 *    strictly before `content` starts (schema property order is load-bearing,
 *    files.ts), so the moment `content` appears the path is final and gate-able.
 *  - Writes flush on LINE BOUNDARIES: char-by-char markdown churns the
 *    ProseMirror fragment (~30× the Yjs traffic + rewrites earlier text); line
 *    boundaries are clean.
 *
 * Scope: room-mode addFile for MARKDOWN, `.cell`, `.dsl`, and `openapi.yaml`.
 * `.cell` is the project-level architecture DSL, `.dsl` a component's
 * wireframes, `openapi.yaml` a component's API contract; streaming any of
 * them line-by-line renders its view live as the model writes. Markdown and
 * `.cell` have no content-gate; `.dsl` (INVALID_DSL) and `openapi.yaml`
 * (INVALID_YAML) are gated at execute() — when a write is rejected, the
 * tool-result handler below rolls the optimistic preview back, same as any
 * rejected op. Other / already-existing paths are skipped (execute owns
 * them). Non-finalized streams (truncation / reject) are rolled back.
 */

import { parsePartialJson } from "ai";
import { isMarkdownPath } from "@aep/collab-doc";
import type { FileBundle, StreamPart } from "@aep/agent-stream";
import type { RoomPeer } from "./room-peer.js";
import { ADD_FILE } from "../agents/main/tools/files.js";

/**
 * Paths safe to optimistically stream: markdown (Y.XmlFragment) or Y.Text
 * artifacts — `.cell` architecture DSL, `.dsl` wireframes, `openapi.yaml`.
 * YAML is line-oriented like the DSLs, so line-boundary prefixes parse and
 * the OpenAPI view accumulates endpoints as the model writes them.
 */
function isStreamablePath(path: string): boolean {
  return (
    isMarkdownPath(path) ||
    path.endsWith('.cell') ||
    path.endsWith('.dsl') ||
    path.endsWith('openapi.yaml')
  );
}

interface CallState {
  /** Accumulated tool-input args text (JSON) for this tool call. */
  buf: string;
  /** Resolved + gated path; set once we commit to streaming this call. */
  path?: string;
  /** True once the path passed the gate and we are mirroring content. */
  streaming: boolean;
  /** True once the path failed the gate — ignore this call's deltas. */
  skipped: boolean;
  /** Chars of `content` already written to the doc (line-boundary watermark). */
  flushedLen: number;
  /** True once `execute` confirmed the write (ok) — excludes it from rollback. */
  finalized: boolean;
}

export class StreamingDocWriter {
  private readonly calls = new Map<string, CallState>();
  /** Serializes async handling so `observe` stays sync and writes stay ordered. */
  private tail: Promise<void> = Promise.resolve();

  constructor(
    private readonly peer: RoomPeer,
    private readonly bundle: FileBundle,
  ) {}

  /** Feed one stream part (call for EVERY part the turn forwards). Never throws. */
  observe(part: StreamPart): void {
    this.tail = this.tail.then(() => this.handle(part)).catch(() => {});
  }

  /** Await all queued work. Call once the stream ends, before rollbackDangling. */
  async drain(): Promise<void> {
    await this.tail;
  }

  /**
   * Delete any path we optimistically streamed but that `execute` never
   * confirmed (a severed/truncated stream, or a rejected op). Idempotent; call
   * on every turn exit path after `drain()`. Returns the paths dropped.
   */
  rollbackDangling(): string[] {
    const dropped: string[] = [];
    for (const s of this.calls.values()) {
      if (s.streaming && s.path && !s.finalized) {
        this.peer.delete(s.path);
        s.finalized = true; // don't double-drop
        dropped.push(s.path);
      }
    }
    return dropped;
  }

  private async handle(part: StreamPart): Promise<void> {
    switch (part.type) {
      case "tool-input-start":
        if (part.toolName === ADD_FILE && part.id) {
          this.calls.set(part.id, {
            buf: "",
            streaming: false,
            skipped: false,
            flushedLen: 0,
            finalized: false,
          });
        }
        return;

      case "tool-input-delta": {
        const s = part.id ? this.calls.get(part.id) : undefined;
        if (!s || s.skipped) return;
        s.buf += part.delta ?? "";
        const content = await this.decodeContent(s.buf);
        if (content === undefined) return; // content not started (or unparseable yet)

        // Gate exactly once: content having started means `path` is final.
        if (!s.streaming) {
          const path = await this.decodePath(s.buf);
          if (path && isStreamablePath(path) && !this.bundle.has(path)) {
            s.streaming = true;
            s.path = path;
          } else {
            s.skipped = true; // non-md / already-exists → execute owns it, no preview
            return;
          }
        }
        this.flushLines(s, content);
        return;
      }

      case "tool-input-end": {
        const s = part.id ? this.calls.get(part.id) : undefined;
        if (!s || !s.streaming || !s.path) return;
        const content = await this.decodeContent(s.buf);
        // Final flush: the whole body (incl. the trailing partial line).
        if (content !== undefined && content.length > s.flushedLen) {
          this.peer.set(s.path, content, false); // addFile preview → unmarked
          s.flushedLen = content.length;
        }
        return;
      }

      case "tool-result": {
        const s = part.toolCallId ? this.calls.get(part.toolCallId) : undefined;
        if (!s || !s.streaming || !s.path) return;
        const out = part.output as { ok?: boolean } | undefined;
        if (out?.ok) {
          s.finalized = true; // execute wrote authoritatively — keep the doc content
        } else {
          this.peer.delete(s.path); // execute rejected — undo the optimistic preview
          s.finalized = true;
        }
        return;
      }
    }
  }

  /** Write all COMPLETE lines of `content` past the watermark (diff ⇒ tail insert). */
  private flushLines(s: CallState, content: string): void {
    const lastNl = content.lastIndexOf("\n");
    const boundary = lastNl + 1; // include the newline; 0 when there is none yet
    if (boundary > s.flushedLen) {
      // Streaming only ever previews addFile bodies (a new file) → unmarked, so
      // the growing tail isn't re-highlighted (and re-rendered) each flush.
      this.peer.set(s.path!, content.slice(0, boundary), false);
      s.flushedLen = boundary;
    }
  }

  private async decodeContent(buf: string): Promise<string | undefined> {
    const { value } = await parsePartialJson(buf);
    const c = (value as { content?: unknown } | undefined)?.content;
    return typeof c === "string" ? c : undefined;
  }

  private async decodePath(buf: string): Promise<string> {
    const { value } = await parsePartialJson(buf);
    const p = (value as { path?: unknown } | undefined)?.path;
    return typeof p === "string" ? p.trim() : "";
  }
}
