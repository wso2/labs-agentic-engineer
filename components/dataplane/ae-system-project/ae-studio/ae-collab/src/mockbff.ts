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

// Mock of the two BFF operations this service consumes, so the REAL auth and
// seed code paths run end-to-end while the Go implementations land (#81 /
// #86 phase 2). This is the collab-stack sibling of the console's MSW layer —
// same fixtures, same spirit. Enabled via COLLAB_MOCK_BFF=1; never in cluster.
//
// Behavior contract (kept deliberately boring):
// - GET /api/v1/collab/validate
//     401 without a Bearer token; 403 when the token is literally "deny"
//     (test hook for the rejection path); otherwise 200 with an identity —
//     decoded from the token's JWT payload (name/email claims) when it looks
//     like a JWT, else a fixed mock identity — plus `projectName` resolved
//     from the room ID. The real BFF splits `spec-<org>-<project>` using the
//     caller's org; the mock uses its configured org ("acme" by default).
// - GET /api/v1/projects/{project}/files            → FileMeta list
// - GET /api/v1/projects/{project}/files/{path...}  → FileContent (404 unknown)
//     Mirror the real Files API (#114): repo-relative specs/ paths. Like the
//     console's MSW layer, every project gets the same demo files unless
//     `projects` overrides it — the mock oracle is org-permissive, so the
//     files must be too.

import http from "node:http";
import { devSpecFiles, type RepoSpecFile } from "./fixtures.js";

export interface MockBffOptions {
  /** Per-project file overrides; unlisted projects get the dev files. */
  projects?: Record<string, RepoSpecFile[]>;
  /** The org used to split room IDs, like the real oracle does. */
  org?: string;
}

interface MockIdentity {
  name: string;
  email: string;
}

function identityFromToken(token: string): MockIdentity {
  const fallback = { name: "Mock User", email: "mock@localhost" };
  const parts = token.split(".");
  if (parts.length !== 3 || !parts[1]) return fallback;
  try {
    const claims = JSON.parse(
      Buffer.from(parts[1], "base64url").toString("utf8"),
    ) as Record<string, unknown>;
    return {
      name: typeof claims.name === "string" ? claims.name : fallback.name,
      email: typeof claims.email === "string" ? claims.email : fallback.email,
    };
  } catch {
    return fallback;
  }
}

function json(res: http.ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { "Content-Type": "application/json" });
  res.end(JSON.stringify(body));
}

const FILES_LIST_PATH = /^\/api\/v1\/projects\/([^/]+)\/files$/;
const FILES_APPLY_PATH = /^\/api\/v1\/projects\/([^/]+)\/files\/apply$/;
const FILES_BUNDLE_PATH = /^\/api\/v1\/projects\/([^/]+)\/files\/bundle$/;
// Matched LAST: `bundle` and `apply` are literal segments under what is
// otherwise a trailing wildcard, so they must be tried first here exactly as the
// real server's ServeMux prefers a literal over a wildcard.
const FILE_READ_PATH = /^\/api\/v1\/projects\/([^/]+)\/files\/(.+)$/;

let applyCount = 0;

// Deterministic stand-in for the git blob sha (unused by the seeder, present
// for shape fidelity with the real FileMeta/FileContent).
function mockSha(input: string): string {
  let hash = 0x811c9dc5;
  for (let i = 0; i < input.length; i++) {
    hash = ((hash ^ input.charCodeAt(i)) * 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}

export function createMockBff(options: MockBffOptions = {}): http.Server {
  const projects = options.projects ?? { "demo-shop": devSpecFiles };
  const org = options.org ?? "acme";

  return http.createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://localhost");
    const auth = req.headers.authorization ?? "";
    const token = auth.startsWith("Bearer ") ? auth.slice(7) : "";

    if (req.method === "GET" && url.pathname === "/api/v1/collab/validate") {
      if (!token) return json(res, 401, { title: "Unauthorized" });
      if (token === "deny") return json(res, 403, { title: "Forbidden" });
      const room = req.headers["x-room-id"];
      const prefix = `spec-${org}-`;
      if (typeof room !== "string" || !room.startsWith(prefix)) {
        return json(res, 403, { title: "room not in caller org" });
      }
      return json(res, 200, {
        ...identityFromToken(token),
        projectName: room.slice(prefix.length),
      });
    }

    const listMatch =
      req.method === "GET" && url.pathname.match(FILES_LIST_PATH);
    if (listMatch) {
      if (!token) return json(res, 401, { title: "Unauthorized" });
      const project = decodeURIComponent(listMatch[1] ?? "");
      const files = projects[project] ?? devSpecFiles;
      return json(
        res,
        200,
        files.map((f) => ({
          path: f.path,
          sha: mockSha(f.path + f.content),
          size: f.content.length,
        })),
      );
    }

    // The seed read: every readable file under `prefix`, at one commit. Mirrors
    // the real endpoint's shape — a commitSha plus content-bearing entries — so
    // the client's one-call seed path is exercised for real here.
    const bundleMatch =
      req.method === "GET" && url.pathname.match(FILES_BUNDLE_PATH);
    if (bundleMatch) {
      if (!token) return json(res, 401, { title: "Unauthorized" });
      const project = decodeURIComponent(bundleMatch[1] ?? "");
      const prefix = url.searchParams.get("prefix") ?? "";
      const files = projects[project] ?? devSpecFiles;
      return json(res, 200, {
        commitSha: "mockhead",
        files: files
          .filter((f) => f.path.startsWith(prefix))
          .map((f) => ({
            path: f.path,
            content: f.content,
            sha: mockSha(f.path + f.content),
          })),
      });
    }

    const readMatch =
      req.method === "GET" && url.pathname.match(FILE_READ_PATH);
    if (readMatch) {
      if (!token) return json(res, 401, { title: "Unauthorized" });
      const project = decodeURIComponent(readMatch[1] ?? "");
      const path = decodeURIComponent(readMatch[2] ?? "");
      const files = projects[project] ?? devSpecFiles;
      const file = files.find((f) => f.path === path);
      if (!file) return json(res, 404, { title: "not found" });
      return json(res, 200, {
        path: file.path,
        content: file.content,
        sha: mockSha(file.path + file.content),
      });
    }

    // Committer seam (#133): atomic apply → one mock commit. A write whose
    // baseSha is literally "stale" trips the 409 conflict arm (test hook).
    const applyMatch =
      req.method === "POST" && url.pathname.match(FILES_APPLY_PATH);
    if (applyMatch) {
      if (!token) return json(res, 401, { title: "Unauthorized" });
      let raw = "";
      req.on("data", (chunk: Buffer) => (raw += chunk.toString()));
      req.on("end", () => {
        const body = JSON.parse(raw || "{}") as {
          writes?: { path: string; content: string; baseSha: string }[];
          deletes?: { path: string; baseSha: string }[];
        };
        const writes = body.writes ?? [];
        const stale = writes.filter((w) => w.baseSha === "stale");
        if (stale.length > 0) {
          return json(res, 409, {
            conflicts: stale.map((w) => ({
              path: w.path,
              baseSha: w.baseSha,
              currentSha: "current",
            })),
          });
        }
        applyCount += 1;
        return json(res, 200, {
          commitSha: `mockcommit${applyCount}`,
          files: writes.map((w) => ({
            path: w.path,
            sha: mockSha(w.path + w.content),
          })),
        });
      });
      return;
    }

    return json(res, 404, { title: "not found" });
  });
}

export function startMockBff(
  port: number,
  options: MockBffOptions = {},
): Promise<http.Server> {
  const server = createMockBff(options);
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => resolve(server));
  });
}
