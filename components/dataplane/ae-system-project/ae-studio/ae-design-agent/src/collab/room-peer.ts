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

// The design agent's side of a spec Room: join it as a live Yjs peer for the
// duration of one turn. The LLM only ever sees plain text; this module owns
// the doc: snapshot for the turn's file bundle, per-op writes via
// @aep/collab-doc (Y.Text diff-and-patch, md fragment reparse), presence as
// an agent (`kind: "agent"`, the console renders square avatars, #86 d7).
//
// The pod joins through `local-room.ts` (07 §9): ae-collab's Room socket,
// whose access is the agent's identity (no token is sent), and the credited
// user as a connection parameter.
//
// Every connection gets a FRESH Y.Doc, and a dropped connection is never
// resumed with the doc it had (C13): a client that reconnects with a kept doc
// merges its old copy of the seed into a Room that re-seeded after a restart,
// and the document doubles. A dropped connection is replaced by a new one
// (new doc), and the files this peer wrote are written again where
// the new doc differs.
//
// The rejoin is bounded (`REJOIN_POLICY`): a refused or silent attempt backs
// off, and the peer gives up after the last attempt. Writes the Room never
// confirmed are not hidden: `leave()` resolves to their count (logged as
// `room_writes_dropped`), and the turn ends failed on it.

import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
} from "@hocuspocus/provider";
import WebSocket from "ws";
import * as Y from "yjs";
import {
  deleteDocFile,
  readDocFile,
  setDocFile,
  setDocFileAsAgent,
  snapshotDoc,
} from "@aep/collab-doc";

/** Transaction origin for every doc write this peer makes. */
export const AGENT_ORIGIN = "aep-agent";

/**
 * The reason the collab server tags a refusal with when ITS upstream was
 * unreachable, rather than when the join itself was refused. Duplicated from
 * `components/dataplane/ae-system-project/ae-studio/ae-collab/src/pod/auth.ts`
 * (the console spells it out on its side of this socket too).
 */
const UPSTREAM_UNAVAILABLE = "upstream-unavailable";

const SYNC_TIMEOUT_MS = 10_000;
/**
 * How a dropped connection is replaced. The first attempt goes at once; after
 * a failed one the wait starts at 1 s and doubles to a 15 s cap; an attempt
 * that has not synced in 10 s has failed. After 10 attempts the peer gives up:
 * 0 + 1 + 2 + 4 + 8 + 15 × 5 = 90 s of waits, plus at most 10 s per attempt,
 * so about 2 minutes at worst, well inside the 30-minute turn cap.
 */
export interface RejoinPolicy {
  firstDelayMs: number;
  maxDelayMs: number;
  attemptTimeoutMs: number;
  maxAttempts: number;
  /** How long `leave()` waits for a rejoin in flight to sync before it counts the writes as dropped. */
  leaveWaitMs: number;
}

const REJOIN_POLICY: RejoinPolicy = {
  firstDelayMs: 1_000,
  maxDelayMs: 15_000,
  attemptTimeoutMs: 10_000,
  maxAttempts: 10,
  leaveWaitMs: 3_000,
};

/** One structured, value-free log line (no token, no content). */
export interface RoomLogLine {
  msg: "room_rejoin" | "room_rejoin_failed" | "room_writes_dropped";
  source: "ae-design-agent";
  /** The rejoin attempt (1-based). */
  attempt?: number;
  /** With `room_rejoin_failed`: the last attempt, the peer gave up. */
  gaveUp?: boolean;
  /** With `room_writes_dropped`: files written this turn the Room never confirmed. */
  count?: number;
}

const stdoutLog = (line: RoomLogLine): void => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};

export interface RoomPeer {
  /** The synced doc's files (path → content) — the turn's initial bundle. */
  files(): Record<string, string>;
  /**
   * Write one file into the live doc (agent-origin transaction). `mark=true`
   * records the change as reviewable agent insertions (an edit to committed
   * content); `mark=false` writes it plainly — a brand-new file the agent is
   * creating is accept-by-default, so it carries no review highlights (and,
   * load-bearing for streaming: an unmarked write does not re-render a
   * highlighted tail on every line flush → no flicker).
   */
  set(path: string, content: string, mark: boolean): void;
  /** Remove one file from the live doc. */
  delete(path: string): void;
  /**
   * Clear presence and close the connection. Resolves to the number of files
   * this peer wrote that the Room never confirmed (a rejoin that failed or
   * had not synced): non-zero means the turn's edits did not all land.
   * Waits at most `leaveWaitMs` for a rejoin in flight. Safe to call twice.
   */
  leave(): Promise<number>;
}

export interface JoinRoomInput {
  /**
   * The collab listener's ws URL: in the pod, the Room socket's
   * `ws+unix:<path>:/` (`local-room.ts`). No token is sent: the listener
   * the peer reaches decides who it is.
   */
  url: string;
  /** Room id: `spec-<orgHandle>-<project>`. */
  roomId: string;
  /** Connection parameters, sent in the upgrade URL's query (the Room socket's `credit`). */
  parameters?: Record<string, string>;
  /** Presence label (defaults to "Spec Agent"). */
  agentName?: string;
  /** Overrides of `REJOIN_POLICY` (tests shorten it). */
  rejoin?: Partial<RejoinPolicy>;
  /** Where log lines go; stdout unless a test captures them. */
  log?: (line: RoomLogLine) => void;
}

/** One connection to the Room: its own socket, provider and doc. */
interface Connection {
  socket: HocuspocusProviderWebsocket;
  provider: HocuspocusProvider;
  doc: Y.Doc;
  synced: boolean;
}

/** What the peer wrote this turn: content, or `null` for a removed file. */
type Written = { content: string; mark: boolean } | null;

function withParameters(url: string, parameters: Record<string, string> | undefined): string {
  if (!parameters || Object.keys(parameters).length === 0) return url;
  const u = new URL(url);
  for (const [key, value] of Object.entries(parameters)) u.searchParams.set(key, value);
  return u.toString();
}

function refusalError(roomId: string, reason: string | undefined): Error {
  return new Error(
    reason === UPSTREAM_UNAVAILABLE
      ? `collab: room ${roomId} is unavailable — the collab server could not reach its upstream (${reason})`
      : `collab: room ${roomId} refused the join (${reason ?? "no reason"})`,
  );
}

/**
 * Join a room and resolve once the doc has synced (or reject on auth
 * failure / timeout — a room-scoped turn must not run against an empty
 * unsynced replica).
 */
export async function joinRoom(input: JoinRoomInput): Promise<RoomPeer> {
  const agentName = input.agentName ?? "Spec Agent";
  const url = withParameters(input.url, input.parameters);

  /** A new connection, not yet attached: a fresh doc every time (C13). */
  const open = (): Connection => {
    // Explicit websocket sub-provider so the Node `ws` implementation is
    // pinned (the polyfill knob lives on the websocket configuration). The
    // trailing slash is kept: in `ws+unix:<path>:/` it is the request path.
    const socket = new HocuspocusProviderWebsocket({ url, WebSocketPolyfill: WebSocket, preserveTrailingSlash: true });
    const doc = new Y.Doc();
    // No token: the provider's auth message carries an empty one.
    const provider = new HocuspocusProvider({ websocketProvider: socket, name: input.roomId, document: doc });
    provider.setAwarenessField("user", { name: agentName, color: "#b57edc", kind: "agent" });
    return { socket, provider, doc, synced: false };
  };
  const close = (c: Connection): void => {
    // Presence goes first, so every other peer drops the agent at once.
    c.provider.awareness?.setLocalState(null);
    c.provider.destroy();
    c.socket.destroy(); // we created the sub-provider, we close it
  };

  let conn = open();
  try {
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => {
        // A room the server REFUSES does not land here — that arrives
        // immediately as `authenticationFailed` below, because a rejected load
        // is answered with a permission-denied frame rather than a closed
        // socket. This branch is the genuinely slow or silent room: a seed
        // still running, or a socket that never came up at all.
        reject(new Error(`collab: room ${input.roomId} could not be loaded — no sync within ${SYNC_TIMEOUT_MS}ms`));
      }, SYNC_TIMEOUT_MS);
      conn.provider.on("synced", () => {
        clearTimeout(timer);
        conn.synced = true;
        resolve();
      });
      // A refused room and a refused join arrive through the SAME event
      // (#586): the collab server answers an unseedable room with a
      // permission-denied frame too, tagging it so the two can be told apart.
      // Both are terminal for this turn — an agent has no committed copy to
      // fall back on the way the console does, and writing into a room that
      // was never seeded is what corrupted the spec in the first place — but
      // they must not be REPORTED alike, or a restart upstream shows up in
      // the turn log as an access problem.
      conn.provider.on("authenticationFailed", ({ reason }: { reason?: string }) => {
        clearTimeout(timer);
        reject(refusalError(input.roomId, reason));
      });
      conn.provider.attach();
    });
  } catch (err) {
    close(conn);
    throw err;
  }

  const policy: RejoinPolicy = { ...REJOIN_POLICY, ...input.rejoin };
  const log = input.log ?? stdoutLog;
  const written = new Map<string, Written>();
  let left = false;
  /** The rejoin gave up: what was written is not in the Room. */
  let lost = false;
  /** Writes already reported as dropped (at the give-up). */
  let reported = 0;
  /** Failed attempts of the current rejoin. */
  let attempt = 0;
  let retry: ReturnType<typeof setTimeout> | undefined;
  /** Told when the current connection syncs or the peer gives up. */
  let settled: Array<() => void> = [];
  const notify = (): void => {
    const waiters = settled;
    settled = [];
    for (const w of waiters) w();
  };

  const write = (c: Connection, path: string, w: Written): void => {
    if (w === null) {
      deleteDocFile(c.doc, path, AGENT_ORIGIN);
    } else if (!w.mark) {
      // A brand-new file (addFile) is accept-by-default — there is nothing to
      // review on content the agent is creating. Write it PLAINLY so chunked
      // streaming doesn't paint it as reviewable edits, which would re-render
      // the highlighted tail on every line flush (visible flicker).
      setDocFile(c.doc, path, w.content, AGENT_ORIGIN);
    } else {
      // Reviewable, character-exact write (#86 phase 6): inserted ranges get
      // the agentInsertion mark; the caret rides awareness so every browser
      // renders the agent's cursor at its last insertion.
      const { caret } = setDocFileAsAgent(c.doc, path, w.content, AGENT_ORIGIN, {
        agent: agentName,
        at: new Date().toISOString(),
      });
      if (caret) c.provider.setAwarenessField("cursor", { anchor: caret, head: caret });
    }
  };

  /** Write again what this peer wrote, where the new doc differs. */
  const restore = (c: Connection): void => {
    for (const [path, w] of written) {
      const current = readDocFile(c.doc, path);
      if (w === null ? current !== undefined : current !== w.content) write(c, path, w);
    }
  };

  /** The wait before attempt `n` (1-based): none for the first, then doubling to the cap. */
  const delayBefore = (n: number): number =>
    n <= 1 ? 0 : Math.min(policy.firstDelayMs * 2 ** (n - 2), policy.maxDelayMs);

  /** Give up: the writes of this turn are not confirmed in the Room. */
  const giveUp = (): void => {
    lost = true;
    reported = written.size;
    if (reported > 0) log({ msg: "room_writes_dropped", source: "ae-design-agent", count: reported });
    notify();
  };

  /** The next attempt to replace a dropped connection, after its backoff. */
  const scheduleRejoin = (): void => {
    if (left || lost) return;
    attempt++;
    retry = setTimeout(rejoin, delayBefore(attempt));
    retry.unref?.();
  };

  /** One attempt: a new connection with a fresh doc. */
  const rejoin = (): void => {
    retry = undefined;
    if (left || lost) return;
    log({ msg: "room_rejoin", source: "ae-design-agent", attempt });
    const next = open();
    conn = next;
    const fail = (): void => {
      if (left || conn !== next || next.synced) return;
      clearTimeout(deadline);
      close(next);
      const gaveUp = attempt >= policy.maxAttempts;
      log({ msg: "room_rejoin_failed", source: "ae-design-agent", attempt, ...(gaveUp ? { gaveUp } : {}) });
      if (gaveUp) giveUp();
      else scheduleRejoin();
    };
    // A silent Room (no sync, no refusal) fails the attempt too.
    const deadline = setTimeout(fail, policy.attemptTimeoutMs);
    deadline.unref?.();
    next.provider.on("synced", () => {
      if (left || conn !== next || next.synced) return;
      clearTimeout(deadline);
      next.synced = true;
      attempt = 0;
      restore(next);
      notify();
    });
    next.provider.on("authenticationFailed", fail);
    watch(next);
    next.provider.attach();
  };

  /** Never resume a synced connection with its kept doc: replace it. */
  const watch = (c: Connection): void => {
    c.provider.on("close", () => {
      if (left || conn !== c || !c.synced) return;
      c.synced = false;
      close(c);
      scheduleRejoin();
    });
  };
  watch(conn);

  /** The writes the Room has not confirmed, after a bounded wait for a rejoin in flight. */
  const unconfirmed = async (): Promise<number> => {
    if (conn.synced) return 0;
    if (!lost && written.size > 0) {
      let timer: ReturnType<typeof setTimeout> | undefined;
      await new Promise<void>((resolve) => {
        settled.push(resolve);
        timer = setTimeout(resolve, policy.leaveWaitMs);
      });
      clearTimeout(timer);
    }
    return conn.synced ? 0 : written.size;
  };

  let leaving: Promise<number> | undefined;
  const leave = async (): Promise<number> => {
    const dropped = await unconfirmed();
    left = true;
    if (retry) clearTimeout(retry);
    close(conn);
    // What the give-up reported is not counted twice in the log.
    if (dropped > reported) log({ msg: "room_writes_dropped", source: "ae-design-agent", count: dropped - reported });
    return dropped;
  };

  return {
    files: () => snapshotDoc(conn.doc),
    set: (path, content, mark) => {
      const w = { content, mark };
      written.set(path, w);
      // Unsynced (rejoining), the write waits for the new doc's `restore`.
      if (conn.synced) write(conn, path, w);
    },
    delete: (path) => {
      written.set(path, null);
      if (conn.synced) write(conn, path, null);
    },
    leave: () => (leaving ??= leave()),
  };
}
