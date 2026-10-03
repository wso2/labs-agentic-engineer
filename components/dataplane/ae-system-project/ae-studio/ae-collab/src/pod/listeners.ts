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
 * The pod's listeners (07 §11, 08 §2): two that hand WebSockets to ONE
 * Hocuspocus instance, and the health port.
 *
 *   public  0.0.0.0:`listenPort`. `/v1` HTTP sits behind the user gate (a
 *           Platform IdP user token of the pod's org, never M2M; before route
 *           matching, so an unknown `/v1` path is 401/403 before 404; no `/v1`
 *           operation yet). A WebSocket upgrade must pass `originAllowed` (403)
 *           and name exactly `/v1/rooms` (404).
 *   local   127.0.0.1:`localPort` (8091), for the in-pod agent: any upgrade
 *           path, no Origin check; plain HTTP is 404.
 *   health  `/healthz` (liveness) and `/readyz` (200 once both room listeners
 *           are bound, 503 while closing); not in the Service, not routed.
 *
 * Closing (SIGTERM, 07 §10): both room listeners stop accepting, then `drain`
 * runs (the Room's shutdown flush), handed `endSockets`: it ends the open room
 * sockets first, so no edit reaches a room after its flush read it, then
 * flushes. The sockets are ended in any case once drain settles, and the
 * health listener closes last; close resolves only then.
 *
 * The listener a socket came in on rides in its Hocuspocus context
 * (`{listener}`), which is how `auth.ts` picks the token kind; the client
 * cannot set it.
 */

import { createServer, STATUS_CODES, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import type { Duplex } from "node:stream";
import { WebSocketServer, type RawData, type WebSocket } from "ws";
import type { Hocuspocus } from "@hocuspocus/server";
import { IdpUnavailableError, problem, UnauthenticatedError, userRule } from "@aep/platform-idp-auth";
import type { CollabContext, ListenerKind, Verify } from "./auth.js";
import type { ListenerConfig, PodConfig } from "./config.js";
import { stdoutLog, type PodLog, type PodLogLine } from "./log.js";

export interface PodListeners {
  publicUrl: string;
  localUrl: string;
  healthUrl: string;
  close(): Promise<void>;
}

/** The `/v1` HTTP gate: `null` admits the request. */
export type HttpGate = (req: IncomingMessage) => Promise<Refusal | null>;

export interface PodListenerDeps {
  /** The Room: every accepted upgrade on either listener is handed to it. */
  rooms: Hocuspocus<CollabContext>;
  gate: HttpGate;
  /** Where log lines go; stdout unless a test captures them. */
  log?: PodLog;
  /**
   * Runs on close, after both room listeners stop accepting. `endSockets`
   * ends every open room socket on both listeners; drain calls it before it
   * reads the rooms (close calls it again afterwards, a no-op by then).
   */
  drain?: (endSockets: () => void) => Promise<void>;
}

const BEARER = /^Bearer ([^\s]+)$/i;
/** `/v1` and everything under it, any casing (the gate must not be dodged by `/V1`). */
const V1 = /^\/v1(?:\/|$)/i;
const ROOMS_PATH = "/v1/rooms";
const LOOPBACK = "127.0.0.1";
/**
 * The largest WebSocket frame either listener takes (1009 above it), buffered
 * before auth: above the 25 MiB Files apply cap, far below ws's 100 MiB.
 */
const MAX_FRAME_BYTES = 32 << 20;

/** A refusal, as the gate decides it. Details are fixed sentences: no token or claim value. */
interface Refusal {
  status: 401 | 403 | 503;
  code: string;
  detail: string;
  challenge?: string;
  retryAfter?: string;
}

/** The raw request path, without the query. Never URL-normalised: a path is matched as sent. */
function pathOf(req: IncomingMessage): string {
  return (req.url ?? "").split("?", 1)[0] ?? "";
}

function sendProblem(
  res: ServerResponse,
  status: number,
  code: string,
  detail: string,
  extra: { challenge?: string | undefined; retryAfter?: string | undefined } = {},
): void {
  const p = problem(status, code, detail);
  res
    .writeHead(p.status, {
      "content-type": "application/problem+json",
      ...(extra.challenge ? { "www-authenticate": extra.challenge } : {}),
      ...(extra.retryAfter ? { "retry-after": extra.retryAfter } : {}),
    })
    .end(JSON.stringify(p.body));
}

/** Answers an upgrade request on the raw socket, then closes it. */
function refuseUpgrade(socket: Duplex, status: number, code: string, detail: string): void {
  const p = problem(status, code, detail);
  const body = JSON.stringify(p.body);
  socket.end(
    `HTTP/1.1 ${p.status} ${STATUS_CODES[p.status] ?? ""}\r\n` +
      "content-type: application/problem+json\r\n" +
      `content-length: ${Buffer.byteLength(body)}\r\n` +
      "connection: close\r\n\r\n" +
      body,
    () => socket.destroy(),
  );
}

/**
 * The public listener's Origin rule. A present `Origin` must be listed in
 * `AE_ALLOWED_ORIGINS`, exactly.
 */
export function originAllowed(origin: string | undefined, cfg: Pick<ListenerConfig, "allowedOrigins">): boolean {
  if (origin === undefined) {
    // TEMPORARY (phase 3 deletes): absent Origin accepted for the old agents bridge
    return true;
  }
  return cfg.allowedOrigins.includes(origin);
}

/** The pod's `/v1` user gate over the shared verifier. */
export function userGate(cfg: Pick<PodConfig, "orgId" | "orgHandle" | "userAudiences">, verify: Verify): HttpGate {
  const kinds = [{ name: "user" as const, audiences: cfg.userAudiences }];
  const pod = { orgId: cfg.orgId, orgHandle: cfg.orgHandle };
  return async (req) => {
    const token = BEARER.exec(req.headers.authorization ?? "")?.[1];
    if (!token) return { status: 401, code: "unauthenticated", detail: "a bearer token is required", challenge: "Bearer" };
    let verified;
    try {
      verified = await verify(token, kinds);
    } catch (err) {
      if (err instanceof IdpUnavailableError) {
        // No verdict on the token: the IdP's keys could not be fetched.
        return { status: 503, code: "idp_unavailable", detail: "the identity provider cannot be reached", retryAfter: "5" };
      }
      if (!(err instanceof UnauthenticatedError)) throw err;
      return {
        status: 401,
        code: "unauthenticated",
        detail: "the bearer token is not valid here",
        challenge: 'Bearer error="invalid_token"',
      };
    }
    // Only the user kind is offered, so anything else is a wiring fault.
    if (verified.kind !== "user") throw new Error("pod gate: unexpected token kind");
    if (!userRule(verified.claims, pod)) {
      return { status: 403, code: "org_mismatch", detail: "the token is not for this organization" };
    }
    return null;
  };
}

/**
 * Hands an accepted socket to Hocuspocus. Only the query of the upgrade URL
 * travels (the connection parameters); no path and no request header does,
 * so a malformed request target (the local listener takes any) cannot fail
 * the URL parse.
 */
function attach(rooms: Hocuspocus<CollabContext>, ws: WebSocket, req: IncomingMessage, listener: ListenerKind): void {
  const url = new URL("http://ae-collab.invalid/");
  url.search = queryOf(req);
  const request = new Request(url);
  const conn = rooms.handleConnection(ws, request, { listener } as CollabContext);
  ws.on("message", (data: RawData) => conn.handleMessage(bytes(data)));
  ws.on("close", (code: number, reason: Buffer) => conn.handleClose({ code, reason: reason.toString() }));
  ws.on("error", () => ws.terminate());
}

/** The raw query of the request target, without the `?`. */
function queryOf(req: IncomingMessage): string {
  const target = req.url ?? "";
  const at = target.indexOf("?");
  return at === -1 ? "" : target.slice(at + 1);
}

function bytes(data: RawData): Uint8Array {
  if (Array.isArray(data)) return new Uint8Array(Buffer.concat(data));
  return new Uint8Array(data);
}

function roomServer(
  kind: ListenerKind,
  wss: WebSocketServer,
  deps: { rooms: Hocuspocus<CollabContext>; accept: (req: IncomingMessage, socket: Duplex) => boolean },
  onRequest: (req: IncomingMessage, res: ServerResponse) => void,
): Server {
  const server = createServer(onRequest);
  server.on("upgrade", (req: IncomingMessage, socket: Duplex, head: Buffer) => {
    socket.on("error", () => socket.destroy());
    if (!deps.accept(req, socket)) return;
    wss.handleUpgrade(req, socket, head, (ws) => attach(deps.rooms, ws, req, kind));
  });
  return server;
}

function publicServer(cfg: ListenerConfig, deps: Pick<Required<PodListenerDeps>, "rooms" | "gate" | "log">, wss: WebSocketServer): Server {
  const handle = async (req: IncomingMessage, res: ServerResponse): Promise<void> => {
    if (V1.test(pathOf(req))) {
      const refusal = await deps.gate(req);
      if (refusal) {
        sendProblem(res, refusal.status, refusal.code, refusal.detail, refusal);
        return;
      }
    }
    // No operation is served yet: every admitted request is 404.
    sendProblem(res, 404, "not_found", "no such route");
  };
  const accept = (req: IncomingMessage, socket: Duplex): boolean => {
    if (!originAllowed(req.headers.origin, cfg)) {
      refuseUpgrade(socket, 403, "origin_not_allowed", "the request origin is not allowed");
      return false;
    }
    if (pathOf(req) !== ROOMS_PATH) {
      refuseUpgrade(socket, 404, "not_found", "no such route");
      return false;
    }
    return true;
  };
  return roomServer("public", wss, { rooms: deps.rooms, accept }, (req, res) => {
    handle(req, res).catch(() => {
      deps.log({ msg: "pod_request_failed", source: "ae-collab" });
      // Never a stack or a message.
      if (res.headersSent) res.destroy();
      else sendProblem(res, 500, "internal", "");
    });
  });
}

/** The agent's listener: every upgrade goes to the Room, which checks the token. */
function localServer(rooms: Hocuspocus<CollabContext>, wss: WebSocketServer): Server {
  return roomServer("local", wss, { rooms, accept: () => true }, (_req, res) =>
    sendProblem(res, 404, "not_found", "no such route"),
  );
}

function healthServer(ready: () => boolean): Server {
  return createServer((req, res) => {
    const path = pathOf(req);
    if (req.method === "GET" && path === "/healthz") {
      res.writeHead(200, { "content-type": "text/plain" }).end("OK\n");
    } else if (req.method === "GET" && path === "/readyz") {
      const ok = ready();
      res.writeHead(ok ? 200 : 503, { "content-type": "text/plain" }).end(ok ? "OK\n" : "Service Unavailable\n");
    } else {
      sendProblem(res, 404, "not_found", "no such route");
    }
  });
}

function listen(server: Server, port: number, host?: string): Promise<Server> {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, host, () => {
      server.off("error", reject);
      resolve(server);
    });
  });
}

/** Stops accepting and ends open connections (keep-alive ones included) at once. */
function closeServer(server: Server): Promise<void> {
  if (!server.listening) return Promise.resolve();
  const closed = new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
  server.closeAllConnections();
  return closed;
}

function urlOf(server: Server): string {
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}

/**
 * Starts the listeners: health first, so `/readyz` answers 503 until both
 * room listeners are bound. A failed bind closes whatever was bound.
 */
export async function startPodListeners(cfg: ListenerConfig, deps: PodListenerDeps): Promise<PodListeners> {
  const log = deps.log ?? stdoutLog;
  const line = (msg: PodLogLine["msg"], server?: Server): PodLogLine => ({
    msg,
    source: "ae-collab",
    ...(server ? { port: (server.address() as AddressInfo).port } : {}),
  });
  let ready = false;
  // One WebSocket server per listener: its client set is that listener's sockets.
  const publicWss = new WebSocketServer({ noServer: true, maxPayload: MAX_FRAME_BYTES });
  const localWss = new WebSocketServer({ noServer: true, maxPayload: MAX_FRAME_BYTES });
  const pub = publicServer(cfg, { ...deps, log }, publicWss);
  const local = localServer(deps.rooms, localWss);
  const health = await listen(healthServer(() => ready), cfg.healthPort);
  log(line("pod_health_listening", health));
  try {
    await listen(pub, cfg.listenPort);
    log(line("pod_public_listening", pub));
    await listen(local, cfg.localPort, LOOPBACK);
    log(line("pod_local_listening", local));
  } catch (err) {
    await Promise.all([closeServer(pub), closeServer(local), closeServer(health)]);
    throw err;
  }
  ready = true;
  return {
    publicUrl: urlOf(pub),
    localUrl: urlOf(local),
    healthUrl: urlOf(health),
    async close() {
      ready = false;
      // Upgraded sockets left the HTTP servers' books: they are ended here.
      const endSockets = (): void => {
        for (const ws of [...publicWss.clients, ...localWss.clients]) ws.terminate();
      };
      try {
        const stopped = Promise.all([closeServer(pub), closeServer(local)]);
        try {
          await deps.drain?.(endSockets);
        } finally {
          endSockets();
        }
        await stopped;
      } finally {
        await closeServer(health);
        log(line("pod_listeners_stopped"));
      }
    },
  };
}
