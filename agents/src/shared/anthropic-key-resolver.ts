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
 * Anthropic key resolver.
 *
 * Per-request resolution of the effective Anthropic API key for a given OC
 * org. Resolves via git-service's
 * GET /internal/credentials/orgs/{orgId}/anthropic/effective-key endpoint,
 * which returns `{source:"org"|"platform"|"none", key:"sk-ant-..."}`. The
 * key bytes are cached in-process in a 5-minute LRU keyed by orgId; the
 * invalidate route at POST /v1/internal/cache/invalidate drops one entry
 * eagerly on Connect/Disconnect events from git-service.
 *
 * See docs/design/anthropic-key-dual-token.md §6.4.
 */

export type EffectiveKeySource = "org" | "platform" | "none";

export interface EffectiveKey {
  source: EffectiveKeySource;
  key: string; // empty when source === "none"
}

export class AnthropicKeyError extends Error {
  constructor(
    message: string,
    public readonly code:
      | "no_anthropic_key_configured"
      | "resolver_unreachable"
      | "resolver_error",
    public readonly status: number = 502,
  ) {
    super(message);
    this.name = "AnthropicKeyError";
  }
}

const CACHE_TTL_MS = 5 * 60 * 1000;
const DEFAULT_RESOLVE_TIMEOUT_MS = 5_000;

interface CacheEntry {
  value: EffectiveKey;
  expiresAt: number; // ms-since-epoch
}

const cache = new Map<string, CacheEntry>();

/**
 * In-flight resolves keyed by orgId. Single-flight: a cache miss under
 * concurrency (cold start, or right after an invalidate) coalesces onto one
 * upstream call instead of firing N parallel resolves for the same org.
 */
const inFlight = new Map<string, Promise<EffectiveKey>>();

/**
 * Invalidate the cached entry for ocOrgId. Called by the
 * /v1/internal/cache/invalidate route on Connect/Disconnect.
 */
export function invalidateAnthropicCache(ocOrgId: string): void {
  cache.delete(ocOrgId);
}

/**
 * Reset the entire cache (and any in-flight resolves). Useful in tests.
 */
export function resetAnthropicCache(): void {
  cache.clear();
  inFlight.clear();
}

/**
 * Upper bound for a single effective-key resolve against asdlc-api. Read at
 * call time so it can be tuned via env without a rebuild. A hung BFF must not
 * be able to hang every agent request indefinitely.
 */
function resolveTimeoutMs(): number {
  const raw = parseInt(process.env.ANTHROPIC_KEY_RESOLVE_TIMEOUT_MS ?? "", 10);
  return Number.isFinite(raw) && raw > 0 ? raw : DEFAULT_RESOLVE_TIMEOUT_MS;
}

function asdlcApiUrl(): string {
  const url =
    process.env.ASDLC_API_URL ||
    process.env.GIT_SERVICE_URL || // legacy fallback, removed in a follow-up
    "http://asdlc-api:9090";
  return url.replace(/\/+$/, "");
}

/**
 * Resolve the effective Anthropic key for ocOrgId. Throws AnthropicKeyError
 * when no key is configured (org row absent AND platform env empty) or
 * when the resolver itself is unreachable / times out / returns a non-2xx.
 *
 * Hot path: called once per agent request. A fresh value is served from the
 * 5-minute cache; a miss goes to asdlc-api under a single-flight guard (so a
 * burst of concurrent misses for one org makes a single upstream call) with a
 * bounded timeout (so a hung BFF can't hang agent requests indefinitely).
 *
 * The returned key is used inline by `createAnthropic({ apiKey: key })`
 * — see shared/create-agent.ts. It is never logged.
 */
export async function resolveAnthropicKey(
  ocOrgId: string,
): Promise<EffectiveKey> {
  if (!ocOrgId) {
    throw new AnthropicKeyError(
      "orgId is required to resolve an Anthropic API key",
      "resolver_error",
      400,
    );
  }

  const cached = cache.get(ocOrgId);
  if (cached && cached.expiresAt > Date.now()) {
    return cached.value;
  }

  // Single-flight: if a resolve for this org is already running, await it
  // rather than starting another. The cache read above and the map ops below
  // run synchronously with no intervening await, so at most one fetch is in
  // flight per org. Errors (and "none") are intentionally not cached, so a
  // failed resolve clears the slot and the next call retries.
  const pending = inFlight.get(ocOrgId);
  if (pending) return pending;

  const p = fetchEffectiveKey(ocOrgId);
  inFlight.set(ocOrgId, p);
  try {
    return await p;
  } finally {
    inFlight.delete(ocOrgId);
  }
}

/**
 * AbortSignal.timeout rejects with a "TimeoutError"; a manual abort surfaces
 * as "AbortError". Checked by name (not `instanceof Error`) because Node's
 * DOMException does not extend Error.
 */
function isAbortLike(err: unknown): boolean {
  const name = (err as { name?: string } | null)?.name;
  return name === "TimeoutError" || name === "AbortError";
}

/**
 * One uncached fetch of the effective key from asdlc-api, bounded by a
 * timeout that covers both the request and the response body read. Caches the
 * value on success; throws (without caching) otherwise.
 */
async function fetchEffectiveKey(ocOrgId: string): Promise<EffectiveKey> {
  const timeoutMs = resolveTimeoutMs();

  let resp: Response;
  try {
    resp = await fetch(
      `${asdlcApiUrl()}/internal/credentials/orgs/${encodeURIComponent(ocOrgId)}/anthropic/effective-key`,
      {
        method: "GET",
        headers: {
          accept: "application/json",
        },
        signal: AbortSignal.timeout(timeoutMs),
      },
    );
  } catch (err) {
    // A timeout/abort means the upstream is effectively unreachable — surface
    // it as such rather than hanging the caller.
    if (isAbortLike(err)) {
      throw new AnthropicKeyError(
        `asdlc-api did not respond within ${timeoutMs}ms`,
        "resolver_unreachable",
        502,
      );
    }
    throw new AnthropicKeyError(
      `asdlc-api unreachable: ${(err as Error)?.message ?? String(err)}`,
      "resolver_unreachable",
      502,
    );
  }

  if (!resp.ok) {
    const body = await resp.text().catch(() => "");
    throw new AnthropicKeyError(
      `asdlc-api returned ${resp.status}: ${body.slice(0, 200)}`,
      "resolver_error",
      502,
    );
  }

  let data: EffectiveKey;
  try {
    data = (await resp.json()) as EffectiveKey;
  } catch (err) {
    // The timeout also covers the body read: a BFF that sends 200 headers and
    // then stalls the body is still a hung upstream, not a bad payload.
    if (isAbortLike(err)) {
      throw new AnthropicKeyError(
        `asdlc-api did not respond within ${timeoutMs}ms`,
        "resolver_unreachable",
        502,
      );
    }
    throw new AnthropicKeyError(
      `asdlc-api returned an unreadable body: ${(err as Error)?.message ?? String(err)}`,
      "resolver_error",
      502,
    );
  }
  if (!data || (data.source !== "org" && data.source !== "platform" && data.source !== "none")) {
    throw new AnthropicKeyError(
      "asdlc-api returned an unexpected effective-key shape",
      "resolver_error",
      502,
    );
  }

  if (data.source === "none" || !data.key) {
    // No org key + no platform fallback. Don't cache the absence — the user
    // may configure a key seconds from now and we want the next call to
    // pick it up without waiting for TTL.
    throw new AnthropicKeyError(
      "no Anthropic API key configured for this organization (and no platform fallback)",
      "no_anthropic_key_configured",
      503,
    );
  }

  cache.set(ocOrgId, {
    value: data,
    expiresAt: Date.now() + CACHE_TTL_MS,
  });

  console.log(
    `[anthropic-key-resolver] orgId=${ocOrgId} source=${data.source}`,
  );

  return data;
}
