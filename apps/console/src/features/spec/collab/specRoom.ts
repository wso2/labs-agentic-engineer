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

import { useCallback, useEffect, useSyncExternalStore } from "react";
import * as Y from "yjs";
import { HocuspocusProvider } from "@hocuspocus/provider";
import type { components } from "../../../generated/aep-api";
import { reportPodOutage } from "../../../api/aeStudio";
import {
  getAccessToken,
  redirectToSignIn,
  renewAccessToken,
  subscribeAccessTokenRefresh,
} from "../../../auth/token";
import { useAeStudio } from "../../ae-studio/api/queries";

// The project's spec room (N5): the Room of the org's AE Studio (its ae-collab
// container), `spec-<org>-<project>`, a Y.Doc holding every file under
// specs/ by its repo path. The design agent writes into it too, so its edits
// arrive as the room's updates, marked as the agent's.
//
// ONE room per project for the whole app, shared by every component that
// reads the spec (the cards, the overview, the chat beside them): a module-
// level room, counted by who is using it, and closed a little after the last
// one lets go, so moving between cards does not reconnect.
//
// Its address comes from AE Studio's `ready` answer (`urls.collab`), and the
// room is joined only then. A later `provisioning` keeps the held address and
// the open socket (most converges leave the pod running; a pod that really
// restarts drops the socket, which the drop paths below answer); `failed` or
// `absent` leaves it.
//
// Its lifecycle is the old console's (useCollabSpec at the classic-console
// tag), copied rather than shared (one consumer each): a doc never outlives
// the connection that filled it — the Room reseeds from git on every load,
// and a reconnect that carried a seeded doc back would double every file — so
// a drop after sync throws the doc away and joins fresh, backing off while
// drops keep coming. The Room holds a connection only until its token's `exp`,
// so every OIDC renewal goes up through Hocuspocus token sync (`sendToken()`),
// which moves the deadline. A bearer the Room drops anyway (expired, or a
// refused join or pushed token) renews the session once per room and rejoins;
// a second loss before that rejoin synced is a verdict a new token did not
// change, so the room stays offline. A refusal tagged as the Room's own
// upstream being down (the IdP's keys or the pod's Files socket) is retried,
// and re-reads AE Studio.

export type RoomStatus = "connecting" | "connected" | "offline";

/** A soft problem the Room's last commit reported about one file. */
export interface FlushWarning {
  path: string;
  message: string;
}

export interface RoomState {
  /** The room's doc once synced; null before, and while rejoining. */
  doc: Y.Doc | null;
  status: RoomStatus;
  /** The last failure to commit the room's edits to git; null when clear. */
  flushError: string | null;
  /** The warnings the Room's last commit reported; each commit's set replaces the last. */
  flushWarnings: FlushWarning[];
}

type AeStudio = components["schemas"]["AeStudio"];

// The reasons ae-collab gives the client, spelled as its pod/auth.ts and
// pod/expiry.ts spell them.
/** A refusal that is not about the bearer: the Room's own upstream failed. Retry. */
const UPSTREAM_UNAVAILABLE = "upstream-unavailable";
/** A refusal verdict, on joining or on a token the client pushed. */
const PERMISSION_DENIED = "permission-denied";
/** The Room closed the connection at its token's `exp`. */
const TOKEN_EXPIRED = "token-expired";

const REBUILD_DELAY_MS = 1_000;
const REBUILD_MAX_DELAY_MS = 30_000;
const REBUILD_RESET_MS = 30_000;
// A commit is one write through the pod's Files socket. The deadlines nest,
// and this one must stay the outermost: the pod answers a Files-socket
// request within its own budget (40 s, ae-studio-tools edge/files_sock.go),
// and ae-collab gives up on the socket at 45 s (files-client.ts) and reports
// that as a flush-error. Waiting less reports a slow but good save as failed.
const FLUSH_TIMEOUT_MS = 50_000;
/** How long a room stays open after the last user lets go. */
const LINGER_MS = 5_000;

/**
 * The Room address to hold after AE Studio's latest answer, given the one
 * held now: `ready` names it, `provisioning` keeps it, anything else (or no
 * answer yet) is none.
 */
function heldRoomUrl(held: string | null, answer: AeStudio | undefined): string | null {
  switch (answer?.state) {
    case "ready":
      return answer.urls?.collab ? `${answer.urls.collab}/v1/rooms` : null;
    case "provisioning":
      return held;
    default:
      return null;
  }
}

/** The well-formed entries of a `flush-warnings` message's `warnings`. */
function readFlushWarnings(raw: unknown): FlushWarning[] {
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((w: unknown) => {
    const { path, message } = (w ?? {}) as { path?: unknown; message?: unknown };
    return typeof path === "string" && typeof message === "string" ? [{ path, message }] : [];
  });
}

const NO_WARNINGS: FlushWarning[] = [];

class SpecRoom {
  state: RoomState = { doc: null, status: "connecting", flushError: null, flushWarnings: NO_WARNINGS };
  private readonly listeners = new Set<() => void>();
  private users = 0;
  private url: string | null = null;
  private lingerTimer: ReturnType<typeof setTimeout> | null = null;
  private provider: HocuspocusProvider | null = null;
  private doc: Y.Doc | null = null;
  private synced = false;
  private syncedAt = 0;
  private attempts = 0;
  private rebuildTimer: ReturnType<typeof setTimeout> | null = null;
  /** The bearer was lost again before a renewed rejoin synced: offline until the room closes. */
  private authFailed = false;
  /** This room already renewed the session for a lost bearer; a sync earns the next loss its own. */
  private renewalSpent = false;
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
    if (this.url && !this.provider && !this.rebuildTimer && !this.authFailed) this.join();
    return () => {
      this.listeners.delete(fn);
      this.users -= 1;
      if (this.users === 0) this.lingerTimer = setTimeout(() => this.close(), LINGER_MS);
    };
  }

  /** Follow AE Studio's latest answer: join at a new address, keep it through `provisioning`, leave it otherwise. */
  follow(answer: AeStudio | undefined): void {
    const next = heldRoomUrl(this.url, answer);
    if (next === this.url) return;
    this.url = next;
    this.stop();
    this.authFailed = false;
    this.renewalSpent = false;
    this.attempts = 0;
    if (!next) {
      this.set({ status: "offline" });
      return;
    }
    if (this.users > 0) this.join();
  }

  private set(next: Partial<RoomState>): void {
    this.state = { ...this.state, ...next };
    for (const fn of this.listeners) fn();
  }

  private join(): void {
    const url = this.url;
    if (!url) return;
    const doc = new Y.Doc();
    this.doc = doc;
    this.synced = false;
    this.set({ doc: null, status: "connecting" });
    const provider = new HocuspocusProvider({
      url,
      name: this.name,
      document: doc,
      token: async () => (await getAccessToken()) ?? "",
      onSynced: () => {
        this.synced = true;
        this.syncedAt = Date.now();
        this.renewalSpent = false;
        this.set({ doc, status: "connected" });
      },
      onStatus: ({ status }) => {
        if (status === "disconnected" && this.provider === provider) {
          this.set({ status: "offline" });
          this.scheduleRebuild(true);
        }
      },
      // A refusal is about the bearer only when it is a verdict: the Room
      // reports its own upstream (the IdP's keys, the Files socket) being down
      // as `upstream-unavailable`, and the pod may be restarting.
      onAuthenticationFailed: ({ reason }) => {
        if (this.provider !== provider) return;
        if (reason === UPSTREAM_UNAVAILABLE) {
          this.set({ status: "offline" });
          reportPodOutage();
          this.scheduleRebuild(false);
          return;
        }
        this.onBearerLost();
      },
      // The Room ends a connection with a Close message, not an auth failure,
      // when its token ran out or a pushed token was refused; the socket's own
      // close is `onStatus`'s.
      onClose: ({ event }) => {
        if (this.provider !== provider) return;
        if (event.reason === TOKEN_EXPIRED || event.reason === PERMISSION_DENIED) this.onBearerLost();
      },
    });
    provider.on("stateless", ({ payload }: { payload: string }) => this.onStateless(payload));
    this.unsubscribeToken = subscribeAccessTokenRefresh(() => {
      if (this.provider === provider) void provider.sendToken();
    });
    this.provider = provider;
    provider.attach();
  }

  /**
   * The Room dropped the bearer. Renew the session once for this room and
   * rejoin with the fresh token (or send the user to sign in when it cannot
   * be renewed); a second loss before that rejoin synced stays offline.
   */
  private onBearerLost(): void {
    const url = this.url;
    if (this.rebuildTimer) {
      clearTimeout(this.rebuildTimer);
      this.rebuildTimer = null;
    }
    this.leave();
    this.set({ status: "offline" });
    if (this.renewalSpent) {
      this.authFailed = true;
      return;
    }
    this.renewalSpent = true;
    void renewAccessToken().then((token) => {
      // Closed, moved to another address, or rejoined meanwhile: not ours to act on.
      if (this.provider !== null || this.rebuildTimer || this.users === 0 || this.url !== url) return;
      if (token) this.armRebuild();
      else redirectToSignIn();
    });
  }

  private onStateless(payload: string): void {
    let msg: { type?: string; id?: string; message?: string; warnings?: unknown };
    try {
      msg = JSON.parse(payload) as typeof msg;
    } catch {
      return;
    }
    if (msg.type === "flush-warnings") {
      this.set({ flushWarnings: readFlushWarnings(msg.warnings) });
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
    this.leave();
    this.armRebuild();
  }

  /** Join again after the backoff: doubling while drops keep coming, from the start after a healthy session. */
  private armRebuild(): void {
    const held = this.syncedAt > 0 && Date.now() - this.syncedAt >= REBUILD_RESET_MS;
    this.syncedAt = 0;
    const attempt = held ? 0 : this.attempts;
    this.attempts = attempt + 1;
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

  /** Hide the last commit's warnings until the next commit reports some. */
  dismissFlushWarnings(): void {
    if (this.state.flushWarnings.length > 0) this.set({ flushWarnings: NO_WARNINGS });
  }

  private leave(): void {
    this.unsubscribeToken?.();
    this.unsubscribeToken = null;
    for (const pending of this.flushes.values()) pending.reject(new Error("The room closed before the commit finished."));
    this.flushes.clear();
    // Forget the provider before destroying it: its own close events then
    // find it no longer current and do nothing.
    const { provider, doc } = this;
    this.provider = null;
    this.doc = null;
    this.synced = false;
    provider?.destroy();
    doc?.destroy();
    this.set({ doc: null });
  }

  /** Leave, and cancel any rejoin. */
  private stop(): void {
    if (this.rebuildTimer) {
      clearTimeout(this.rebuildTimer);
      this.rebuildTimer = null;
    }
    this.leave();
  }

  private close(): void {
    this.lingerTimer = null;
    this.stop();
    this.authFailed = false;
    this.renewalSpent = false;
    this.attempts = 0;
    this.set({ status: "connecting", flushError: null, flushWarnings: NO_WARNINGS });
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

export interface SpecRoomHandle extends RoomState {
  /** Commit the room's pending edits now. */
  flush: () => Promise<void>;
  /** Hide the last commit's warnings. */
  dismissFlushWarnings: () => void;
}

/** The project's room: its doc once synced, its status, its last commit's warnings, and a commit of its edits. */
export function useSpecRoom(orgHandle: string | null, projectName: string, enabled: boolean): SpecRoomHandle {
  const studio = useAeStudio();
  const name = orgHandle ? `spec-${orgHandle}-${projectName}` : null;
  const room = enabled && name ? roomOf(name) : null;
  if (room) byProject.set(projectName, room);
  useEffect(() => room?.follow(studio.data), [room, studio.data]);
  const subscribe = useCallback((fn: () => void) => (room ? room.subscribe(fn) : () => undefined), [room]);
  const state = useSyncExternalStore(subscribe, () => room?.state ?? IDLE);
  const flush = useCallback(() => room?.flush() ?? Promise.resolve(), [room]);
  const dismissFlushWarnings = useCallback(() => room?.dismissFlushWarnings(), [room]);
  return { ...state, flush, dismissFlushWarnings };
}

/**
 * Commit a project's room now, when it is open: before an agent turn starts,
 * so the commit the turn records as its base holds what the room holds — the
 * design's record of what it read depends on it. Nothing to do otherwise.
 */
export function flushSpecRoom(projectName: string): Promise<void> {
  return byProject.get(projectName)?.flush() ?? Promise.resolve();
}

const IDLE: RoomState = { doc: null, status: "connecting", flushError: null, flushWarnings: NO_WARNINGS };
