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
//                         unreachable or stalled socket): retry later; a
//                         refused room load is tagged `upstream-unavailable`,
//                         a failed flush keeps the doc live for the next one.

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

/** A non-fatal note about one file of a commit (e.g. a scaffolded file). */
export interface ApplyWarning {
  path: string;
  message: string;
}

export interface ApplyOutcome {
  /** New per-file shas on success (full repo paths). */
  files: { path: string; sha: string }[];
  commitSha: string;
  warnings: ApplyWarning[];
}

export interface ProjectRepository {
  owner: string;
  repo: string;
  headSha: string;
}

/** The Files port: Room seeding, the project lookup and the committer. */
export interface FilesClient {
  /** The project's repository and current head; throws FilesDeniedError when unknown. */
  lookup(project: string): Promise<ProjectRepository>;
  /** Every spec file (under specs/) at one commit. */
  bundle(project: string): Promise<SpecFile[]>;
  /** Land a batch as ONE commit; throws ApplyConflictError on stale baseShas. */
  apply(project: string, batch: ApplyBatch): Promise<ApplyOutcome>;
}

export interface ApplyConflict {
  path: string;
  baseSha: string;
  currentSha: string;
}

/** A stale-baseSha rejection: nothing was applied (#133 doc-wins retry). */
export class ApplyConflictError extends Error {
  /** The conflicted paths, in the pod's order. */
  readonly paths: string[];

  /** One entry per stale write or delete: the sha sent and the sha git holds ("" = absent). */
  constructor(readonly conflicts: ApplyConflict[]) {
    const paths = conflicts.map((c) => c.path);
    super(`apply conflict on: ${paths.join(", ")}`);
    this.name = "ApplyConflictError";
    this.paths = paths;
  }
}

/**
 * A verdict: any 4xx except 408/425/429 (and except the 409s above). The
 * same call will get the same answer, so it is never tagged transient.
 * `code` is the problem code, or `http_<status>` when the body had none.
 * `path` is the one path an apply's write rules refused, when the pod named
 * one (`path_invalid`): the rest of the batch may still be saved.
 */
export class FilesDeniedError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    detail = "",
    readonly path?: string,
  ) {
    super(`files socket denied (${status} ${code})${detail ? `: ${detail}` : ""}`);
    this.name = "FilesDeniedError";
  }
}

/**
 * An outage: a 5xx, a 408/425/429, a `not_fast_forward` 409 (the branch moved
 * during the save; the pod says re-read and retry), a malformed reply, a
 * socket that cannot be reached (`code` `socket_unreachable`, `status` 0) or
 * one that does not answer in time (`timeout`, `status` 0).
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
/**
 * The longest one request may take, reply body included. A stalled sidecar
 * must not hold a flush (or a room load) open: the request fails as an
 * outage and the next debounce retries. It is longer than the pod's own
 * per-request budget (`filesSocketRequestBudget`, 40 s,
 * ae-studio-tools/internal/edge/files_sock.go), which nests every aep-api
 * call and the git work, so the pod answers first and this deadline only
 * ever fires on a sidecar that stopped answering; raise both together, and
 * keep the console's flush wait (`FLUSH_TIMEOUT_MS`, 50 s, in
 * apps/console/src/features/spec/collab/useCollabSpec.ts) above this one. A
 * cold clone runs detached in the pod: a join that times out waiting on one
 * leaves it running, and the client's retry reuses it. The shutdown flush has
 * its own, shorter budget (`SHUTDOWN_FLUSH_BUDGET_MS` in committer.ts); the
 * process exits when that ends, whatever is still in flight.
 */
const REQUEST_TIMEOUT_MS = 45_000;

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

/**
 * The problem `code`, `detail` and refused `path`, or `http_<status>` for a
 * non-problem body.
 */
function problemOf(reply: Reply): { code: string; detail: string; path?: string } {
  const body = parseJSON(reply.text);
  if (isRecord(body) && typeof body.code === "string" && body.code) {
    const detail = typeof body.detail === "string" ? body.detail : "";
    const path = typeof body.path === "string" && body.path ? body.path : undefined;
    return { code: body.code, detail: detail.slice(0, MAX_DETAIL), ...(path ? { path } : {}) };
  }
  return { code: `http_${reply.status}`, detail: "" };
}

/** Maps a non-2xx reply to its class (ApplyConflictError is checked by apply). */
function failure(reply: Reply): FilesDeniedError | FilesUnavailableError {
  const { code, detail, path } = problemOf(reply);
  const transient =
    reply.status >= 500 ||
    RETRYABLE_4XX.has(reply.status) ||
    (reply.status === 409 && code === "not_fast_forward");
  return transient
    ? new FilesUnavailableError(code, reply.status, detail)
    : new FilesDeniedError(code, reply.status, detail, path);
}

/** A 2xx whose body is not what the contract promises: the pod is broken, not denying. */
function malformed(op: string, reply: Reply): FilesUnavailableError {
  return new FilesUnavailableError(
    "invalid_response",
    reply.status,
    `${op} returned an unexpected body`,
  );
}

function asConflicts(reply: Reply): ApplyConflict[] | null {
  if (reply.status !== 409 || !reply.contentType.startsWith("application/json")) {
    return null;
  }
  const body = parseJSON(reply.text) as Partial<Schemas["ApplyConflicts"]> | undefined;
  if (!isRecord(body) || body.code !== "conflict" || !Array.isArray(body.conflicts)) {
    return null;
  }
  return body.conflicts.map((c) => ({ path: c.path, baseSha: c.baseSha, currentSha: c.currentSha }));
}

export interface FilesClientOptions {
  /** Per-request deadline; `REQUEST_TIMEOUT_MS` unless a test shortens it. */
  requestTimeoutMs?: number;
}

/**
 * The FilesClient over the Files socket at `socketPath`. One keep-alive
 * dispatcher per client; a request never leaves the socket.
 */
export function createFilesClient(socketPath: string, options: FilesClientOptions = {}): FilesClient {
  const dispatcher = new Agent({ connect: { socketPath } });
  const timeoutMs = options.requestTimeoutMs ?? REQUEST_TIMEOUT_MS;

  async function send(
    method: "GET" | "POST",
    path: string,
    body?: unknown,
  ): Promise<Reply> {
    // One deadline for the whole exchange: connect, headers and body.
    const signal = AbortSignal.timeout(timeoutMs);
    try {
      const res = await fetch(
        `${ORIGIN}${path}`,
        body === undefined
          ? { method, dispatcher, signal }
          : {
              method,
              dispatcher,
              signal,
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
      if (signal.aborted) {
        throw new FilesUnavailableError("timeout", 0, "the Files socket did not answer in time", { cause: err });
      }
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
