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

import createClient, { type Client } from "openapi-fetch";
import type { paths } from "../generated/ae-studio-tools";
import type { paths as DesignAgentPaths } from "../generated/ae-design-agent";
import { getAccessToken, redirectToSignIn, renewAccessToken } from "../auth/token";
import { createAuthFetch } from "./authFetch";
import { ApiRequestError } from "./errors";

// The org's AE Studio pods have no fixed address: GET /ae-studio names their
// URLs once AE Studio is `ready`. useAeStudio's query hands each answer to
// setAeStudioUrls before any consumer sees it, so the accessor below is
// current whenever a component reads `ready`, and code outside React (which
// cannot see the query) gets the same answer.

/** Thrown by studioTools() and designAgent() while AE Studio has no ready URLs. */
export class AeStudioNotReadyError extends Error {
  constructor() {
    super("AE Studio is not ready");
    this.name = "AeStudioNotReadyError";
  }
}

let tools: Client<paths> | null = null;
let currentToolsUrl = "";
let design: Client<DesignAgentPaths> | null = null;
let currentDesignUrl = "";

// Same session as aep-api: the pods take the user's own token.
function podClient<P extends object>(origin: string): Client<P> {
  return createClient<P>({
    baseUrl: `${origin}/v1`,
    fetch: createAuthFetch({
      getToken: getAccessToken,
      renewToken: renewAccessToken,
      redirectToSignIn,
    }),
  });
}

/**
 * Point the pod clients at AE Studio's URLs, or drop them (`null`) when the
 * state is anything but `ready`. Same URL, same client.
 */
export function setAeStudioUrls(urls: { tools: string; designAgent: string } | null): void {
  if (!urls) {
    tools = null;
    currentToolsUrl = "";
    design = null;
    currentDesignUrl = "";
    return;
  }
  if (urls.tools !== currentToolsUrl || !tools) {
    currentToolsUrl = urls.tools;
    tools = podClient<paths>(urls.tools);
  }
  if (urls.designAgent !== currentDesignUrl || !design) {
    currentDesignUrl = urls.designAgent;
    design = podClient<DesignAgentPaths>(urls.designAgent);
  }
}

/** The ae-studio-tools `/v1` client. Throws AeStudioNotReadyError before `ready`. */
export function studioTools(): Client<paths> {
  if (!tools) throw new AeStudioNotReadyError();
  return tools;
}

/** The ae-design-agent `/v1` client. Throws AeStudioNotReadyError before `ready`. */
export function designAgent(): Client<DesignAgentPaths> {
  if (!design) throw new AeStudioNotReadyError();
  return design;
}

/**
 * A failed ae-studio-tools call: the problem's message and code (via
 * ApiRequestError), plus the HTTP status (undefined for a network failure)
 * and the server's Retry-After.
 */
export class StudioToolsError extends ApiRequestError {
  readonly status: number | undefined;
  readonly retryAfterMs: number | undefined;

  constructor(
    error: unknown,
    fallback: string,
    { status, retryAfterMs }: { status?: number | undefined; retryAfterMs?: number | undefined },
  ) {
    super(error, fallback);
    this.name = "StudioToolsError";
    this.status = status;
    this.retryAfterMs = retryAfterMs;
  }
}

/**
 * The pod is not serving: a 503 (disk_full, aep_api_unavailable,
 * idp_unavailable) or no answer at all. That says more about AE Studio than
 * about the file, so it is the cue to re-read AE Studio's state.
 */
export function isStudioToolsUnavailable(error: unknown): boolean {
  return error instanceof StudioToolsError && (error.status === undefined || error.status === 503);
}

function retryAfterMs(response: Response): number | undefined {
  const seconds = Number(response.headers.get("Retry-After"));
  return Number.isFinite(seconds) && seconds > 0 ? seconds * 1000 : undefined;
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

/**
 * Run one ae-studio-tools call and return its data, or throw: a
 * StudioToolsError for any failed answer or network failure,
 * AeStudioNotReadyError before `ready`. Aborts pass through untouched.
 */
export async function studioToolsRead<T>(
  fallback: string,
  call: (client: Client<paths>) => Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const client = studioTools();
  let result: { data?: T; error?: unknown; response: Response };
  try {
    result = await call(client);
  } catch (error) {
    if (isAbort(error)) throw error;
    throw new StudioToolsError(undefined, fallback, {});
  }
  const { data, error, response } = result;
  if (error !== undefined || data === undefined) {
    throw new StudioToolsError(error, fallback, {
      status: response.status,
      retryAfterMs: retryAfterMs(response),
    });
  }
  return data;
}

const UNAVAILABLE_RETRY_MS = 5_000;

/**
 * Query `retryDelay` for pod reads: a 503 waits what Retry-After says (5 s
 * without it); anything else backs off like react-query's default.
 */
export function studioToolsRetryDelay(failureCount: number, error: unknown): number {
  if (error instanceof StudioToolsError && error.status === 503) {
    return error.retryAfterMs ?? UNAVAILABLE_RETRY_MS;
  }
  return Math.min(1000 * 2 ** failureCount, 30_000);
}
