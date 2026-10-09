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
 * Minimal Model Context Protocol client for the design tools ae-studio-tools
 * serves on the MCP socket (`POST /mcp`, packages/contracts/sockets/ae-studio/
 * mcp/openapi.yaml). It speaks JSON-RPC in its simplest single-response form
 * (POST a request, read an `application/json` response) over the socket's
 * fetch (`ToolsSocket.mcpFetch`): no URL, no bearer, as the socket mount is
 * the gate. That server is stateless and answers `tools/list` without an
 * `initialize`, so this client skips it.
 *
 * `loadMcpTools` DISCOVERS the server's tools via `tools/list` and wraps each as
 * an AI SDK `dynamicTool` whose execution proxies to `tools/call`. The turn loop
 * (`run-conversation-turn.ts`) merges these into the tool set so the main agent
 * can look up the org's already-registered external resources / org endpoints /
 * platform resource types before proposing a `dependencies` entry — reusing an
 * existing name + schema instead of inventing one.
 *
 * Best-effort throughout: the socket being unreachable, answering an HTTP
 * error, or returning a malformed response all degrade to an EMPTY tool set
 * (logged), never a thrown error. Discovery is enrichment, never a hard
 * dependency of the turn.
 */

import { dynamicTool, jsonSchema, type ToolSet } from "ai";
import type { ToolsSocket } from "../tools-socket/client.js";

/** What the MCP client needs of the tools socket: its fetch. */
export type McpTransport = Pick<ToolsSocket, "mcpFetch">;

/** The JSON-RPC endpoint on the socket. */
const MCP_PATH = "/mcp";

interface JsonRpcResponse {
  jsonrpc: string;
  id: number;
  result?: unknown;
  error?: { code: number; message: string };
}

interface McpToolDescriptor {
  name: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
}

interface McpToolContent {
  type: string;
  text?: string;
}

interface McpToolCallResult {
  content?: McpToolContent[];
  isError?: boolean;
}

let nextRpcId = 1;

/**
 * Wall-clock ceiling on one JSON-RPC round trip.
 *
 * "Best-effort" covered every failure the server could REPORT but not the one
 * where it says nothing: `fetch` has no default timeout, so an unresponsive
 * server left both calls hanging indefinitely. The two hangs read differently
 * to a user and neither is attributable without a trace — `tools/list` runs
 * BEFORE the first model call, so it stalls time-to-first-token with the stream
 * already open and nothing on it; `tools/call` stalls mid-turn between steps.
 *
 * 10s is well past a healthy discovery response and well short of the kind of
 * wait a user reads as a hang.
 */
const RPC_TIMEOUT_MS = 10_000;

/** POST one JSON-RPC request; throws on a non-2xx response or an `error` envelope. */
async function rpc(
  mcpFetch: typeof fetch,
  method: string,
  params: unknown,
  timeoutMs: number,
): Promise<unknown> {
  const res = await mcpFetch(MCP_PATH, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: JSON.stringify({ jsonrpc: "2.0", id: nextRpcId++, method, params }),
    // A timeout aborts the request, which surfaces as a thrown TimeoutError —
    // the same degradation path as any other transport failure.
    signal: AbortSignal.timeout(timeoutMs),
  });
  if (!res.ok) {
    throw new Error(`mcp ${method}: HTTP ${res.status}`);
  }
  const body = (await res.json()) as JsonRpcResponse;
  if (body.error) {
    throw new Error(`mcp ${method}: ${body.error.message}`);
  }
  return body.result;
}

/**
 * Discover the tools on the socket and return them as an AI SDK `ToolSet`. On
 * any failure (unreachable, an HTTP error, malformed JSON/shape,
 * unresponsive past `timeoutMs`) it logs a warning and returns `{}` — the caller
 * merges an empty set as a no-op.
 *
 * `options.timeoutMs` overrides `RPC_TIMEOUT_MS`. Callers leave
 * it unset; the tests set it small so the no-response path is assertable without
 * a ten-second wait.
 */
export async function loadMcpTools(
  socket: McpTransport,
  options: { timeoutMs?: number } = {},
): Promise<ToolSet> {
  const timeoutMs = options.timeoutMs ?? RPC_TIMEOUT_MS;
  try {
    const listed = (await rpc(socket.mcpFetch, "tools/list", {}, timeoutMs)) as
      | { tools?: McpToolDescriptor[] }
      | undefined;
    const descriptors = Array.isArray(listed?.tools) ? listed.tools : [];
    const tools: ToolSet = {};
    for (const d of descriptors) {
      if (!d || typeof d.name !== "string" || d.name === "") continue; // malformed descriptor — skip, don't fail the batch
      tools[d.name] = dynamicTool({
        description: typeof d.description === "string" ? d.description : "",
        inputSchema: jsonSchema(d.inputSchema ?? { type: "object", properties: {} }),
        execute: async (args) => {
          const result = (await rpc(
            socket.mcpFetch,
            "tools/call",
            { name: d.name, arguments: args ?? {} },
            timeoutMs,
          )) as McpToolCallResult | undefined;
          // MCP tool result: { content: [{ type: 'text', text }], isError? }.
          const text = (result?.content ?? [])
            .filter((c) => c?.type === "text" && typeof c.text === "string")
            .map((c) => c.text)
            .join("\n");
          // isError:true is the MCP server flagging the CALL itself as a failure
          // (bad args, downstream error, etc.) — distinct from a normal result
          // that merely reports "not found". Throwing here (rather than
          // returning the text as an ordinary success) mirrors the AI SDK's
          // tool-error convention: `executeToolCall` catches a thrown error and
          // emits a `tool-error` stream part, so the model sees a flagged
          // failure instead of misreading it as a successful lookup.
          if (result?.isError) {
            throw new Error(text || `mcp tool ${d.name} reported an error`);
          }
          return text || JSON.stringify(result ?? {});
        },
      });
    }
    console.log(`[mcp] loaded ${Object.keys(tools).length} tool(s) from the tools socket`);
    return tools;
  } catch (err) {
    console.warn(`[mcp] tool discovery failed: ${err instanceof Error ? err.message : String(err)}`);
    return {};
  }
}
