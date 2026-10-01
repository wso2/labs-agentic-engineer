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
 * Provider cassettes: a turn's model calls, recorded as the provider answered
 * them, replayed through `createModel`'s fetch seam. A tool loop makes one
 * request per step, so a turn is a DIRECTORY of `@aep/sse-cassette` files,
 * served in filename order, one per request.
 *
 * `recordingFetch` wraps a real fetch and writes each exchange as the next
 * cassette, auth headers scrubbed (the cassette package's
 * `DEFAULT_SCRUB_HEADERS`). A live turn run through it — `createModel(conn,
 * { fetch: recordingFetch(guardedFetch, dir) })` — leaves a fixture this file
 * replays; grep the directory for the key literal before committing it.
 */

import { mkdirSync, readdirSync } from "node:fs";
import { join } from "node:path";
import {
  CASSETTE_VERSION,
  cassetteFilename,
  cassetteToStream,
  loadCassette,
  saveCassette,
  scrubHeaders,
  type Cassette,
} from "@aep/sse-cassette";

/** One request the replayed model sent. */
export interface ReplayedRequest {
  url: string;
  headers: Record<string, string>;
  body: Record<string, unknown>;
}

/** Serve `dir`'s cassettes in order, one per request, recording what was sent. */
export function replayProvider(dir: string): { fetch: typeof globalThis.fetch; requests: ReplayedRequest[] } {
  const cassettes = readdirSync(dir)
    .filter((f) => f.endsWith(".json") || f.endsWith(".json.gz"))
    .sort()
    .map((f) => loadCassette(join(dir, f)));
  const requests: ReplayedRequest[] = [];
  const fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    requests.push({
      url: String(url),
      headers: Object.fromEntries(new Headers(init?.headers).entries()),
      body: JSON.parse(String(init?.body)) as Record<string, unknown>,
    });
    const cassette = cassettes[requests.length - 1];
    if (!cassette) throw new Error(`cassette ${dir}: request ${requests.length} has no recording`);
    return new Response(cassetteToStream(cassette), {
      status: cassette.response.status,
      headers: cassette.response.headers,
    });
  }) as typeof globalThis.fetch;
  return { fetch, requests };
}

/** `base`, writing every exchange to `dir` as the next cassette. */
export function recordingFetch(base: typeof globalThis.fetch, dir: string): typeof globalThis.fetch {
  mkdirSync(dir, { recursive: true });
  let seq = 0;
  return (async (url: string | URL | Request, init?: RequestInit) => {
    const res = await base(url, init);
    const recordedAt = new Date().toISOString();
    const target = new URL(String(url));
    const chunks: Cassette["chunks"] = [];
    const started = Date.now();
    const reader = res.body?.getReader();
    for (;;) {
      if (!reader) break;
      const { done, value } = await reader.read();
      if (done) break;
      chunks.push({ tMs: Date.now() - started, b64: Buffer.from(value).toString("base64") });
    }
    const responseHeaders = scrubHeaders(Object.fromEntries(res.headers.entries()));
    const cassette: Cassette = {
      version: CASSETTE_VERSION,
      recordedAt,
      request: {
        method: init?.method ?? "GET",
        path: target.pathname + target.search,
        headers: scrubHeaders(Object.fromEntries(new Headers(init?.headers).entries())),
        body: JSON.parse(String(init?.body)) as unknown,
      },
      response: { status: res.status, headers: responseHeaders },
      chunks,
    };
    seq += 1;
    saveCassette(join(dir, `${cassetteFilename(seq, cassette.request.method, cassette.request.path)}.gz`), cassette);
    const body = Buffer.concat(chunks.map((c) => Buffer.from(c.b64, "base64")));
    return new Response(body, { status: res.status, headers: res.headers });
  }) as typeof globalThis.fetch;
}
