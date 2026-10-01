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

import { Server } from "@hocuspocus/server";
import type {
  afterUnloadDocumentPayload,
  beforeUnloadDocumentPayload,
  onAuthenticatePayload,
  onLoadDocumentPayload,
  onRequestPayload,
  onStatelessPayload,
  onStoreDocumentPayload,
} from "@hocuspocus/server";
import type { CollabConfig } from "./env.js";
import {
  BffAccessDeniedError,
  BffReadError,
  type BffClient,
  type CollabIdentity,
} from "./bff.js";
import { isSpecRoom } from "./room.js";
import { isReferenceDocPath, seedDocument } from "./seed.js";
import { devSeedFiles } from "./fixtures.js";
import { flushAllRooms, flushRoom } from "./committer.js";
import {
  addParticipant,
  dropRoomState,
  ensureRoomState,
  roomState,
} from "./rooms.js";

// Connection context established by onAuthenticate and consumed by later
// hooks. The token is retained for the seed read (performed as the first
// joiner) — it never leaves this process except toward the BFF.
export interface CollabContext {
  user: { name: string; email: string; kind: "user" | "dev" };
  token: string | null;
  /** Resolved by the oracle from the room ID (only the BFF can split
   *  `spec-<org>-<project>` — it knows the caller's org). Null in dev mode. */
  projectName: string | null;
}

export interface CollabDeps {
  bff: BffClient | null;
  log?: (message: string) => void;
}

/** Brief wait for a client reply to `{type:"token-please"}` (D6 pull). */
const TOKEN_PLEASE_TIMEOUT_MS = 5_000;

/** In-flight pull-on-401 waits keyed by correlation id. */
const pendingTokenPlease = new Map<
  string,
  { resolve: (token: string | null) => void }
>();

/**
 * Ask connected clients for a fresh bearer, resolve with the first matching
 * `{type:"token", value, id}` or null on timeout / no connections (D6).
 */
export function requestFreshToken(
  document: Pick<
    onStoreDocumentPayload["document"],
    "getConnections" | "broadcastStateless"
  >,
  deps: Pick<CollabDeps, "log">,
  documentName: string,
): Promise<string | null> {
  const connections = document.getConnections?.() ?? [];
  if (connections.length === 0) {
    deps.log?.(
      `token-please: no connections for ${documentName} — cannot refresh`,
    );
    return Promise.resolve(null);
  }
  const id = crypto.randomUUID();
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      pendingTokenPlease.delete(id);
      deps.log?.(
        `token-please: timed out waiting for ${documentName} (id=${id})`,
      );
      resolve(null);
    }, TOKEN_PLEASE_TIMEOUT_MS);
    pendingTokenPlease.set(id, {
      resolve: (token) => {
        clearTimeout(timer);
        pendingTokenPlease.delete(id);
        resolve(token);
      },
    });
    const payload = JSON.stringify({ type: "token-please", id });
    for (const conn of connections) {
      conn.sendStateless(payload);
    }
  });
}

function tokenRefreshFor(
  document: onStoreDocumentPayload["document"],
  documentName: string,
  deps: CollabDeps,
): () => Promise<string | null> {
  return () => requestFreshToken(document, deps, documentName);
}

function surfaceFlushError(
  document: Pick<
    onStoreDocumentPayload["document"],
    "broadcastStateless" | "getConnections"
  >,
  message: string,
): void {
  const connections = document.getConnections?.() ?? [];
  if (connections.length === 0) return;
  const payload = JSON.stringify({ type: "flush-error", message });
  try {
    if (typeof document.broadcastStateless === "function") {
      document.broadcastStateless(payload);
      return;
    }
  } catch {
    // fall through to per-connection send
  }
  for (const conn of connections) {
    conn.sendStateless(payload);
  }
}

// A rejected bearer and an unreachable oracle both surface here as a thrown
// error, and Hocuspocus answers either one with the same permission-denied
// frame — which the console latches on, correctly, because a bearer that was
// rejected will be rejected again. An `aep-api` that is merely restarting is
// NOT that (#586): every non-ok status became BffAccessDeniedError, so a 503
// during a deploy left the spec view offline until the page was reloaded.
//
// So the reason is classified before it leaves. Hocuspocus forwards
// `error.reason` verbatim into the frame and the provider re-emits it as
// `onAuthenticationFailed({ reason })`, which is where the console decides
// whether to latch. The string is duplicated there rather than shared, the
// same way the stateless message types already are.
export const UPSTREAM_UNAVAILABLE = "upstream-unavailable";

/**
 * An error tagged as "not now" rather than "not you".
 *
 * The load hook needs this as much as the auth hook does, which is not
 * obvious: Hocuspocus runs `onLoadDocument` inside the SAME try/catch as
 * `onAuthenticate` (it is reached through `setUpNewConnection`), so a refused
 * room reaches the client as a permission-denied frame — not as a dropped
 * socket. Untagged, refusing an unseedable room would read to the console as a
 * rejected bearer and latch the view offline for the life of the page, which is
 * worse than the wedge it replaces.
 */
function unavailable(message: string): Error {
  return Object.assign(new Error(message), { reason: UPSTREAM_UNAVAILABLE });
}

/**
 * True for a status that says "not you" rather than "not now".
 *
 * The three retryable 4xx are excluded deliberately. A rate-limited or
 * timing-out oracle is the SAME outage this fix exists for, reached through a
 * different status code — latching the console offline on a 429 would
 * reintroduce #586 the moment the BFF starts shedding load.
 */
const RETRYABLE_4XX = new Set([408, 425, 429]);

function isDenial(status: number): boolean {
  return status < 500 && !RETRYABLE_4XX.has(status);
}

async function validated(
  bff: BffClient,
  token: string,
  documentName: string,
): Promise<CollabIdentity> {
  try {
    return await bff.validateAccess(token, documentName);
  } catch (err) {
    // Anything that is not a decision the oracle actually made — a 5xx, a
    // refused connection, a DNS failure mid-redeploy — is transient, and the
    // client should keep retrying rather than treat it as a verdict.
    if (err instanceof BffAccessDeniedError && isDenial(err.status)) throw err;
    throw unavailable(
      `collab oracle unavailable for ${documentName}: ${String(err)}`,
    );
  }
}

export function buildAuthenticateHook(config: CollabConfig, deps: CollabDeps) {
  return async (
    data: Pick<onAuthenticatePayload, "token" | "documentName">,
  ): Promise<CollabContext> => {
    const { token, documentName } = data;
    if (!isSpecRoom(documentName)) {
      throw new Error(`unknown room: ${documentName}`);
    }

    if (config.devMode) {
      return {
        user: { name: "Dev User", email: "dev@localhost", kind: "dev" },
        token: null,
        projectName: null,
      };
    }

    if (!deps.bff) throw new Error("no BFF configured");
    if (!token) throw new Error("missing token");
    // The oracle does both halves: JWT verification (Thunder JWKS) and the
    // room's project-ownership/tenancy check. This service verifies nothing
    // itself (#86: identity stays the BFF's problem). It also resolves the
    // room into a project name for the seed read.
    const identity = await validated(deps.bff, token, documentName);
    // Committer bookkeeping (#133): the session's participants become the
    // commit's Co-authored-by trailers; the latest token backs the forced
    // unload flush (no connection context exists by then).
    const state = ensureRoomState(documentName, identity.projectName);
    state.lastToken = token;
    addParticipant(documentName, {
      name: identity.name,
      email: identity.email,
    });
    return {
      user: { name: identity.name, email: identity.email, kind: "user" },
      token,
      projectName: identity.projectName,
    };
  };
}

export function buildLoadDocumentHook(config: CollabConfig, deps: CollabDeps) {
  return async (
    data: Pick<onLoadDocumentPayload, "document" | "documentName"> & {
      context: CollabContext;
    },
  ) => {
    const { document, documentName, context } = data;

    // A seed anomaly is never fatal (the content still lands) but it must not be
    // silent — it is the one path where this room could lose a file's identity.
    const onAnomaly = (message: string) =>
      deps.log?.(`seed anomaly in ${documentName}: ${message}`);

    if (config.devMode) {
      seedDocument(document, devSeedFiles, onAnomaly);
      deps.log?.(`seeded ${documentName} from dev fixtures`);
      return document;
    }

    // Real path: read the spec files as the first joiner (the oracle
    // resolved the room into context.projectName).
    //
    // A ROOM EXISTS ONLY IF IT WAS SEEDED (#586). Both failure branches below
    // therefore reject the load rather than returning an unseeded document.
    //
    // This one is NOT tagged transient. `deps.bff` and `context.token` cannot
    // be missing here at all (the auth hook throws first), and a room the
    // oracle resolved without a project is a room it will keep resolving
    // without one — so it is a verdict, and the client should stop asking
    // rather than reconnect every 30s for the life of the page.
    if (!deps.bff || !context.token || !context.projectName) {
      throw new Error(`cannot seed ${documentName}: missing bff/token/project`);
    }
    // Returning an empty document here is what wedged a project's spec view
    // (#586): the baseline stays empty, so every path writes with baseSha ""
    // — which the Files API reads as "must not exist" — and every flush 409s
    // against the real file for as long as the room lives. Meanwhile the
    // console could not tell the empty room from an empty project, and an
    // agent turn joined a room that synced perfectly and reported no files.
    //
    // Failing the load costs nothing that a retry does not recover: the
    // provider reconnects on its own (1s doubling to 30s, unbounded), so the
    // next attempt reseeds from git once the read works. That IS the retry —
    // marking the room unseeded would buy a flag with no trigger, because
    // `onLoadDocument` fires per LOAD and a room stays loaded while any client
    // is connected.
    try {
      const fetched = await deps.bff.fetchSpecFiles(
        context.token,
        context.projectName,
      );
      // Reference documents never enter the room — not seeded, not baselined
      // (the committer filters its side too; see isReferenceDocPath).
      const files = fetched.filter((f) => !isReferenceDocPath(f.path));
      seedDocument(document, files, onAnomaly);
      // Committer baseline (#133): the flush diffs the live doc against what
      // was seeded (content) and preconditions on the shas we read.
      const state = ensureRoomState(documentName, context.projectName);
      for (const f of files) {
        state.baseline.set(f.path, { content: f.content, sha: f.sha });
      }
      deps.log?.(`seeded ${documentName} (${files.length} files) from BFF`);
    } catch (err) {
      // Clear the BASELINE, not the room state. A seed that threw partway
      // leaves entries the next successful seed would not overwrite, so they
      // have to go — but the state itself belongs to every connection that
      // authenticated into this room, not to this load. Dropping it wholesale
      // loses another tab's `lastToken`, and a room with no token skips its
      // flush (committer.ts) — that tab's edits would be discarded in silence
      // at exactly the moment several tabs are reconnecting together.
      roomState(documentName)?.baseline.clear();
      deps.log?.(
        `seed failed for ${documentName} (${String(err)}) — refusing the room`,
      );
      // A read the BFF answered permanently — a 404 for a project whose repo
      // row is missing — is a verdict, not an outage. Tagging it transient
      // would have every open tab reconnect forever against a room that can
      // never be seeded.
      if (err instanceof BffReadError && isDenial(err.status)) throw err;
      throw unavailable(`could not load ${documentName}: ${String(err)}`);
    }
    return document;
  };
}

// buildStoreDocumentHook is the #133 committer trigger: Hocuspocus debounces
// it per document (Server debounce/maxDebounce) and runs it one final time
// before unload, so "quiet period", "max age", and "last leave" all funnel
// here. Dev mode has no BFF-backed rooms to commit.
export function buildStoreDocumentHook(config: CollabConfig, deps: CollabDeps) {
  return async (
    data: Pick<onStoreDocumentPayload, "document" | "documentName"> & {
      lastContext: CollabContext | null;
    },
  ) => {
    if (config.devMode || !deps.bff) return;
    if (!roomState(data.documentName)) return;
    try {
      await flushRoom(
        {
          bff: deps.bff,
          log: deps.log,
          tokenRefresh: tokenRefreshFor(
            data.document,
            data.documentName,
            deps,
          ),
        },
        data.documentName,
        data.document,
        data.lastContext ?? undefined,
      );
    } catch (err) {
      // A failed flush must not kill connections; the doc stays live and the
      // next store attempt (or session end) retries from the same baseline.
      // Surface to clients so the failure is not silent (D6).
      const message = err instanceof Error ? err.message : "flush failed";
      deps.log?.(
        `committer: flush failed for ${data.documentName} (${message})`,
      );
      surfaceFlushError(data.document, message);
    }
  };
}

// The forced session-end flush (#86 ph6 review gate): interim stores HOLD
// files with pending agent marks, so the last leave must commit everything
// (accept-by-default) BEFORE the doc unloads. beforeUnloadDocument has no
// connection context — the room state's lastToken authenticates the apply.
export function buildBeforeUnloadHook(config: CollabConfig, deps: CollabDeps) {
  return async (
    data: Pick<beforeUnloadDocumentPayload, "document" | "documentName">,
  ) => {
    if (config.devMode || !deps.bff) return;
    if (!roomState(data.documentName)) return;
    try {
      await flushRoom(
        {
          bff: deps.bff,
          log: deps.log,
          // Last-leave residual (D6): often no client left to refresh from.
          tokenRefresh: tokenRefreshFor(
            data.document,
            data.documentName,
            deps,
          ),
        },
        data.documentName,
        data.document,
        undefined,
        true,
      );
    } catch (err) {
      const message = err instanceof Error ? err.message : "flush failed";
      deps.log?.(
        `committer: final flush failed for ${data.documentName} (${message})`,
      );
      surfaceFlushError(data.document, message);
    }
  };
}

export function buildAfterUnloadHook(deps: CollabDeps) {
  return (data: Pick<afterUnloadDocumentPayload, "documentName">) => {
    dropRoomState(data.documentName);
    deps.log?.(`room ${data.documentName} unloaded — committer state dropped`);
    return Promise.resolve();
  };
}

// Flush-on-demand (#162): the console requests a commit BEFORE triggering a
// build. `POST /build` tags git HEAD, so the room's live doc edits (which
// otherwise reach git only on the debounce / last-peer-leave) must land first.
// A stateless `{type:"flush",id}` message force-flushes the room through the
// committer (accept-by-default for held agent suggestions) and acks the
// requesting connection; the console awaits the ack, then builds. Unknown
// payloads are ignored; a clean/dev/no-BFF room acks immediately (nothing to
// commit → HEAD is already the truth).
export function buildStatelessHook(config: CollabConfig, deps: CollabDeps) {
  return async (
    data: Pick<
      onStatelessPayload,
      "connection" | "documentName" | "document" | "payload"
    >,
  ) => {
    let msg: { type?: string; id?: string; value?: unknown };
    try {
      msg = JSON.parse(data.payload) as {
        type?: string;
        id?: string;
        value?: unknown;
      };
    } catch {
      return; // not our protocol
    }

    // D6 push (+ pull reply): client refreshed its access token.
    if (msg.type === "token" && typeof msg.value === "string") {
      const value = msg.value;
      const ctx = data.connection.context as CollabContext | undefined;
      if (ctx) ctx.token = value;
      const state = roomState(data.documentName);
      if (state) state.lastToken = value;
      if (typeof msg.id === "string") {
        pendingTokenPlease.get(msg.id)?.resolve(value);
      }
      deps.log?.(`token refreshed for ${data.documentName}`);
      return;
    }

    if (msg.type !== "flush") return;

    const ack = (extra: Record<string, unknown> = {}) =>
      data.connection.sendStateless(
        JSON.stringify({ type: "flushed", id: msg.id, ...extra }),
      );

    if (config.devMode || !deps.bff || !roomState(data.documentName)) {
      ack(); // nothing to commit — proceed to build against current HEAD
      return;
    }
    try {
      await flushRoom(
        {
          bff: deps.bff,
          log: deps.log,
          tokenRefresh: tokenRefreshFor(
            data.document,
            data.documentName,
            deps,
          ),
        },
        data.documentName,
        data.document,
        undefined,
        true,
      );
      ack();
    } catch (err) {
      const message = err instanceof Error ? err.message : "flush failed";
      deps.log?.(
        `committer: on-demand flush failed for ${data.documentName} (${message})`,
      );
      data.connection.sendStateless(
        JSON.stringify({
          type: "flush-error",
          id: msg.id,
          message,
        }),
      );
    }
  };
}

function isHealthzRequest(url: string | undefined): boolean {
  return url === "/healthz" || (url?.startsWith("/healthz?") ?? false);
}

export function buildHealthzHandler() {
  return async (data: Pick<onRequestPayload, "request" | "response">) => {
    if (!isHealthzRequest(data.request.url)) return;
    data.response.writeHead(200, { "Content-Type": "text/plain" });
    data.response.end("ok");
    // Suppress Hocuspocus's default "Welcome to Hocuspocus!" body.
    throw undefined;
  };
}

export function registerGracefulShutdown(
  server: Server<CollabContext>,
  config: CollabConfig,
  deps: CollabDeps,
): void {
  let shuttingDown = false;
  const shutdown = async (signal: string) => {
    if (shuttingDown) return;
    shuttingDown = true;
    deps.log?.(`${signal} — flushing rooms before shutdown`);
    if (!config.devMode && deps.bff) {
      await flushAllRooms(
        { bff: deps.bff, log: deps.log },
        server.hocuspocus.documents,
        { concurrency: 8, force: true },
      );
    }
    await server.destroy();
    process.exit(0);
  };
  for (const signal of ["SIGINT", "SIGTERM", "SIGQUIT"] as const) {
    process.on(signal, () => {
      void shutdown(signal);
    });
  }
}

export function createCollabServer(
  config: CollabConfig,
  deps: CollabDeps,
): Server<CollabContext> {
  return new Server<CollabContext>({
    name: "aep-collab",
    // Hocuspocus defaults to port 80; listen(0) is ignored (falsy) and would
    // also fall back to 80 — always set an explicit port from config.
    port: config.port,
    stopOnSignals: false,
    // The committer (#86 phase 3 / #133): onStoreDocument is Hocuspocus's
    // debounced persistence seam — a quiet period commits, maxDebounce caps
    // the wait during continuous editing, and unload runs one final store.
    // A doc's life is still its room's life; git is the durable truth and
    // rejoin reseeds from HEAD.
    unloadImmediately: true,
    debounce: config.commitDebounceMs,
    maxDebounce: config.commitMaxDebounceMs,
    onAuthenticate: buildAuthenticateHook(config, deps),
    onLoadDocument: buildLoadDocumentHook(config, deps) as (
      data: onLoadDocumentPayload<CollabContext>,
    ) => Promise<unknown>,
    onStoreDocument: buildStoreDocumentHook(config, deps) as (
      data: onStoreDocumentPayload<CollabContext>,
    ) => Promise<unknown>,
    beforeUnloadDocument: buildBeforeUnloadHook(config, deps) as (
      data: beforeUnloadDocumentPayload,
    ) => Promise<unknown>,
    afterUnloadDocument: buildAfterUnloadHook(deps),
    onStateless: buildStatelessHook(config, deps) as (
      data: onStatelessPayload,
    ) => Promise<unknown>,
    onRequest: buildHealthzHandler(),
  });
}
