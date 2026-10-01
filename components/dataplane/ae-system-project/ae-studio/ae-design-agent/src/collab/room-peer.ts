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

// The agents service's side of #86 phase 4: join a spec room as a live Yjs
// peer for the duration of one turn. The LLM only ever sees plain text — this
// module owns the doc: snapshot for the turn's file bundle, per-op writes via
// @aep/collab-doc (Y.Text diff-and-patch, md fragment reparse), presence as
// an agent (`kind: "agent"` — the console renders square avatars, #86 d7).
//
// Access is REQUEST-SCOPED (#86 d7): the caller's bearer rides the turn
// payload and the collab server's BFF oracle validates it exactly like a
// browser join. The ws URL comes from service config (AGENT_COLLAB_WS_URL);
// the caller names only the room.

import {
  HocuspocusProvider,
  HocuspocusProviderWebsocket,
} from "@hocuspocus/provider";
import WebSocket from "ws";
import type * as Y from "yjs";
import {
  deleteDocFile,
  setDocFile,
  setDocFileAsAgent,
  snapshotDoc,
} from "@aep/collab-doc";

/** Transaction origin for every doc write this peer makes. */
export const AGENT_ORIGIN = "aep-agent";

/**
 * The reason the collab server tags a refusal with when ITS upstream was
 * unreachable, rather than when the bearer was refused. Duplicated from
 * `services/collab/src/server.ts` — the console spells it out on its side of
 * this socket too, the same way the stateless message types are.
 */
const UPSTREAM_UNAVAILABLE = "upstream-unavailable";

const SYNC_TIMEOUT_MS = 10_000;

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
  /** Detach presence and close the connection. Safe to call twice. */
  leave(): void;
}

export interface JoinRoomInput {
  /** Collab server ws URL (service config, e.g. ws://collab-server:3400). */
  url: string;
  /** Room id (`spec-<org>-<project>`), resolved by the BFF. */
  roomId: string;
  /** The caller's bearer, validated by the collab server's oracle. */
  token: string;
  /** Presence label (defaults to "Spec Agent"). */
  agentName?: string;
}

/**
 * Join a room and resolve once the doc has synced (or reject on auth
 * failure / timeout — a room-scoped turn must not run against an empty
 * unsynced replica).
 */
export async function joinRoom(input: JoinRoomInput): Promise<RoomPeer> {
  // Explicit websocket sub-provider so the Node `ws` implementation is pinned
  // (the polyfill knob lives on the websocket configuration, not the provider).
  const socket = new HocuspocusProviderWebsocket({
    url: input.url,
    WebSocketPolyfill: WebSocket,
  });
  const provider = new HocuspocusProvider({
    websocketProvider: socket,
    name: input.roomId,
    token: input.token,
  });

  provider.setAwarenessField("user", {
    name: input.agentName ?? "Spec Agent",
    color: "#b57edc",
    kind: "agent",
  });

  try {
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => {
        // A room the server REFUSES does not land here — that arrives
        // immediately as `authenticationFailed` below, because a rejected load
        // is answered with a permission-denied frame rather than a closed
        // socket. This branch is the genuinely slow or silent room: a seed
        // still running, or a socket that never came up at all.
        reject(
          new Error(
            `collab: room ${input.roomId} could not be loaded — no sync within ${SYNC_TIMEOUT_MS}ms`,
          ),
        );
      }, SYNC_TIMEOUT_MS);
      provider.on("synced", () => {
        clearTimeout(timer);
        resolve();
      });
      // A refused room and a refused bearer arrive through the SAME event
      // (#586): the collab server answers an unseedable room with a
      // permission-denied frame too, tagging it so the two can be told apart.
      // Both are terminal for this turn — an agent has no committed copy to
      // fall back on the way the console does, and writing into a room that
      // was never seeded is what corrupted the spec in the first place — but
      // they must not be REPORTED alike, or an `aep-api` restart shows up in
      // the turn log as a credentials problem.
      provider.on("authenticationFailed", ({ reason }: { reason?: string }) => {
        clearTimeout(timer);
        reject(
          new Error(
            reason === UPSTREAM_UNAVAILABLE
              ? `collab: room ${input.roomId} is unavailable — the collab server could not reach its upstream (${reason})`
              : `collab: room ${input.roomId} rejected the forwarded bearer`,
          ),
        );
      });
      provider.attach();
    });
  } catch (err) {
    provider.destroy();
    socket.destroy();
    throw err;
  }

  const doc: Y.Doc = provider.document;
  const agentName = input.agentName ?? "Spec Agent";
  let left = false;
  return {
    files: () => snapshotDoc(doc),
    set: (path, content, mark) => {
      if (!mark) {
        // A brand-new file (addFile) is accept-by-default — there is nothing to
        // review on content the agent is creating. Write it PLAINLY so chunked
        // streaming doesn't paint it as reviewable edits, which would re-render
        // the highlighted tail on every line flush (visible flicker).
        setDocFile(doc, path, content, AGENT_ORIGIN);
        return;
      }
      // Reviewable, character-exact write (#86 phase 6): inserted ranges get
      // the agentInsertion mark; the caret rides awareness so every browser
      // renders the agent's cursor at its last insertion.
      const { caret } = setDocFileAsAgent(doc, path, content, AGENT_ORIGIN, {
        agent: agentName,
        at: new Date().toISOString(),
      });
      if (caret) {
        provider.setAwarenessField("cursor", { anchor: caret, head: caret });
      }
    },
    delete: (path) => deleteDocFile(doc, path, AGENT_ORIGIN),
    leave: () => {
      if (left) return;
      left = true;
      provider.setAwarenessField("cursor", null);
      provider.destroy();
      socket.destroy(); // we created the sub-provider, we close it
    },
  };
}
