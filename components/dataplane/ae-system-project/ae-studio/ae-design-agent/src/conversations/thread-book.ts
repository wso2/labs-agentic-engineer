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
 * The pod's per-project current thread. Every member of a
 * project shares one current thread, minted lazily as a plain uuid; rotating
 * replaces it and drops the old thread's messages, which are never listed or
 * served again. The book holds only the current id, its creator and the last
 * turn's context size; the messages stay behind the `ConversationStore` port.
 * Nothing outlives the process: after a pod roll every project starts fresh
 * (a local run may `resume` a thread it kept).
 *
 * Auto-rotation ports `A/spec/context_rotation.go`: a send is admitted only
 * to the current thread, and a thread whose last measured context is past
 * 80 % of the connection's declared window is rotated before the send, which
 * is then refused (`409 conversation_rotated`). A connection that declares no
 * window has no token bound, so the thread's stored messages are bounded
 * instead: past `THREAD_FALLBACK_BYTES` (base64 attachments counted)
 * it rotates the same way. Without it, a thread on such a connection would
 * grow in the pod's memory until the pod's death.
 */

import { randomUUID } from "node:crypto";
import type { components } from "../generated/api.js";
import { projectDisplayHistory, type DisplayMessage } from "../conversation/display-history.js";
import type { ConversationStore } from "../store/conversation-store.js";

export type ThreadView = components["schemas"]["ProjectConversationView"];

/** Rotation fires once the context is past ROTATE_AT_NUM/ROTATE_AT_DEN of the window. */
const ROTATE_AT_NUM = 4;
const ROTATE_AT_DEN = 5;

/**
 * The stored size past which a thread rotates when the connection declares no
 * context window: 8 MiB of messages, base64 file parts included.
 */
export const THREAD_FALLBACK_BYTES = 8 << 20;

/**
 * The approximate stored size of `value`: string lengths (base64 and most
 * prose are one byte a character), binary byte lengths, object keys.
 */
function storedBytes(value: unknown): number {
  if (typeof value === "string") return value.length;
  if (value instanceof Uint8Array || value instanceof ArrayBuffer) return value.byteLength;
  if (Array.isArray(value)) return value.reduce((n: number, v) => n + storedBytes(v), 0);
  if (typeof value === "object" && value !== null) {
    return Object.entries(value).reduce((n, [k, v]) => n + k.length + storedBytes(v), 0);
  }
  return 8;
}

/** Whether `tokens` is past the rotation share of `window` (strictly, as in Go). */
function contextFull(tokens: number, window: number): boolean {
  return tokens * ROTATE_AT_DEN > window * ROTATE_AT_NUM;
}

interface Thread {
  id: string;
  createdAt: Date;
  /** The JWT display name of the member who opened the thread. */
  createdBy?: string;
  /** The context size the thread's last finished turn measured. */
  contextTokens?: number;
}

export interface ThreadBookDeps {
  store: ConversationStore;
  now?: () => Date;
}

export class ThreadBook {
  private readonly threads = new Map<string, Thread>();
  private readonly store: ConversationStore;
  private readonly now: () => Date;

  constructor(deps: ThreadBookDeps) {
    this.store = deps.store;
    this.now = deps.now ?? (() => new Date());
  }

  /** The project's current thread, opened (credited to `by`) when there is none. */
  current(project: string, by?: string): ThreadView {
    return view(this.threads.get(project) ?? this.open(project, by));
  }

  /**
   * Adopt `conversationId` as the project's current thread when it has none:
   * a local run (the playground) keeps its thread across processes, its
   * messages behind a file store. The pod never calls this. A project that
   * already has a thread keeps it.
   */
  resume(project: string, conversationId: string): ThreadView {
    const open = this.threads.get(project);
    if (open) return view(open);
    const thread: Thread = { id: conversationId, createdAt: this.now() };
    this.threads.set(project, thread);
    return view(thread);
  }

  /** Replace the project's current thread with a fresh one credited to `by`. */
  async rotate(project: string, by?: string): Promise<ThreadView> {
    const old = this.threads.get(project);
    const fresh = this.open(project, by);
    if (old) await this.store.delete(old.id);
    return view(fresh);
  }

  /**
   * The current thread's display history, oldest first: `[]` before its first
   * turn, `null` for any id that is not this project's current thread.
   */
  async history(project: string, conversationId: string): Promise<DisplayMessage[] | null> {
    if (this.threads.get(project)?.id !== conversationId) return null;
    const conversation = await this.store.get(conversationId);
    return conversation ? projectDisplayHistory(conversation) : [];
  }

  /**
   * Record a finished turn's context size. A turn that ran on a thread that is
   * no longer current says nothing about the thread that replaced it.
   */
  noteContextTokens(project: string, conversationId: string, tokens: number): void {
    const thread = this.threads.get(project);
    if (thread?.id === conversationId) thread.contextTokens = tokens;
  }

  /**
   * Whether a send to `conversationId` may start. `"rotated"` when the id is
   * not the project's current thread, or when the current thread was full
   * and has just been rotated away; the next `current()` opens a fresh one,
   * credited to whoever asks.
   */
  async admit(project: string, conversationId: string, contextWindow?: number): Promise<"ok" | "rotated"> {
    const thread = this.threads.get(project);
    if (thread?.id !== conversationId) return "rotated";
    if (!(await this.full(thread, contextWindow))) return "ok";
    this.threads.delete(project);
    await this.store.delete(thread.id);
    return "rotated";
  }

  /** Past 80 % of a declared window, or past `THREAD_FALLBACK_BYTES` stored when none is declared. */
  private async full(thread: Thread, contextWindow: number | undefined): Promise<boolean> {
    if (contextWindow !== undefined && contextWindow > 0) {
      return thread.contextTokens !== undefined && contextFull(thread.contextTokens, contextWindow);
    }
    const stored = await this.store.get(thread.id);
    return stored !== null && storedBytes(stored.messages) > THREAD_FALLBACK_BYTES;
  }

  private open(project: string, by?: string): Thread {
    const thread: Thread = { id: randomUUID(), createdAt: this.now(), ...(by ? { createdBy: by } : {}) };
    this.threads.set(project, thread);
    return thread;
  }
}

function view(thread: Thread): ThreadView {
  return {
    conversationId: thread.id,
    createdAt: thread.createdAt.toISOString(),
    ...(thread.createdBy ? { createdBy: thread.createdBy } : {}),
    current: true,
  };
}
