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

// The MCP socket port (07 §9): the agent's only door out of the AE Studio pod.
// ae-studio-tools serves it on the Unix socket in AE_MCP_SOCKET
// (packages/contracts/sockets/ae-studio/mcp/openapi.yaml). The mount is the
// gate, so no call carries a token. Two adapters implement `ToolsSocket`:
// `createToolsSocket` here (undici over the socket) and `FakeToolsSocket`
// (fake.ts, in process, for tests and the playground).

import { Agent, fetch as undiciFetch, type RequestInfo as UndiciRequestInfo, type RequestInit as UndiciRequestInit } from "undici";
import type { components } from "../generated/mcp-socket.js";

type Schemas = components["schemas"];

/** One finished turn's usage record, as the ledger stores it. */
export type TurnRecord = Schemas["TurnRecord"];

/** A known project's snapshots: the repo and skills shas, and its reference document names (sorted). */
export type ProjectSnapshot = Omit<Schemas["ProjectSnapshot"], "known">;

/** The Org skills snapshot's sha. */
export type SkillsSnapshot = Schemas["SkillsSnapshot"];

export interface ToolsSocket {
  /**
   * A fetch bound to the socket: only the URL's path and query reach
   * ae-studio-tools (the MCP client posts to `/mcp`).
   */
  mcpFetch: typeof fetch;
  /** Hand over one finished turn's record; resolves once it is accepted. */
  postUsage(r: TurnRecord): Promise<void>;
  /**
   * Resolve `project` and write its snapshots at `at` (the head when
   * omitted). `null` means the project is not this org's.
   */
  lookup(project: string, at?: string): Promise<ProjectSnapshot | null>;
  /** Write the Org skills snapshot at its tip. */
  skills(): Promise<SkillsSnapshot>;
}

/** 4xx statuses that mean "not now" rather than "no". */
const RETRYABLE_4XX = new Set([408, 425, 429]);
/** Problem detail kept in a message; problem details are short, bodies may not be. */
const MAX_DETAIL = 500;

/**
 * A failed socket call. `code` is the problem code, `http_<status>` when the
 * body had none, `socket_unreachable` or `timeout` (status 0), or
 * `invalid_response` for a 2xx without the contract's shape. `permanent`
 * means the same call will get the same answer: a 4xx other than
 * 408/425/429.
 */
export class ToolsSocketError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    detail = "",
    options?: ErrorOptions,
  ) {
    super(`tools socket (${status} ${code})${detail ? `: ${detail}` : ""}`, options);
    this.name = "ToolsSocketError";
  }

  get permanent(): boolean {
    return this.status >= 400 && this.status < 500 && !RETRYABLE_4XX.has(this.status);
  }
}

/**
 * The longest one request may take, reply body included. Longer than the
 * pod's own per-request budget on this socket (`mcpSocketRequestBudget`, 20 s,
 * ae-studio-tools/internal/edge/mcp_sock.go), so the pod answers first and
 * this deadline only fires on a sidecar that stopped answering. A caller's
 * own `signal` (the MCP client's 10 s) still applies to `mcpFetch`.
 */
const REQUEST_TIMEOUT_MS = 25_000;

// The host is a placeholder: the dispatcher connects to the socket, so only
// the path reaches the server.
const ORIGIN = "http://mcp.sock";

interface Reply {
  status: number;
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

function failure(reply: Reply): ToolsSocketError {
  const body = parseJSON(reply.text);
  if (isRecord(body) && typeof body.code === "string" && body.code) {
    const detail = typeof body.detail === "string" ? body.detail.slice(0, MAX_DETAIL) : "";
    return new ToolsSocketError(body.code, reply.status, detail);
  }
  return new ToolsSocketError(`http_${reply.status}`, reply.status);
}

function malformed(op: string, reply: Reply): ToolsSocketError {
  return new ToolsSocketError("invalid_response", reply.status, `${op} returned an unexpected body`);
}

const isString = (v: unknown): v is string => typeof v === "string";

export interface ToolsSocketOptions {
  /** Per-request deadline; `REQUEST_TIMEOUT_MS` unless a test shortens it. */
  requestTimeoutMs?: number;
}

/**
 * The ToolsSocket over the Unix socket at `socketPath`. One keep-alive
 * dispatcher per client; a request never leaves the socket.
 */
export function createToolsSocket(socketPath: string, options: ToolsSocketOptions = {}): ToolsSocket {
  const dispatcher = new Agent({ connect: { socketPath } });
  const timeoutMs = options.requestTimeoutMs ?? REQUEST_TIMEOUT_MS;

  // undici's own fetch with its own dispatcher: Node's global fetch bundles a
  // different undici major, and a dispatcher is not portable across the two.
  // The casts cross that boundary (as shared/guarded-fetch.ts does).
  const mcpFetch = (async (input: Parameters<typeof fetch>[0], init?: Parameters<typeof fetch>[1]) => {
    const raw = input instanceof Request ? input.url : String(input);
    const url = new URL(raw, ORIGIN);
    const deadline = AbortSignal.timeout(timeoutMs);
    const signal = init?.signal ? AbortSignal.any([init.signal, deadline]) : deadline;
    return undiciFetch(`${ORIGIN}${url.pathname}${url.search}` as UndiciRequestInfo, {
      ...(init as unknown as UndiciRequestInit),
      dispatcher,
      signal,
    });
  }) as unknown as typeof fetch;

  async function send(method: "GET" | "POST", path: string, body?: unknown): Promise<Reply> {
    // One deadline for the whole exchange: connect, headers and body.
    const signal = AbortSignal.timeout(timeoutMs);
    try {
      const res = await undiciFetch(`${ORIGIN}${path}`, {
        method,
        dispatcher,
        signal,
        ...(body === undefined
          ? {}
          : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
      });
      return { status: res.status, text: await res.text() };
    } catch (err) {
      if (signal.aborted) {
        throw new ToolsSocketError("timeout", 0, "the tools socket did not answer in time", { cause: err });
      }
      throw new ToolsSocketError("socket_unreachable", 0, "the tools socket could not be reached", { cause: err });
    }
  }

  return {
    mcpFetch,

    async postUsage(record) {
      const reply = await send("POST", "/turn-usage", record);
      if (reply.status < 200 || reply.status >= 300) throw failure(reply);
    },

    async lookup(project, at) {
      const query = at === undefined ? "" : `?at=${encodeURIComponent(at)}`;
      const reply = await send("GET", `/projects/${encodeURIComponent(project)}${query}`);
      if (reply.status === 404) {
        // ref_not_found is a 404 too, but it says `at` is wrong, not the project.
        const err = failure(reply);
        if (err.code === "ref_not_found") throw err;
        return null;
      }
      if (reply.status !== 200) throw failure(reply);
      const body = parseJSON(reply.text);
      if (
        !isRecord(body) ||
        !isString(body.headSha) ||
        !isString(body.skillsSha) ||
        !Array.isArray(body.references) ||
        !body.references.every(isString) ||
        (body.idea !== undefined && !isString(body.idea))
      ) {
        throw malformed("lookup", reply);
      }
      return {
        headSha: body.headSha,
        skillsSha: body.skillsSha,
        references: body.references,
        ...(body.idea !== undefined ? { idea: body.idea } : {}),
      };
    },

    async skills() {
      const reply = await send("GET", "/skills");
      if (reply.status !== 200) throw failure(reply);
      const body = parseJSON(reply.text);
      if (!isRecord(body) || !isString(body.skillsSha)) throw malformed("skills", reply);
      return { skillsSha: body.skillsSha };
    },
  };
}
