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
 * The whole capability in two steps: resolve which version to read, then read it
 * once.
 *
 * They are separate because the resolution rule is the part with a cost. An
 * explicit version is free; a `Dependencies.toml` is a file read; asking Central
 * is a round trip. Callers that already know the version should never pay for the
 * ones that follow.
 *
 * `loadPackage` is deliberately the only load: all four verbs read the same
 * payload and differ only in which document they write from it, so a verb cannot
 * be cheap because it skipped work another verb does.
 */

import {
  fetchDocs,
  lockedVersion,
  resolveLatestVersion,
  type HttpOptions,
  type ResolvedVersion,
  type Source,
} from "./central/client.js";
import { fromCentral, selectModule } from "./from-central.js";
import type { Library } from "./model.js";
import { applyPatches } from "./patches.js";
import { parseVersion, type QualifiedName, type Version } from "./qualified.js";
import { collectReadmes, type ModuleReadme } from "./readme.js";
import { ok, type Result } from "./result.js";

export interface ResolveOptions extends HttpOptions {
  /** An explicit version wins over everything else. */
  readonly version?: string;
  /** A component directory whose `Dependencies.toml` a build may have written. */
  readonly projectDir?: string;
}

/**
 * Which version of the package to read.
 *
 * `Dependencies.toml` outranks Central's latest deliberately: once a build has
 * resolved the package, the locked version is the one the component will
 * actually compile against, and reading a newer one produces signatures that do
 * not exist for this caller. Both of the answers above Central also bypass the
 * versions-list TTL entirely, so caching changes nothing about this precedence.
 */
export async function resolveVersion(
  qualified: QualifiedName,
  options: ResolveOptions = {},
): Promise<Result<ResolvedVersion>> {
  const fixed = (input: string): Result<ResolvedVersion> => {
    const parsed = parseVersion(input);
    return parsed.ok ? ok({ version: parsed.value, stale: false }) : parsed;
  };
  if (options.version !== undefined) return fixed(options.version);
  if (options.projectDir !== undefined) {
    const locked = lockedVersion(options.projectDir, qualified);
    if (locked !== undefined) return fixed(locked);
  }
  return resolveLatestVersion(qualified, options);
}

/**
 * One package, read once: its coordinates, its API as the IR, its guide, and
 * where the bytes came from.
 */
export interface LoadedPackage {
  readonly qualified: QualifiedName;
  readonly version: Version;
  readonly library: Library;
  /** Every module of the payload that wrote a guide, in Central's order. */
  readonly readmes: readonly ModuleReadme[];
  /** The `Source` line every document carries — see `describeProvenance`. */
  readonly provenance: string;
}

/**
 * What the provenance header says, in the four states it can be in.
 *
 * The two facts are INDEPENDENT and the header has to keep them so. Where the
 * bytes came from and whether the version was verified are answered by different
 * endpoints: the registry can be unreachable — so the version comes off disk
 * unverified — while the docs endpoint for that version answers perfectly well.
 * Collapsing on `stale` alone stamped `cache` on bytes that had just been
 * downloaded, which is the one thing this line exists to get right.
 *
 * It makes stdout run-order-dependent, which is a real if small change to the
 * stdout-is-the-document discipline: the same command run twice prints `central`
 * then `cache`. Snapshot tests therefore pin the body under a fixed header rather
 * than pretending the line is deterministic. The alternative — omitting it — costs
 * an operator the only way to tell a cache hit from a fetch, and costs an agent
 * the only warning that a version was never verified.
 */
export function describeProvenance(source: Source, stale: boolean): string {
  if (!stale) return source;
  return source === "cache"
    ? "cache (stale: registry unreachable, version unverified)"
    : "central (version unverified: registry unreachable)";
}

export async function loadPackage(
  qualified: QualifiedName,
  options: ResolveOptions = {},
): Promise<Result<LoadedPackage>> {
  const resolved = await resolveVersion(qualified, options);
  if (!resolved.ok) return resolved;
  const { version, stale } = resolved.value;

  const docs = await fetchDocs(qualified, version, options);
  if (!docs.ok) return docs;

  const module = selectModule(docs.value.docs, qualified);
  if (!module.ok) return module;

  return ok({
    qualified,
    version,
    library: applyPatches(fromCentral(module.value)),
    readmes: collectReadmes(docs.value.docs),
    provenance: describeProvenance(docs.value.source, stale),
  });
}
