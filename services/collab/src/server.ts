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
import type { BffClient } from "./bff.js";
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
    const identity = await deps.bff.validateAccess(token, documentName);
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
    if (!deps.bff || !context.token || !context.projectName) {
      deps.log?.(
        `cannot seed ${documentName}: missing bff/token/project — opening empty`,
      );
      return document;
    }
    // A failed seed must not kill the room: access was already authorized
    // by the oracle, and an unseeded-but-live doc beats a dead connection
    // (transient BFF errors would otherwise hard-fail every join).
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
      deps.log?.(
        `seed failed for ${documentName} (${String(err)}) — opening empty`,
      );
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
