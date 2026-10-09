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
 * The turn's `web_search` tool (external-dependency-discovery #252), chosen by
 * the connection's `capabilities.webSearch` strategy — never by asking which
 * provider SDK the model came from, since an Anthropic-format model on another
 * host reports the same provider and cannot run Anthropic's server tool.
 *
 * - `anthropic-server-tool`: Anthropic's provider-executed tool (model seam).
 * - `ollama-api`: a platform-executed tool over Ollama's search API
 *   (`@aep/web-search`), on the connection's own host with its own key.
 * - `none`: no tool.
 *
 * Both tools are named `web_search` and share one per-turn budget, so the
 * model's prompt reads the same on every connection that searches.
 */

import { tool, type ToolSet } from "ai";
import { z } from "zod";
import { searchOllama, WebSearchError } from "@aep/web-search";
import { anthropicWebSearchTool, type ModelConnection } from "../../../shared/model.js";

export const WEB_SEARCH = "web_search" as const;

/** Searches one turn may run, on every strategy. */
export const MAX_SEARCHES_PER_TURN = 4;

const webSearchInputSchema = z.object({
  query: z.string().min(1).describe("What to search the web for."),
});

/**
 * The `web_search` tool for `conn`, or none. `fetch` is what the Ollama
 * strategy sends through: the model's own host-guarded fetch in production.
 */
export function buildWebSearchTools(
  conn: Pick<ModelConnection, "baseURL" | "apiKey" | "capabilities">,
  fetch: typeof globalThis.fetch,
): ToolSet {
  switch (conn.capabilities.webSearch) {
    case "anthropic-server-tool":
      return { [WEB_SEARCH]: anthropicWebSearchTool(MAX_SEARCHES_PER_TURN) };
    case "ollama-api":
      return { [WEB_SEARCH]: ollamaWebSearchTool(conn, fetch) };
    case "none":
      return {};
  }
}

function ollamaWebSearchTool(conn: Pick<ModelConnection, "baseURL" | "apiKey">, fetch: typeof globalThis.fetch) {
  let used = 0;
  return tool({
    description:
      "Search the web and get the text of the top results, each with its URL. " +
      "Use it to confirm that an external API, SDK or service exists and how it is called before relying on it, " +
      "and cite the URL of the result you relied on.",
    inputSchema: webSearchInputSchema,
    execute: async ({ query }, { abortSignal }) => {
      if (used >= MAX_SEARCHES_PER_TURN) {
        throw new WebSearchError(`web search budget spent: ${MAX_SEARCHES_PER_TURN} searches per turn`);
      }
      used += 1;
      const results = await searchOllama(query, {
        baseURL: conn.baseURL,
        apiKey: conn.apiKey,
        fetch,
        ...(abortSignal ? { signal: abortSignal } : {}),
      });
      return { results };
    },
  });
}
