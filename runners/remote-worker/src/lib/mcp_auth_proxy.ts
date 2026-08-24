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

// Loopback reverse-proxy in front of POST /internal/v1/mcp.
//
// The Claude Agent SDK's HTTP MCP config only accepts static headers, so a
// token minted at query() construction time cannot rotate. This proxy is the
// SDK's MCP URL; it attaches a live bearer (ClientCredentialsTokenProvider
// or a snapshot) and runs fetchWith401Retry. Local and cloud use the same
// proxy; canRefresh is true when the Job can remint via publisher CC.

import http from "node:http";
import type { AddressInfo } from "node:net";
import { FatalAuthError, fetchWith401Retry, type AccessTokenSource } from "./auth_retry.js";

export interface McpAuthProxy {
  url: string;
  close: () => Promise<void>;
}

export interface StartMcpAuthProxyOpts {
  upstreamUrl: string;
  source: AccessTokenSource;
  canRefresh: boolean;
  onToken?: (token: string) => void | Promise<void>;
  onFatal: (err: FatalAuthError) => void;
}

const HOP = new Set(["host", "connection", "transfer-encoding", "keep-alive", "authorization", "content-length"]);

// fetch() already decompresses the body. Forwarding content-encoding /
// content-length from the upstream reply would label plaintext as gzip with
// the compressed length — Envoy on the cloud gateway compresses.
const RESP_HOP = new Set([
  "transfer-encoding",
  "content-encoding",
  "content-length",
  "connection",
  "keep-alive",
]);

export async function startMcpAuthProxy(opts: StartMcpAuthProxyOpts): Promise<McpAuthProxy> {
  const server = http.createServer((req, res) => {
    void handle(req, res, opts);
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve());
  });
  const addr = server.address() as AddressInfo;
  return {
    url: `http://127.0.0.1:${addr.port}/`,
    close: () =>
      new Promise<void>((resolve, reject) => {
        server.close((err) => (err ? reject(err) : resolve()));
      }),
  };
}

async function handle(
  req: http.IncomingMessage,
  res: http.ServerResponse,
  opts: StartMcpAuthProxyOpts,
): Promise<void> {
  try {
    const body = await readRequestBody(req);
    const headers: Record<string, string> = {};
    for (const [k, v] of Object.entries(req.headers)) {
      if (v === undefined || HOP.has(k.toLowerCase())) continue;
      headers[k] = Array.isArray(v) ? v.join(", ") : v;
    }
    const upstream = await fetchWith401Retry(
      opts.upstreamUrl,
      {
        method: req.method,
        headers,
        body: body.length > 0 ? body : undefined,
      },
      { source: opts.source, canRefresh: opts.canRefresh, onToken: opts.onToken },
    );
    const outHeaders: Record<string, string> = {};
    upstream.headers.forEach((value, key) => {
      if (RESP_HOP.has(key.toLowerCase())) return;
      outHeaders[key] = value;
    });
    res.writeHead(upstream.status, outHeaders);
    res.end(Buffer.from(await upstream.arrayBuffer()));
  } catch (err) {
    if (res.headersSent) {
      res.destroy();
      return;
    }
    if (err instanceof PayloadTooLargeError) {
      res.writeHead(413, { "content-type": "text/plain" });
      res.end("payload too large");
      return;
    }
    if (err instanceof FatalAuthError) {
      opts.onFatal(err);
      res.writeHead(401, { "content-type": "text/plain" });
      res.end("unauthorized");
      return;
    }
    const msg = err instanceof Error ? err.message : String(err);
    res.writeHead(502, { "content-type": "text/plain" });
    res.end(msg);
  }
}

// MCP JSON-RPC is small (initialize / tools/list / tools/call). Cap the
// inbound buffer so a stuck or hostile client cannot grow the Job RSS.
const MAX_MCP_PROXY_BODY = 1024 * 1024;

class PayloadTooLargeError extends Error {
  constructor() {
    super("payload too large");
    this.name = "PayloadTooLargeError";
  }
}

async function readRequestBody(req: http.IncomingMessage): Promise<Buffer> {
  const chunks: Buffer[] = [];
  let n = 0;
  try {
    for await (const c of req) {
      const b = Buffer.isBuffer(c) ? c : Buffer.from(c);
      n += b.length;
      if (n > MAX_MCP_PROXY_BODY) {
        throw new PayloadTooLargeError();
      }
      chunks.push(b);
    }
  } catch (err) {
    if (err instanceof PayloadTooLargeError) throw err;
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`mcp proxy request body: ${msg}`);
  }
  return Buffer.concat(chunks);
}
