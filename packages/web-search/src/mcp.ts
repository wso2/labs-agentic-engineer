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
 * Web search as a Model Context Protocol server: the one `web_search` tool,
 * over MCP's stdio transport (newline-delimited JSON-RPC 2.0). A coding run's
 * runtime spawns it as `aep-web` when its connection's search strategy is one
 * the runtime cannot run itself (`runners/remote-worker/src/lib/aep_web.ts`).
 *
 * Hand-written: the package is dependency-free (README).
 *
 * Every query is judged by `deny` BEFORE it leaves the process; a refused query
 * reaches no network, and the refusal is the tool's error text, which is what
 * the model reads.
 */

import type { Readable, Writable } from "node:stream";
import { WebSearchError, type WebSearchResult } from "./ollama.js";

/** The tool's name, as the server lists it. A runtime namespaces it by the server's key. */
export const WEB_SEARCH_TOOL = "web_search";

/** Protocol versions this server speaks; the client's is echoed when listed, else the newest. */
const PROTOCOL_VERSIONS = ["2025-06-18", "2025-03-26", "2024-11-05"] as const;

export interface WebSearchServerOptions {
  /** The server's name, as `initialize` reports it. */
  name: string;
  version: string;
  /** Runs one query. Its `WebSearchError` message is shown to the model as is. */
  search(query: string): Promise<WebSearchResult[]>;
  /** The reason to refuse a query before it is sent, or null to send it. */
  deny(query: string): string | null;
}

interface JsonRpcRequest {
  jsonrpc: "2.0";
  id?: string | number | null;
  method: string;
  params?: Record<string, unknown>;
}

type JsonRpcResponse =
  | { jsonrpc: "2.0"; id: string | number | null; result: unknown }
  | { jsonrpc: "2.0"; id: string | number | null; error: { code: number; message: string } };

/** One JSON-RPC message in, the response to write (undefined for a notification). */
export type McpHandler = (message: unknown) => Promise<JsonRpcResponse | undefined>;

/**
 * The tool's text result: every result's title, URL and (capped) content, in
 * the order the search answered, or a sentence saying there were none.
 */
export function formatResults(query: string, results: readonly WebSearchResult[]): string {
  if (results.length === 0) return `No web results for "${query}".`;
  return results.map((r, i) => `[${i + 1}] ${r.title || r.url}\n${r.url}\n\n${r.content}`).join("\n\n---\n\n");
}

/** Build the server's message handler. Pure over `search` and `deny`, so every method is a test. */
export function createWebSearchServer(opts: WebSearchServerOptions): McpHandler {
  const tool = {
    name: WEB_SEARCH_TOOL,
    description:
      "Search the web. Returns up to five results, each with its title, URL and page text. " +
      "Name the library, API or technology you need; never include a credential or a configuration value.",
    inputSchema: {
      type: "object",
      properties: { query: { type: "string", description: "What to search for." } },
      required: ["query"],
      additionalProperties: false,
    },
  };

  async function callTool(params: Record<string, unknown> | undefined): Promise<unknown> {
    const args = (params?.arguments ?? {}) as Record<string, unknown>;
    if (params?.name !== WEB_SEARCH_TOOL) {
      return toolError(`unknown tool ${JSON.stringify(params?.name)}`);
    }
    const query = typeof args.query === "string" ? args.query : "";
    const refusal = opts.deny(query);
    if (refusal !== null) return toolError(refusal);
    try {
      return { content: [{ type: "text", text: formatResults(query.trim(), await opts.search(query)) }] };
    } catch (err) {
      // A WebSearchError's message is written for the model; anything else is
      // not, and may carry more than it should, so it is named only by class.
      return toolError(err instanceof WebSearchError ? err.message : "web search failed unexpectedly");
    }
  }

  return async (message) => {
    const req = message as Partial<JsonRpcRequest> | null;
    if (!req || typeof req !== "object" || typeof req.method !== "string") {
      return { jsonrpc: "2.0", id: null, error: { code: -32600, message: "invalid request" } };
    }
    const id = req.id;
    // A notification (no id) is never answered, whatever its method.
    if (id === undefined) return undefined;
    switch (req.method) {
      case "initialize": {
        const asked = req.params?.protocolVersion;
        const protocolVersion = PROTOCOL_VERSIONS.find((v) => v === asked) ?? PROTOCOL_VERSIONS[0];
        return ok(id, { protocolVersion, capabilities: { tools: {} }, serverInfo: { name: opts.name, version: opts.version } });
      }
      case "ping":
        return ok(id, {});
      case "tools/list":
        return ok(id, { tools: [tool] });
      case "tools/call":
        return ok(id, await callTool(req.params));
      default:
        return { jsonrpc: "2.0", id, error: { code: -32601, message: `method not found: ${req.method}` } };
    }
  };
}

function ok(id: string | number | null, result: unknown): JsonRpcResponse {
  return { jsonrpc: "2.0", id, result };
}

function toolError(text: string): unknown {
  return { content: [{ type: "text", text }], isError: true };
}

/**
 * Serve a handler over MCP's stdio transport: one JSON-RPC message per line in,
 * one per line out. Resolves when the input ends. Nothing else is ever written
 * to `output`: it IS the protocol channel.
 */
export async function serveStdio(handle: McpHandler, input: Readable, output: Writable): Promise<void> {
  let buffered = "";
  const pending: Promise<void>[] = [];
  const answer = async (line: string): Promise<void> => {
    let message: unknown;
    try {
      message = JSON.parse(line);
    } catch {
      output.write(JSON.stringify({ jsonrpc: "2.0", id: null, error: { code: -32700, message: "parse error" } }) + "\n");
      return;
    }
    const response = await handle(message);
    if (response) output.write(JSON.stringify(response) + "\n");
  };
  input.setEncoding("utf8");
  for await (const chunk of input) {
    buffered += chunk as string;
    let newline: number;
    while ((newline = buffered.indexOf("\n")) >= 0) {
      const line = buffered.slice(0, newline).trim();
      buffered = buffered.slice(newline + 1);
      if (line !== "") pending.push(answer(line));
    }
  }
  if (buffered.trim() !== "") pending.push(answer(buffered.trim()));
  await Promise.all(pending);
}
