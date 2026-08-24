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

// One retry on HTTP 401: invalidate the access-token cache, remint, retry
// once. A second 401 or a remint failure is fatal — the coding agent must
// stop rather than loop. Used by the MCP loopback proxy and other runner
// callbacks that present the publisher CC token.

export class FatalAuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "FatalAuthError";
  }
}

export interface AccessTokenSource {
  getToken(): Promise<string>;
  invalidate(): void;
}

export function staticTokenSource(token: string): AccessTokenSource {
  return {
    getToken: async () => token,
    invalidate: () => {
      /* snapshot tokens cannot be reminted in the Job */
    },
  };
}

async function authorizedFetch(
  url: string,
  init: RequestInit,
  token: string,
  fetchImpl: typeof fetch,
): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Authorization", `Bearer ${token}`);
  return fetchImpl(url, { ...init, headers });
}

export async function fetchWith401Retry(
  url: string,
  init: RequestInit,
  opts: {
    source: AccessTokenSource;
    canRefresh: boolean;
    onToken?: (token: string) => void | Promise<void>;
    fetchImpl?: typeof fetch;
  },
): Promise<Response> {
  const doFetch = opts.fetchImpl ?? fetch;
  let first: string;
  try {
    first = await opts.source.getToken();
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new FatalAuthError(`token mint failed: ${msg}`);
  }
  await opts.onToken?.(first);
  const res = await authorizedFetch(url, init, first, doFetch);
  if (res.status !== 401) {
    return res;
  }
  if (!opts.canRefresh) {
    throw new FatalAuthError("unauthorized and no token refresh available");
  }
  opts.source.invalidate();
  let second: string;
  try {
    second = await opts.source.getToken();
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new FatalAuthError(`unauthorized; token refresh failed: ${msg}`);
  }
  await opts.onToken?.(second);
  const retry = await authorizedFetch(url, init, second, doFetch);
  if (retry.status === 401) {
    throw new FatalAuthError("unauthorized after token refresh");
  }
  return retry;
}
