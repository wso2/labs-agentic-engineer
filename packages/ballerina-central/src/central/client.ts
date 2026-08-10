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
import { centralDocsSchema, type CentralDocs } from "./schema.js";
import { formatQualifiedName, type QualifiedName, type Version } from "../qualified.js";
import { err, ok, type Failure, type Result, type SchemaIssue } from "../result.js";

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
}

const DEFAULTS = {
  timeoutMs: 120_000,
  maxAttempts: 3,
  budgetMs: 300_000,
  baseDelayMs: 200,
} as const;

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
      : { kind: "upstream", url, attempts: maxAttempts, message: "no attempt was made" },
  );
}

function toFailure(attempt: Attempt, url: string, attempts: number, budgetMs: number): Failure {
  if (attempt.failure.kind === "timeout") return { kind: "timeout", url, budgetMs };
  const { message, status } = attempt.failure;
  return { kind: "upstream", url, attempts, message, ...(status === undefined ? {} : { status }) };
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
): Promise<Result<Version>> {
  const url = `${CENTRAL_BASE_URL}registry/packages/${encodeURIComponent(qualified.org)}/${encodeURIComponent(qualified.name)}`;
  const response = await fetchJson(url, options);
  if (!response.ok) {
    // Central answers an unpublished org/name with a 400, not a 404. Either way
    // the fact is "no such package", and reporting it as a transport error
    // would send the caller looking at the network for a typo.
    const status = response.error.kind === "upstream" ? response.error.status : undefined;
    if (status === 400 || status === 404) return err(notFound(qualified));
    return response;
  }
  const versions = response.value;
  const latest = Array.isArray(versions) ? versions[0] : undefined;
  if (typeof latest !== "string" || latest === "") return err(notFound(qualified));
  return ok(latest as Version);
}

function notFound(qualified: QualifiedName): Failure {
  return {
    kind: "package-not-found",
    qualified: formatQualifiedName(qualified),
    suggestion: "Check the org/name spelling; `bal search <name>` lists what Central publishes.",
  };
}

/** The API docs for one published version. */
export async function fetchDocs(
  qualified: QualifiedName,
  version: Version,
  options: HttpOptions = {},
): Promise<Result<CentralDocs>> {
  const url =
    `${CENTRAL_BASE_URL}docs/${encodeURIComponent(qualified.org)}` +
    `/${encodeURIComponent(qualified.name)}/${encodeURIComponent(version)}`;
  const response = await fetchJson(url, options);
  if (!response.ok) {
    // 404 here is specific: the org/name may well exist, this VERSION does not.
    if (response.error.kind === "upstream" && response.error.status === 404) {
      return err({
        kind: "package-not-found",
        qualified: `${formatQualifiedName(qualified)}:${version}`,
        suggestion: `Verify version '${version}' is published; omit the version to take the latest.`,
      });
    }
    return response;
  }
  return parseCentralDocs(response.value, `${formatQualifiedName(qualified)}:${version}`);
}

/** Validate a raw payload against the schema, reporting every mismatch at once. */
export function parseCentralDocs(raw: unknown, qualified: string): Result<CentralDocs> {
  const parsed = centralDocsSchema.safeParse(raw);
  if (parsed.success) return ok(parsed.data);
  const issues: SchemaIssue[] = parsed.error.issues.map((issue) => ({
    path: issue.path.join("."),
    message: issue.message,
  }));
  return err({ kind: "schema-drift", qualified, issues });
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
