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
import { useQueryClient } from "@tanstack/react-query";
import { listDocPaths } from "@aep/collab-doc";
import {
  getAccessToken,
  redirectToSignIn,
  renewAccessToken,
  subscribeAccessTokenRefresh,
} from "../../../auth/token";
import type { components } from "../../../generated/aep-api";
import { aeStudioKeys, useAeStudio } from "../../ae-studio/api/queries";

// Console side of #86 phase 5: connect the spec view to the Room, the
// `ae-collab` container of the org's AE Studio pod (10 §6). One room + one
// Y.Doc per project (`spec-<org>-<project>`), Y.Map('files') of path → Y.Text.
// The Room's URL comes from AE Studio's `ready` answer; without one (AE Studio
// not ready, or the Room unreachable) the view degrades to solo (#86
// decision 10) — callers keep their non-collaborative fallback.
//
// The connection authenticates with the session's access token (#91), read
// through a getter, so a reconnect presents the current one. The Room holds a
// connection only until its token's `exp`, so every OIDC renewal is pushed
// through the provider's token sync (`sendToken()`), which the Room verifies
// and moves the deadline on. A connection it closes anyway (expired, or the
// pushed token refused) is renewed once and rejoined; see `onAuthLost`.

// The reasons ae-collab gives the client. Duplicated from
// `components/dataplane/ae-system-project/ae-studio/ae-collab/src/pod/auth.ts`
// and `pod/expiry.ts` rather than shared, matching how the stateless message
// types are already spelled on both sides of this socket.
/** A refusal that is not about the bearer: the Room's own upstream failed. Retry. */
const UPSTREAM_UNAVAILABLE = "upstream-unavailable";
/** A refusal verdict, on joining or on a token the client pushed. */
const PERMISSION_DENIED = "permission-denied";
/** The Room closed the connection at its token's `exp`. */
const TOKEN_EXPIRED = "token-expired";

export interface CollabPeer {
  clientId: number;
  name: string;
  color: string;
  kind: "user" | "agent";
}

export type CollabStatus = "connecting" | "connected" | "offline";

/** A soft problem the Room's last commit reported about one file. */
export interface FlushWarning {
  path: string;
  message: string;
}

export interface CollabSpec {
  status: CollabStatus;
  peers: CollabPeer[];
  /** Y.Text for a non-md path, once synced; null → REST-content fallback. */
  getFileText: (path: string) => Y.Text | null;
  /** Y.XmlFragment for an md path, once synced (#86 phase 6 doc model).
   *  Never creates the fragment — a read is not authorship (ADR-0020). */
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
  /** The warnings the Room's last commit reported; each commit's set replaces
   *  the last, and a commit without warnings clears them. */
  flushWarnings: FlushWarning[];
  /** Dismiss the flush-warnings Alert until the next commit reports some. */
  dismissFlushWarnings: () => void;
}

// A forced flush is one commit through the pod's Files socket. The deadlines
// nest, and this one must stay the outermost: the pod answers a Files-socket
// request within its own budget (`filesSocketRequestBudget`, 40 s, in
// ae-studio-tools/internal/edge/files_sock.go), and ae-collab gives up on the
// socket at 45 s (`REQUEST_TIMEOUT_MS` in ae-collab/src/files-client.ts) and
// reports that as a flush-error. Waiting less than either reports a slow but
// successful save as a failure and blocks Build on it; raise this with them.
const FLUSH_TIMEOUT_MS = 50_000;

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

type AeStudio = components["schemas"]["AeStudio"];

/**
 * The Room URL to hold after AE Studio's latest answer, given the one held now.
 * `ready` names the Room. A `provisioning` answer after it is most often a
 * converge that leaves the pod running, so it keeps the held URL and with it
 * the live provider (a pod that really restarts drops the socket, and the
 * provider's own drop paths answer that). `failed`, `absent`, or no answer
 * yet: no Room.
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

const PEER_COLORS = [
  "#e57373", "#64b5f6", "#81c784", "#ffb74d",
  "#ba68c8", "#4dd0e1", "#f06292", "#aed581",
];

/** The well-formed entries of a `flush-warnings` message's `warnings`. */
function readFlushWarnings(raw: unknown): FlushWarning[] {
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((w: unknown) => {
    const { path, message } = (w ?? {}) as { path?: unknown; message?: unknown };
    return typeof path === "string" && typeof message === "string" ? [{ path, message }] : [];
  });
}

export function useCollabSpec(
  projectName: string,
  user: { name: string; email: string },
  orgHandle: string,
): CollabSpec {
  const queryClient = useQueryClient();
  const studio = useAeStudio();
  // No provider without a Room URL (10 §2): the Room is joined once AE Studio
  // is `ready`, kept through a transient `provisioning`, and left when AE
  // Studio fails or is gone (see `heldRoomUrl`). Held as state, adjusted while
  // rendering, because the URL to hold depends on the one held before.
  const [roomUrl, setRoomUrl] = useState(() => heldRoomUrl(null, studio.data));
  const nextRoomUrl = heldRoomUrl(roomUrl, studio.data);
  if (nextRoomUrl !== roomUrl) setRoomUrl(nextRoomUrl);
  const roomName = `spec-${orgHandle}-${projectName}`;
  const [status, setStatus] = useState<CollabStatus>("connecting");
  // Flips true once the local Y.Doc is created (mount), so the memoized return
  // re-exposes `doc` even when the room never connects (offline/solo).
  const [docReady, setDocReady] = useState(false);
  const [peers, setPeers] = useState<CollabPeer[]>([]);
  const [version, setVersion] = useState(0);
  const [flushError, setFlushError] = useState<string | null>(null);
  const [flushWarnings, setFlushWarnings] = useState<FlushWarning[]>([]);
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
  // The room whose auth loss renewed the session; a session that syncs again
  // earns the next loss its own renewal, and joining another room (a project
  // or org switch) starts with one. See `onAuthLost`.
  const renewalSpentForRef = useRef<string | null>(null);
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
    // The budget is per room: a rebuild of this room keeps it spent.
    if (renewalSpentForRef.current !== roomName) renewalSpentForRef.current = null;
    // The local doc exists either way, so the view works solo without a Room.
    if (!roomUrl) {
      return () => {
        doc.destroy();
        docRef.current = null;
      };
    }
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
    // A lost bearer is not answered here: `onAuthLost` decides, and
    // `authLost` keeps any drop that follows from queueing a rebuild of its own
    // for the life of this provider.
    let authLost = false;
    // `requireSynced: false` is for the one drop that never synced and never
    // will on its own: a room the server REFUSED (#586, an unseedable room or
    // an unreachable oracle). That arrives as a permission-denied frame, which
    // leaves the socket open — so the websocket's own reconnect never fires and
    // nothing retries unless this does. The doc is empty in that case, so the
    // doubling this ladder normally guards against cannot happen.
    //
    // Every rebuild, this one and the rejoin after a renewal (`onAuthLost`),
    // is armed through `armRebuild`, so all of them climb the same ladder.
    const scheduleRebuild = ({ requireSynced = true } = {}) => {
      if (authLost || rebuildTimerRef.current) return;
      if (requireSynced && !syncedRef.current) return;
      syncedRef.current = false;
      // Silence the OLD provider's own retry first. Its websocket re-arms a
      // reconnect from `onClose` on the same 1s delay as ours, so a socket that
      // reopens before React commits the teardown would carry this doc's
      // already-seeded fragments into the room the server just reseeded — the
      // exact doubling the rebuild exists to prevent. The doc is being thrown
      // away regardless, so there is nothing to lose by closing it now.
      provider.disconnect();
      armRebuild();
    };

    // Bumps `epoch` after the backoff delay, which tears this room down and
    // joins fresh. The caller has already silenced the provider.
    const armRebuild = () => {
      // Back off while drops keep coming, and start over once a session has
      // held long enough to count as healthy.
      // `syncedAtRef` is 0 until a session actually syncs, so "has it held long
      // enough to count as healthy?" must ask whether it EVER synced first —
      // otherwise a room that never synced (a server that refuses it, #586)
      // reads as infinitely healthy, resets the ladder on every attempt, and
      // retries once a second forever against the service that is already
      // struggling.
      const held =
        syncedAtRef.current > 0 &&
        Date.now() - syncedAtRef.current >= REBUILD_RESET_MS;
      // Spend that credit ONCE. The healthy session earns the next attempt a
      // fast retry, not every attempt: `syncedAtRef` belongs to a provider that
      // is being thrown away, and leaving it set means each replacement room —
      // none of which ever syncs, because the server is refusing them — still
      // reads as "just came off a healthy session" and restarts the ladder. The
      // result is the same once-a-second hammer the ladder exists to prevent,
      // reached from the other side. The next successful sync sets it again.
      syncedAtRef.current = 0;
      const attempt = held ? 0 : rebuildAttemptsRef.current;
      rebuildAttemptsRef.current = attempt + 1;
      rebuildTimerRef.current = setTimeout(
        () => {
          rebuildTimerRef.current = null;
          setEpoch((e) => e + 1);
        },
        Math.min(REBUILD_DELAY_MS * 2 ** attempt, REBUILD_MAX_DELAY_MS),
      );
    };

    // The bearer was refused, or the Room closed the connection at its
    // token's `exp`. Handled ONCE per loss: renew the session and rejoin with
    // the fresh token; if the session cannot be renewed, the user signs in
    // again. A loss before the rejoin synced is a verdict a new token did not
    // change (another org, an unknown project), so it is terminal: a rebuild
    // would present the same token and be refused again, and the room stays
    // offline until something else remounts the hook.
    //
    // The server closes the socket right after refusing, and the provider
    // emits the refusal and the close in either order, so this latches
    // `authLost` and cancels a bump the close already queued.
    const onAuthLost = () => {
      if (authLost) return;
      authLost = true;
      if (rebuildTimerRef.current) {
        clearTimeout(rebuildTimerRef.current);
        rebuildTimerRef.current = null;
      }
      syncedRef.current = false;
      // This doc is not rejoined: silence the provider's own reconnect, which
      // would present the renewed token and carry the doc's seeded fragments
      // into a reseeded room (the doubling `scheduleRebuild` also guards).
      provider.disconnect();
      setStatus("offline");
      if (renewalSpentForRef.current === roomName) return;
      renewalSpentForRef.current = roomName;
      void renewAccessToken().then((token) => {
        // Unmounted, or moved to another room, while renewing: not ours to act on.
        if (providerRef.current !== provider) return;
        // A Room that refuses each pushed token right after the rejoin syncs
        // earns a renewal every round, so the rejoin backs off like any other.
        if (token) armRebuild();
        else redirectToSignIn();
      });
    };

    // A known gap, left as is: the provider's websocket closes a socket that
    // has been silent for 30 s (Hocuspocus `messageReconnectTimeout`), while a
    // first join that makes the pod clone the repo can wait up to the pod's
    // 40 s Files-socket budget before the Room answers. That join drops before
    // it syncs, so no rebuild follows; the provider's own retry rejoins, and
    // the clone, which runs detached in the pod, is reused, so the retry
    // converges. Nothing else here depends on that timeout, so it stays at the
    // provider's default.
    const provider = new HocuspocusProvider({
      url: roomUrl,
      name: roomName,
      document: doc,
      token: async () => (await getAccessToken()) ?? "",
      onSynced: () => {
        syncedRef.current = true;
        syncedAtRef.current = Date.now();
        renewalSpentForRef.current = null;
        setStatus("connected");
      },
      onStatus: ({ status: s }) => {
        if (s === "disconnected") {
          setStatus("offline");
          scheduleRebuild();
        }
      },
      // A refusal is about the bearer only when it is a VERDICT (#586). The
      // Room reaches the IdP's keys and the Files socket on every join, and
      // reports either one being down as `upstream-unavailable`: retrying is
      // the whole answer, and the pod may be restarting, so AE Studio is
      // re-read too (a restart then shows as the banner).
      onAuthenticationFailed: ({ reason }) => {
        if (reason === UPSTREAM_UNAVAILABLE) {
          setStatus("offline");
          void queryClient.invalidateQueries({ queryKey: aeStudioKeys.all });
          scheduleRebuild({ requireSynced: false });
          return;
        }
        onAuthLost();
      },
      // The Room ends a connection with a Close message, not an auth failure,
      // when its token ran out or a pushed token was refused. The socket stays
      // open, so nothing below would ever rejoin. Any other close is the
      // socket's own, which `onStatus` answers.
      onClose: ({ event }) => {
        if (event.reason === TOKEN_EXPIRED || event.reason === PERMISSION_DENIED) onAuthLost();
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
    // Joining fires no "change", so read the room's current peers once.
    onAwareness();

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

    // Stateless protocol: flush acks (#162), flush-error and flush-warnings UI.
    const onStateless = ({ payload }: { payload: string }) => {
      let msg: {
        type?: string;
        id?: string;
        message?: string;
        warnings?: unknown;
      };
      try {
        msg = JSON.parse(payload) as typeof msg;
      } catch {
        return;
      }

      // After every commit: that commit's warnings, replacing the last set.
      if (msg.type === "flush-warnings") {
        setFlushWarnings(readFlushWarnings(msg.warnings));
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

    // Token sync: whenever OIDC renews, the provider sends the token its
    // getter now returns, and the Room moves this connection's deadline to it.
    const unsubscribeToken = subscribeAccessTokenRefresh(() => {
      void provider.sendToken();
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
      // Peers belong to this provider; a rebuild must not carry them over.
      setPeers([]);
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
  }, [roomName, user.name, user.email, epoch, roomUrl, queryClient]);

  // No Room to be in: still finding out (the first AE Studio read), or offline.
  const roomStatus: CollabStatus = roomUrl ? status : studio.isPending ? "connecting" : "offline";

  return useMemo(
    () => ({
      status: roomStatus,
      peers,
      version,
      flushError,
      clearFlushError: () => setFlushError(null),
      flushWarnings,
      dismissFlushWarnings: () => setFlushWarnings([]),
      getFileText: (path: string) =>
        roomStatus === "connected"
          ? (docRef.current?.getMap<Y.Text>("files").get(path) ?? null)
          : null,
      // A READ MUST NOT CREATE (ADR-0020). `Y.Doc.getXmlFragment` registers a
      // fragment for any path it is asked for, which made "does the room hold
      // this file?" unanswerable — asking made the answer yes. That is what
      // let an unseeded room paint a blank document over a PRD that exists in
      // git (#586), because `usesCollab` saw a fragment and stood the
      // committed-git fallback down; and it is what conjured phantom entries
      // into `docPaths`, which the file list unions into the rail.
      //
      // `share.has` can be trusted as "the room holds this file" because an
      // empty markdown document seeds as one empty paragraph rather than zero
      // blocks (`markdownToFragment`) — without that, an emptied file would
      // generate no update, never replicate its key, and read as absent here.
      getFileFragment: (path: string) => {
        const doc = docRef.current;
        if (roomStatus !== "connected" || !doc?.share.has(path)) return null;
        return doc.getXmlFragment(path);
      },
      docPaths:
        roomStatus === "connected" && docRef.current
          ? listDocPaths(docRef.current)
          : [],
      provider: roomStatus === "connected" ? providerRef.current : null,
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
          if (roomStatus !== "connected" || !provider) {
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
    [roomStatus, peers, version, user.name, docReady, flushError, flushWarnings],
  );
}
