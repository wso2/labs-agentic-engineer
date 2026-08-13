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

import { useEffect, useMemo, useRef, useState } from "react";
import * as Y from "yjs";
import { HocuspocusProvider } from "@hocuspocus/provider";
import { listDocPaths } from "@aep/collab-doc";
import {
  getAccessToken,
  renewAccessToken,
  subscribeAccessTokenRefresh,
} from "../../../auth/token";

// Console side of #86 phase 5: connect the spec view to the collab service.
// One room + one Y.Doc per project (`spec-<org>-<project>`), Y.Map('files')
// of path → Y.Text. If no collab server is reachable the view degrades to
// solo (#86 decision 10) — callers keep their non-collaborative fallback.
//
// The connection authenticates with the session's access token (#91): a real
// Thunder JWT in thunder mode (verified by the BFF oracle), the unsigned
// mock token in mock mode (decoded by the collab mock BFF). The token is a
// getter so reconnects pick up silently-renewed tokens. Live refresh also
// pushes `{type:"token"}` over the stateless channel (D6) so long sessions
// keep flushing after the initial handshake token expires.

export interface CollabPeer {
  clientId: number;
  name: string;
  color: string;
  kind: "user" | "agent";
}

export type CollabStatus = "connecting" | "connected" | "offline";

export interface CollabSpec {
  status: CollabStatus;
  peers: CollabPeer[];
  /** Y.Text for a non-md path, once synced; null → REST-content fallback. */
  getFileText: (path: string) => Y.Text | null;
  /** Y.XmlFragment for an md path, once synced (#86 phase 6 doc model). */
  getFileFragment: (path: string) => Y.XmlFragment | null;
  /** Live file paths in the doc (Y.Map entries + md fragments) — the source
   *  for the reactive spec list; empty until connected. */
  docPaths: string[];
  /** The live provider (for CollaborationCaret); null until connected. */
  provider: HocuspocusProvider | null;
  /** The room Y.Doc — exists from mount regardless of connection (so an
   *  offline/solo client still has a local doc to work on). Null before the
   *  first effect run. */
  doc: Y.Doc | null;
  /** This client's presence identity (name/color) for caret labels. */
  self: { name: string; color: string };
  /** True while a transaction originates from this client (binding helper). */
  isLocalTransaction: (transaction: Y.Transaction) => boolean;
  /** Bumped on any files-map change so selections can re-resolve. */
  version: number;
  /** Force the room's pending doc edits to commit to git NOW and resolve once
   *  done (#162) — the console awaits this before triggering a build, which
   *  tags HEAD. Resolves immediately when offline (nothing shared to commit);
   *  rejects on a flush error or timeout. */
  flush: () => Promise<void>;
  /** Last committer/flush failure message for UI surfacing (D6); null when clear. */
  flushError: string | null;
  /** Dismiss the flush-error banner. */
  clearFlushError: () => void;
}

// A forced flush is one files/apply commit — quick, but allow slack for the
// git op before the build is blocked on a hung reply.
const FLUSH_TIMEOUT_MS = 30_000;

// Settle time before rebuilding the room after a post-sync drop (see
// `scheduleRebuild`). The fresh provider retries on its own backoff from
// there; this only stops a server that accepts-then-drops from spinning a
// full resync per round-trip.
const REBUILD_DELAY_MS = 1_000;
// …and the ceiling once that delay doubles per consecutive rebuild. A rebuild
// costs the SERVER a room load and a git seed, so a crash-looping collab pod
// must not be held at one per second by every open tab.
const REBUILD_MAX_DELAY_MS = 30_000;
// A session that stayed synced this long was healthy, so the next drop starts
// the backoff over. Without it, a server that syncs before each drop would
// reset the count every round and never back off at all.
const REBUILD_RESET_MS = 30_000;

const PEER_COLORS = [
  "#e57373", "#64b5f6", "#81c784", "#ffb74d",
  "#ba68c8", "#4dd0e1", "#f06292", "#aed581",
];

function collabWsUrl(): string {
  const env = (window as { _env_?: { collabWsUrl?: string } })._env_;
  if (env?.collabWsUrl) return env.collabWsUrl;
  if (import.meta.env.DEV) return "ws://localhost:8091";
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/collab`;
}

export function useCollabSpec(
  projectName: string,
  user: { name: string; email: string },
  orgHandle: string,
): CollabSpec {
  const [status, setStatus] = useState<CollabStatus>("connecting");
  // Flips true once the local Y.Doc is created (mount), so the memoized return
  // re-exposes `doc` even when the room never connects (offline/solo).
  const [docReady, setDocReady] = useState(false);
  const [peers, setPeers] = useState<CollabPeer[]>([]);
  const [version, setVersion] = useState(0);
  const [flushError, setFlushError] = useState<string | null>(null);
  // Bumped to rebuild the Y.Doc + provider after a post-sync drop; it is an
  // effect dependency, so a bump tears the old room down and joins fresh.
  const [epoch, setEpoch] = useState(0);
  const docRef = useRef<Y.Doc | null>(null);
  const providerRef = useRef<HocuspocusProvider | null>(null);
  // True once this doc has taken server state. Only a drop AFTER that point
  // needs a rebuild — see `scheduleRebuild`.
  const syncedRef = useRef(false);
  const rebuildTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Consecutive rebuilds with no healthy session between them, and when the
  // live one last synced — together they set the backoff (see `scheduleRebuild`).
  const rebuildAttemptsRef = useRef(0);
  const syncedAtRef = useRef(0);
  // Last-seen file-path set (serialized) so we re-render the list only when a
  // file is added/removed/created — not on every keystroke within a file.
  const pathKeyRef = useRef("");
  // In-flight flush requests keyed by correlation id (#162): the server acks
  // via a stateless message that resolves/rejects the matching promise.
  const pendingFlushes = useRef(
    new Map<string, { resolve: () => void; reject: (e: Error) => void }>(),
  );

  useEffect(() => {
    const doc = new Y.Doc();
    docRef.current = doc;
    syncedRef.current = false;
    pathKeyRef.current = "";
    setDocReady(true);
    setStatus("connecting");
    // Stable across this effect — captured so the cleanup doesn't read a ref
    // that could have moved (react-hooks/exhaustive-deps).
    const flushes = pendingFlushes.current;

    // A doc must never outlive the connection that filled it. The server
    // treats git as the durable truth: it unloads a room on last-leave and
    // RESEEDS a brand-new Y.Doc from HEAD on the next join. Markdown files are
    // top-level Y.XmlFragments, which have no key to converge on — so a
    // reconnect that carried this doc's already-seeded fragments back would
    // merge them with the fresh seed's independent items and the file would
    // come back DOUBLED (non-md files hide the bug: they are Y.Map entries,
    // where a colliding key resolves last-writer-wins).
    //
    // So on a drop that happens after we synced, throw the doc away and rejoin
    // from scratch — the server's copy is authoritative. Unsynced local edits
    // are lost, which is the same content the server already discarded when it
    // unloaded the room. Before the first sync the doc is still empty and can
    // carry nothing back, so the provider's own retry is left to do its job.
    //
    // A rejected bearer is the one drop this must NOT answer: the rebuild would
    // present the same token and be rejected again, so `authFailed` latches for
    // the life of this provider.
    let authFailed = false;
    const scheduleRebuild = () => {
      if (authFailed || !syncedRef.current || rebuildTimerRef.current) return;
      syncedRef.current = false;
      // Silence the OLD provider's own retry first. Its websocket re-arms a
      // reconnect from `onClose` on the same 1s delay as ours, so a socket that
      // reopens before React commits the teardown would carry this doc's
      // already-seeded fragments into the room the server just reseeded — the
      // exact doubling the rebuild exists to prevent. The doc is being thrown
      // away regardless, so there is nothing to lose by closing it now.
      provider.disconnect();
      // Back off while drops keep coming, and start over once a session has
      // held long enough to count as healthy.
      const attempt =
        Date.now() - syncedAtRef.current >= REBUILD_RESET_MS
          ? 0
          : rebuildAttemptsRef.current;
      rebuildAttemptsRef.current = attempt + 1;
      rebuildTimerRef.current = setTimeout(
        () => {
          rebuildTimerRef.current = null;
          setEpoch((e) => e + 1);
        },
        Math.min(REBUILD_DELAY_MS * 2 ** attempt, REBUILD_MAX_DELAY_MS),
      );
    };

    const provider = new HocuspocusProvider({
      url: collabWsUrl(),
      name: `spec-${orgHandle}-${projectName}`,
      document: doc,
      token: async () => (await getAccessToken()) ?? "",
      onSynced: () => {
        syncedRef.current = true;
        syncedAtRef.current = Date.now();
        setStatus("connected");
      },
      onStatus: ({ status: s }) => {
        if (s === "disconnected") {
          setStatus("offline");
          scheduleRebuild();
        }
      },
      // Terminal: a rejected bearer will be rejected again on a rebuild. The
      // server closes the socket right after rejecting, and the provider emits
      // the rejection and the close in either order, so this both latches the
      // flag and cancels a bump the close already queued. The room stays
      // offline until something else remounts the hook.
      onAuthenticationFailed: () => {
        authFailed = true;
        if (rebuildTimerRef.current) {
          clearTimeout(rebuildTimerRef.current);
          rebuildTimerRef.current = null;
        }
        setStatus("offline");
      },
    });
    providerRef.current = provider;

    provider.setAwarenessField("user", {
      name: user.name,
      color: PEER_COLORS[doc.clientID % PEER_COLORS.length],
      kind: "user",
    });
    const awareness = provider.awareness;
    const onAwareness = () => {
      if (!awareness) return;
      const list: CollabPeer[] = [];
      awareness.getStates().forEach((state, clientId) => {
        if (clientId === doc.clientID) return;
        const u = (state as { user?: Partial<CollabPeer> }).user;
        if (!u?.name) return;
        list.push({
          clientId,
          name: u.name,
          color: u.color ?? PEER_COLORS[clientId % PEER_COLORS.length] ?? "#888",
          kind: u.kind === "agent" ? "agent" : "user",
        });
      });
      setPeers(list);
    };
    awareness?.on("change", onAwareness);

    // Re-render the list when the FILE SET changes. Watching the whole doc
    // (not just Y.Map('files')) is load-bearing: markdown files are top-level
    // Y.XmlFragments, so a new agent-created .md file appears as a new share,
    // which a Y.Map observer never sees. afterAllTransactions fires on local
    // and synced remote changes alike; we diff the path set so content edits
    // (which bind to the editor directly) don't thrash the list.
    const onDocChange = () => {
      const key = listDocPaths(doc).join("\n");
      if (key !== pathKeyRef.current) {
        pathKeyRef.current = key;
        setVersion((v) => v + 1);
      }
    };
    doc.on("afterAllTransactions", onDocChange);

    // Stateless protocol: flush acks (#162), token push/pull (D6), flush-error UI.
    const onStateless = ({ payload }: { payload: string }) => {
      let msg: {
        type?: string;
        id?: string;
        message?: string;
        value?: string;
      };
      try {
        msg = JSON.parse(payload) as typeof msg;
      } catch {
        return;
      }

      // D6 pull: server asks for a fresh bearer after apply 401/403.
      if (msg.type === "token-please" && msg.id) {
        const requestId = msg.id;
        void (async () => {
          const token =
            (await renewAccessToken()) ?? (await getAccessToken());
          if (!token || providerRef.current !== provider) return;
          provider.sendStateless(
            JSON.stringify({
              type: "token",
              value: token,
              id: requestId,
            }),
          );
        })();
        return;
      }

      if (msg.type === "flush-error") {
        const errMsg = msg.message ?? "Failed to commit the workspace.";
        setFlushError(errMsg);
        if (msg.id) {
          const pending = pendingFlushes.current.get(msg.id);
          if (pending) {
            pendingFlushes.current.delete(msg.id);
            pending.reject(new Error(errMsg));
          }
        }
        return;
      }

      if (!msg.id) return;
      const pending = pendingFlushes.current.get(msg.id);
      if (!pending) return;
      pendingFlushes.current.delete(msg.id);
      if (msg.type === "flushed") pending.resolve();
    };
    provider.on("stateless", onStateless);

    // D6 push: whenever OIDC silently renews, ship the new JWT to the room.
    const unsubscribeToken = subscribeAccessTokenRefresh((newToken) => {
      if (providerRef.current !== provider) return;
      provider.sendStateless(
        JSON.stringify({ type: "token", value: newToken }),
      );
    });

    provider.attach();
    return () => {
      // A rebuild already consumed its timer; this covers unmount and a
      // project/org switch, where a pending bump must not resurrect the room.
      if (rebuildTimerRef.current) {
        clearTimeout(rebuildTimerRef.current);
        rebuildTimerRef.current = null;
      }
      syncedRef.current = false;
      unsubscribeToken();
      doc.off("afterAllTransactions", onDocChange);
      awareness?.off("change", onAwareness);
      provider.off("stateless", onStateless);
      // Fail any in-flight flush so a caller (Build) never hangs on teardown.
      for (const p of flushes.values())
        p.reject(new Error("Collaboration session ended before the commit finished."));
      flushes.clear();
      provider.destroy();
      doc.destroy();
      docRef.current = null;
      providerRef.current = null;
    };
  }, [projectName, user.name, user.email, orgHandle, epoch]);

  return useMemo(
    () => ({
      status,
      peers,
      version,
      flushError,
      clearFlushError: () => setFlushError(null),
      getFileText: (path: string) =>
        status === "connected"
          ? (docRef.current?.getMap<Y.Text>("files").get(path) ?? null)
          : null,
      getFileFragment: (path: string) =>
        status === "connected"
          ? (docRef.current?.getXmlFragment(path) ?? null)
          : null,
      docPaths:
        status === "connected" && docRef.current
          ? listDocPaths(docRef.current)
          : [],
      provider: status === "connected" ? providerRef.current : null,
      doc: docReady ? docRef.current : null,
      self: {
        name: user.name,
        color:
          PEER_COLORS[(docRef.current?.clientID ?? 0) % PEER_COLORS.length] ??
          "#888",
      },
      isLocalTransaction: (transaction: Y.Transaction) => transaction.local,
      flush: () =>
        new Promise<void>((resolve, reject) => {
          const provider = providerRef.current;
          if (status !== "connected" || !provider) {
            resolve(); // offline / solo — nothing shared to commit
            return;
          }
          const id = crypto.randomUUID();
          const timer = setTimeout(() => {
            pendingFlushes.current.delete(id);
            reject(new Error("Timed out waiting for the workspace to commit."));
          }, FLUSH_TIMEOUT_MS);
          pendingFlushes.current.set(id, {
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
        }),
    }),
    // A rebuild always moves `status` (offline → connecting → connected), so
    // the refs are re-read and consumers holding a fragment from the discarded
    // doc are handed the new one. No `epoch` dependency is needed for that.
    [status, peers, version, user.name, docReady, flushError],
  );
}
