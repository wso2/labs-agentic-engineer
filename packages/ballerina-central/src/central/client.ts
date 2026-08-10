/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * Everything that talks to Ballerina Central, or reads a version off disk.
 *
 * This is the boundary: it is the only module that handles `unknown`, and the
 * only one that can fail for reasons outside the process. Callers get a
 * `Result` — a network hiccup is a value here, not an exception threading
 * through the render pipeline.
 */

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { NULL_CACHE, type DocsCache, type DocsKey, type PackageKey } from "../cache/store.js";
import { coordinatesMatch } from "./coordinates.js";
import { centralDocsSchema, type CentralDocs } from "./schema.js";
import { formatQualifiedName, parseVersion, type QualifiedName, type Version } from "../qualified.js";
import {
  err,
  ok,
  SCHEMA_DRIFT_SUGGESTION,
  TIMEOUT_SUGGESTION,
  UPSTREAM_SUGGESTION,
  type Failure,
  type Result,
  type SchemaIssue,
} from "../result.js";

export const CENTRAL_BASE_URL = "https://api.central.ballerina.io/2.0/";

/** Injectable for tests; production passes nothing and gets global `fetch`. */
export type FetchLike = (url: string, init?: { signal?: AbortSignal }) => Promise<Response>;

export interface HttpOptions {
  readonly fetch?: FetchLike;
  /** Per-attempt ceiling. Central is slow for large packages; this is not a p99. */
  readonly timeoutMs?: number;
  readonly maxAttempts?: number;
  /** Wall clock across every attempt including backoff. */
  readonly budgetMs?: number;
  readonly baseDelayMs?: number;
  /**
   * Where already-fetched payloads live. Defaults to a store that does nothing,
   * which is what keeps `src/` free of environment reads and every existing test
   * hermetic: only `src/main.ts` constructs a real one.
   */
  readonly cache?: DocsCache;
  /** Ignore any cached copy and rewrite it. */
  readonly refresh?: boolean;
  /** Injectable clock, so the versions-list TTL can be tested at its boundary. */
  readonly now?: () => number;
}

const DEFAULTS = {
  timeoutMs: 120_000,
  maxAttempts: 3,
  budgetMs: 300_000,
  baseDelayMs: 200,
} as const;

/**
 * How long Central's answer to "what is the latest version" is believed.
 *
 * The measured lookup episode runs 70 to 260 seconds, so ten minutes spans a
 * whole episode without a second registry round trip — they cost 1.0 to 1.5s
 * each — while a package published mid-run is still picked up. It is the one
 * mutable response this reader caches; a docs payload for a named version is
 * immutable and never expires.
 */
export const LATEST_TTL_MS = 600_000;

/** Where a document's bytes came from, for the provenance header. */
export type Source = "central" | "cache";

export interface FetchedDocs {
  readonly docs: CentralDocs;
  readonly source: Source;
}

export interface ResolvedVersion {
  readonly version: Version;
  /** The registry was unreachable and this came off disk unverified. */
  readonly stale: boolean;
}

/**
 * Statuses worth trying again. A 404 is an answer — the package is not there —
 * and retrying it only spends the caller's budget.
 */
function isRetryableStatus(status: number): boolean {
  return status === 429 || status === 502 || status === 503 || status === 504;
}

/** `Retry-After` in either of its legal forms, as milliseconds. */
export function parseRetryAfter(header: string | null): number | undefined {
  if (!header) return undefined;
  const trimmed = header.trim();
  const asInt = Number.parseInt(trimmed, 10);
  if (!Number.isNaN(asInt) && String(asInt) === trimmed) return asInt * 1000;
  const asDate = Date.parse(trimmed);
  if (!Number.isNaN(asDate)) return Math.max(0, asDate - Date.now());
  return undefined;
}

/** Exponential backoff with up to 25% jitter, so parallel callers do not resonate. */
export function backoffMs(attempt: number, baseDelayMs: number): number {
  return Math.floor(baseDelayMs * 2 ** attempt * (1 + Math.random() * 0.25));
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

interface Attempt {
  readonly failure:
    | { readonly kind: "upstream"; readonly message: string; readonly status?: number }
    | { readonly kind: "timeout" };
  readonly retryable: boolean;
  readonly retryAfterMs?: number;
}

async function attemptFetch(url: string, fetchImpl: FetchLike, timeoutMs: number): Promise<Result<unknown> | Attempt> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  let response: Response;
  try {
    response = await fetchImpl(url, { signal: controller.signal });
  } catch (cause) {
    if (controller.signal.aborted) return { failure: { kind: "timeout" }, retryable: true };
    const message = cause instanceof Error ? cause.message : String(cause);
    return { failure: { kind: "upstream", message: `network error: ${message}` }, retryable: true };
  } finally {
    clearTimeout(timer);
  }

  if (!response.ok) {
    const retryable = isRetryableStatus(response.status);
    const retryAfterMs = retryable ? parseRetryAfter(response.headers.get("retry-after")) : undefined;
    return {
      failure: { kind: "upstream", message: `HTTP ${response.status}`, status: response.status },
      retryable,
      ...(retryAfterMs === undefined ? {} : { retryAfterMs }),
    };
  }

  try {
    return ok((await response.json()) as unknown);
  } catch (cause) {
    // Upstream serving something that is not JSON is not a transient condition.
    const message = cause instanceof Error ? cause.message : String(cause);
    return { failure: { kind: "upstream", message: `malformed JSON: ${message}` }, retryable: false };
  }
}

/**
 * GET a JSON document, retrying the failures that are worth retrying and
 * stopping at a wall-clock budget.
 *
 * Retries live here and nowhere else: Central is a remote service that can 429
 * or 5xx for reasons that pass, whereas everything else this package does is
 * local and deterministic.
 */
export async function fetchJson(url: string, options: HttpOptions = {}): Promise<Result<unknown>> {
  const fetchImpl = options.fetch ?? globalThis.fetch;
  const timeoutMs = options.timeoutMs ?? DEFAULTS.timeoutMs;
  const maxAttempts = options.maxAttempts ?? DEFAULTS.maxAttempts;
  const budgetMs = options.budgetMs ?? DEFAULTS.budgetMs;
  const baseDelayMs = options.baseDelayMs ?? DEFAULTS.baseDelayMs;

  const deadline = Date.now() + budgetMs;
  let last: Attempt | undefined;

  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    if (attempt > 0) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) break;
      await sleep(Math.min(remaining, last?.retryAfterMs ?? backoffMs(attempt - 1, baseDelayMs)));
    }
    const outcome = await attemptFetch(url, fetchImpl, timeoutMs);
    if ("ok" in outcome) return outcome;
    if (!outcome.retryable) {
      return err(toFailure(outcome, url, attempt + 1, budgetMs));
    }
    last = outcome;
  }

  return err(
    last
      ? toFailure(last, url, maxAttempts, budgetMs)
      : {
          kind: "upstream",
          url,
          attempts: maxAttempts,
          message: "no attempt was made",
          suggestion: UPSTREAM_SUGGESTION,
        },
  );
}

function toFailure(attempt: Attempt, url: string, attempts: number, budgetMs: number): Failure {
  if (attempt.failure.kind === "timeout") {
    return { kind: "timeout", url, budgetMs, suggestion: TIMEOUT_SUGGESTION };
  }
  const { message, status } = attempt.failure;
  return {
    kind: "upstream",
    url,
    attempts,
    message,
    suggestion: UPSTREAM_SUGGESTION,
    ...(status === undefined ? {} : { status }),
  };
}

/**
 * The latest published version of one package.
 *
 * `registry/packages/<org>/<name>` answers with just this package's versions,
 * newest first. The alternative — listing the org and filtering client-side —
 * costs about 45 seconds for `ballerinax`, which has roughly a thousand
 * packages, and that cost lands on every lookup an agent makes without an
 * explicit version.
 */
export async function resolveLatestVersion(
  qualified: QualifiedName,
  options: HttpOptions = {},
): Promise<Result<ResolvedVersion>> {
  const cache = options.cache ?? NULL_CACHE;
  const now = options.now ?? Date.now;
  const key: PackageKey = { org: qualified.org, name: qualified.name };

  // `--refresh` re-resolves unconditionally. An earlier draft made the
  // re-download conditional on the version having changed, which made the flag a
  // no-op in exactly the case its own error message recommends it for.
  if (options.refresh !== true) {
    const entry = cache.readLatest(key);
    // The lower bound matters as much as the TTL: a clock that jumped backwards
    // leaves a future-stamped entry looking fresh forever.
    if (entry !== undefined && now() >= entry.atMs && now() - entry.atMs < LATEST_TTL_MS) {
      const cached = parseVersion(entry.version);
      if (cached.ok) return ok({ version: cached.value, stale: false });
    }
  }

  const url = `${CENTRAL_BASE_URL}registry/packages/${encodeURIComponent(qualified.org)}/${encodeURIComponent(qualified.name)}`;
  const response = await fetchJson(url, options);
  if (!response.ok) {
    // Central answers an unpublished org/name with a 400, not a 404. Either way
    // the fact is "no such package", and reporting it as a transport error
    // would send the caller looking at the network for a typo.
    const status = response.error.kind === "upstream" ? response.error.status : undefined;
    if (status === 400 || status === 404) return err(notFound(qualified));
    const offline = offlineVersion(cache, key, now);
    if (offline !== undefined) return ok({ version: offline, stale: true });
    return response;
  }
  const versions = response.value;
  const latest = Array.isArray(versions) ? versions[0] : undefined;
  if (typeof latest !== "string" || latest === "") return err(notFound(qualified));
  // Through the parser rather than branded by assertion: this is a string off the
  // network, and the cache turns it into a path segment.
  const parsed = parseVersion(latest);
  if (!parsed.ok) return parsed;
  cache.writeLatest(key, { version: parsed.value, atMs: now() });
  return ok({ version: parsed.value, stale: false });
}

/**
 * The best version answer available with the registry unreachable: an expired
 * `latest` entry first, then the newest docs payload already on disk.
 *
 * Without this, a warm cached payload plus one registry blip is a hard failure
 * that can burn the client's full 300s budget — four times over in the
 * four-invocation episode this reader is designed around. `stale: true` is how
 * the caller learns to say so on the provenance line rather than claiming a
 * version it did not verify.
 */
function offlineVersion(cache: DocsCache, key: PackageKey, now: () => number): Version | undefined {
  const expired = cache.readLatest(key);
  if (expired !== undefined) {
    const parsed = parseVersion(expired.version);
    // Only trust a stamp that is not from the future; a bogus one is no better
    // than the directory listing below it.
    if (parsed.ok && now() >= expired.atMs) return parsed.value;
  }
  for (const candidate of cache.listVersions(key)) {
    const parsed = parseVersion(candidate);
    if (parsed.ok) return parsed.value;
  }
  return undefined;
}

function notFound(qualified: QualifiedName): Failure {
  return {
    kind: "package-not-found",
    qualified: formatQualifiedName(qualified),
    suggestion: "Check the org/name spelling; `bal search <name>` lists what Central publishes.",
  };
}

/**
 * The API docs for one published version, from disk when they are already there.
 *
 * This is where the cache belongs: above the retry loop, so a hit costs no
 * attempt, and below the schema, so what gets stored is not derived from our own
 * code. It is also the reason the addressed verbs are affordable at all — at 4.9
 * to 6.6 seconds and 12.4MB per invocation the CLI can only be asked once per
 * package, which is what forces a 21,818-line document to be navigated by hand.
 * Once re-opening a package costs about 250ms, four precise questions are
 * cheaper than one big answer.
 *
 * ANY problem with a cached entry is a miss, never a failure: a missing file, an
 * unreadable one, a truncated one, one that is not JSON, one the schema no longer
 * accepts, one whose coordinates do not match its own path. Each of those drops
 * the entry and uses the network, so a corrupt entry cannot produce a wrong
 * document and heals on the next successful fetch.
 */
export async function fetchDocs(
  qualified: QualifiedName,
  version: Version,
  options: HttpOptions = {},
): Promise<Result<FetchedDocs>> {
  const cache = options.cache ?? NULL_CACHE;
  const key: DocsKey = { org: qualified.org, name: qualified.name, version };
  const label = `${formatQualifiedName(qualified)}:${version}`;

  if (options.refresh === true) {
    cache.removeDocs(key);
  } else {
    const cached = cache.readDocs(key);
    if (cached !== undefined) {
      const parsed = coordinatesMatch(cached, qualified, version) ? parseCentralDocs(cached, label) : undefined;
      if (parsed?.ok === true) return ok({ docs: parsed.value, source: "cache" });
      cache.removeDocs(key);
    }
  }

  const url =
    `${CENTRAL_BASE_URL}docs/${encodeURIComponent(qualified.org)}` +
    `/${encodeURIComponent(qualified.name)}/${encodeURIComponent(version)}`;
  const response = await fetchJson(url, options);
  if (!response.ok) {
    // 404 here is specific: the org/name may well exist, this VERSION does not.
    if (response.error.kind === "upstream" && response.error.status === 404) {
      return err({
        kind: "package-not-found",
        qualified: label,
        suggestion: `Verify version '${version}' is published; omit the version to take the latest.`,
      });
    }
    return response;
  }
  const parsed = parseCentralDocs(response.value, label);
  if (!parsed.ok) return parsed;
  // Written only after it parses: a payload this reader cannot read is not worth
  // storing, and storing it would make every later run pay the same drift.
  cache.writeDocs(key, response.value);
  return ok({ docs: parsed.value, source: "central" });
}

/** Validate a raw payload against the schema, reporting every mismatch at once. */
export function parseCentralDocs(raw: unknown, qualified: string): Result<CentralDocs> {
  const parsed = centralDocsSchema.safeParse(raw);
  if (parsed.success) return ok(parsed.data);
  const issues: SchemaIssue[] = parsed.error.issues.map((issue) => ({
    path: issue.path.join("."),
    message: issue.message,
  }));
  return err({ kind: "schema-drift", qualified, issues, suggestion: SCHEMA_DRIFT_SUGGESTION });
}

/**
 * Versions a build has already locked, keyed `org/name`.
 *
 * `Dependencies.toml` is generated by `bal build` and is the most useful answer
 * available: it names the version the component will actually compile against,
 * which is not always the latest. Parsed by hand rather than with a TOML
 * library because one `[[package]]` table shape is the whole requirement, and
 * the bundled CLI is better off without another dependency.
 */
export function parseDependenciesToml(content: string): ReadonlyMap<string, string> {
  const versions = new Map<string, string>();
  let inPackage = false;
  let org: string | undefined;
  let name: string | undefined;
  let version: string | undefined;

  const flush = (): void => {
    if (inPackage && org && name && version) versions.set(`${org}/${name}`, version);
    org = undefined;
    name = undefined;
    version = undefined;
  };

  for (const raw of content.split("\n")) {
    const line = raw.trim();
    if (line.startsWith("[[package]]")) {
      flush();
      inPackage = true;
      continue;
    }
    if (line.startsWith("[")) {
      flush();
      inPackage = false;
      continue;
    }
    if (!inPackage) continue;
    const match = /^(\w+)\s*=\s*"([^"]*)"$/.exec(line);
    if (!match) continue;
    const [, key, value] = match as unknown as [string, string, string];
    if (key === "org") org = value;
    else if (key === "name") name = value;
    else if (key === "version") version = value;
  }
  flush();
  return versions;
}

/**
 * The locked version of one package in a component directory, if a build has
 * written one. A missing file is not an error — most lookups happen before the
 * first build.
 */
export function lockedVersion(projectDir: string, qualified: QualifiedName): string | undefined {
  let content: string;
  try {
    content = readFileSync(join(projectDir, "Dependencies.toml"), "utf-8");
  } catch {
    return undefined;
  }
  return parseDependenciesToml(content).get(formatQualifiedName(qualified));
}
