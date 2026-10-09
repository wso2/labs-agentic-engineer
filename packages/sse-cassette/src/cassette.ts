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
 * The cassette format: one recorded HTTP exchange, response body kept as the
 * EXACT byte chunks the origin produced, each stamped with its arrival offset.
 *
 * Chunks are base64 — a streamed response may split a multibyte UTF-8
 * character across two chunks, and reproducing exactly that hazard in replay
 * is part of the point. `tMs` is milliseconds since the first response byte,
 * so replay can reproduce (or scale) the real pacing.
 */

import { readFileSync, writeFileSync, readdirSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { gzipSync, gunzipSync } from "node:zlib";
import { z } from "zod";

export const CASSETTE_VERSION = 1;

export const CassetteSchema = z.object({
  version: z.literal(CASSETTE_VERSION),
  recordedAt: z.string(),
  request: z.object({
    method: z.string(),
    /** Path + query, as sent on the wire (no scheme/host). */
    path: z.string(),
    headers: z.record(z.string(), z.string()),
    /** Parsed JSON request body (when the request was `application/json`). */
    body: z.unknown().optional(),
    /** Raw request body for non-JSON payloads. */
    bodyB64: z.string().optional(),
  }),
  response: z.object({
    status: z.number(),
    headers: z.record(z.string(), z.string()),
  }),
  chunks: z.array(
    z.object({
      /** Milliseconds since the first response chunk arrived. */
      tMs: z.number(),
      /** Exact bytes of this chunk. */
      b64: z.string(),
    }),
  ),
});

export type Cassette = z.infer<typeof CassetteSchema>;
export type CassetteChunk = Cassette["chunks"][number];

/** Headers whose values never belong in a committed fixture. */
export const DEFAULT_SCRUB_HEADERS = [
  "authorization",
  "x-anthropic-key",
  "cookie",
  "set-cookie",
  "x-api-key",
];

export const REDACTED = "<redacted>";

/** Lowercase keys and redact sensitive values (never drop — shape is data too). */
export function scrubHeaders(
  headers: Record<string, string | string[] | undefined>,
  scrub: string[] = DEFAULT_SCRUB_HEADERS,
): Record<string, string> {
  const denied = new Set(scrub.map((h) => h.toLowerCase()));
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(headers)) {
    if (value === undefined) continue;
    const k = key.toLowerCase();
    const v = Array.isArray(value) ? value.join(", ") : value;
    out[k] = denied.has(k) ? REDACTED : v;
  }
  return out;
}

/** Cassettes are big (per-token frames); `.json.gz` keeps fixtures small. */
export function loadCassette(file: string): Cassette {
  const raw = file.endsWith(".gz")
    ? gunzipSync(readFileSync(file)).toString("utf8")
    : readFileSync(file, "utf8");
  return CassetteSchema.parse(JSON.parse(raw));
}

/** Load every `*.json` / `*.json.gz` cassette in a directory, sorted by filename. */
export function loadCassettes(dir: string): Cassette[] {
  return readdirSync(dir)
    .filter((f) => f.endsWith(".json") || f.endsWith(".json.gz"))
    .sort()
    .map((f) => loadCassette(join(dir, f)));
}

export function saveCassette(file: string, cassette: Cassette): void {
  const json = `${JSON.stringify(cassette, null, 2)}\n`;
  if (file.endsWith(".gz")) writeFileSync(file, gzipSync(Buffer.from(json, "utf8")));
  else writeFileSync(file, json);
}

/** `001-post-api-v1-projects-x-turns.json` — ordered, greppable, fs-safe. */
export function cassetteFilename(seq: number, method: string, path: string): string {
  const slug =
    path
      .split("?")[0]!
      .replace(/[^a-zA-Z0-9]+/g, "-")
      .replace(/^-|-$/g, "")
      .toLowerCase()
      .slice(0, 80) || "root";
  return `${String(seq).padStart(3, "0")}-${method.toLowerCase()}-${slug}.json`;
}

export function ensureDir(dir: string): void {
  mkdirSync(dir, { recursive: true });
}

/** All response chunks concatenated — the full body bytes. */
export function cassetteBytes(cassette: Cassette): Uint8Array {
  const parts = cassette.chunks.map((c) => Buffer.from(c.b64, "base64"));
  return new Uint8Array(Buffer.concat(parts));
}
