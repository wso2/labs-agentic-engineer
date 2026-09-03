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

// The validation run's PREFLIGHT: where the deployed system actually is.
//
// This is a platform fact, so platform code fetches it — before the agent
// starts, and fatally. It used to be a `curl` in the aep-validation skill, with
// prose telling the agent to stop if the fetch failed. The agent did not stop: on
// a 404 it spent half an hour scanning the pod network and reading an Envoy admin
// config dump trying to infer the URL itself. An unanswerable platform question
// is not an obstacle for an agent to work around, so it never reaches one.

import fs from "node:fs";
import path from "node:path";
import { fetchWith401Retry, staticTokenSource, type AccessTokenSource } from "./auth_retry.js";

// The file the aep-validation skill reads. Deliberately under /tmp and NOT under
// the workspace's `.aep/` — the base skill forbids the agent from looking in
// there at all, and it must not need to break that rule to find its targets.
// Outside the work tree either way, so it can never be committed.
export const VALIDATION_CONTEXT_FILE = "/tmp/validation-context.json";

/** One deployed component's reachable URL — the runner's e2e target. */
export interface ComponentEndpoint {
  component: string;
  url: string;
}

export interface ValidationContext {
  endpoints: ComponentEndpoint[];
  criteriaPath: string;
}

export interface FetchValidationContextOptions {
  /** AEP_PLATFORM_URL, with or without a trailing slash. */
  platformUrl: string;
  /** The dispatched CYCLE id — AEP_TASK_ID, and the subject its bearer is bound to. */
  cycleId: string;
  bearer: string;
  /** When set, GET uses fetchWith401Retry (remint once on 401). */
  source?: AccessTokenSource;
  canRefresh?: boolean;
  /** Overridable for tests; defaults to VALIDATION_CONTEXT_FILE. */
  file?: string;
  fetchImpl?: typeof fetch;
}

/** The callback URL for a cycle's validation context. */
export function validationContextUrl(platformUrl: string, cycleId: string): string {
  const base = platformUrl.endsWith("/") ? platformUrl.slice(0, -1) : platformUrl;
  return `${base}/internal/v1/validation/${encodeURIComponent(cycleId)}/context`;
}

/**
 * Fetch the run's validation context and write it where the skill reads it.
 *
 * Throws on every outcome the agent could not honestly proceed from — an unset
 * platform URL, a non-2xx answer, an unparseable body, or a context naming no
 * endpoints. That last one matters: validation is dispatched at deployed-green,
 * so an empty endpoint list means the platform cannot say where the system is,
 * and "no targets" is exactly the state that made the agent start guessing.
 */
export async function fetchValidationContext(
  opts: FetchValidationContextOptions,
): Promise<ValidationContext> {
  const { platformUrl, cycleId, bearer } = opts;
  if (platformUrl === "") {
    throw new Error("AEP_PLATFORM_URL is unset — the deployed endpoints cannot be resolved");
  }
  const source = opts.source ?? (bearer !== "" ? staticTokenSource(bearer) : undefined);
  if (source === undefined) {
    throw new Error("no bearer token for the validation-context callback");
  }
  const url = validationContextUrl(platformUrl, cycleId);

  let res: Response;
  try {
    res = await fetchWith401Retry(
      url,
      { headers: { Accept: "application/json" } },
      {
        source,
        canRefresh: opts.canRefresh ?? false,
        fetchImpl: opts.fetchImpl,
      },
    );
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    throw new Error(`GET ${url}: ${msg}`);
  }
  const body = await res.text();
  if (!res.ok) {
    // The status is the diagnosis: 404 means the platform does not recognise this
    // runner's cycle id, 403 means its bearer is not bound to it.
    throw new Error(`GET ${url} → HTTP ${res.status}: ${body.slice(0, 500)}`);
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    throw new Error(`validation context is not JSON: ${body.slice(0, 200)}`);
  }
  // `null` is valid JSON and survives the parse, so without this the cast below
  // would hand back a null `ctx` and reading `.endpoints` off it would throw a
  // TypeError. Every other failure here is a sentence naming what the platform
  // could not answer; a raw "Cannot read properties of null" in the pod log is
  // the one diagnosis this preflight exists to avoid. Non-objects (a bare number,
  // a string) are caught here too, for the same reason.
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new Error(`validation context is not a JSON object: ${body.slice(0, 200)}`);
  }
  const ctx = parsed as Partial<ValidationContext>;
  if (!Array.isArray(ctx.endpoints) || ctx.endpoints.length === 0) {
    throw new Error(
      "validation context names no deployed endpoints — there is nothing to validate against",
    );
  }
  // The oracle's path is required by the internal contract, and the skill is told
  // in so many words that the context "is present and non-empty whenever you are
  // running … there is no failure mode here for you to recover from". Defaulting a
  // missing one to "" would hand the agent a well-formed file that quietly fails
  // that promise, and an agent that cannot find the oracle goes looking — the exact
  // behaviour this preflight exists to prevent. Same reasoning as the endpoints
  // check above, so it fails the same way.
  if (typeof ctx.criteriaPath !== "string" || ctx.criteriaPath === "") {
    throw new Error(
      "validation context names no validation-criteria path — there is nothing to validate against",
    );
  }

  const file = opts.file ?? VALIDATION_CONTEXT_FILE;
  const dir = path.dirname(file);
  await fs.promises.mkdir(dir, { recursive: true });
  // Staged in a private directory and renamed into place, rather than written
  // straight to `file`. Three separate things that buys, all of which the direct
  // write got wrong:
  //
  //  - `mode` is honoured only when the write CREATES the file, so writing over an
  //    existing path silently kept whatever permissions it already had.
  //  - the target is a fixed, predictable name under a world-writable /tmp, so it
  //    can be pre-created as a symlink pointing anywhere. mkdtemp's name is not
  //    predictable and the directory is 0700, so nothing can sit in the way.
  //  - rename is atomic and does not follow a symlink at the destination, so there
  //    is no window between clearing the path and filling it, and a write that
  //    fails leaves the previous context intact instead of destroying it.
  const staging = await fs.promises.mkdtemp(path.join(dir, ".aep-valctx-"));
  try {
    const staged = path.join(staging, "context.json");
    // Written verbatim, not re-serialised from the parsed shape: the skill's
    // contract is the platform's payload, and a field this runner does not model
    // must still reach it.
    await fs.promises.writeFile(staged, body, { mode: 0o600 });
    await fs.promises.rename(staged, file);
  } finally {
    await fs.promises.rm(staging, { recursive: true, force: true });
  }
  return { endpoints: ctx.endpoints, criteriaPath: ctx.criteriaPath };
}
