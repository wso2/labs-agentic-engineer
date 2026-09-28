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
 * The `ollama-api` web search strategy: Ollama's own search endpoint, called on
 * the connection's host with the connection's key. The key never follows
 * another host: the endpoint is derived from the connection's base URL, and a
 * redirect is refused rather than followed.
 *
 * The endpoint answers whole pages (one measured result held 169,745
 * characters), so every result is capped before it reaches a model's context,
 * and so is the count.
 */

/** Results a search returns at most. */
export const MAX_RESULTS = 5;

/** Characters of each result's content a search keeps. */
export const MAX_CONTENT_CHARS = 4000;

/** Characters of a query a search sends at most. */
export const MAX_QUERY_CHARS = 400;

/** One search result, as a model reads it. */
export interface WebSearchResult {
  title: string;
  url: string;
  /** The page's text, cut at `MAX_CONTENT_CHARS`. */
  content: string;
}

export interface OllamaSearchOptions {
  /**
   * The connection's base URL (`https://ollama.com/v1`, either format's). Only
   * its origin is used: the search API lives at `/api/web_search` on the same
   * host.
   */
  baseURL: string;
  /** The connection key, sent as `Authorization: Bearer`. */
  apiKey: string;
  /** The fetch to send through (the agents service passes its host-guarded one). */
  fetch?: typeof globalThis.fetch;
  signal?: AbortSignal;
}

/**
 * A search that failed. Its message is written for the model that called the
 * tool: it names the host and the status, and never echoes the key or the
 * response body.
 */
export class WebSearchError extends Error {
  constructor(
    message: string,
    readonly status?: number,
  ) {
    super(message);
    this.name = "WebSearchError";
  }
}

/** The search endpoint on the connection's own host. */
export function ollamaSearchURL(baseURL: string): URL {
  return new URL("/api/web_search", baseURL);
}

/** Search the web through Ollama's API. */
export async function searchOllama(query: string, opts: OllamaSearchOptions): Promise<WebSearchResult[]> {
  const q = query.trim();
  if (q === "") throw new WebSearchError("web search needs a non-empty query");
  const url = ollamaSearchURL(opts.baseURL);
  const doFetch = opts.fetch ?? globalThis.fetch;
  let res: Response;
  try {
    res = await doFetch(url.toString(), {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${opts.apiKey}` },
      body: JSON.stringify({ query: q.slice(0, MAX_QUERY_CHARS), max_results: MAX_RESULTS }),
      redirect: "error",
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
  } catch (err) {
    if (opts.signal?.aborted) throw err;
    throw new WebSearchError(`web search could not reach ${url.host}`);
  }
  if (!res.ok) {
    // Drain so the connection is reusable; the body is never echoed.
    await res.body?.cancel();
    throw new WebSearchError(`web search failed: ${url.host} answered ${res.status}`, res.status);
  }
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    throw new WebSearchError(`web search failed: ${url.host} answered something other than JSON`);
  }
  return capResults(body, url.host);
}

/** The response's results, validated and capped. */
function capResults(body: unknown, host: string): WebSearchResult[] {
  const results = (body as { results?: unknown } | null)?.results;
  if (!Array.isArray(results)) {
    throw new WebSearchError(`web search failed: ${host} answered without a results list`);
  }
  const out: WebSearchResult[] = [];
  for (const r of results) {
    if (out.length === MAX_RESULTS) break;
    const { title, url, content } = (r ?? {}) as Record<string, unknown>;
    if (typeof url !== "string" || url === "") continue;
    out.push({
      title: typeof title === "string" ? title : "",
      url,
      content: typeof content === "string" ? capContent(content) : "",
    });
  }
  return out;
}

function capContent(content: string): string {
  return content.length > MAX_CONTENT_CHARS ? `${content.slice(0, MAX_CONTENT_CHARS)}…` : content;
}
