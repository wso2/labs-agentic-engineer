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

import { createServer, type Server } from "node:http";

export interface StubCall {
  method: string;
  path: string;
  operationId?: string | undefined;
  /** True when the contract defines this operation but `allow` withholds it. */
  denied?: boolean;
}

interface Op {
  method: string;
  /**
   * The contract path split on `/`, one entry per segment. A `{name}`
   * segment is a path parameter and matches any single non-empty request
   * segment; every other segment must match literally.
   */
  segments: string[];
  operationId?: string | undefined;
  example: unknown;
  allowed: boolean;
}

/**
 * A path always starts with `/`, so the leading split element is always the
 * empty string before it — dropped here rather than trimmed off both ends,
 * because a genuinely empty segment ELSEWHERE in the path (a doubled `/`)
 * must survive into matching: a `{param}` segment matching it would silently
 * accept a request no real client sends.
 */
function pathSegments(path: string): string[] {
  return path.replace(/^\//, "").split("/");
}

function isParamSegment(segment: string): boolean {
  return segment.startsWith("{") && segment.endsWith("}");
}

/**
 * Whether a request's segments satisfy a contract path's segments, and — for
 * picking the best of several matches — how many of them matched literally.
 * A literal path always wins over a templated one because it scores higher:
 * every one of its segments counts, where a template's parameter segments
 * never do.
 */
function matchSegments(pattern: string[], request: string[]): number | undefined {
  if (pattern.length !== request.length) return undefined;
  let literalCount = 0;
  for (let i = 0; i < pattern.length; i++) {
    const p = pattern[i]!;
    const r = request[i]!;
    if (isParamSegment(p)) {
      if (r.length === 0) return undefined;
    } else {
      if (p !== r) return undefined;
      literalCount++;
    }
  }
  return literalCount;
}

/** Build the operation list from the contract. Order does not matter — a request is matched by scoring every candidate, not by insertion order. */
function indexOperations(spec: unknown, allow: readonly string[] | undefined): Op[] {
  const out: Op[] = [];
  const paths = (spec as { paths?: Record<string, Record<string, unknown>> })?.paths ?? {};
  for (const [path, methods] of Object.entries(paths)) {
    for (const [method, opRaw] of Object.entries(methods)) {
      const op = opRaw as {
        operationId?: string;
        responses?: Record<string, { content?: Record<string, { example?: unknown }> }>;
      };
      const example = op.responses?.["200"]?.content?.["application/json"]?.example ?? [];
      // `allow === undefined` defaults every operation to allowed — open by
      // default, not closed. That is safe ONLY because it never happens on
      // the real path: `agent-doc.ts` (`readToolStubs`) refuses to build a
      // stub for a contract with an absent or empty `allow` before this
      // function is ever called. This seam exists so `startStubServer` can
      // also be driven standalone (e.g. serving a whole contract for a test
      // of the stub itself) — do not lean on the permissive default for
      // anything that reaches a real agent under evaluation, and do not
      // "fix" it to closed-by-default here without also updating the guard
      // in `agent-doc.ts` that actually carries the security promise.
      const allowed =
        allow === undefined || (op.operationId !== undefined && allow.includes(op.operationId));
      out.push({
        method: method.toUpperCase(),
        segments: pathSegments(path),
        operationId: op.operationId,
        example,
        allowed,
      });
    }
  }
  if (allow !== undefined) {
    // An `allow` entry the contract does not define is a design-time error the
    // platform already rejects at save. Reaching it here means the agent was
    // generated without that tool, so it would score badly for a reason no
    // prompt fix can address — say so rather than serving a contract that
    // quietly grants less than the document claims.
    const defined = new Set(out.map((op) => op.operationId));
    const unknown = allow.filter((id) => !defined.has(id));
    if (unknown.length > 0) {
      throw new Error(
        `startStubServer: allow names operations this contract does not define: ${unknown.join(", ")}`,
      );
    }
  }
  return out;
}

/**
 * The one operation a request resolves to, or `undefined` for a path the
 * contract does not model at all. Ties cannot occur between two DIFFERENT
 * contract paths — a literal segment fixes an exact string, so at most one
 * literal path can match a given request — but a literal and a template can
 * both match, and the literal's strictly higher score always wins.
 */
function findOperation(ops: Op[], method: string, path: string): Op | undefined {
  const request = pathSegments(path);
  let best: Op | undefined;
  let bestScore = -1;
  for (const op of ops) {
    if (op.method !== method) continue;
    const score = matchSegments(op.segments, request);
    if (score !== undefined && score > bestScore) {
      best = op;
      bestScore = score;
    }
  }
  return best;
}

/**
 * A provider component's contract, served from its own declared examples.
 *
 * The world an evaluation runs in must be FIXED: the same prompt against the
 * same stubs twice must produce the same tool calls, or a score change cannot
 * be attributed to the prompt. Serving the contract's `example` also keeps the
 * fixture honest — it is what the provider itself documents, not data invented
 * for the test.
 */
export async function startStubServer(
  spec: unknown,
  /**
   * `x-aep.tools.openapi[].allow`, when the caller has one. It is the agent's
   * security boundary, and the built agent only carries tools for the
   * operations on it — so a stub answering 200 to anything else could only
   * ever hide an over-reach, never enable one. Omitted, the whole contract is
   * served, which is what a caller testing the stub itself wants.
   */
  allow?: readonly string[],
): Promise<{
  url: string;
  calls: StubCall[];
  close: () => Promise<void>;
}> {
  const ops = indexOperations(spec, allow);
  const calls: StubCall[] = [];

  const server: Server = createServer((req, res) => {
    const path = (req.url ?? "/").split("?")[0]!;
    const op = findOperation(ops, (req.method ?? "GET").toUpperCase(), path);
    calls.push({
      method: req.method ?? "GET",
      path,
      operationId: op?.operationId,
      ...(op !== undefined && !op.allowed ? { denied: true } : {}),
    });
    res.setHeader("content-type", "application/json");
    if (!op) {
      res.statusCode = 404;
      res.end(JSON.stringify({ error: "no such operation in the contract" }));
      return;
    }
    if (!op.allowed) {
      // 403, not 404: the operation is real, the agent simply may not call
      // it. A 404 would read as a broken contract and send a fix round after
      // the wrong thing.
      res.statusCode = 403;
      res.end(
        JSON.stringify({
          error: `${op.operationId ?? "this operation"} is not on this agent's allow-list`,
        }),
      );
      return;
    }
    res.end(JSON.stringify(op.example));
  });

  // Port 0: the OS assigns a free port, so parallel test files never collide.
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const addr = server.address();
  const port = typeof addr === "object" && addr ? addr.port : 0;
  return {
    url: `http://127.0.0.1:${port}`,
    calls,
    close: () => new Promise<void>((resolve) => server.close(() => resolve())),
  };
}
