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
 * The preview server: the host page, the theme's frame runtime, an event
 * stream of revisions and findings, and `POST /feedback`. Local only — it
 * binds 127.0.0.1, answers only requests addressed to it by that name or
 * `localhost` (no DNS rebinding), refuses to have its page framed by another
 * site, and takes feedback only as JSON from its own origin (no cross-site
 * posts).
 */

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { dirname, join } from "node:path";
import type { ThemeRuntimes } from "@wso2/prototype-kit/check";
import { HOST_SCRIPT_PATH } from "../assets.js";
import { parseFeedbackSubmission } from "@wso2/prototype-kit/feedback";
import { FEEDBACK_PATH, FEEDBACK_SCHEMA_VERSION, type FeedbackFile } from "../feedback.js";
import { renderHostPage } from "../host-page.js";
import { EventStream, type ServerEvent } from "./sse.js";
import { PrototypeWatcher, type PrototypeStatus } from "./watcher.js";

/** The largest feedback body accepted. */
const MAX_BODY = 1024 * 1024;

/** How much of a refused body is read and thrown away before the connection is cut. */
const DISCARD_CAP = 8 * MAX_BODY;

/** The host page holds the Save control: no other site may frame it (clickjacking). */
const NOT_FRAMEABLE = { "content-security-policy": "frame-ancestors 'none'", "x-frame-options": "DENY" };

export interface PreviewServerOptions {
  dir: string;
  /** 0 picks a free port. */
  port: number;
  persist: boolean;
  theme: ThemeRuntimes;
}

export interface RunningPreview {
  url: string;
  close(): Promise<void>;
}

function statusEvents(status: PrototypeStatus): ServerEvent[] {
  return [...(status.lastGood ? [{ event: "update", data: status.lastGood }] : []), { event: "findings", data: { findings: status.findings } }];
}

/** Serve a file read per request; one that cannot be read answers 500 and never takes the server down. */
function sendFile(res: ServerResponse, path: string): void {
  let body: Buffer;
  try {
    body = readFileSync(path);
  } catch {
    return send(res, 500, "text/plain", "the preview could not read one of its own files");
  }
  send(res, 200, "text/javascript; charset=utf-8", body);
}

function send(res: ServerResponse, status: number, type: string, body: string | Buffer, extra: Record<string, string> = {}): void {
  res.writeHead(status, { "content-type": type, "cache-control": "no-store", "x-content-type-options": "nosniff", ...extra });
  res.end(body);
}

/**
 * The request body as text, or null when it is over the limit. An oversized
 * body is read and discarded, not buffered, so the client finishes its upload
 * and receives the 413; only a body past the hard cap is cut off.
 */
function readBody(req: IncomingMessage): Promise<string | null> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let size = 0;
    req.on("data", (chunk: Buffer) => {
      size += chunk.length;
      if (size > DISCARD_CAP) return req.destroy();
      if (size > MAX_BODY) chunks.length = 0;
      else chunks.push(chunk);
    });
    req.on("end", () => resolve(size > MAX_BODY ? null : Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

export async function startPreviewServer(options: PreviewServerOptions): Promise<RunningPreview> {
  const events = new EventStream();
  const watcher = new PrototypeWatcher(options.dir, options.theme, (status) => statusEvents(status).forEach((e) => events.send(e)));
  await watcher.refresh();

  let port = 0;
  const hosts = () => new Set([`127.0.0.1:${port}`, `localhost:${port}`]);

  const saveFeedback = async (req: IncomingMessage, res: ServerResponse) => {
    if (!(req.headers["content-type"] ?? "").startsWith("application/json")) return send(res, 415, "text/plain", "feedback is JSON");
    const origin = req.headers.origin;
    if (origin !== undefined && !hosts().has(origin.replace(/^http:\/\//, ""))) return send(res, 403, "text/plain", "cross-origin feedback is refused");
    const body = await readBody(req);
    if (body === null) {
      return send(res, 413, "text/plain", "feedback is too large", { connection: "close" });
    }
    let value: unknown;
    try {
      value = JSON.parse(body);
    } catch {
      return send(res, 400, "text/plain", "feedback is not JSON");
    }
    const parsed = parseFeedbackSubmission(value);
    if ("reason" in parsed) return send(res, 400, "text/plain", `feedback is refused: ${parsed.reason}`);
    const { submission } = parsed;
    const file: FeedbackFile = { schemaVersion: FEEDBACK_SCHEMA_VERSION, savedAt: new Date().toISOString(), ...submission };
    const path = join(options.dir, FEEDBACK_PATH);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, `${JSON.stringify(file, null, 2)}\n`, "utf8");
    send(res, 200, "application/json", JSON.stringify({ path: FEEDBACK_PATH, requests: file.requests.length }));
  };

  const server = createServer((req, res) => {
    if (!hosts().has(req.headers.host ?? "")) return send(res, 403, "text/plain", "the preview answers only on 127.0.0.1 and localhost");
    const path = new URL(req.url ?? "/", "http://localhost").pathname;
    if (req.method === "GET" && path === "/") {
      const title = watcher.status().lastGood?.manifest.name ?? "Prototype";
      return send(res, 200, "text/html; charset=utf-8", renderHostPage(`${title} — prototype preview`, { mode: "preview", persist: options.persist }, { src: "host.js" }), NOT_FRAMEABLE);
    }
    if (req.method === "GET" && path === "/host.js") return sendFile(res, HOST_SCRIPT_PATH);
    if (req.method === "GET" && path === "/frame-runtime.js") return sendFile(res, options.theme.frameRuntimePath);
    if (req.method === "GET" && path === "/events") return events.attach(req, res, statusEvents(watcher.status()));
    if (req.method === "POST" && path === "/feedback") {
      saveFeedback(req, res).catch((e: unknown) => send(res, 500, "text/plain", e instanceof Error ? e.message : String(e)));
      return;
    }
    send(res, 404, "text/plain", "not found");
  });

  try {
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(options.port, "127.0.0.1", () => resolve());
    });
  } catch (e) {
    events.close();
    throw e;
  }
  port = (server.address() as AddressInfo).port;
  try {
    watcher.start();
  } catch (e) {
    events.close();
    server.close();
    throw e;
  }

  return {
    url: `http://127.0.0.1:${port}/`,
    close: () =>
      new Promise<void>((resolve) => {
        watcher.stop();
        events.close();
        server.close(() => resolve());
        server.closeAllConnections();
      }),
  };
}
