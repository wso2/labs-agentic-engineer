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
import type { paths } from "../generated/ae-design-agent";
import { getAccessToken, redirectToSignIn, renewAccessToken } from "../auth/token";
import { createAuthFetch } from "./authFetch";
import { ApiRequestError } from "./errors";

// The org's AE Studio pod has no fixed address: GET /ae-studio names its URLs
// once AE Studio is `ready`. useAeStudio's query hands each answer to
// setAeStudioUrls before any consumer sees it, so the accessor below is
// current whenever a component reads `ready`, and code outside React (the
// chat store, which cannot see the query) gets the same answer.
//
// The browser calls one pod API: the design agent's `/v1` (chat turns and the
// conversation). The Room is a WebSocket (spec/collab/specRoom.ts); spec files
// are read from the Room, never from the pod's tools `/v1`.

/** The sentence for an AE Studio that cannot take a request right now: it is restarting, as far as the user can act on it. */
export const AE_STUDIO_RESTARTING = "AE Studio is restarting. Try again in a moment.";

/** Thrown by designAgent() while AE Studio has no ready URLs. */
export class AeStudioNotReadyError extends Error {
  constructor() {
    super(AE_STUDIO_RESTARTING);
    this.name = "AeStudioNotReadyError";
  }
}

let design: Client<paths> | null = null;
let currentDesignUrl = "";
const readinessListeners = new Set<() => void>();

/**
 * Point the design-agent client at AE Studio's URL, or drop it (`null`) when
 * the state is anything but `ready`. Same URL, same client.
 */
export function setAeStudioUrls(urls: { designAgent: string } | null): void {
  const wasReady = design !== null;
  if (!urls) {
    design = null;
    currentDesignUrl = "";
  } else if (urls.designAgent !== currentDesignUrl || !design) {
    currentDesignUrl = urls.designAgent;
    // Same session as aep-api: the pod takes the user's own token.
    design = createClient<paths>({
      baseUrl: `${urls.designAgent}/v1`,
      fetch: createAuthFetch({
        getToken: getAccessToken,
        renewToken: renewAccessToken,
        redirectToSignIn,
      }),
    });
  }
  if ((design !== null) !== wasReady) for (const listener of readinessListeners) listener();
}

/** Whether the design-agent client is there: AE Studio's latest answer was `ready`. */
export function isDesignAgentReady(): boolean {
  return design !== null;
}

/**
 * Be told when the design agent becomes reachable or stops being so (for
 * useSyncExternalStore, outside the query cache). Returns the unsubscribe.
 */
export function subscribeDesignAgentReady(listener: () => void): () => void {
  readinessListeners.add(listener);
  return () => readinessListeners.delete(listener);
}

/** The ae-design-agent `/v1` client. Throws AeStudioNotReadyError before `ready`. */
export function designAgent(): Client<paths> {
  if (!design) throw new AeStudioNotReadyError();
  return design;
}

/**
 * A failed design-agent call: the problem's message and code (via
 * ApiRequestError), the HTTP status (undefined when nothing answered) and
 * the server's Retry-After. A 503 reads as AE Studio restarting, whatever the
 * pod's own words: the user's next step is the same.
 */
export class PodRequestError extends ApiRequestError {
  readonly status: number | undefined;

  constructor(
    error: unknown,
    fallback: string,
    { status, retryAfterMs }: { status?: number | undefined; retryAfterMs?: number | undefined },
  ) {
    super(error, fallback, { retryAfterMs });
    this.name = "PodRequestError";
    this.status = status;
    if (status === 503) this.message = AE_STUDIO_RESTARTING;
  }
}

/**
 * The pod is not serving: a 503 (idp_unavailable, shutting_down,
 * tools_unavailable) or no answer at all. That says more about AE Studio than
 * about the request, so it is the cue to re-read AE Studio's state.
 */
export function isPodUnavailable(error: unknown): boolean {
  return error instanceof PodRequestError && (error.status === undefined || error.status === 503);
}

const outageListeners = new Set<() => void>();

/**
 * Be told whenever a design-agent call finds the pod not serving. The chat's
 * calls are not queries, so the AE Studio gate cannot see them fail in the
 * query cache; it listens here as well. Returns the unsubscribe.
 */
export function onPodOutage(listener: () => void): () => void {
  outageListeners.add(listener);
  return () => outageListeners.delete(listener);
}

function reportPodOutage(): void {
  for (const listener of outageListeners) listener();
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

/**
 * Run one design-agent call and hand its answer back as openapi-fetch read it,
 * for the caller to read its own refusals. A 503 or no answer tells the
 * outage listeners; no answer throws a PodRequestError with `fallback` as its
 * message. AeStudioNotReadyError (before `ready`) and aborts pass through.
 */
export async function designAgentCall<R extends { response: Response }>(
  fallback: string,
  call: (agent: Client<paths>) => Promise<R>,
): Promise<R> {
  const agent = designAgent();
  let result: R;
  try {
    result = await call(agent);
  } catch (error) {
    if (isAbort(error)) throw error;
    reportPodOutage();
    throw new PodRequestError(undefined, fallback, {});
  }
  if (result.response.status === 503) reportPodOutage();
  return result;
}
