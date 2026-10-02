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

// The Files port (07 §11): spec content and commits come from ae-studio-tools
// over the pod-local Files socket
// (packages/contracts/sockets/ae-studio/files/openapi.yaml). The socket is
// reachable only inside the pod, so the calls carry no token, and the pod
// resolves owner/repo per call: this client names a project, never a repo.
//
// Every failure lands in one of three classes, which is what the callers act on:
//   ApplyConflictError    a baseSha moved; the committer's doc-wins retry.
//   FilesDeniedError      a verdict (4xx): retrying the same call cannot help.
//   FilesUnavailableError an outage (5xx, 408/425/429, a lost push race, an
//                         unreachable socket): retry later; a refused room
//                         load is tagged `upstream-unavailable`.

import { Agent, fetch } from "undici";
import type { components } from "./generated/files-socket.js";

type Schemas = components["schemas"];

/**
 * One spec file ready for seeding. `path` is the FULL repo-relative path
 * (e.g. specs/requirements/prd.md). Doc keys, commits, the console's file
 * model, and the agents' live-peer writes all share this ONE verbatim scheme
 * — no strip/re-add anywhere. (Retires #113 decision 2's stripped room-key
 * scheme, whose only rationale was matching a historical unprefixed console
 * model; the strip-here/re-add-on-commit dance double-prefixed agent-created
 * files into specs/specs/…, so it's gone.)
 */
export interface SpecFile {
  path: string;
  content: string;
  /** Git blob sha at read time — the committer's baseSha precondition (#133). */
  sha: string;
}

/** The spec room seeds only files under specs/. */
export const SPECS_PREFIX = "specs/";

/** One write in a commit batch: full repo path + full content + baseSha. */
export interface ApplyWrite {
  path: string;
  content: string;
  /** Blob sha this write supersedes; "" = the file must not exist yet. */
  baseSha: string;
}

export interface ApplyDelete {
  path: string;
  baseSha: string;
}

export interface ApplyBatch {
  writes: ApplyWrite[];
  deletes: ApplyDelete[];
  message: string;
}

/**
 * @knipkeep wired in Task 2.12 (the committer switches to the FilesClient).
 */
export interface ApplyOutcome {
  /** New per-file shas on success (full repo paths). */
  files: { path: string; sha: string }[];
  commitSha: string;
  /** Non-fatal notes from the pod about this commit (e.g. a scaffolded file). */
  warnings: { path: string; message: string }[];
}

export interface ProjectRepository {
  owner: string;
  repo: string;
  headSha: string;
}

/** The Files port: Room seeding and project lookup now, the committer from Task 2.12. */
export interface FilesClient {
  /** The project's repository and current head; throws FilesDeniedError when unknown. */
  lookup(project: string): Promise<ProjectRepository>;
  /** Every spec file (under specs/) at one commit. */
  bundle(project: string): Promise<SpecFile[]>;
  /** Land a batch as ONE commit; throws ApplyConflictError on stale baseShas. */
  apply(project: string, batch: ApplyBatch): Promise<ApplyOutcome>;
}

/** A stale-baseSha rejection: nothing was applied (#133 doc-wins retry). */
export class ApplyConflictError extends Error {
  constructor(readonly paths: string[]) {
    super(`apply conflict on: ${paths.join(", ")}`);
    this.name = "ApplyConflictError";
  }
}

/**
 * A verdict: any 4xx except 408/425/429 (and except the 409s above). The
 * same call will get the same answer, so it is never tagged transient.
 * `code` is the problem code, or `http_<status>` when the body had none.
 */
export class FilesDeniedError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    detail = "",
  ) {
    super(`files socket denied (${status} ${code})${detail ? `: ${detail}` : ""}`);
    this.name = "FilesDeniedError";
  }
}

/**
 * An outage: a 5xx, a 408/425/429, a `not_fast_forward` 409 (the branch moved
 * during the save; the pod says re-read and retry), a malformed reply, or a
 * socket that cannot be reached (`code` `socket_unreachable`, `status` 0).
 */
export class FilesUnavailableError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    detail = "",
    options?: ErrorOptions,
  ) {
    super(
      `files socket unavailable (${status} ${code})${detail ? `: ${detail}` : ""}`,
      options,
    );
    this.name = "FilesUnavailableError";
  }
}

/** 4xx statuses that mean "not now" rather than "no". */
const RETRYABLE_4XX = new Set([408, 425, 429]);
/** Error text kept in a message; problem details are short, bodies may not be. */
const MAX_DETAIL = 500;

// The host is a placeholder: the dispatcher connects to the socket, so only
// the path reaches the server.
const ORIGIN = "http://files.sock";

interface Reply {
  status: number;
  contentType: string;
  text: string;
}

function parseJSON(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** The problem `code` and `detail`, or `http_<status>` for a non-problem body. */
function problemOf(reply: Reply): { code: string; detail: string } {
  const body = parseJSON(reply.text);
  if (isRecord(body) && typeof body.code === "string" && body.code) {
    const detail = typeof body.detail === "string" ? body.detail : "";
    return { code: body.code, detail: detail.slice(0, MAX_DETAIL) };
  }
  return { code: `http_${reply.status}`, detail: "" };
}

/** Maps a non-2xx reply to its class (ApplyConflictError is checked by apply). */
function failure(reply: Reply): FilesDeniedError | FilesUnavailableError {
  const { code, detail } = problemOf(reply);
  const transient =
    reply.status >= 500 ||
    RETRYABLE_4XX.has(reply.status) ||
    (reply.status === 409 && code === "not_fast_forward");
  return transient
    ? new FilesUnavailableError(code, reply.status, detail)
    : new FilesDeniedError(code, reply.status, detail);
}

/** A 2xx whose body is not what the contract promises: the pod is broken, not denying. */
function malformed(op: string, reply: Reply): FilesUnavailableError {
  return new FilesUnavailableError(
    "invalid_response",
    reply.status,
    `${op} returned an unexpected body`,
  );
}

function asConflicts(reply: Reply): string[] | null {
  if (reply.status !== 409 || !reply.contentType.startsWith("application/json")) {
    return null;
  }
  const body = parseJSON(reply.text) as Partial<Schemas["ApplyConflicts"]> | undefined;
  if (!isRecord(body) || body.code !== "conflict" || !Array.isArray(body.conflicts)) {
    return null;
  }
  return body.conflicts.map((c) => c.path);
}

/**
 * The FilesClient over the Files socket at `socketPath`. One keep-alive
 * dispatcher per client; a request never leaves the socket.
 */
export function createFilesClient(socketPath: string): FilesClient {
  const dispatcher = new Agent({ connect: { socketPath } });

  async function send(
    method: "GET" | "POST",
    path: string,
    body?: unknown,
  ): Promise<Reply> {
    try {
      const res = await fetch(
        `${ORIGIN}${path}`,
        body === undefined
          ? { method, dispatcher }
          : {
              method,
              dispatcher,
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify(body),
            },
      );
      return {
        status: res.status,
        contentType: res.headers.get("content-type") ?? "",
        text: await res.text(),
      };
    } catch (err) {
      throw new FilesUnavailableError(
        "socket_unreachable",
        0,
        "the Files socket could not be reached",
        { cause: err },
      );
    }
  }

  const projectPath = (project: string) => `/projects/${encodeURIComponent(project)}`;

  return {
    async lookup(project) {
      const reply = await send("GET", projectPath(project));
      if (reply.status !== 200) throw failure(reply);
      const body = parseJSON(reply.text) as Partial<Schemas["ProjectLookup"]> | undefined;
      if (
        !isRecord(body) ||
        typeof body.owner !== "string" ||
        typeof body.repo !== "string" ||
        typeof body.headSha !== "string"
      ) {
        throw malformed("lookup", reply);
      }
      return { owner: body.owner, repo: body.repo, headSha: body.headSha };
    },

    async bundle(project) {
      // ONE request for the whole seed: the pod resolves one commit and reads
      // everything at it, so the document never mixes two commits.
      const reply = await send(
        "GET",
        `${projectPath(project)}/bundle?prefix=${encodeURIComponent(SPECS_PREFIX)}`,
      );
      if (reply.status !== 200) throw failure(reply);
      const body = parseJSON(reply.text) as Partial<Schemas["FileBundle"]> | undefined;
      if (!isRecord(body) || !Array.isArray(body.files)) {
        throw malformed("bundle", reply);
      }
      // Paths arrive VERBATIM (full repo paths): the one doc-key scheme.
      return body.files.map((f) => ({ path: f.path, content: f.content, sha: f.sha }));
    },

    async apply(project, batch) {
      // Exactly the contract's fields: the pod rejects anything else, and
      // owner/repo are the pod's to resolve.
      const request: Schemas["ApplyRequest"] = {
        writes: batch.writes.map((w) => ({
          path: w.path,
          content: w.content,
          baseSha: w.baseSha,
        })),
        deletes: batch.deletes.map((d) => ({ path: d.path, baseSha: d.baseSha })),
        message: batch.message,
      };
      const reply = await send("POST", `${projectPath(project)}/apply`, request);
      const conflicts = asConflicts(reply);
      if (conflicts) throw new ApplyConflictError(conflicts);
      if (reply.status !== 200) throw failure(reply);
      const body = parseJSON(reply.text) as Partial<Schemas["ApplyResult"]> | undefined;
      if (
        !isRecord(body) ||
        typeof body.commitSha !== "string" ||
        !Array.isArray(body.files) ||
        !Array.isArray(body.warnings)
      ) {
        throw malformed("apply", reply);
      }
      return {
        commitSha: body.commitSha,
        files: body.files.map((f) => ({ path: f.path, sha: f.sha })),
        warnings: body.warnings.map((w) => ({ path: w.path, message: w.message })),
      };
    },
  };
}
