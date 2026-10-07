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

import { useCallback, useSyncExternalStore } from "react";
import * as Y from "yjs";
import { HocuspocusProvider } from "@hocuspocus/provider";
import { getAccessToken, renewAccessToken, subscribeAccessTokenRefresh } from "../../../auth/token";

// The project's spec room (N5): the collab server's Y.Doc for the project,
// `spec-<org>-<project>`, holding every file under specs/ by its repo path.
// The agent writes into it too, so its edits arrive as the room's updates,
// marked as the agent's.
//
// ONE room per project for the whole app, shared by every component that
// reads the spec (the cards, the overview, the chat beside them): a module-
// level room, counted by who is using it, and closed a little after the last
// one lets go, so moving between cards does not reconnect.
//
// Its lifecycle is the old console's (useCollabSpec at the classic-console tag), copied rather
// than shared (one consumer each): a doc never outlives the connection that
// filled it — the server reseeds from git on every load, and a reconnect that
// carried a seeded doc back would double every file — so a drop after sync
// throws the doc away and joins fresh, backing off while drops keep coming; a
// refused bearer latches the room offline; a refusal tagged as the server's
// own upstream being down is retried; a silently renewed token is pushed to
// the room, and pulled when the server asks.

export type RoomStatus = "connecting" | "connected" | "offline";

export interface RoomState {
  /** The room's doc once synced; null before, and while rejoining. */
  doc: Y.Doc | null;
  status: RoomStatus;
  /** The last failure to commit the room's edits to git; null when clear. */
  flushError: string | null;
}

/** The reason the collab server gives when its own upstream was unreachable, not when the bearer was refused. */
const UPSTREAM_UNAVAILABLE = "upstream-unavailable";
const REBUILD_DELAY_MS = 1_000;
const REBUILD_MAX_DELAY_MS = 30_000;
const REBUILD_RESET_MS = 30_000;
const FLUSH_TIMEOUT_MS = 30_000;
/** How long a room stays open after the last user lets go. */
const LINGER_MS = 5_000;

function collabWsUrl(): string {
  const env = (window as { _env_?: { collabWsUrl?: string } })._env_;
  if (env?.collabWsUrl) return env.collabWsUrl;
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/collab`;
}

class SpecRoom {
  state: RoomState = { doc: null, status: "connecting", flushError: null };
  private readonly listeners = new Set<() => void>();
  private users = 0;
  private lingerTimer: ReturnType<typeof setTimeout> | null = null;
  private provider: HocuspocusProvider | null = null;
  private doc: Y.Doc | null = null;
  private synced = false;
  private syncedAt = 0;
  private attempts = 0;
  private rebuildTimer: ReturnType<typeof setTimeout> | null = null;
  private authFailed = false;
  private unsubscribeToken: (() => void) | null = null;
  private readonly flushes = new Map<string, { resolve: () => void; reject: (e: Error) => void }>();

  constructor(private readonly name: string) {}

  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    this.users += 1;
    if (this.lingerTimer) {
      clearTimeout(this.lingerTimer);
      this.lingerTimer = null;
    }
    if (!this.provider && !this.rebuildTimer) this.join();
    return () => {
      this.listeners.delete(fn);
      this.users -= 1;
      if (this.users === 0) this.lingerTimer = setTimeout(() => this.close(), LINGER_MS);
    };
  }

  private set(next: Partial<RoomState>): void {
    this.state = { ...this.state, ...next };
    for (const fn of this.listeners) fn();
  }

  private join(): void {
    const doc = new Y.Doc();
    this.doc = doc;
    this.synced = false;
    this.set({ doc: null, status: "connecting" });
    const provider = new HocuspocusProvider({
      url: collabWsUrl(),
      name: this.name,
      document: doc,
      token: async () => (await getAccessToken()) ?? "",
      onSynced: () => {
        this.synced = true;
        this.syncedAt = Date.now();
        this.set({ doc, status: "connected" });
      },
      onStatus: ({ status }) => {
        if (status === "disconnected") {
          this.set({ status: "offline" });
          this.scheduleRebuild(true);
        }
      },
      onAuthenticationFailed: ({ reason }) => {
        if (reason === UPSTREAM_UNAVAILABLE) {
          this.set({ status: "offline" });
          this.scheduleRebuild(false);
          return;
        }
        this.authFailed = true;
        if (this.rebuildTimer) {
          clearTimeout(this.rebuildTimer);
          this.rebuildTimer = null;
        }
        this.set({ status: "offline" });
      },
    });
    provider.on("stateless", ({ payload }: { payload: string }) => this.onStateless(provider, payload));
    this.unsubscribeToken = subscribeAccessTokenRefresh((token) => {
      if (this.provider === provider) provider.sendStateless(JSON.stringify({ type: "token", value: token }));
    });
    this.provider = provider;
    provider.attach();
  }

  private onStateless(provider: HocuspocusProvider, payload: string): void {
    let msg: { type?: string; id?: string; message?: string };
    try {
      msg = JSON.parse(payload) as typeof msg;
    } catch {
      return;
    }
    if (msg.type === "token-please" && msg.id) {
      const id = msg.id;
      void (async () => {
        const token = (await renewAccessToken()) ?? (await getAccessToken());
        if (token && this.provider === provider) provider.sendStateless(JSON.stringify({ type: "token", value: token, id }));
      })();
      return;
    }
    if (msg.type === "flush-error") {
      const message = msg.message ?? "Failed to commit the workspace.";
      this.set({ flushError: message });
      if (msg.id) this.settleFlush(msg.id, new Error(message));
      return;
    }
    if (msg.type === "flushed" && msg.id) this.settleFlush(msg.id, null);
  }

  private settleFlush(id: string, error: Error | null): void {
    const pending = this.flushes.get(id);
    if (!pending) return;
    this.flushes.delete(id);
    if (error) pending.reject(error);
    else pending.resolve();
  }

  /** Drop the doc and rejoin after a drop: the server's copy is the truth (see above). */
  private scheduleRebuild(requireSynced: boolean): void {
    if (this.authFailed || this.rebuildTimer) return;
    if (requireSynced && !this.synced) return;
    const held = this.syncedAt > 0 && Date.now() - this.syncedAt >= REBUILD_RESET_MS;
    this.syncedAt = 0;
    const attempt = held ? 0 : this.attempts;
    this.attempts = attempt + 1;
    this.leave();
    this.rebuildTimer = setTimeout(
      () => {
        this.rebuildTimer = null;
        if (this.users > 0) this.join();
      },
      Math.min(REBUILD_DELAY_MS * 2 ** attempt, REBUILD_MAX_DELAY_MS),
    );
  }

  /** Commit the room's pending edits to git now; resolves once done (a build tags HEAD). */
  flush(): Promise<void> {
    const provider = this.provider;
    if (this.state.status !== "connected" || !provider) return Promise.resolve();
    return new Promise<void>((resolve, reject) => {
      const id = crypto.randomUUID();
      const timer = setTimeout(() => {
        this.flushes.delete(id);
        reject(new Error("Timed out waiting for the workspace to commit."));
      }, FLUSH_TIMEOUT_MS);
      this.flushes.set(id, {
        resolve: () => {
          clearTimeout(timer);
          resolve();
        },
        reject: (e) => {
          clearTimeout(timer);
          reject(e);
        },
      });
      provider.sendStateless(JSON.stringify({ type: "flush", id }));
    });
  }

  private leave(): void {
    this.unsubscribeToken?.();
    this.unsubscribeToken = null;
    for (const pending of this.flushes.values()) pending.reject(new Error("The room closed before the commit finished."));
    this.flushes.clear();
    this.provider?.destroy();
    this.provider = null;
    this.doc?.destroy();
    this.doc = null;
    this.synced = false;
    this.set({ doc: null });
  }

  private close(): void {
    this.lingerTimer = null;
    if (this.rebuildTimer) {
      clearTimeout(this.rebuildTimer);
      this.rebuildTimer = null;
    }
    this.leave();
    this.authFailed = false;
    this.attempts = 0;
    this.set({ status: "connecting", flushError: null });
  }
}

const rooms = new Map<string, SpecRoom>();
/** The open room of each project, by project name: what a turn flushes before it starts. */
const byProject = new Map<string, SpecRoom>();

function roomOf(name: string): SpecRoom {
  let room = rooms.get(name);
  if (!room) {
    room = new SpecRoom(name);
    rooms.set(name, room);
  }
  return room;
}

/** The project's room: its doc once synced, its status, and a commit of its edits. */
export function useSpecRoom(orgHandle: string | null, projectName: string, enabled: boolean): RoomState & { flush: () => Promise<void> } {
  const name = orgHandle ? `spec-${orgHandle}-${projectName}` : null;
  const room = enabled && name ? roomOf(name) : null;
  if (room) byProject.set(projectName, room);
  const subscribe = useCallback((fn: () => void) => (room ? room.subscribe(fn) : () => undefined), [room]);
  const state = useSyncExternalStore(subscribe, () => room?.state ?? IDLE);
  const flush = useCallback(() => room?.flush() ?? Promise.resolve(), [room]);
  return { ...state, flush };
}

/**
 * Commit a project's room now, when it is open: before an agent turn starts,
 * so the commit the turn records as its base holds what the room holds — the
 * design's record of what it read depends on it. Nothing to do otherwise.
 */
export function flushSpecRoom(projectName: string): Promise<void> {
  return byProject.get(projectName)?.flush() ?? Promise.resolve();
}

const IDLE: RoomState = { doc: null, status: "connecting", flushError: null };
