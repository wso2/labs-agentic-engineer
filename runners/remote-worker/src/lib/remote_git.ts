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

// The two READ-ONLY remote-git MCP tools, served in-process with the Job's
// mounted GitHub credential: an agent reads a provider's OpenAPI contract
// straight out of its repo (endpoint spec discovery), no clone.
//
//   - get_remote_git_file_contents → GET /repos/{owner}/{repo}/contents/{path}?ref=
//   - search_remote_git_code       → GET /search/code?q=<query> repo:{owner}/{repo}
//
// There is deliberately no write surface. Three guards bound the reads, all
// checked before any GitHub request:
//
//  1. Owner-must-match-org. `owner` must be the org's own GitHub account
//     (AEP_GITHUB_OWNER, stamped by aep-api from the github_login the org
//     connected), compared case-insensitively. An agent in org A cannot read
//     org B's repos, or any other account's, through the org's token.
//  2. The coordinates stay inside that owner's repo. `repo` must be a plain
//     repository name and no path segment may be "." or "..": fetch resolves
//     dot segments, so `repo: ".."` or `path: "../../victim/x/contents/y"`
//     would otherwise re-point the read at another owner past guard 1, and a
//     `repo` carrying a space could smuggle a search qualifier past guard 3.
//  3. No scope qualifier in a search query. GitHub OR-combines `repo:` (and
//     `org:`/`user:`) qualifiers, so `secret repo:acme/other` next to the
//     appended `repo:{owner}/{repo}` would search both. Refused, so the
//     appended scope is always the query's only one.
//
// The names, descriptions and input schemas are the contract aep-api's
// `mcpdiscovery` served before this moved into the runner, and the AE Studio
// pod's `internal/mcp/tools.go` still serves: two runtimes, one contract —
// keep the two copies byte-identical.

import type { LocalMcpTools, McpToolDescriptor, McpToolResult } from "./mcp_local_tools.js";

const GET_FILE = "get_remote_git_file_contents";
const SEARCH_CODE = "search_remote_git_code";

const DEFAULT_API_BASE = "https://api.github.com";
const REQUEST_TIMEOUT_MS = 15_000;
// GitHub inlines at most ~1 MiB of base64 content; a larger decode is refused.
const MAX_CONTENT_BYTES = 1 << 20;
// Base64 inflates ~4/3, plus the JSON envelope: headroom over the content cap
// keeps well-formed replies whole while refusing an unbounded body.
const MAX_BODY_BYTES = MAX_CONTENT_BYTES * 2 + (1 << 16);
// A tool result is prompt input: 128 KiB of text is far beyond any OpenAPI
// document these tools exist to read (an 868 KB PDF once rode a turn as
// ~1.5M junk tokens).
const MAX_TOOL_FILE_BYTES = 128 << 10;
const SEARCH_PER_PAGE = 30;
const MAX_SEARCH_ITEMS = 30;
const MAX_ERROR_BODY = 512;

const SCOPE_QUALIFIER = /\b(repo|org|user|fork):/i;
// A GitHub repository name: letters, digits, ".", "-", "_" ("." and ".." are
// not names).
const REPO_NAME = /^[A-Za-z0-9._-]+$/;

const OWNER_PROPERTY = { type: "string", description: "repo owner — MUST be your organization's GitHub account" };
const REPO_PROPERTY = { type: "string", description: "repository name" };

const REMOTE_GIT_TOOL_DESCRIPTORS: McpToolDescriptor[] = [
  {
    name: GET_FILE,
    description:
      "Read a file (or list a directory) from a repository in THIS organization over the " +
      "GitHub API — no clone. Use this AFTER list_org_component_endpoints reports a provider whose " +
      "`spec.availability` is `repo`: pass that row's owner/repo plus the spec path to read the real " +
      "OpenAPI document. A file returns decoded `content` + `sha`; a directory returns `entries[]` " +
      "(each with path/type/sha) so you can drill down. `ref` is optional (branch/tag/commit; " +
      "defaults to the repo's default branch). TEXT ONLY: a binary file (PDF, image, …) answers with " +
      "its sha and a `note` instead of content — do not retry, it will never return bytes; oversized " +
      "text is truncated with a note. Read-only, and restricted to your own organization's " +
      "repos — a request for any other owner is refused.",
    inputSchema: {
      type: "object",
      properties: {
        owner: OWNER_PROPERTY,
        repo: REPO_PROPERTY,
        path: { type: "string", description: "repo-relative file or directory path (empty = repo root)" },
        ref: { type: "string", description: "optional branch/tag/commit" },
      },
      required: ["owner", "repo", "path"],
    },
  },
  {
    name: SEARCH_CODE,
    description:
      "Search code in a repository in THIS organization over the GitHub API to LOCATE a " +
      "file when you do not know its exact path (e.g. find where an `openapi.yaml` lives before " +
      "reading it with get_remote_git_file_contents). Returns matching `items[]` of {path, sha}. " +
      "Read-only, and restricted to your own organization's repos — a request for any other owner " +
      "is refused.",
    inputSchema: {
      type: "object",
      properties: {
        owner: OWNER_PROPERTY,
        repo: REPO_PROPERTY,
        query: { type: "string", description: "code search query (the repo scope is added for you)" },
      },
      required: ["owner", "repo", "query"],
    },
  },
];

export interface RemoteGitToolsOpts {
  /** The Job's mounted GitHub token. Never logged, never echoed. */
  token: string;
  /** The org's GitHub account: the only owner these tools read. */
  owner: string;
  /** Test seam; production reads api.github.com. */
  apiBase?: string;
  /** Test seam; production uses the global fetch. */
  fetchImpl?: typeof fetch;
}

/** Refused before any GitHub request: an owner or query the guards reject. */
class RefusedError extends Error {}

type CallStatus = "ok" | "refused" | "error";

export function createRemoteGitTools(opts: RemoteGitToolsOpts): LocalMcpTools {
  const apiBase = opts.apiBase ?? DEFAULT_API_BASE;
  const doFetch = opts.fetchImpl ?? fetch;

  function authorize(owner: string): void {
    if (owner === "" || opts.owner === "" || owner.toLowerCase() !== opts.owner.toLowerCase()) {
      throw new RefusedError(
        `repo owner is not owned by the caller's organization: ${JSON.stringify(owner)} (org owns ${JSON.stringify(opts.owner)})`,
      );
    }
  }

  function checkRepo(repo: string): void {
    if (!REPO_NAME.test(repo) || repo === "." || repo === "..") {
      throw new RefusedError(`invalid repository name: ${JSON.stringify(repo)}`);
    }
  }

  async function get(url: string, label: string): Promise<Buffer> {
    let res: Response;
    try {
      res = await doFetch(url, {
        headers: {
          Authorization: `Bearer ${opts.token}`,
          Accept: "application/vnd.github+json",
          "X-GitHub-Api-Version": "2022-11-28",
        },
        // Never follow: GitHub answers a renamed or transferred repo with a 301
        // to /repositories/{id}/…, and following it with the bearer would read
        // whatever account holds that repo now, past the owner guard.
        redirect: "manual",
        signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
      });
    } catch (err) {
      throw new Error(`github ${label} request: ${err instanceof Error ? err.message : String(err)}`);
    }
    if (res.status >= 300 && res.status < 400) {
      await res.body?.cancel();
      throw new Error(`github ${label} redirected (status ${res.status}); redirects are not followed`);
    }
    const body = await readCapped(res, MAX_BODY_BYTES);
    if (res.status !== 200) {
      throw new Error(`github ${label} failed (status ${res.status}): ${truncateForError(body)}`);
    }
    return body;
  }

  async function getFileContents(args: Record<string, unknown>): Promise<RemoteGitFileView> {
    const owner = str(args.owner);
    const repo = str(args.repo);
    if (owner === "" || repo === "") throw new RefusedError("missing required arguments: owner and repo");
    authorize(owner);
    checkRepo(repo);
    const path = str(args.path);
    if (path.split("/").some((seg) => seg === "." || seg === "..")) {
      throw new RefusedError(`path must not contain "." or ".." segments: ${JSON.stringify(path)}`);
    }
    const ref = str(args.ref);
    let url = `${apiBase}/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/contents/${escapePath(path)}`;
    if (ref !== "") url += `?ref=${encodeURIComponent(ref)}`;

    const body = await get(url, "contents");
    // The Contents API answers an array for a directory, an object for a file.
    const parsed = parseJson(body, body.toString("utf8").trimStart().startsWith("[") ? "directory listing" : "file contents");
    if (Array.isArray(parsed)) {
      const entries = parsed.map((e: Record<string, unknown>) => ({ path: str(e?.path), type: str(e?.type), sha: str(e?.sha) }));
      return toFileView({ content: Buffer.alloc(0), sha: "", isDirectory: true, entries });
    }
    const file = (parsed ?? {}) as Record<string, unknown>;
    const content = decodeContent(str(file.content), str(file.encoding));
    return toFileView({ content, sha: str(file.sha), isDirectory: false, entries: [] });
  }

  async function searchCode(args: Record<string, unknown>): Promise<{ items: { path: string; sha: string }[] }> {
    const owner = str(args.owner);
    const repo = str(args.repo);
    const query = str(args.query);
    if (owner === "" || repo === "" || query === "") {
      throw new RefusedError("missing required arguments: owner, repo and query");
    }
    authorize(owner);
    checkRepo(repo);
    if (SCOPE_QUALIFIER.test(query)) {
      throw new RefusedError(`query must not contain a repo/org/user/fork scope qualifier: ${JSON.stringify(query)}`);
    }
    // The guard above leaves this repo: qualifier as the query's only scope.
    const q = `${query.trim()} repo:${owner}/${repo}`;
    const body = await get(`${apiBase}/search/code?q=${encodeURIComponent(q)}&per_page=${SEARCH_PER_PAGE}`, "code search");
    const out = parseJson(body, "search results") as { items?: Record<string, unknown>[] } | null;
    const items = (Array.isArray(out?.items) ? out.items : [])
      .slice(0, MAX_SEARCH_ITEMS)
      .map((it) => ({ path: str(it?.path), sha: str(it?.sha) }));
    return { items };
  }

  async function run(tool: string, label: string, args: Record<string, unknown>, read: () => Promise<unknown>): Promise<McpToolResult> {
    let status: CallStatus = "ok";
    try {
      return { content: [{ type: "text", text: JSON.stringify(await read()) }] };
    } catch (err) {
      status = err instanceof RefusedError ? "refused" : "error";
      const msg = err instanceof Error ? err.message : String(err);
      return { content: [{ type: "text", text: `${label}: ${msg}` }], isError: true };
    } finally {
      // Value-free: the repo coordinates and the outcome, never the token or
      // the content.
      process.stderr.write(
        JSON.stringify({ event: "remote_git.call", tool, repo: `${str(args.owner)}/${str(args.repo)}`, status }) + "\n",
      );
    }
  }

  return {
    descriptors: REMOTE_GIT_TOOL_DESCRIPTORS,
    call(name, args) {
      switch (name) {
        case GET_FILE:
          return run(name, "get remote git file contents", args, () => getFileContents(args));
        case SEARCH_CODE:
          return run(name, "search remote git code", args, () => searchCode(args));
        default:
          return undefined;
      }
    },
  };
}

interface RemoteGitFile {
  content: Buffer;
  sha: string;
  isDirectory: boolean;
  entries: { path: string; type: string; sha: string }[];
}

interface RemoteGitFileView {
  content?: string;
  sha?: string;
  isDirectory: boolean;
  entries?: { path: string; type: string; sha: string }[];
  note?: string;
}

/**
 * Projects a Contents read to what may ride a prompt: binary content is
 * withheld (its sha and size still answer), oversized text is truncated on a
 * character boundary with a note. NUL is checked on its own: it is valid UTF-8,
 * but Postgres jsonb refuses it, and no text document these tools read has one.
 */
function toFileView(f: RemoteGitFile): RemoteGitFileView {
  let content = f.content.toString("utf8");
  let note = "";
  if (!f.isDirectory && (!isValidUtf8(f.content) || f.content.includes(0))) {
    content = "";
    note = `binary file (${f.content.length} bytes) — content withheld; this tool reads text documents`;
  } else if (!f.isDirectory && f.content.length > MAX_TOOL_FILE_BYTES) {
    let cut = MAX_TOOL_FILE_BYTES;
    while (cut > 0 && (f.content[cut] & 0xc0) === 0x80) cut--; // never split a character
    content = f.content.subarray(0, cut).toString("utf8");
    note = `truncated to the first ${cut} of ${f.content.length} bytes`;
  }
  // The shape aep-api answered: empty fields are omitted, isDirectory never is.
  return {
    ...(content !== "" ? { content } : {}),
    ...(f.sha !== "" ? { sha: f.sha } : {}),
    isDirectory: f.isDirectory,
    ...(f.entries.length > 0 ? { entries: f.entries } : {}),
    ...(note !== "" ? { note } : {}),
  };
}

/**
 * Decodes the Contents API `content` field. Encoding "none" is GitHub's "too
 * large to inline" signal and is refused rather than read as an empty file; a
 * real empty file comes back as base64 with empty content.
 */
function decodeContent(content: string, encoding: string): Buffer {
  if (encoding === "none") throw new Error("file too large to inline (GitHub reported encoding=none)");
  if (content === "") return Buffer.alloc(0);
  if (encoding !== "" && encoding !== "base64") throw new Error(`unsupported content encoding ${JSON.stringify(encoding)}`);
  // GitHub wraps the payload at 60 columns.
  const clean = content.replace(/[\r\n]/g, "");
  if (clean.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(clean)) throw new Error("decode base64 content: malformed");
  const decoded = Buffer.from(clean, "base64");
  if (decoded.length > MAX_CONTENT_BYTES) {
    throw new Error(`file content ${decoded.length} bytes exceeds cap ${MAX_CONTENT_BYTES}`);
  }
  return decoded;
}

/** Reads at most `limit` bytes of the body, so an oversized reply cannot grow the Job. */
async function readCapped(res: Response, limit: number): Promise<Buffer> {
  if (!res.body) return Buffer.alloc(0);
  const reader = res.body.getReader();
  const chunks: Buffer[] = [];
  let total = 0;
  try {
    while (total < limit) {
      const { done, value } = await reader.read();
      if (done) break;
      const take = Math.min(value.length, limit - total);
      chunks.push(Buffer.from(value.subarray(0, take)));
      total += take;
    }
  } finally {
    await reader.cancel().catch(() => {});
  }
  return Buffer.concat(chunks);
}

function parseJson(body: Buffer, what: string): unknown {
  try {
    return JSON.parse(body.toString("utf8"));
  } catch (err) {
    throw new Error(`decode ${what}: ${err instanceof Error ? err.message : String(err)}`);
  }
}

const utf8Strict = new TextDecoder("utf-8", { fatal: true });

function isValidUtf8(b: Buffer): boolean {
  try {
    utf8Strict.decode(b);
    return true;
  } catch {
    return false;
  }
}

/**
 * Escapes each segment of a repo-relative path, keeping the slashes. Empty =
 * repo root. Dot segments are refused before this; an empty segment ("a//b")
 * stays under `contents/` and cannot re-root the URL.
 */
function escapePath(p: string): string {
  return p.replace(/^\//, "").split("/").map(encodeURIComponent).join("/");
}

function truncateForError(body: Buffer): string {
  const s = body.toString("utf8");
  return s.length <= MAX_ERROR_BODY ? s : `${s.slice(0, MAX_ERROR_BODY)}…`;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}
