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
 * The pod-mode listeners (07 §11, 08 §2). The public port serves `/v1` behind
 * the user gate: a Platform IdP user token of the pod's org, never an M2M
 * token. The gate runs before route matching, so an unknown `/v1` path is
 * 401/403 before it is 404; no `/v1` operation exists yet. A WebSocket
 * upgrade needs an `Origin` in `AE_ALLOWED_ORIGINS` (403 otherwise, a missing
 * one included) and the path `/v1/rooms` (404 otherwise); phase 2 hands that
 * socket to Hocuspocus, which authenticates in-protocol. Until then every
 * upgrade is answered and closed. The health port (not in the Service, not
 * routed) serves `/healthz` (liveness) and `/readyz` (200 once the public
 * port is bound, 503 while closing).
 */

import { createServer, STATUS_CODES, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import type { Duplex } from "node:stream";
import { createVerifier, problem, UnauthenticatedError, userRule } from "@aep/platform-idp-auth";
import type { PodConfig } from "./config.js";

export interface PodListeners {
  publicUrl: string;
  healthUrl: string;
  close(): Promise<void>;
}

/** One structured, value-free log line. */
export interface PodLogLine {
  msg: "pod_health_listening" | "pod_public_listening" | "pod_listeners_stopped" | "pod_request_failed";
  source: "ae-collab";
  port?: number;
}

export interface PodListenerDeps {
  /** Where log lines go; stdout unless a test captures them. */
  log?: (line: PodLogLine) => void;
}

const stdoutLog = (line: PodLogLine): void => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};

const BEARER = /^Bearer ([^\s]+)$/i;
/** `/v1` and everything under it, any casing (the gate must not be dodged by `/V1`). */
const V1 = /^\/v1(?:\/|$)/i;
const ROOMS_PATH = "/v1/rooms";

/** A refusal, as the gate decides it. Details are fixed sentences: no token or claim value. */
interface Refusal {
  status: 401 | 403;
  code: string;
  detail: string;
  challenge?: string;
}

/** The raw request path, without the query. Never URL-normalised: a path is matched as sent. */
function pathOf(req: IncomingMessage): string {
  return (req.url ?? "").split("?", 1)[0] ?? "";
}

function sendProblem(res: ServerResponse, status: number, code: string, detail: string, challenge?: string): void {
  const p = problem(status, code, detail);
  res
    .writeHead(p.status, {
      "content-type": "application/problem+json",
      ...(challenge ? { "www-authenticate": challenge } : {}),
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

/** The `/v1` user gate: `null` admits the request. Built once per process. */
function userGate(cfg: PodConfig): (req: IncomingMessage) => Promise<Refusal | null> {
  const verify = createVerifier({ issuer: cfg.issuer, jwksUrl: cfg.jwksUrl });
  const kinds = [{ name: "user" as const, audiences: cfg.userAudiences }];
  const pod = { orgId: cfg.orgId, orgHandle: cfg.orgHandle };
  return async (req) => {
    const token = BEARER.exec(req.headers.authorization ?? "")?.[1];
    if (!token) return { status: 401, code: "unauthenticated", detail: "a bearer token is required", challenge: "Bearer" };
    let verified;
    try {
      verified = await verify(token, kinds);
    } catch (err) {
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

function publicServer(cfg: PodConfig, log: (line: PodLogLine) => void): Server {
  const gate = userGate(cfg);
  const allowedOrigins = new Set(cfg.allowedOrigins);
  const handle = async (req: IncomingMessage, res: ServerResponse): Promise<void> => {
    if (V1.test(pathOf(req))) {
      const refusal = await gate(req);
      if (refusal) {
        sendProblem(res, refusal.status, refusal.code, refusal.detail, refusal.challenge);
        return;
      }
    }
    // No operation is served yet: every admitted request is 404.
    sendProblem(res, 404, "not_found", "no such route");
  };
  const server = createServer((req, res) => {
    handle(req, res).catch(() => {
      log({ msg: "pod_request_failed", source: "ae-collab" });
      // Never a stack or a message.
      if (res.headersSent) res.destroy();
      else sendProblem(res, 500, "internal", "");
    });
  });
  server.on("upgrade", (req: IncomingMessage, socket: Duplex) => {
    socket.on("error", () => socket.destroy());
    const origin = req.headers.origin;
    if (!origin || !allowedOrigins.has(origin)) {
      refuseUpgrade(socket, 403, "origin_not_allowed", "the request origin is not allowed");
      return;
    }
    if (pathOf(req) !== ROOMS_PATH) {
      refuseUpgrade(socket, 404, "not_found", "no such route");
      return;
    }
    // Phase 2: Hocuspocus takes the socket here.
    refuseUpgrade(socket, 404, "not_found", "rooms are not served yet");
  });
  return server;
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

function listen(server: Server, port: number): Promise<Server> {
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, () => {
      server.off("error", reject);
      resolve(server);
    });
  });
}

/** Stops accepting and ends open connections (keep-alive ones included) at once. */
function closeServer(server: Server): Promise<void> {
  const closed = new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
  server.closeAllConnections();
  return closed;
}

function urlOf(server: Server): string {
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}

/** Starts both listeners: health first, so `/readyz` answers 503 until the public port is bound. */
export async function startPodListeners(cfg: PodConfig, deps: PodListenerDeps = {}): Promise<PodListeners> {
  const log = deps.log ?? stdoutLog;
  const line = (msg: PodLogLine["msg"], server?: Server): PodLogLine => ({
    msg,
    source: "ae-collab",
    ...(server ? { port: (server.address() as AddressInfo).port } : {}),
  });
  let ready = false;
  // Built before anything binds: a wiring error (empty issuer, JWKS URL or
  // org) fails the start instead of every request.
  const pub = publicServer(cfg, log);
  const health = await listen(healthServer(() => ready), cfg.healthPort);
  log(line("pod_health_listening", health));
  try {
    await listen(pub, cfg.listenPort);
  } catch (err) {
    await closeServer(health);
    throw err;
  }
  log(line("pod_public_listening", pub));
  ready = true;
  return {
    publicUrl: urlOf(pub),
    healthUrl: urlOf(health),
    async close() {
      ready = false;
      try {
        await closeServer(pub);
      } finally {
        await closeServer(health);
        log(line("pod_listeners_stopped"));
      }
    },
  };
}
