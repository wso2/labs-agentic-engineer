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

// A stand-in for ae-studio-tools' Files socket, for tests and `pnpm dev`
// (07 §11): an HTTP server on a temp Unix socket over ONE in-memory tree,
// shared by every project name. Shas are real git blob shas, so a seed
// baseline means what it would against git. It speaks the contract
// (packages/contracts/sockets/ae-studio/files/openapi.yaml) and the pod's
// error shapes: problem+json, except the 409 conflict body. Never in a cluster.

import { createHash } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import type { components } from "./generated/files-socket.js";

type Schemas = components["schemas"];

export interface FakeFilesSocketOptions {
  /** The tree at start: full repo path → content. */
  files: Record<string, string>;
  /** Returned on every apply that changes the tree. */
  warnings?: Schemas["ApplyWarning"][];
  /** Project names aep-api does not know: every route answers 404 `project_unknown`. */
  unknownProjects?: string[];
}

/** The socket's three operations, as `failNext` can target them. */
export type FilesOp = "lookup" | "bundle" | "apply";

export interface FakeFilesSocket {
  /** The socket path to hand to `createFilesClient`. */
  readonly path: string;
  /** Every request seen, in order (method + raw URL). */
  readonly requests: { method: string; url: string }[];
  /** A commit landed outside the room: changes one file and moves the head. */
  pushExternal(filePath: string, content: string): void;
  /** Answer the next request (of `op`, when given) with this problem. */
  failNext(status: number, code: string, op?: FilesOp): void;
  /** Answer the next request with an arbitrary body (a proxy's error page). */
  failNextRaw(status: number, contentType: string, body: string): void;
  /** Stop serving and remove the socket and its directory. */
  close(): Promise<void>;
}

/** The pod's request body cap (04 §7). */
const BODY_LIMIT = 25 << 20;
/** The project path segment: a DNS-label slug, as the contract requires. */
const PROJECT_NAME = /^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$/;
const ROUTE = /^\/projects\/([^/]+)(\/bundle|\/apply)?$/;

function gitBlobSha(content: string): string {
  const buf = Buffer.from(content, "utf8");
  return createHash("sha1").update(`blob ${buf.length}\0`).update(buf).digest("hex");
}

function send(
  res: http.ServerResponse,
  status: number,
  contentType: string,
  body: string,
  headers: Record<string, string> = {},
): void {
  res.writeHead(status, { "Content-Type": contentType, ...headers });
  res.end(body);
}

function json(res: http.ServerResponse, status: number, body: unknown): void {
  send(res, status, "application/json", JSON.stringify(body));
}

function problem(res: http.ServerResponse, status: number, code: string): void {
  const body: Schemas["Problem"] = {
    type: "about:blank",
    title: http.STATUS_CODES[status] ?? "Error",
    status,
    code,
    detail: code,
  };
  // The pod answers aep_api_unavailable with Retry-After: 5.
  const headers: Record<string, string> =
    code === "aep_api_unavailable" ? { "Retry-After": "5" } : {};
  send(res, status, "application/problem+json", JSON.stringify(body), headers);
}

function hasExactly(v: unknown, keys: string[]): v is Record<string, unknown> {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return false;
  const own = Object.keys(v);
  return own.length === keys.length && keys.every((k) => own.includes(k));
}

function isFileWrite(v: unknown): v is Schemas["FileWrite"] {
  return (
    hasExactly(v, ["path", "content", "baseSha"]) &&
    typeof v.path === "string" &&
    typeof v.content === "string" &&
    typeof v.baseSha === "string"
  );
}

function isFileDelete(v: unknown): v is Schemas["FileDelete"] {
  return (
    hasExactly(v, ["path", "baseSha"]) &&
    typeof v.path === "string" &&
    typeof v.baseSha === "string"
  );
}

/**
 * The request schema rejects unknown fields (no owner/repo, ever), as the
 * pod's validator does.
 */
function parseApplyRequest(raw: string): Schemas["ApplyRequest"] | null {
  let body: unknown;
  try {
    body = JSON.parse(raw);
  } catch {
    return null;
  }
  if (
    !hasExactly(body, ["writes", "deletes", "message"]) ||
    typeof body.message !== "string" ||
    !Array.isArray(body.writes) ||
    !Array.isArray(body.deletes) ||
    !body.writes.every(isFileWrite) ||
    !body.deletes.every(isFileDelete)
  ) {
    return null;
  }
  return body as Schemas["ApplyRequest"];
}

/** Starts the fake on a fresh temp socket. */
export async function startFakeFilesSocket(
  options: FakeFilesSocketOptions,
): Promise<FakeFilesSocket> {
  const tree = new Map(Object.entries(options.files));
  const warnings = options.warnings ?? [];
  const unknown = new Set(options.unknownProjects ?? []);
  const requests: { method: string; url: string }[] = [];
  let commits = 0;
  let headSha = gitBlobSha("commit 0");
  let nextFailure: { op: FilesOp | undefined; send: (res: http.ServerResponse) => void } | null = null;

  const newCommit = () => {
    commits += 1;
    headSha = gitBlobSha(`commit ${commits}`);
  };

  const apply = (res: http.ServerResponse, request: Schemas["ApplyRequest"]) => {
    const conflicts: Schemas["Conflict"][] = [];
    for (const change of [...request.writes, ...request.deletes]) {
      const current = tree.get(change.path);
      const currentSha = current === undefined ? "" : gitBlobSha(current);
      // A delete needs the file to exist; a write's "" baseSha needs it absent.
      const isDelete = !("content" in change);
      if (change.baseSha !== currentSha || (isDelete && current === undefined)) {
        conflicts.push({ path: change.path, baseSha: change.baseSha, currentSha });
      }
    }
    if (conflicts.length > 0) {
      const body: Schemas["ApplyConflicts"] = { code: "conflict", conflicts };
      return json(res, 409, body);
    }
    const changed =
      request.writes.some((w) => tree.get(w.path) !== w.content) ||
      request.deletes.length > 0;
    for (const w of request.writes) tree.set(w.path, w.content);
    for (const d of request.deletes) tree.delete(d.path);
    if (changed) newCommit();
    const result: Schemas["ApplyResult"] = {
      commitSha: headSha,
      changed,
      files: request.writes.map((w) => ({ path: w.path, sha: gitBlobSha(w.content) })),
      warnings: changed ? warnings : [],
    };
    return json(res, 200, result);
  };

  const server = http.createServer((req, res) => {
    requests.push({ method: req.method ?? "", url: req.url ?? "" });
    const url = new URL(req.url ?? "/", "http://files.sock");
    const match = url.pathname.match(ROUTE);
    const op = match?.[2] ?? "";
    const filesOp: FilesOp = op === "/bundle" ? "bundle" : op === "/apply" ? "apply" : "lookup";
    if (nextFailure && (nextFailure.op === undefined || nextFailure.op === filesOp)) {
      const fail = nextFailure.send;
      nextFailure = null;
      // Drain any body so the connection stays usable.
      req.resume();
      return fail(res);
    }
    if (!match) return problem(res, 404, "not_found");
    const project = decodeURIComponent(match[1] ?? "");
    if (!PROJECT_NAME.test(project)) return problem(res, 400, "path_invalid");
    if (unknown.has(project)) {
      req.resume();
      return problem(res, 404, "project_unknown");
    }

    if (req.method === "GET" && op === "") {
      const body: Schemas["ProjectLookup"] = {
        known: true,
        owner: "fake-owner",
        repo: project,
        headSha,
      };
      return json(res, 200, body);
    }
    if (req.method === "GET" && op === "/bundle") {
      const prefix = url.searchParams.get("prefix") ?? "";
      const body: Schemas["FileBundle"] = {
        commitSha: headSha,
        files: [...tree]
          .filter(([p]) => p.startsWith(prefix))
          .map(([p, content]) => ({ path: p, content, sha: gitBlobSha(content) })),
      };
      return json(res, 200, body);
    }
    if (req.method === "POST" && op === "/apply") {
      const chunks: Buffer[] = [];
      let size = 0;
      let tooLarge = false;
      req.on("data", (chunk: Buffer) => {
        size += chunk.length;
        if (size > BODY_LIMIT) tooLarge = true;
        else chunks.push(chunk);
      });
      req.on("end", () => {
        if (tooLarge) return problem(res, 413, "payload_too_large");
        const request = parseApplyRequest(Buffer.concat(chunks).toString("utf8"));
        if (!request) return problem(res, 400, "path_invalid");
        return apply(res, request);
      });
      return;
    }
    return problem(res, 404, "not_found");
  });

  const dir = await mkdtemp(path.join(os.tmpdir(), "ae-files-"));
  const socketPath = path.join(dir, "files.sock");
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(socketPath, () => resolve());
  });

  return {
    path: socketPath,
    requests,
    pushExternal(filePath, content) {
      tree.set(filePath, content);
      newCommit();
    },
    failNext(status, code, op) {
      nextFailure = { op, send: (res) => problem(res, status, code) };
    },
    failNextRaw(status, contentType, body) {
      nextFailure = { op: undefined, send: (res) => send(res, status, contentType, body) };
    },
    async close() {
      await new Promise<void>((resolve) => {
        server.close(() => resolve());
        server.closeAllConnections();
      });
      await rm(dir, { recursive: true, force: true });
    },
  };
}
